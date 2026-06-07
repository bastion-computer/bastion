//nolint:wsl_v5,goconst // End-to-end worker tests keep fakes and assertions together.
package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/integrations/linear/internal/bastion"
	"github.com/bastion-computer/bastion/integrations/linear/internal/database"
	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
	"github.com/bastion-computer/bastion/integrations/linear/internal/opencode"
)

func TestServiceProcessesCreatedWebhook(t *testing.T) {
	t.Parallel()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	linearClient := newFakeLinear()
	bastionClient := fakeBastion{environments: []bastion.Environment{{ID: "env_1", Status: "running", Tags: []string{"linear"}}}}
	opencodeClient := &fakeOpenCode{response: "implemented"}
	svc := New(db, linearClient, bastionClient, opencodeClient, Config{Selector: Selector{Tags: []string{"linear"}}, AppUserID: "app_1", WorkerInterval: time.Millisecond}, nil)

	ctx := t.Context()

	svc.Start(ctx)

	err = svc.AcceptWebhook(ctx, linear.AgentSessionEventWebhookPayload{
		Action:           "created",
		WebhookID:        "wh_1",
		WebhookTimestamp: float64(time.Now().UnixMilli()),
		PromptContext:    "please implement",
		AgentSession: linear.AgentSessionWebhook{ID: "as_1", IssueID: "issue_1", Issue: &linear.IssueWebhook{
			ID:         "issue_1",
			Identifier: "BAS-12",
			Title:      "Implement Linear integration",
			TeamID:     "team_1",
		}},
	}, []byte(`{}`))
	if err != nil {
		t.Fatalf("accept webhook: %v", err)
	}

	activity := linearClient.waitActivity(t, "response")
	if activity["body"] != "implemented" {
		t.Fatalf("response body = %q, want implemented", activity["body"])
	}

	if opencodeClient.startedEnv != "env_1" || opencodeClient.stoppedEnv != "env_1" {
		t.Fatalf("opencode start/stop env = %q/%q, want env_1/env_1", opencodeClient.startedEnv, opencodeClient.stoppedEnv)
	}

	if linearClient.issueID != "issue_1" || linearClient.stateID != "state_started" || linearClient.delegateID != "app_1" {
		t.Fatalf("issue update = %q/%q/%q", linearClient.issueID, linearClient.stateID, linearClient.delegateID)
	}
}

func TestServiceProcessesAssignmentNotification(t *testing.T) {
	t.Parallel()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	linearClient := newFakeLinear()
	bastionClient := fakeBastion{environments: []bastion.Environment{{ID: "env_1", Status: "running", Tags: []string{"linear"}}}}
	opencodeClient := &fakeOpenCode{response: "implemented from assignment"}
	svc := New(db, linearClient, bastionClient, opencodeClient, Config{Selector: Selector{Tags: []string{"linear"}}, AppUserID: "app_1", WorkerInterval: time.Millisecond}, nil)

	ctx := t.Context()

	svc.Start(ctx)

	err = svc.AcceptWebhook(ctx, linear.AgentSessionEventWebhookPayload{
		Type:             "AppUserNotification",
		Action:           "issueAssignedToYou",
		WebhookID:        "wh_1",
		WebhookTimestamp: float64(time.Now().UnixMilli()),
		Notification: &linear.NotificationWebhook{
			ID:      "notif_1",
			Type:    "issueAssignedToYou",
			IssueID: "issue_1",
			Issue: &linear.IssueWebhook{
				ID:          "issue_1",
				Identifier:  "BAS-24",
				Title:       "Debug Linear integration",
				Description: "Use the live Linear API.",
				TeamID:      "team_1",
			},
		},
	}, []byte(`{}`))
	if err != nil {
		t.Fatalf("accept webhook: %v", err)
	}

	activity := linearClient.waitActivity(t, "response")
	if activity["body"] != "implemented from assignment" {
		t.Fatalf("response body = %q, want implemented from assignment", activity["body"])
	}

	if linearClient.createdSessionIssueID != "issue_1" {
		t.Fatalf("created session issue ID = %q, want issue_1", linearClient.createdSessionIssueID)
	}
}

func TestServiceRetriesAssignmentNotificationAfterCreateSessionFailure(t *testing.T) {
	t.Parallel()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	linearClient := newFakeLinear()
	linearClient.createSessionErr = errors.New("temporary Linear error")
	bastionClient := fakeBastion{environments: []bastion.Environment{{ID: "env_1", Status: "running"}}}
	opencodeClient := &fakeOpenCode{response: "implemented after retry"}
	svc := New(db, linearClient, bastionClient, opencodeClient, Config{WorkerInterval: time.Millisecond}, nil)

	ctx := t.Context()
	svc.Start(ctx)

	payload := linear.AgentSessionEventWebhookPayload{
		Type:             "AppUserNotification",
		Action:           "issueAssignedToYou",
		WebhookID:        "wh_1",
		WebhookTimestamp: float64(time.Now().UnixMilli()),
		Notification: &linear.NotificationWebhook{
			ID:      "notif_1",
			Type:    "issueAssignedToYou",
			IssueID: "issue_1",
			Issue:   &linear.IssueWebhook{ID: "issue_1", Identifier: "BAS-24", Title: "Debug Linear integration"},
		},
	}

	if err := svc.AcceptWebhook(ctx, payload, []byte(`{}`)); err == nil {
		t.Fatalf("accept webhook succeeded, want temporary error")
	}

	linearClient.createSessionErr = nil
	if err := svc.AcceptWebhook(ctx, payload, []byte(`{}`)); err != nil {
		t.Fatalf("accept webhook retry: %v", err)
	}

	activity := linearClient.waitActivity(t, "response")
	if activity["body"] != "implemented after retry" {
		t.Fatalf("response body = %q, want implemented after retry", activity["body"])
	}
}

func TestServiceProcessesStopWebhook(t *testing.T) {
	t.Parallel()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = db.Close() }()

	linearClient := newFakeLinear()
	bastionClient := fakeBastion{environments: []bastion.Environment{{ID: "env_1", Status: "running"}}}
	opencodeClient := &fakeOpenCode{response: "implemented"}
	svc := New(db, linearClient, bastionClient, opencodeClient, Config{WorkerInterval: time.Millisecond}, nil)

	ctx := t.Context()

	svc.Start(ctx)

	if err := svc.AcceptWebhook(ctx, linear.AgentSessionEventWebhookPayload{Action: "created", WebhookID: "wh_1", WebhookTimestamp: float64(time.Now().UnixMilli()), AgentSession: linear.AgentSessionWebhook{ID: "as_1"}}, []byte(`{}`)); err != nil {
		t.Fatalf("accept created webhook: %v", err)
	}
	_ = linearClient.waitActivity(t, "response")

	if err := svc.AcceptWebhook(ctx, linear.AgentSessionEventWebhookPayload{Action: "prompted", WebhookID: "wh_2", WebhookTimestamp: float64(time.Now().UnixMilli()), AgentSession: linear.AgentSessionWebhook{ID: "as_1"}, AgentActivity: &linear.AgentActivityWebhook{Signal: "stop"}}, []byte(`{}`)); err != nil {
		t.Fatalf("accept stop webhook: %v", err)
	}

	activity := linearClient.waitActivity(t, "response")
	if activity["body"] == "" {
		t.Fatalf("stop response missing body")
	}
}

type fakeLinear struct {
	mu         sync.Mutex
	activities chan linear.ActivityContent
	issueID    string
	stateID    string
	delegateID string

	createdSessionIssueID string
	createSessionErr      error
}

func newFakeLinear() *fakeLinear {
	return &fakeLinear{activities: make(chan linear.ActivityContent, 20)}
}

func (f *fakeLinear) CreateActivity(_ context.Context, _ string, content linear.ActivityContent, _ bool, _ string, _ map[string]any) error {
	f.activities <- content
	return nil
}

func (f *fakeLinear) AgentSessionForIssue(context.Context, string, string) (linear.AgentSessionWebhook, bool, error) {
	return linear.AgentSessionWebhook{}, false, nil
}

func (f *fakeLinear) CreateAgentSessionOnIssue(_ context.Context, issueID string) (linear.AgentSessionWebhook, error) {
	f.mu.Lock()
	if f.createSessionErr != nil {
		err := f.createSessionErr
		f.mu.Unlock()
		return linear.AgentSessionWebhook{}, err
	}
	f.createdSessionIssueID = issueID
	f.mu.Unlock()

	return linear.AgentSessionWebhook{ID: "as_created", IssueID: issueID, Issue: &linear.IssueWebhook{ID: issueID, Identifier: "BAS-24", Title: "Debug Linear integration", TeamID: "team_1"}}, nil
}

func (f *fakeLinear) UpdatePlan(context.Context, string, []linear.PlanStep) error { return nil }
func (f *fakeLinear) StartedState(context.Context, string) (string, error) {
	return "state_started", nil
}

func (f *fakeLinear) UpdateIssue(_ context.Context, issueID, stateID, delegateID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueID = issueID
	f.stateID = stateID
	f.delegateID = delegateID
	return nil
}

func (f *fakeLinear) IssueAttachments(context.Context, string) ([]linear.Attachment, error) {
	return nil, nil
}

func (f *fakeLinear) waitActivity(t *testing.T, activityType string) linear.ActivityContent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case activity := <-f.activities:
			if activity["type"] == activityType {
				return activity
			}
		case <-deadline:
			t.Fatalf("timed out waiting for activity %q", activityType)
		}
	}
}

type fakeBastion struct {
	environments []bastion.Environment
}

func (f fakeBastion) ListEnvironments(context.Context, []string) ([]bastion.Environment, error) {
	return f.environments, nil
}

type fakeOpenCode struct {
	response   string
	startedEnv string
	stoppedEnv string
}

func (f *fakeOpenCode) StartServer(_ context.Context, environmentID string) (int, error) {
	f.startedEnv = environmentID
	return 123, nil
}

func (f *fakeOpenCode) StopServer(_ context.Context, environmentID string) error {
	f.stoppedEnv = environmentID
	return nil
}

func (f *fakeOpenCode) CreateSession(context.Context, string, string) (opencode.Session, error) {
	return opencode.Session{ID: "oc_1"}, nil
}

func (f *fakeOpenCode) SendMessage(context.Context, string, string, string, []linear.Attachment) (opencode.Response, error) {
	return opencode.Response{Text: f.response}, nil
}

func (f *fakeOpenCode) Abort(context.Context, string, string) error { return nil }
