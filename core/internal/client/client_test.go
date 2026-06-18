package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
)

const (
	clientTestBaseURL  = "http://bastion.test"
	clientTestOKStatus = "200 OK"
)

func TestCreateEnvironmentStreamsLogsAndResult(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer

	encoder := json.NewEncoder(&body)
	if err := encoder.Encode(environment.CreateStreamEvent{Type: environment.StreamEventLog, Log: "installing docker\n"}); err != nil {
		t.Fatalf("encode log event: %v", err)
	}

	if err := encoder.Encode(environment.CreateStreamEvent{Type: environment.StreamEventResult, Environment: &environment.Environment{ID: "env_test", Status: "running"}}); err != nil {
		t.Fatalf("encode result event: %v", err)
	}

	client := &Client{
		baseURL: clientTestBaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.Path != "/v1/environments" {
				t.Fatalf("request = %s %s, want POST /v1/environments", req.Method, req.URL.Path)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     clientTestOKStatus,
				Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
			}, nil
		})},
	}

	var logs bytes.Buffer

	created, err := client.CreateEnvironment(context.Background(), environment.CreateRequest{TemplateKey: "dev", Logs: &logs})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if created.ID != "env_test" || created.Status != "running" {
		t.Fatalf("created = %#v, want env_test running", created)
	}

	if logs.String() != "installing docker\n" {
		t.Fatalf("logs = %q, want streamed log", logs.String())
	}
}

func TestListEnvironmentsIncludesTagFilters(t *testing.T) {
	t.Parallel()

	client := &Client{
		baseURL: clientTestBaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet || req.URL.Path != "/v1/environments" {
				t.Fatalf("request = %s %s, want GET /v1/environments", req.Method, req.URL.Path)
			}

			query := req.URL.Query()
			if query.Get("limit") != "10" || query.Get("cursor") != "next" || !slices.Equal(query["tag"], []string{"prod", "gpu"}) {
				t.Fatalf("query = %v, want limit/cursor/tag filters", query)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     clientTestOKStatus,
				Body:       io.NopCloser(bytes.NewBufferString(`{"cursor":null,"entries":[]}`)),
			}, nil
		})},
	}

	if _, err := client.ListEnvironments(context.Background(), 10, "next", []string{"prod", "gpu"}); err != nil {
		t.Fatalf("list environments: %v", err)
	}
}

func TestEnvironmentByKeyPaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 2)
	client := &Client{
		baseURL: clientTestBaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.Method+" "+req.URL.Path)

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     clientTestOKStatus,
				Body:       io.NopCloser(bytes.NewBufferString(`{"id":"env_keyed","status":"running","templateId":"tpl_test","tags":[],"createdAt":"","updatedAt":""}`)),
			}, nil
		})},
	}

	if _, err := client.GetEnvironmentByKey(context.Background(), "dev-env"); err != nil {
		t.Fatalf("get environment by key: %v", err)
	}

	if _, err := client.RemoveEnvironmentByKey(context.Background(), "dev-env"); err != nil {
		t.Fatalf("remove environment by key: %v", err)
	}

	want := []string{"GET /v1/environments/by-key/dev-env", "DELETE /v1/environments/by-key/dev-env"}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestSecretClientPaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 4)
	secretKey := "client-secret"
	client := newSecretClientPathTestClient(t, &paths, secretKey)

	if _, err := client.CreateSecret(context.Background(), secret.CreateRequest{Key: &secretKey, Value: "secret-value"}); err != nil {
		t.Fatalf("create secret: %v", err)
	}

	if _, err := client.ListSecrets(context.Background(), 10, "next"); err != nil {
		t.Fatalf("list secrets: %v", err)
	}

	if _, err := client.GetSecret(context.Background(), "", secretKey); err != nil {
		t.Fatalf("get secret: %v", err)
	}

	if _, err := client.RemoveSecret(context.Background(), "sec_created", ""); err != nil {
		t.Fatalf("remove secret: %v", err)
	}

	want := []string{
		"POST http://bastion.test/v1/secrets",
		"GET http://bastion.test/v1/secrets?cursor=next&limit=10",
		"GET http://bastion.test/v1/secrets/by-key/client-secret",
		"DELETE http://bastion.test/v1/secrets/sec_created",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func newSecretClientPathTestClient(t *testing.T, paths *[]string, secretKey string) *Client {
	t.Helper()

	return &Client{
		baseURL: clientTestBaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			*paths = append(*paths, req.Method+" "+req.URL.String())

			return secretClientPathResponse(t, req, secretKey), nil
		})},
	}
}

func secretClientPathResponse(t *testing.T, req *http.Request, secretKey string) *http.Response {
	t.Helper()

	switch req.Method {
	case http.MethodPost:
		assertSecretClientCreateRequest(t, req, secretKey)

		return clientJSONResponse(http.StatusCreated, "201 Created", `{"id":"sec_created","key":"client-secret","createdAt":"now"}`)
	case http.MethodGet:
		if strings.Contains(req.URL.Path, "/by-key/") {
			return clientJSONResponse(http.StatusOK, clientTestOKStatus, `{"id":"sec_created","key":"client-secret","value":"secret-value","createdAt":"now"}`)
		}

		return clientJSONResponse(http.StatusOK, clientTestOKStatus, `{"cursor":null,"entries":[{"id":"sec_created","key":"client-secret","createdAt":"now"}]}`)
	case http.MethodDelete:
		return clientJSONResponse(http.StatusOK, clientTestOKStatus, `{"id":"sec_created","key":"client-secret","createdAt":"now"}`)
	default:
		t.Fatalf("unexpected method %s", req.Method)

		return nil
	}
}

func assertSecretClientCreateRequest(t *testing.T, req *http.Request, secretKey string) {
	t.Helper()

	var createReq secret.CreateRequest
	if err := json.NewDecoder(req.Body).Decode(&createReq); err != nil {
		t.Fatalf("decode create request: %v", err)
	}

	if createReq.Key == nil || *createReq.Key != secretKey || createReq.Value != "secret-value" {
		t.Fatalf("create request = %#v, want keyed secret value", createReq)
	}
}

func clientJSONResponse(statusCode int, status, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestEnvironmentTunnelsPaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 2)
	client := &Client{
		baseURL: clientTestBaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.Method+" "+req.URL.Path)

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     clientTestOKStatus,
				Body:       io.NopCloser(bytes.NewBufferString(`{"entries":[{"name":"frontend","port":3000}]}`)),
			}, nil
		})},
	}

	if _, err := client.GetEnvironmentTunnels(context.Background(), "env_123", ""); err != nil {
		t.Fatalf("get environment tunnels: %v", err)
	}

	if _, err := client.GetEnvironmentTunnels(context.Background(), "", "dev-env"); err != nil {
		t.Fatalf("get environment tunnels by key: %v", err)
	}

	want := []string{"GET /v1/environments/env_123/tunnels", "GET /v1/environments/by-key/dev-env/tunnels"}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
