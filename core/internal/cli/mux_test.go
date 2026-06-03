package cli

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

const muxTestAPIURL = "http://bastion.test"

func TestMuxCommandPreflightsTmux(t *testing.T) {
	t.Parallel()

	runCalled := false
	cmd := newMuxCommandWithOptions(&rootOptions{apiURL: muxTestAPIURL}, muxOptions{
		lookPath: func(string) (string, error) {
			return "", errors.New("missing")
		},
		runTUI: func(context.Context, muxRunOptions) error {
			runCalled = true
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "tmux is required") {
		t.Fatalf("execute error = %v, want tmux preflight error", err)
	}

	if runCalled {
		t.Fatal("runTUI called despite missing tmux")
	}
}

func TestMuxCommandRunsTUIWithoutStartingSSH(t *testing.T) {
	t.Parallel()

	var got muxRunOptions

	cmd := newMuxCommandWithOptions(&rootOptions{apiURL: muxTestAPIURL}, muxOptions{
		sessionName: "default-session",
		lookPath: func(name string) (string, error) {
			if name != "tmux" {
				t.Fatalf("lookPath name = %q, want tmux", name)
			}

			return "/usr/bin/tmux", nil
		},
		executable: func() (string, error) { return "/usr/bin/bastion", nil },
		runTUI: func(_ context.Context, opts muxRunOptions) error {
			got = opts
			return nil
		},
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--session", "test-session"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if got.sessionName != "test-session" {
		t.Fatalf("session name = %q, want test-session", got.sessionName)
	}

	if got.backend == nil {
		t.Fatal("backend = nil, want tmux backend")
	}
}

func TestMuxSSHConnectableEnvironments(t *testing.T) {
	t.Parallel()

	pausedKey := "paused"
	environments := []environment.Environment{
		{ID: "env_creating", Status: "creating"},
		{ID: "env_running", Status: "running"},
		{ID: "env_paused", Key: &pausedKey, Status: "paused"},
		{ID: "env_error", Status: "error"},
	}

	got := muxSSHConnectableEnvironments(environments)
	want := []environment.Environment{environments[1], environments[2]}

	if !slices.EqualFunc(got, want, func(a, b environment.Environment) bool { return a.ID == b.ID }) {
		t.Fatalf("connectable environments = %#v, want running and paused", got)
	}
}

func TestTmuxMuxBackendStartsBastionSSHSession(t *testing.T) {
	t.Parallel()

	runner := &fakeTmuxRunner{
		outputs: map[string]fakeTmuxResponse{
			"has-session -t test-mux": {err: errors.New("missing")},
			"new-session -d -P -F #{window_id}\t#{pane_id} -s test-mux -n dev-env exec '/usr/bin/bastion' --api-url 'http://bastion.test' ssh --id env_123": {out: "@1\t%1\n"},
		},
	}
	backend := tmuxMuxBackend{
		sessionName: "test-mux",
		runner:      runner,
		executable:  "/usr/bin/bastion",
		apiURL:      muxTestAPIURL,
	}
	key := "dev-env"

	session, err := backend.OpenEnvironment(context.Background(), environment.Environment{ID: cliTestEnvironmentID, Key: &key})
	if err != nil {
		t.Fatalf("open environment: %v", err)
	}

	if session.EnvironmentID != cliTestEnvironmentID || session.WindowID != "@1" || session.PaneID != "%1" {
		t.Fatalf("session = %#v, want tmux window/pane for env", session)
	}

	if !runner.called("set-window-option", "-t", "@1", "@bastion_env_id", cliTestEnvironmentID) {
		t.Fatalf("tmux calls = %#v, want env id metadata", runner.calls)
	}
}

func TestTmuxMuxBackendReusesExistingEnvironmentWindow(t *testing.T) {
	t.Parallel()

	runner := &fakeTmuxRunner{
		outputs: map[string]fakeTmuxResponse{
			"has-session -t test-mux": {out: ""},
			"list-windows -t test-mux -F #{window_id}\t#{window_name}\t#{pane_id}\t#{@bastion_env_id}\t#{@bastion_env_key}": {out: "@4\tdev-env\t%7\tenv_123\tdev-env\n"},
		},
	}
	backend := tmuxMuxBackend{
		sessionName: "test-mux",
		runner:      runner,
		executable:  "/usr/bin/bastion",
		apiURL:      muxTestAPIURL,
	}
	key := "dev-env"

	session, err := backend.OpenEnvironment(context.Background(), environment.Environment{ID: cliTestEnvironmentID, Key: &key})
	if err != nil {
		t.Fatalf("open environment: %v", err)
	}

	if session.WindowID != "@4" || session.PaneID != "%7" {
		t.Fatalf("session = %#v, want existing window", session)
	}

	if runner.called("new-window") || runner.called("new-session") {
		t.Fatalf("tmux calls = %#v, did not expect new tmux window", runner.calls)
	}
}

type fakeTmuxRunner struct {
	calls   [][]string
	outputs map[string]fakeTmuxResponse
}

type fakeTmuxResponse struct {
	out string
	err error
}

func (r *fakeTmuxRunner) run(_ context.Context, args ...string) (string, error) {
	call := append([]string(nil), args...)
	r.calls = append(r.calls, call)

	key := strings.Join(args, " ")
	if res, ok := r.outputs[key]; ok {
		return res.out, res.err
	}

	return "", nil
}

func (r *fakeTmuxRunner) called(args ...string) bool {
	for _, call := range r.calls {
		if len(args) > len(call) {
			continue
		}

		if slices.Equal(call[:len(args)], args) {
			return true
		}
	}

	return false
}
