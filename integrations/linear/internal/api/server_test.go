package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
)

func TestLinearWebhookIgnoresUnsupportedSignedWebhook(t *testing.T) {
	t.Parallel()

	secret := "secret"

	body, err := json.Marshal(map[string]any{
		"type":             "Issue",
		"action":           "create",
		"webhookId":        "wh_1",
		"webhookTimestamp": float64(time.Now().UnixMilli()),
	})
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/webhooks/linear", bytes.NewReader(body))
	req.Header.Set(linear.SignatureHeader(), linear.SignWebhook(body, secret))

	res := httptest.NewRecorder()

	server := NewServer("", secret, nil, slog.New(slog.DiscardHandler))
	server.linearWebhook(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
}
