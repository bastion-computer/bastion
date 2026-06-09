package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRootCommandIncludesOpenCode(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	for _, subcommand := range cmd.Commands() {
		if subcommand.Name() == "opencode" && !subcommand.Hidden {
			return
		}
	}

	t.Fatal("root command is missing visible opencode subcommand")
}

func TestOpenCodeCommandUsesIDProxyURL(t *testing.T) {
	t.Parallel()

	got := runOpenCodeCommandProxyURL(t, "http://localhost:3148/api/", []string{cliTestIDFlag, "env 123"})
	want := "http://localhost:3148/api/v1/environments/env%20123/agents/opencode"

	if got != want {
		t.Fatalf("proxy URL = %q, want %q", got, want)
	}
}

func TestOpenCodeCommandUsesKeyProxyURL(t *testing.T) {
	t.Parallel()

	got := runOpenCodeCommandProxyURL(t, "http://localhost:3148", []string{cliTestKeyFlag, "feature/dev"})
	want := "http://localhost:3148/v1/environments/by-key/feature%2Fdev/agents/opencode"

	if got != want {
		t.Fatalf("proxy URL = %q, want %q", got, want)
	}
}

func runOpenCodeCommandProxyURL(t *testing.T, apiURL string, args []string) string {
	t.Helper()

	gotProxyURL := make(chan string, 1)
	cmd := newOpenCodeCommandWithRunner(&rootOptions{apiURL: apiURL}, func(_ context.Context, _ io.Reader, _, _ io.Writer, proxyURL string) error {
		gotProxyURL <- proxyURL

		return nil
	})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case got := <-gotProxyURL:
		return got
	case <-time.After(time.Second):
		t.Fatal("opencode runner was not called")
	}

	return ""
}

func TestRunOpenCodeAttachReturnsMissingBinary(t *testing.T) {
	t.Setenv("PATH", "")

	err := runOpenCodeAttach(context.Background(), bytes.NewBuffer(nil), &bytes.Buffer{}, &bytes.Buffer{}, "http://localhost:3148/v1/environments/env_123/agents/opencode")
	if err == nil || !strings.Contains(err.Error(), "opencode is not available") {
		t.Fatalf("run opencode attach error = %v, want missing opencode", err)
	}
}
