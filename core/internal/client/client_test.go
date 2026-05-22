package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/environment"
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
		baseURL: "http://bastion.test",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.Path != "/v1/environments" {
				t.Fatalf("request = %s %s, want POST /v1/environments", req.Method, req.URL.Path)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
