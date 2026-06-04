package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/client"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

func TestRootCommandIncludesMux(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	for _, subcommand := range cmd.Commands() {
		if subcommand.Name() == "mux" && !subcommand.Hidden {
			return
		}
	}

	t.Fatal("root command is missing visible mux subcommand")
}

func TestMuxTmuxConfigEmbedsNordSelectorTheme(t *testing.T) {
	t.Parallel()

	config := string(bastionTmuxConfig)
	for _, want := range []string{
		`status-style "bg=#2E3440,fg=#ECEFF4"`,
		`set-hook -t bastion after-new-window[90]`,
		`menu-style "bg=#2E3440,fg=#D8DEE9"`,
		`menu-selected-style "bg=#88C0D0,fg=#2E3440,bold"`,
		`menu-border-style "fg=#88C0D0,bg=#2E3440,bold"`,
		`popup-style "bg=#2E3440,fg=#D8DEE9"`,
		`popup-border-style "fg=#88C0D0,bg=#2E3440,bold"`,
		`window-status-format "#[fg=#D8DEE9,bg=#3B4252]  #I #W #F  "`,
		`window-status-current-format "#[fg=#2E3440,bg=#88C0D0,bold]  #I #W #F  "`,
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("tmux config missing %q:\n%s", want, config)
		}
	}
}

func TestMuxTmuxConfigDoesNotLoadExternalTheme(t *testing.T) {
	t.Parallel()

	config := string(bastionTmuxConfig)
	for _, disallowed := range []string{"run-shell", "source-file", "@plugin", "set -g", "setw -g", "set-option -g", "set-window-option -g"} {
		if strings.Contains(config, disallowed) {
			t.Fatalf("tmux config contains non-isolated theme hook %q:\n%s", disallowed, config)
		}
	}
}

func TestMuxEnvironmentLabelPrefersKey(t *testing.T) {
	t.Parallel()

	key := cliTestEnvironmentKey
	if got := muxEnvironmentLabel(environment.Environment{ID: cliTestEnvironmentID, Key: &key}); got != cliTestEnvironmentKey {
		t.Fatalf("label = %q, want key %q", got, cliTestEnvironmentKey)
	}

	if got := muxEnvironmentLabel(environment.Environment{ID: cliTestEnvironmentID}); got != cliTestEnvironmentID {
		t.Fatalf("label = %q, want id %q", got, cliTestEnvironmentID)
	}
}

func TestMuxWindowNameAddsDuplicateSuffix(t *testing.T) {
	t.Parallel()

	const muxTestWindowName = "dev"

	tests := []struct {
		name  string
		count int
		want  string
	}{
		{name: muxTestWindowName, count: 0, want: muxTestWindowName},
		{name: muxTestWindowName, count: 1, want: muxTestWindowName + " (2)"},
		{name: muxTestWindowName, count: 2, want: muxTestWindowName + " (3)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := muxWindowName(tt.name, tt.count); got != tt.want {
				t.Fatalf("window name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMuxSameEnvironmentCountSkipsTargetWindow(t *testing.T) {
	t.Parallel()

	windowList := "@1\tenv_same\n@2\tenv_other\n@3\tenv_same\n@4\tenv_same\n"
	if got := muxSameEnvironmentCount(windowList, "env_same", "@3"); got != 2 {
		t.Fatalf("same environment count = %d, want 2", got)
	}
}

func TestCollectMuxEnvironmentsPagesThroughList(t *testing.T) {
	t.Parallel()

	gotCursors := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments" {
			t.Fatalf("request = %s %s, want GET /v1/environments", r.Method, r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("limit") != "100" {
			t.Fatalf("limit = %q, want 100", query.Get("limit"))
		}

		gotCursors <- query.Get("cursor")

		switch query.Get("cursor") {
		case "":
			cursor := cliTestNextCursor
			writeMuxEnvironmentPage(t, w, services.Page[environment.Environment]{
				Cursor:  &cursor,
				Entries: []environment.Environment{{ID: "env_first"}},
			})
		case cliTestNextCursor:
			writeMuxEnvironmentPage(t, w, services.Page[environment.Environment]{
				Entries: []environment.Environment{{ID: "env_second"}},
			})
		default:
			t.Fatalf("unexpected cursor %q", query.Get("cursor"))
		}
	}))
	t.Cleanup(server.Close)

	environments, err := collectMuxEnvironments(context.Background(), client.New(server.URL))
	if err != nil {
		t.Fatalf("collect environments: %v", err)
	}

	ids := make([]string, 0, len(environments))
	for _, env := range environments {
		ids = append(ids, env.ID)
	}

	if !reflect.DeepEqual(ids, []string{"env_first", "env_second"}) {
		t.Fatalf("environment ids = %#v, want first and second", ids)
	}

	if got := drainMuxCursors(gotCursors); !reflect.DeepEqual(got, []string{"", cliTestNextCursor}) {
		t.Fatalf("cursors = %#v, want empty then next", got)
	}
}

func writeMuxEnvironmentPage(t *testing.T, w http.ResponseWriter, page services.Page[environment.Environment]) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(page); err != nil {
		t.Fatalf("encode environment page: %v", err)
	}
}

func drainMuxCursors(values <-chan string) []string {
	var out []string

	for {
		select {
		case value := <-values:
			out = append(out, value)
		default:
			return out
		}
	}
}

func TestMuxConnectTargetCommandQuotesShellArguments(t *testing.T) {
	t.Parallel()

	target := muxTarget{session: muxSessionName, window: "@1", pane: "%2"}
	got := muxConnectTargetShellCommand("/tmp/bastion cli", target, "env_123", "dev env")
	want := "'/tmp/bastion cli' 'mux' 'connect' '--target-session' 'bastion' '--target-window' '@1' '--target-pane' '%2' '--id' 'env_123' '--name' 'dev env'"

	if got != want {
		t.Fatalf("connect command = %q, want %q", got, want)
	}
}

func TestMuxPendingShellCommandSetsAPIURL(t *testing.T) {
	t.Parallel()

	got := muxPendingShellCommand("/tmp/bastion", "http://localhost:9999/api path")
	want := "BASTION_API_URL='http://localhost:9999/api path' '/tmp/bastion' 'mux' 'pending'"

	if got != want {
		t.Fatalf("pending command = %q, want %q", got, want)
	}
}

func TestMuxMenuArgsBuildsEnvironmentMenu(t *testing.T) {
	t.Parallel()

	key := cliTestEnvironmentKey
	target := muxTarget{session: muxSessionName, window: "@1", pane: "%2"}
	args := muxMenuArgs("/tmp/bastion", target, []environment.Environment{{ID: cliTestEnvironmentID, Key: &key, Status: cliTestRunningStatus}})

	if len(args) != 18 {
		t.Fatalf("menu args length = %d, want 18: %#v", len(args), args)
	}

	if got := args[:15]; !reflect.DeepEqual(got, []string{"display-menu", "-t", "%2", "-x", "C", "-y", "C", "-s", muxNordMenuStyle, "-H", muxNordMenuSelectedStyle, "-S", muxNordMenuBorderStyle, "-T", "Bastion environments"}) {
		t.Fatalf("menu prefix = %#v, want centered Nord display-menu", got)
	}

	if args[15] != cliTestEnvironmentKey+"  ["+cliTestEnvironmentID+"]  "+cliTestRunningStatus {
		t.Fatalf("menu label = %q, want keyed environment label", args[15])
	}

	if args[16] != "" {
		t.Fatalf("menu key = %q, want empty shortcut", args[16])
	}

	if !strings.Contains(args[17], "'mux' 'connect'") || !strings.Contains(args[17], "'--id' '"+cliTestEnvironmentID+"'") {
		t.Fatalf("menu command = %q, want mux connect command", args[17])
	}
}

func TestMuxConnectRenamesAndRespawnsPane(t *testing.T) {
	t.Parallel()

	tmux := &fakeMuxTmuxRunner{outputs: map[string]string{
		muxTmuxKey("list-windows", "-t", muxSessionName, "-F", "#{window_id}\t#{@bastion_environment_id}"): "@1\tenv_same\n@2\tenv_other\n",
	}}
	target := muxTarget{session: muxSessionName, window: "@3", pane: "%3"}

	if err := runMuxConnect(context.Background(), tmux, target, "env_same", "dev"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if got := tmux.calls[len(tmux.calls)-3]; !reflect.DeepEqual(got, []string{"set-window-option", "-q", "-t", "@3", "@bastion_environment_id", "env_same"}) {
		t.Fatalf("set-window-option call = %#v", got)
	}

	if got := tmux.calls[len(tmux.calls)-2]; !reflect.DeepEqual(got, []string{"rename-window", "-t", "@3", "dev (2)"}) {
		t.Fatalf("rename-window call = %#v", got)
	}

	if got := tmux.calls[len(tmux.calls)-1]; len(got) != 5 || got[0] != "respawn-pane" || got[1] != "-k" || got[2] != "-t" || got[3] != "%3" || !strings.Contains(got[4], "'ssh' '--id' 'env_same'") {
		t.Fatalf("respawn-pane call = %#v", got)
	}
}

func TestCurrentMuxTargetUsesCurrentPane(t *testing.T) {
	t.Setenv("TMUX_PANE", "%7")

	tmux := &fakeMuxTmuxRunner{outputs: map[string]string{
		muxTmuxKey("display-message", "-p", "-t", "%7", "#{session_name}\t#{window_id}\t#{pane_id}"): "bastion\t@9\t%7\n",
	}}

	target, err := currentMuxTarget(context.Background(), tmux)
	if err != nil {
		t.Fatalf("current target: %v", err)
	}

	if target != (muxTarget{session: "bastion", window: "@9", pane: "%7"}) {
		t.Fatalf("target = %#v, want current tmux pane target", target)
	}
}

type fakeMuxTmuxRunner struct {
	outputs map[string]string
	calls   [][]string
}

func (r *fakeMuxTmuxRunner) run(_ context.Context, args ...string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	key := muxTmuxKey(args...)

	return r.outputs[key], nil
}

func muxTmuxKey(args ...string) string {
	return strings.Join(args, "\x00")
}

func TestTmuxAttachSessionNeedsTerminal(t *testing.T) {
	t.Parallel()

	if !tmuxCommandNeedsTerminal([]string{"attach-session", "-t", muxSessionName}) {
		t.Fatal("attach-session should inherit the terminal")
	}

	if tmuxCommandNeedsTerminal([]string{"new-session", "-d", "-s", muxSessionName}) {
		t.Fatal("new-session should keep captured output")
	}
}

func TestShellQuoteEscapesSingleQuote(t *testing.T) {
	t.Parallel()

	value, err := url.QueryUnescape("dev%27env")
	if err != nil {
		t.Fatalf("unescape: %v", err)
	}

	if got := shellQuote(value); got != "'dev'\\''env'" {
		t.Fatalf("shell quote = %q, want escaped single quote", got)
	}
}
