//nolint:wsl_v5 // Payload assertions stay close to the request setup.
package opencode

import (
	"context"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/integrations/linear/internal/bastion"
	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
)

func TestSendMessageIncludesTextAndAttachments(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{stdout: []byte(`{"parts":[{"type":"text","text":"done"}]}`)}
	client := NewClient(runner, Config{Port: 4096})

	attachments := []linear.Attachment{{ID: "att_1", Title: "image.png", URL: "https://example.com/image.png", Metadata: map[string]any{"contentType": "image/png"}}}
	response, err := client.SendMessage(context.Background(), "env_1", "session_1", "prompt", attachments)
	if err != nil {
		t.Fatalf("send message: %v", err)
	}

	if response.Text != "done" {
		t.Fatalf("response text = %q, want done", response.Text)
	}

	if !strings.Contains(runner.script, "/session/session_1/message") {
		t.Fatalf("script did not call message endpoint: %s", runner.script)
	}

	if !strings.Contains(runner.script, "base64 -d") {
		t.Fatalf("script did not stream JSON through base64: %s", runner.script)
	}
}

type fakeRunner struct {
	stdout []byte
	script string
}

func (f *fakeRunner) RunShell(
	_ context.Context,
	_ string,
	script string,
	_ []byte,
) (bastion.CommandResult, error) {
	f.script = script

	return bastion.CommandResult{Stdout: f.stdout}, nil
}
