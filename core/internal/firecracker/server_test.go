package firecracker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

func TestDaemonRespondsFailedDependencyForVMInitFailure(t *testing.T) {
	t.Parallel()

	res := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(res)
	err := initActionError{index: 2, err: errors.New("guest command failed: exit status 42: intentional failure")}

	respondDaemon(c, VM{}, err)

	if res.Code != http.StatusFailedDependency {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusFailedDependency)
	}

	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}

	if body.Error != "init action 2 failed: guest command failed: exit status 42: intentional failure" {
		t.Fatalf("error body = %q, want sanitized init failure", body.Error)
	}
}
