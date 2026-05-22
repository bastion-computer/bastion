package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/core/internal/api"
	hostclient "github.com/bastion-computer/bastion/core/internal/client"
	fc "github.com/bastion-computer/bastion/core/internal/firecracker"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

const streamTestGuestIP = "10.241.0.2"

func TestCreateEnvironmentStreamsBastiondLogsEndToEnd(t *testing.T) {
	t.Parallel()

	socket := startFakeBastiond(t, func(w http.ResponseWriter, r *http.Request) {
		var req fc.LaunchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		streamBastiondLaunch(t, w,
			fc.LaunchStreamEvent{Type: fc.StreamEventLog, Log: "installing node\n"},
			fc.LaunchStreamEvent{Type: fc.StreamEventResult, VM: &fc.VM{EnvironmentID: req.EnvironmentID, State: fc.StateRunning, GuestIP: streamTestGuestIP, SSHUser: fc.SSHUser, SSHPort: fc.SSHPort}},
		)
	}, fc.StateRunning)

	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithEnvironmentOrchestrator(fc.NewClient(socket)))
	createTemplate(t, router, "stream-template")

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	var logs bytes.Buffer
	created, err := hostclient.New(server.URL).CreateEnvironment(context.Background(), environment.CreateRequest{TemplateKey: "stream-template", Logs: &logs})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if created.ID == "" || created.Status != fc.StateRunning || created.SSHHost != streamTestGuestIP {
		t.Fatalf("created environment = %#v, want running with SSH host", created)
	}

	if logs.String() != "installing node\n" {
		t.Fatalf("logs = %q, want bastiond log", logs.String())
	}
}

func TestCreateEnvironmentClientCutoffAbortsBastiondLaunch(t *testing.T) {
	t.Parallel()

	cancelled := make(chan struct{})
	socket := startFakeBastiond(t, func(w http.ResponseWriter, r *http.Request) {
		streamBastiondLaunch(t, w, fc.LaunchStreamEvent{Type: fc.StreamEventLog, Log: "first log\n"})

		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-time.After(2 * time.Second):
			t.Errorf("bastiond launch context was not cancelled after client cutoff")
		}
	}, fc.StateError)

	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithEnvironmentOrchestrator(fc.NewClient(socket)))
	createTemplate(t, router, "cutoff-template")

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	_, err := hostclient.New(server.URL).CreateEnvironment(context.Background(), environment.CreateRequest{TemplateKey: "cutoff-template", Logs: failingWriter{err: errors.New("client stream closed")}})
	if err == nil || !strings.Contains(err.Error(), "client stream closed") {
		t.Fatalf("create environment error = %v, want client stream closed", err)
	}

	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for bastiond launch cancellation")
	}

	assertFailedEnvironmentRecorded(t, router)
}

func startFakeBastiond(t *testing.T, launch http.HandlerFunc, state string) string {
	t.Helper()

	socket := filepath.Join(t.TempDir(), "bastiond.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen on fake bastiond socket: %v", err)
	}

	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/vms":
			launch(w, r)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/vms/"):
			environmentID := strings.TrimPrefix(r.URL.Path, "/v1/vms/")
			_ = json.NewEncoder(w).Encode(fc.VM{EnvironmentID: environmentID, State: state, GuestIP: streamTestGuestIP, SSHUser: fc.SSHUser, SSHPort: fc.SSHPort, LastError: "init aborted"})
		default:
			http.NotFound(w, r)
		}
	})}

	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}

		errCh <- err
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_ = server.Shutdown(ctx)
		if err := <-errCh; err != nil {
			t.Errorf("fake bastiond server error: %v", err)
		}
	})

	return socket
}

func streamBastiondLaunch(t *testing.T, w http.ResponseWriter, events ...fc.LaunchStreamEvent) {
	t.Helper()

	w.Header().Set("Content-Type", "application/x-ndjson")
	encoder := json.NewEncoder(w)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			t.Errorf("encode bastiond event: %v", err)

			return
		}

		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func assertFailedEnvironmentRecorded(t *testing.T, handler http.Handler) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		res := request(t, handler, http.MethodGet, "/v1/environments", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("list environments status = %d, want %d", res.Code, http.StatusOK)
		}

		var page services.Page[environment.Environment]
		decode(t, res, &page)
		if len(page.Entries) == 1 && page.Entries[0].Status == fc.StateError && page.Entries[0].LastError != "" {
			return
		}

		time.Sleep(25 * time.Millisecond)
	}

	t.Fatal("failed environment was not recorded with an error")
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("write log: %w", w.err)
}
