package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/firecracker"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

func TestSSHCommandRunsSSHWithEnvironmentMetadata(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments/env_123" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}

		_ = json.NewEncoder(w).Encode(environment.Environment{
			ID:         "env_123",
			Status:     firecracker.StateRunning,
			SSHHost:    "10.241.0.2",
			SSHPort:    firecracker.SSHPort,
			SSHUser:    firecracker.SSHUser,
			SSHKeyPath: "/tmp/test.id_rsa",
		})
	}))
	t.Cleanup(server.Close)

	var gotArgs []string

	cmd := newSSHCommandWithRunner(&rootOptions{apiURL: server.URL}, func(_ context.Context, _ io.Reader, _, _ io.Writer, args []string) error {
		gotArgs = append([]string(nil), args...)

		return nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"env_123", "true"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	want := []string{"-i", "/tmp/test.id_rsa", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "LogLevel=ERROR", "-p", "22", "root@10.241.0.2", "true"}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("ssh args = %#v, want %#v", gotArgs, want)
	}
}
