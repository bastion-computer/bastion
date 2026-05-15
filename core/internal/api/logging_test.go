package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterLogsStructuredRequest(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	router := newTestRouter(t, testLogger(&logs))

	res := request(t, router, http.MethodGet, "/v1/health", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", res.Code, http.StatusOK)
	}

	entry := decodeLogEntry(t, &logs)
	if got := logString(t, entry, "msg"); got != "request" {
		t.Fatalf("log msg = %q, want request", got)
	}

	if got := logString(t, entry, "level"); got != "INFO" {
		t.Fatalf("log level = %q, want INFO", got)
	}

	if got := logString(t, entry, "method"); got != http.MethodGet {
		t.Fatalf("log method = %q, want %s", got, http.MethodGet)
	}

	if got := logString(t, entry, "route"); got != "/v1/health" {
		t.Fatalf("log route = %q, want /v1/health", got)
	}

	if got := logStatus(t, entry); got != http.StatusOK {
		t.Fatalf("log status = %d, want %d", got, http.StatusOK)
	}

	requestID := res.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("missing X-Request-ID response header")
	}

	if got := logString(t, entry, "request_id"); got != requestID {
		t.Fatalf("log request_id = %q, want %q", got, requestID)
	}

	if _, ok := entry["path"]; ok {
		t.Fatal("log entry included raw path")
	}

	if _, ok := entry["query"]; ok {
		t.Fatal("log entry included raw query")
	}
}

func TestRouterPropagatesRequestID(t *testing.T) {
	t.Parallel()

	const requestID = "test-request-id"

	var logs bytes.Buffer

	router := newTestRouter(t, testLogger(&logs))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/health", nil)
	res := httptest.NewRecorder()

	req.Header.Set("X-Request-ID", requestID)

	router.ServeHTTP(res, req)

	if got := res.Header().Get("X-Request-ID"); got != requestID {
		t.Fatalf("X-Request-ID response header = %q, want %q", got, requestID)
	}

	entry := decodeLogEntry(t, &logs)
	if got := logString(t, entry, "request_id"); got != requestID {
		t.Fatalf("log request_id = %q, want %q", got, requestID)
	}
}

func TestRouterLogsInvalidJSONAtWarnLevel(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	router := newTestRouter(t, testLogger(&logs))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/templates", bytes.NewBufferString("{"))
	res := httptest.NewRecorder()

	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("create template status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	entry := decodeLogEntry(t, &logs)
	if got := logString(t, entry, "level"); got != "WARN" {
		t.Fatalf("log level = %q, want WARN", got)
	}

	if got := logStatus(t, entry); got != http.StatusBadRequest {
		t.Fatalf("log status = %d, want %d", got, http.StatusBadRequest)
	}

	if got := logString(t, entry, "route"); got != "/v1/templates" {
		t.Fatalf("log route = %q, want /v1/templates", got)
	}

	if got := logString(t, entry, "error"); got == "" {
		t.Fatal("log error is empty")
	}
}

func testLogger(w *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, nil))
}

func decodeLogEntry(t *testing.T, logs *bytes.Buffer) map[string]any {
	t.Helper()

	var entry map[string]any
	if err := json.NewDecoder(logs).Decode(&entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}

	return entry
}

func logString(t *testing.T, entry map[string]any, key string) string {
	t.Helper()

	value, ok := entry[key].(string)
	if !ok {
		t.Fatalf("log %s = %#v, want string", key, entry[key])
	}

	return value
}

func logStatus(t *testing.T, entry map[string]any) int {
	t.Helper()

	value, ok := entry["status"].(float64)
	if !ok {
		t.Fatalf("log status = %#v, want number", entry["status"])
	}

	return int(value)
}
