package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/api"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/checkpoint"
	"github.com/bastion-computer/bastion/core/internal/services/sandbox"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func TestListRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)

	createSecret(t, router, "API_KEY_LIST_1")
	createSecret(t, router, "API_KEY_LIST_2")

	templateOne := createTemplate(t, router, "template-list-1")
	templateTwo := createTemplate(t, router, "template-list-2")

	sandboxOne := createSandbox(t, router, templateOne.Key)
	sandboxTwo := createSandbox(t, router, templateTwo.Key)
	pauseSandbox(t, router, sandboxOne.ID)
	pauseSandbox(t, router, sandboxTwo.ID)

	createCheckpoint(t, router, "checkpoint-list-1", sandboxOne.ID)
	createCheckpoint(t, router, "checkpoint-list-2", sandboxTwo.ID)

	assertList[secret.Secret](t, router, "/v1/secrets", 2)
	assertList[template.Metadata](t, router, "/v1/templates", 2)
	assertList[sandbox.Sandbox](t, router, "/v1/sandboxes", 2)
	assertList[checkpoint.Checkpoint](t, router, "/v1/checkpoints", 2)
}

func TestGetRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t)

	secretByID := createSecret(t, router, "API_KEY_GET_ID")
	assertGet[secret.Secret](t, router, "/v1/secrets/"+secretByID.ID, secretByID.ID)

	secretByKey := createSecret(t, router, "API_KEY_GET_KEY")
	assertGet[secret.Secret](t, router, "/v1/secrets/by-key/"+secretByKey.Key, secretByKey.ID)

	templateByID := createTemplate(t, router, "template-get-id")
	assertGet[template.Template](t, router, "/v1/templates/"+templateByID.ID, templateByID.ID)

	templateByKey := createTemplate(t, router, "template-get-key")
	assertGet[template.Template](t, router, "/v1/templates/by-key/"+templateByKey.Key, templateByKey.ID)

	sandboxTemplate := createTemplate(t, router, "sandbox-get-source")
	sandboxValue := createSandbox(t, router, sandboxTemplate.Key)
	pauseSandbox(t, router, sandboxValue.ID)
	assertGet[sandbox.Sandbox](t, router, "/v1/sandboxes/"+sandboxValue.ID, sandboxValue.ID)

	checkpointByID := createCheckpoint(t, router, "checkpoint-get-id", sandboxValue.ID)
	assertGet[checkpoint.Checkpoint](t, router, "/v1/checkpoints/"+checkpointByID.ID, checkpointByID.ID)

	checkpointByKey := createCheckpoint(t, router, "checkpoint-get-key", sandboxValue.ID)
	assertGet[checkpoint.Checkpoint](t, router, "/v1/checkpoints/by-key/"+checkpointByKey.Key, checkpointByKey.ID)
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

	sandboxTemplate := createTemplate(t, router, "sandbox-delete-source")
	sandboxValue := createSandbox(t, router, sandboxTemplate.Key)
	pauseSandbox(t, router, sandboxValue.ID)

	checkpointByID := createCheckpoint(t, router, "checkpoint-delete-id", sandboxValue.ID)
	assertDelete(t, router, "/v1/checkpoints/"+checkpointByID.ID)

	checkpointByKey := createCheckpoint(t, router, "checkpoint-delete-key", sandboxValue.ID)
	assertDelete(t, router, "/v1/checkpoints/by-key/"+checkpointByKey.Key)

	assertDelete(t, router, "/v1/sandboxes/"+sandboxValue.ID)
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

func createSandbox(t *testing.T, handler http.Handler, templateKey string) sandbox.Sandbox {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/sandboxes", sandbox.CreateRequest{From: "template", Key: templateKey})
	if res.Code != http.StatusOK {
		t.Fatalf("create sandbox status = %d, want %d", res.Code, http.StatusOK)
	}

	var created sandbox.Sandbox
	decode(t, res, &created)

	if created.Status != "pending" {
		t.Fatalf("created sandbox status = %q, want pending", created.Status)
	}

	return created
}

func pauseSandbox(t *testing.T, handler http.Handler, sandboxID string) {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/sandboxes/"+sandboxID+"/pause", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("pause sandbox status = %d, want %d", res.Code, http.StatusOK)
	}

	var paused sandbox.Sandbox
	decode(t, res, &paused)

	if paused.Status != "paused" {
		t.Fatalf("paused sandbox status = %q, want paused", paused.Status)
	}
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

	res = request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("get deleted %s status = %d, want %d", path, res.Code, http.StatusNotFound)
	}
}

func assertList[T any](t *testing.T, handler http.Handler, path string, entries int) {
	t.Helper()

	res := request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list %s status = %d, want %d", path, res.Code, http.StatusOK)
	}

	var got services.Page[T]
	decode(t, res, &got)

	if len(got.Entries) != entries {
		t.Fatalf("list %s entries = %d, want %d", path, len(got.Entries), entries)
	}
}

func assertGet[T any](t *testing.T, handler http.Handler, path, id string) {
	t.Helper()

	res := request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("get %s status = %d, want %d", path, res.Code, http.StatusOK)
	}

	var got T
	decode(t, res, &got)

	fields := reflect.ValueOf(got)
	if fields.Kind() == reflect.Pointer {
		if fields.IsNil() {
			t.Fatalf("get %s response ID: %v", path, fmt.Errorf("response type %T is nil", got))
		}

		fields = fields.Elem()
	}

	if fields.Kind() != reflect.Struct {
		t.Fatalf("get %s response ID: %v", path, fmt.Errorf("response type %T is not a struct", got))
	}

	field := fields.FieldByName("ID")
	if !field.IsValid() || field.Kind() != reflect.String {
		t.Fatalf("get %s response ID: %v", path, fmt.Errorf("response type %T has no string ID field", got))
	}

	if gotID := field.String(); gotID != id {
		t.Fatalf("get %s id = %q, want %q", path, gotID, id)
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
