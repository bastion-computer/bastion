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

func TestPromptBody(t *testing.T) {
	t.Parallel()

	body := PromptBody(&AgentActivityWebhook{Content: []byte(`{"type":"prompt","body":" continue "}`)})
	if body != "continue" {
		t.Fatalf("PromptBody = %q, want continue", body)
	}
}
