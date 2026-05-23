package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

const testEnvironmentID = "env_test"

func TestClientWrapsFailedDependencyResponses(t *testing.T) {
	t.Parallel()

	client := &Client{
		socketPath: "test.sock",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFailedDependency,
				Status:     "424 Failed Dependency",
				Body:       io.NopCloser(strings.NewReader(`{"error":"init action 2 failed"}`)),
			}, nil
		})},
	}

	_, err := client.Launch(context.Background(), LaunchRequest{EnvironmentID: testEnvironmentID})
	if !errors.Is(err, failure.ErrFailedDependency) {
		t.Fatalf("launch error = %v, want failed dependency", err)
	}
}

func TestClientLaunchStreamsLogsAndResult(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer

	encoder := json.NewEncoder(&body)
	if err := encoder.Encode(LaunchStreamEvent{Type: StreamEventLog, Log: "installing node\n"}); err != nil {
		t.Fatalf("encode log event: %v", err)
	}

	if err := encoder.Encode(LaunchStreamEvent{Type: StreamEventResult, VM: &VM{EnvironmentID: testEnvironmentID, State: StateRunning}}); err != nil {
		t.Fatalf("encode result event: %v", err)
	}

	client := &Client{
		socketPath: "test.sock",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.Path != "/v1/vms" {
				t.Fatalf("request = %s %s, want POST /v1/vms", req.Method, req.URL.Path)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
			}, nil
		})},
	}

	var logs bytes.Buffer

	vm, err := client.Launch(context.Background(), LaunchRequest{EnvironmentID: testEnvironmentID, Logs: &logs})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	if vm.EnvironmentID != testEnvironmentID || vm.State != StateRunning {
		t.Fatalf("vm = %#v, want %s running", vm, testEnvironmentID)
	}

	if logs.String() != "installing node\n" {
		t.Fatalf("logs = %q, want streamed log", logs.String())
	}
}

func TestClientLaunchWrapsStreamFailedDependencyEvent(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer

	if err := json.NewEncoder(&body).Encode(LaunchStreamEvent{Type: StreamEventError, Error: "init action 1 failed", Status: http.StatusFailedDependency}); err != nil {
		t.Fatalf("encode error event: %v", err)
	}

	client := &Client{
		socketPath: "test.sock",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(bytes.NewReader(body.Bytes())),
			}, nil
		})},
	}

	_, err := client.Launch(context.Background(), LaunchRequest{EnvironmentID: testEnvironmentID})
	if !errors.Is(err, failure.ErrFailedDependency) {
		t.Fatalf("launch error = %v, want failed dependency", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
