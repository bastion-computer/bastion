//nolint:wsl_v5 // This test intentionally exercises several mock GraphQL operations in one flow.
package mocklinear

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
)

func TestMockLinearSupportsClientOperations(t *testing.T) {
	t.Parallel()

	mock := New("secret")
	mock.Attachments = []linear.Attachment{{ID: "att_1", Title: "image.png", URL: "https://example.com/image.png"}}

	server := httptest.NewServer(mock)
	defer server.Close()

	client := linear.NewClient(server.URL, "token")
	agentSession, err := client.CreateAgentSessionOnIssue(t.Context(), "issue_1")
	if err != nil {
		t.Fatalf("create agent session: %v", err)
	}
	if agentSession.ID == "" || agentSession.IssueID != "issue_1" {
		t.Fatalf("agent session = %#v, want session for issue_1", agentSession)
	}
	found, ok, err := client.AgentSessionForIssue(t.Context(), "issue_1", "app_e2e")
	if err != nil {
		t.Fatalf("find agent session: %v", err)
	}
	if !ok || found.ID != agentSession.ID {
		t.Fatalf("found agent session = %#v/%v, want %s", found, ok, agentSession.ID)
	}

	if err := client.CreateActivity(t.Context(), "as_1", linear.ActivityContent{"type": "thought", "body": "hi"}, true, "", nil); err != nil {
		t.Fatalf("create activity: %v", err)
	}

	if err := client.UpdatePlan(t.Context(), "as_1", []linear.PlanStep{{Content: "step", Status: "pending"}}); err != nil {
		t.Fatalf("update plan: %v", err)
	}
	stateID, err := client.StartedState(t.Context(), "team_1")
	if err != nil {
		t.Fatalf("started state: %v", err)
	}
	if stateID != "state_started" {
		t.Fatalf("stateID = %q, want state_started", stateID)
	}
	if err := client.UpdateIssue(t.Context(), "issue_1", stateID, "app_1"); err != nil {
		t.Fatalf("update issue: %v", err)
	}
	attachments, err := client.IssueAttachments(t.Context(), "issue_1")
	if err != nil {
		t.Fatalf("issue attachments: %v", err)
	}
	if len(attachments) != 1 || attachments[0].ID != "att_1" {
		t.Fatalf("attachments = %#v, want att_1", attachments)
	}

	body, signature, err := mock.SignedWebhook(linear.AgentSessionEventWebhookPayload{WebhookID: "wh_1", WebhookTimestamp: float64(time.Now().UnixMilli()), AgentSession: linear.AgentSessionWebhook{ID: "as_1"}})
	if err != nil {
		t.Fatalf("signed webhook: %v", err)
	}
	if !linear.VerifySignature(body, signature, "secret") {
		t.Fatalf("mock webhook signature did not verify")
	}
}
