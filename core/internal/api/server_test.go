package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/api"
	"github.com/bastion-computer/bastion/core/internal/checkpoint"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/page"
	"github.com/bastion-computer/bastion/core/internal/sandbox"
	"github.com/bastion-computer/bastion/core/internal/secret"
	"github.com/bastion-computer/bastion/core/internal/template"
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

	res := request(t, router, http.MethodPost, "/v1/templates", template.CreateRequest{
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

func TestGetRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)

	secretByID := createSecret(t, router, "API_KEY_GET_ID")
	assertGetSecret(t, router, "/v1/secrets/"+secretByID.ID, secretByID.ID)

	secretByKey := createSecret(t, router, "API_KEY_GET_KEY")
	assertGetSecret(t, router, "/v1/secrets/by-key/"+secretByKey.Key, secretByKey.ID)

	templateByID := createTemplate(t, router, "template-get-id")
	assertGetTemplate(t, router, "/v1/templates/"+templateByID.ID, templateByID.ID)

	templateByKey := createTemplate(t, router, "template-get-key")
	assertGetTemplate(t, router, "/v1/templates/by-key/"+templateByKey.Key, templateByKey.ID)

	sandboxID := createPausedSandbox(t, router)
	assertGetSandbox(t, router, "/v1/sandboxes/"+sandboxID, sandboxID)

	checkpointByID := createCheckpoint(t, router, "checkpoint-get-id", sandboxID)
	assertGetCheckpoint(t, router, "/v1/checkpoints/"+checkpointByID.ID, checkpointByID.ID)

	checkpointByKey := createCheckpoint(t, router, "checkpoint-get-key", sandboxID)
	assertGetCheckpoint(t, router, "/v1/checkpoints/by-key/"+checkpointByKey.Key, checkpointByKey.ID)
}

func TestDeleteRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)

	secretByID := createSecret(t, router, "API_KEY_DELETE_ID")
	assertDelete(t, router, "/v1/secrets/"+secretByID.ID)

	secretByKey := createSecret(t, router, "API_KEY_DELETE_KEY")
	assertDelete(t, router, "/v1/secrets/by-key/"+secretByKey.Key)

	templateByID := createTemplate(t, router, "template-delete-id")
	assertDelete(t, router, "/v1/templates/"+templateByID.ID)

	templateByKey := createTemplate(t, router, "template-delete-key")
	assertDelete(t, router, "/v1/templates/by-key/"+templateByKey.Key)

	sandboxID := createPausedSandbox(t, router)

	checkpointByID := createCheckpoint(t, router, "checkpoint-delete-id", sandboxID)
	assertDelete(t, router, "/v1/checkpoints/"+checkpointByID.ID)

	checkpointByKey := createCheckpoint(t, router, "checkpoint-delete-key", sandboxID)
	assertDelete(t, router, "/v1/checkpoints/by-key/"+checkpointByKey.Key)
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

func createSecret(t *testing.T, handler http.Handler, key string) secret.Secret {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/secrets", secret.CreateRequest{
		Key:        key,
		Env:        key + "_ENV",
		AllowHosts: []string{"*.example.com"},
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create secret status = %d, want %d", res.Code, http.StatusOK)
	}

	var created secret.Secret
	decode(t, res, &created)

	return created
}

func createTemplate(t *testing.T, handler http.Handler, key string) template.Metadata {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/templates", template.CreateRequest{
		Key:    key,
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create template status = %d, want %d", res.Code, http.StatusOK)
	}

	var created template.Metadata
	decode(t, res, &created)

	return created
}

func createPausedSandbox(t *testing.T, handler http.Handler) string {
	t.Helper()

	template := createTemplate(t, handler, "checkpoint-source")

	res := request(t, handler, http.MethodPost, "/v1/sandboxes", sandbox.CreateRequest{From: "template", Key: template.Key})
	if res.Code != http.StatusOK {
		t.Fatalf("create sandbox status = %d, want %d", res.Code, http.StatusOK)
	}

	var created sandbox.Sandbox
	decode(t, res, &created)

	res = request(t, handler, http.MethodPost, "/v1/sandboxes/"+created.ID+"/pause", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("pause sandbox status = %d, want %d", res.Code, http.StatusOK)
	}

	return created.ID
}

func createCheckpoint(t *testing.T, handler http.Handler, key, sandboxID string) checkpoint.Checkpoint {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/checkpoints", checkpoint.CreateRequest{Key: key, SandboxID: sandboxID})
	if res.Code != http.StatusOK {
		t.Fatalf("create checkpoint status = %d, want %d", res.Code, http.StatusOK)
	}

	var created checkpoint.Checkpoint
	decode(t, res, &created)

	return created
}

func assertDelete(t *testing.T, handler http.Handler, path string) {
	t.Helper()

	res := request(t, handler, http.MethodDelete, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("delete %s status = %d, want %d", path, res.Code, http.StatusOK)
	}
}

func assertGetSecret(t *testing.T, handler http.Handler, path, id string) {
	t.Helper()

	res := request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("get secret %s status = %d, want %d", path, res.Code, http.StatusOK)
	}

	var got secret.Secret
	decode(t, res, &got)

	if got.ID != id {
		t.Fatalf("get secret %s id = %q, want %q", path, got.ID, id)
	}
}

func assertGetTemplate(t *testing.T, handler http.Handler, path, id string) {
	t.Helper()

	res := request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("get template %s status = %d, want %d", path, res.Code, http.StatusOK)
	}

	var got template.Template
	decode(t, res, &got)

	if got.ID != id {
		t.Fatalf("get template %s id = %q, want %q", path, got.ID, id)
	}
}

func assertGetSandbox(t *testing.T, handler http.Handler, path, id string) {
	t.Helper()

	res := request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("get sandbox %s status = %d, want %d", path, res.Code, http.StatusOK)
	}

	var got sandbox.Sandbox
	decode(t, res, &got)

	if got.ID != id {
		t.Fatalf("get sandbox %s id = %q, want %q", path, got.ID, id)
	}
}

func assertGetCheckpoint(t *testing.T, handler http.Handler, path, id string) {
	t.Helper()

	res := request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("get checkpoint %s status = %d, want %d", path, res.Code, http.StatusOK)
	}

	var got checkpoint.Checkpoint
	decode(t, res, &got)

	if got.ID != id {
		t.Fatalf("get checkpoint %s id = %q, want %q", path, got.ID, id)
	}
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
