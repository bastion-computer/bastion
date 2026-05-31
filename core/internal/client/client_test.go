package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/queue"
)

const (
	clientTestBaseURL  = "http://bastion.test"
	clientTestStatusOK = "200 OK"
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
				Status:     clientTestStatusOK,
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
				Status:     clientTestStatusOK,
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
				Status:     clientTestStatusOK,
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

func TestQueueByKeyAndTaskPaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 4)
	client := &Client{
		baseURL: clientTestBaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.Method+" "+req.URL.Path)

			body := `{"id":"que_test","createdAt":"","updatedAt":""}`
			if req.URL.Path == "/v1/queues/by-key/jobs/tasks" || req.URL.Path == "/v1/queues/by-key/jobs/tasks/task_test" {
				body = `{"id":"task_test","queueId":"que_test","status":"pending","retry":{"max_attempts":3,"delay_ms":1000},"data":{},"attempts":0,"availableAt":"","createdAt":"","updatedAt":""}`
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     clientTestStatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
			}, nil
		})},
	}

	if _, err := client.GetQueue(context.Background(), "", "jobs"); err != nil {
		t.Fatalf("get queue by key: %v", err)
	}

	if _, err := client.RemoveQueue(context.Background(), "", "jobs"); err != nil {
		t.Fatalf("remove queue by key: %v", err)
	}

	if _, err := client.PublishQueueTask(context.Background(), "", "jobs", queue.PublishRequest{Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("publish queue task: %v", err)
	}

	if _, err := client.GetQueueTask(context.Background(), "", "jobs", "task_test"); err != nil {
		t.Fatalf("get queue task: %v", err)
	}

	want := []string{
		"GET /v1/queues/by-key/jobs",
		"DELETE /v1/queues/by-key/jobs",
		"POST /v1/queues/by-key/jobs/tasks",
		"GET /v1/queues/by-key/jobs/tasks/task_test",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
