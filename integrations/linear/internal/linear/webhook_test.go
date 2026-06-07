package linear

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseVerifiedWebhook(t *testing.T) {
	t.Parallel()

	secret := "secret"
	now := time.Now()

	body, err := json.Marshal(AgentSessionEventWebhookPayload{
		Type:             "AgentSessionEvent",
		Action:           "created",
		WebhookID:        "wh_1",
		WebhookTimestamp: float64(now.UnixMilli()),
		AgentSession:     AgentSessionWebhook{ID: "as_1"},
	})
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}

	payload, err := ParseVerifiedWebhook(body, SignWebhook(body, secret), secret, now)
	if err != nil {
		t.Fatalf("parse verified webhook: %v", err)
	}

	if payload.WebhookID != "wh_1" || payload.AgentSession.ID != "as_1" {
		t.Fatalf("payload = %#v, want webhook and session IDs", payload)
	}
}

func TestParseVerifiedWebhookRejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	body := []byte(`{"webhookId":"wh_1","webhookTimestamp":1,"agentSession":{"id":"as_1"}}`)
	if _, err := ParseVerifiedWebhook(body, "bad", "secret", time.UnixMilli(1)); err == nil {
		t.Fatalf("ParseVerifiedWebhook accepted invalid signature")
	}
}

func TestParseVerifiedWebhookRejectsStaleTimestamp(t *testing.T) {
	t.Parallel()

	secret := "secret"

	body := []byte(`{"webhookId":"wh_1","webhookTimestamp":1,"agentSession":{"id":"as_1"}}`)
	if _, err := ParseVerifiedWebhook(body, SignWebhook(body, secret), secret, time.UnixMilli(3*60*1000)); err == nil {
		t.Fatalf("ParseVerifiedWebhook accepted stale timestamp")
	}
}

func TestParseVerifiedWebhookReturnsUnsupportedType(t *testing.T) {
	t.Parallel()

	secret := "secret"
	now := time.Now()

	body, err := json.Marshal(map[string]any{
		"type":             "Issue",
		"action":           "create",
		"webhookId":        "wh_1",
		"webhookTimestamp": float64(now.UnixMilli()),
	})
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}

	_, err = ParseVerifiedWebhook(body, SignWebhook(body, secret), secret, now)
	if !IsUnsupportedWebhook(err) {
		t.Fatalf("ParseVerifiedWebhook error = %v, want unsupported webhook", err)
	}
}

func TestParseVerifiedWebhookAcceptsAssignmentNotification(t *testing.T) {
	t.Parallel()

	secret := "secret"
	now := time.Now()

	body, err := json.Marshal(AgentSessionEventWebhookPayload{
		Type:             "AppUserNotification",
		Action:           "issueAssignedToYou",
		WebhookID:        "wh_1",
		WebhookTimestamp: float64(now.UnixMilli()),
		Notification: &NotificationWebhook{
			ID:      "notif_1",
			Type:    "issueAssignedToYou",
			IssueID: "issue_1",
			Issue: &IssueWebhook{
				ID:         "issue_1",
				Identifier: "BAS-12",
				Title:      "Implement Linear integration",
				TeamID:     "team_1",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}

	payload, err := ParseVerifiedWebhook(body, SignWebhook(body, secret), secret, now)
	if err != nil {
		t.Fatalf("parse verified webhook: %v", err)
	}

	if payload.Notification == nil || payload.Notification.IssueID != "issue_1" {
		t.Fatalf("notification = %#v, want issue notification", payload.Notification)
	}
}

func TestParseVerifiedWebhookIgnoresUnsupportedNotificationAction(t *testing.T) {
	t.Parallel()

	secret := "secret"
	now := time.Now()

	body, err := json.Marshal(map[string]any{
		"type":             "AppUserNotification",
		"action":           "issueSubscribed",
		"webhookId":        "wh_1",
		"webhookTimestamp": float64(now.UnixMilli()),
		"notification": map[string]any{
			"id":      "notif_1",
			"type":    "issueSubscribed",
			"issueId": "issue_1",
		},
	})
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}

	_, err = ParseVerifiedWebhook(body, SignWebhook(body, secret), secret, now)
	if !IsUnsupportedWebhook(err) {
		t.Fatalf("ParseVerifiedWebhook error = %v, want unsupported webhook", err)
	}
}

func TestPromptBody(t *testing.T) {
	t.Parallel()

	body := PromptBody(&AgentActivityWebhook{Content: []byte(`{"type":"prompt","body":" continue "}`)})
	if body != "continue" {
		t.Fatalf("PromptBody = %q, want continue", body)
	}
}
