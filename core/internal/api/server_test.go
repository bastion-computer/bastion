package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/api"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/page"
	"github.com/bastion-computer/bastion/core/internal/sandbox"
	"github.com/bastion-computer/bastion/core/internal/secret"
	templatepkg "github.com/bastion-computer/bastion/core/internal/template"
)

func TestSecretsRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)

	res := request(t, router, http.MethodPost, "/v1/secrets", secret.CreateRequest{
		Key:        "API_KEY",
		Env:        "BASTION_API_KEY",
		AllowHosts: []string{"*.example.com"},
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create secret status = %d, want %d", res.Code, http.StatusOK)
	}

	var secretValue secret.Secret
	decode(t, res, &secretValue)

	if secretValue.ID == "" || secretValue.Key != "API_KEY" {
		t.Fatalf("unexpected secret response: %#v", secretValue)
	}

	res = request(t, router, http.MethodGet, "/v1/secrets", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list secrets status = %d, want %d", res.Code, http.StatusOK)
	}

	var secretsPage page.Page[secret.Secret]
	decode(t, res, &secretsPage)

	if len(secretsPage.Entries) != 1 {
		t.Fatalf("list secrets entries = %d, want 1", len(secretsPage.Entries))
	}
}

func TestSandboxRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)

	res := request(t, router, http.MethodPost, "/v1/templates", templatepkg.CreateRequest{
		Key:    "dev-env",
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create template status = %d, want %d", res.Code, http.StatusOK)
	}

	res = request(t, router, http.MethodPost, "/v1/sandboxes", sandbox.CreateRequest{From: "template", Key: "dev-env"})
	if res.Code != http.StatusOK {
		t.Fatalf("create sandbox status = %d, want %d", res.Code, http.StatusOK)
	}

	var sandboxValue sandbox.Sandbox
	decode(t, res, &sandboxValue)

	if sandboxValue.Status != "pending" {
		t.Fatalf("sandbox status = %q, want pending", sandboxValue.Status)
	}

	res = request(t, router, http.MethodPost, "/v1/sandboxes/"+sandboxValue.ID+"/pause", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("pause sandbox status = %d, want %d", res.Code, http.StatusOK)
	}
}

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return api.NewRouter(db)
}

func request(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, &payload)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	return res
}

func decode(t *testing.T, res *httptest.ResponseRecorder, dst any) {
	t.Helper()

	if err := json.NewDecoder(res.Body).Decode(dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
