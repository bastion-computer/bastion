package firecracker

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDaemonStateRouteReportsMissingVMStopped(t *testing.T) {
	t.Parallel()

	router := NewRouter(NewManager(t.TempDir(), 0, 0, slog.New(slog.DiscardHandler)), slog.New(slog.DiscardHandler))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/vms/env_missing", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("state status = %d, want %d", res.Code, http.StatusOK)
	}

	var vm VM
	if err := json.NewDecoder(res.Body).Decode(&vm); err != nil {
		t.Fatalf("decode vm: %v", err)
	}

	if vm.EnvironmentID != "env_missing" || vm.State != StateStopped {
		t.Fatalf("vm = %#v, want stopped env_missing", vm)
	}
}
