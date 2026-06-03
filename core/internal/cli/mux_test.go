package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

func TestRootCommandIncludesMux(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	if child, _, err := cmd.Find([]string{"mux"}); err != nil || child == nil || child.Use != "mux" {
		t.Fatalf("root mux command = %v, %v, want mux command", child, err)
	}
}

func TestMuxCommandFailsWhenTmuxIsMissing(t *testing.T) {
	t.Parallel()

	backend := &fakeMuxBackend{preflightErr: errors.New("tmux is required for bastion mux")}
	runCalled := false

	cmd := newMuxCommandWithOptions(&rootOptions{apiURL: "http://api.test"}, muxOptions{
		backend: backend,
		runTUI: func(context.Context, muxModel, io.Reader, io.Writer) error {
			runCalled = true

			return nil
		},
	})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "tmux is required") {
		t.Fatalf("execute error = %v, want tmux missing error", err)
	}

	if backend.ensureCalled {
		t.Fatal("ensure called after failed preflight")
	}

	if runCalled {
		t.Fatal("TUI ran after failed preflight")
	}
}

func TestMuxCommandRunsTUIAfterPreflight(t *testing.T) {
	t.Parallel()

	backend := &fakeMuxBackend{sessions: []muxSession{{Target: "@7", EnvironmentID: cliTestEnvironmentID, EnvironmentKey: cliTestEnvironmentKey}}}
	runCalled := false

	cmd := newMuxCommandWithOptions(&rootOptions{apiURL: "http://api.test"}, muxOptions{
		backend: backend,
		runTUI: func(_ context.Context, model muxModel, _ io.Reader, _ io.Writer) error {
			runCalled = true

			if model.backend != backend {
				t.Fatalf("model backend = %#v, want fake backend", model.backend)
			}

			return nil
		},
	})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if !backend.preflightCalled || !backend.ensureCalled {
		t.Fatalf("backend calls preflight=%v ensure=%v, want both", backend.preflightCalled, backend.ensureCalled)
	}

	if !runCalled {
		t.Fatal("TUI was not run")
	}
}

func TestLoadRunningMuxEnvironmentsFiltersPages(t *testing.T) {
	t.Parallel()

	next := "next"
	key := cliTestEnvironmentKey
	api := &fakeMuxAPI{pages: []services.Page[environment.Environment]{
		{
			Cursor: &next,
			Entries: []environment.Environment{
				{ID: "env_stopped", Status: "stopped"},
				{ID: "env_running", Status: cliTestRunningStatus},
				{ID: "env_paused", Status: "paused"},
			},
		},
		{
			Entries: []environment.Environment{{ID: cliTestKeyedEnvironmentID, Key: &key, Status: cliTestRunningStatus}},
		},
	}}

	got, err := loadRunningMuxEnvironments(context.Background(), api)
	if err != nil {
		t.Fatalf("load running environments: %v", err)
	}

	ids := make([]string, 0, len(got))
	for _, entry := range got {
		ids = append(ids, entry.ID)
	}

	if !slices.Equal(ids, []string{"env_running", cliTestKeyedEnvironmentID}) {
		t.Fatalf("running environment ids = %#v, want env_running/env_keyed", ids)
	}

	if !slices.Equal(api.cursors, []string{"", next}) {
		t.Fatalf("cursors = %#v, want first page then next", api.cursors)
	}
}

func TestMuxModelCreatesSessionFromPicker(t *testing.T) {
	t.Parallel()

	key := cliTestEnvironmentKey
	backend := &fakeMuxBackend{}
	model := newMuxModel(context.Background(), backend, &fakeMuxAPI{}, muxConfig{})
	model.showPicker = true
	model.environments = []environment.Environment{{ID: cliTestEnvironmentID, Key: &key, Status: cliTestRunningStatus}}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on picker returned nil command")
	}

	msg := cmd()
	updated, _ = updated.Update(msg)

	got, ok := updated.(muxModel)
	if !ok {
		t.Fatalf("updated model = %T, want muxModel", updated)
	}

	if !slices.Equal(backend.created, []string{cliTestEnvironmentID}) {
		t.Fatalf("created sessions = %#v, want %s", backend.created, cliTestEnvironmentID)
	}

	if got.showPicker || len(got.sessions) != 1 || got.sessions[0].EnvironmentID != cliTestEnvironmentID || got.selected != 0 {
		t.Fatalf("model after create = %#v, want picker closed with selected session", got)
	}
}

func TestMuxModelForwardsInputToFocusedSession(t *testing.T) {
	t.Parallel()

	backend := &fakeMuxBackend{}
	model := newMuxModel(context.Background(), backend, &fakeMuxAPI{}, muxConfig{})
	model.sessions = []muxSession{{Target: "@9", EnvironmentID: cliTestEnvironmentID}}

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd == nil {
		t.Fatal("typed key returned nil command")
	}

	if msg := cmd(); msg != nil {
		t.Fatalf("send input message = %#v, want nil", msg)
	}

	if len(backend.inputs) != 1 || backend.inputs[0].target != "@9" || backend.inputs[0].input.Literal != "x" {
		t.Fatalf("sent inputs = %#v, want literal x to @9", backend.inputs)
	}
}

func TestMuxModelViewShowsSidebarAndPane(t *testing.T) {
	t.Parallel()

	model := muxModel{
		sessions: []muxSession{
			{Target: "@7", EnvironmentID: "env_prod", EnvironmentKey: "prod"},
			{Target: "@8", EnvironmentID: "env_dev", EnvironmentKey: "dev"},
		},
		selected: 0,
		pane:     "root@env_prod:~# opencode\nrunning",
		width:    80,
		height:   12,
	}

	view := model.View()
	for _, want := range []string{"bastion mux", "SSH Sessions", "> prod", "env_dev", "root@env_prod", "n new session", "d detach"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

type fakeMuxBackend struct {
	preflightErr    error
	preflightCalled bool
	ensureCalled    bool
	sessions        []muxSession
	created         []string
	inputs          []fakeMuxInput
}

func (b *fakeMuxBackend) Preflight(context.Context) error {
	b.preflightCalled = true

	return b.preflightErr
}

func (b *fakeMuxBackend) Ensure(context.Context) error {
	b.ensureCalled = true

	return nil
}

func (b *fakeMuxBackend) Sessions(context.Context) ([]muxSession, error) {
	return append([]muxSession(nil), b.sessions...), nil
}

func (b *fakeMuxBackend) CreateSession(_ context.Context, env environment.Environment) (muxSession, error) {
	b.created = append(b.created, env.ID)

	session := muxSession{Target: "@" + env.ID, EnvironmentID: env.ID}
	if env.Key != nil {
		session.EnvironmentKey = *env.Key
	}

	b.sessions = append(b.sessions, session)

	return session, nil
}

func (b *fakeMuxBackend) Capture(context.Context, string, int) (string, error) {
	return "", nil
}

func (b *fakeMuxBackend) SendInput(_ context.Context, target string, input muxInput) error {
	b.inputs = append(b.inputs, fakeMuxInput{target: target, input: input})

	return nil
}

func (b *fakeMuxBackend) Resize(context.Context, string, int, int) error {
	return nil
}

type fakeMuxInput struct {
	target string
	input  muxInput
}

type fakeMuxAPI struct {
	pages   []services.Page[environment.Environment]
	cursors []string
}

func (a *fakeMuxAPI) ListEnvironments(_ context.Context, _ int, cursor string, _ []string) (services.Page[environment.Environment], error) {
	a.cursors = append(a.cursors, cursor)
	if len(a.pages) == 0 {
		return services.Page[environment.Environment]{}, nil
	}

	page := a.pages[0]
	a.pages = a.pages[1:]

	return page, nil
}
