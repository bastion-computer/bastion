package firecracker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

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

	_, err := client.Launch(context.Background(), LaunchRequest{EnvironmentID: "env_test"})
	if !errors.Is(err, failure.ErrFailedDependency) {
		t.Fatalf("launch error = %v, want failed dependency", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
