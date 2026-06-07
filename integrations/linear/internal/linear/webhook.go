//nolint:wsl_v5 // Webhook verification is a linear validation pipeline.
package linear

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const signatureHeader = "Linear-Signature"

// SignatureHeader is the HTTP header used by Linear webhook signatures.
func SignatureHeader() string { return signatureHeader }

// UnsupportedWebhookError is returned for signed Linear webhooks this integration does not process.
type UnsupportedWebhookError struct {
	Type   string
	Action string
}

func (e UnsupportedWebhookError) Error() string {
	if e.Action == "" {
		return fmt.Sprintf("unsupported Linear webhook type %q", e.Type)
	}

	return fmt.Sprintf("unsupported Linear webhook type %q action %q", e.Type, e.Action)
}

// IsUnsupportedWebhook reports whether err describes a signed but unsupported Linear webhook.
func IsUnsupportedWebhook(err error) bool {
	var unsupported UnsupportedWebhookError
	return errors.As(err, &unsupported)
}

// ParseVerifiedWebhook verifies and decodes a Linear webhook payload.
func ParseVerifiedWebhook(body []byte, signature, secret string, now time.Time) (AgentSessionEventWebhookPayload, error) {
	if strings.TrimSpace(secret) == "" {
		return AgentSessionEventWebhookPayload{}, errors.New("webhook secret is required")
	}

	if !VerifySignature(body, signature, secret) {
		return AgentSessionEventWebhookPayload{}, errors.New("invalid Linear webhook signature")
	}

	var payload AgentSessionEventWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return AgentSessionEventWebhookPayload{}, fmt.Errorf("decode Linear webhook: %w", err)
	}

	if payload.WebhookID == "" {
		return AgentSessionEventWebhookPayload{}, errors.New("linear webhook missing webhookId")
	}

	if math.Abs(float64(now.UnixMilli())-payload.WebhookTimestamp) > float64(time.Minute/time.Millisecond) {
		return AgentSessionEventWebhookPayload{}, errors.New("linear webhook timestamp is stale")
	}

	switch payload.Type {
	case "", "AgentSessionEvent":
		if payload.AgentSession.ID == "" {
			return AgentSessionEventWebhookPayload{}, errors.New("linear webhook missing agentSession.id")
		}
	case "AppUserNotification":
		if payload.Action != "issueAssignedToYou" && payload.Action != "issueUnassignedFromYou" {
			return AgentSessionEventWebhookPayload{}, UnsupportedWebhookError{Type: payload.Type, Action: payload.Action}
		}
		if payload.Notification == nil {
			return AgentSessionEventWebhookPayload{}, errors.New("linear webhook missing notification")
		}
		if payload.Notification.IssueID == "" && (payload.Notification.Issue == nil || payload.Notification.Issue.ID == "") {
			return AgentSessionEventWebhookPayload{}, errors.New("linear webhook missing notification issue")
		}
	default:
		return AgentSessionEventWebhookPayload{}, UnsupportedWebhookError{Type: payload.Type, Action: payload.Action}
	}

	return payload, nil
}

// VerifySignature checks Linear's HMAC-SHA256 signature over the raw body.
func VerifySignature(body []byte, signature, secret string) bool {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return false
	}

	got, err := hex.DecodeString(signature)
	if err != nil || len(got) != sha256.Size {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	want := mac.Sum(nil)

	return subtle.ConstantTimeCompare(got, want) == 1
}

// SignWebhook returns the Linear signature for tests and mock servers.
func SignWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// PromptBody extracts a prompted activity body if one is present.
func PromptBody(activity *AgentActivityWebhook) string {
	if activity == nil || len(activity.Content) == 0 {
		return ""
	}

	var content struct {
		Body string `json:"body"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(activity.Content, &content); err != nil {
		return ""
	}

	return strings.TrimSpace(content.Body)
}
