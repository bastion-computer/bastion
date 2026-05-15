package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/api"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func TestListRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	templateOne := createTemplate(t, router, "template-list-1")
	templateTwo := createTemplate(t, router, "template-list-2")

	createEnvironment(t, router, templateOne.Key)
	createEnvironment(t, router, templateTwo.Key)

	assertList[template.Metadata](t, router, "/v1/templates", 2)
	assertList[environment.Environment](t, router, "/v1/environments", 2)
}

func TestGetRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	templateByID := createTemplate(t, router, "template-get-id")
	assertGet(t, router, "/v1/templates/"+templateByID.ID, templateByID.ID)

	templateByKey := createTemplate(t, router, "template-get-key")
	assertGet(t, router, "/v1/templates/by-key/"+templateByKey.Key, templateByKey.ID)

	env := createEnvironment(t, router, templateByID.Key)
	assertGet(t, router, "/v1/environments/"+env.ID, env.ID)
}

func TestDeleteRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	templateByID := createTemplate(t, router, "template-delete-id")
	assertDelete(t, router, "/v1/templates/"+templateByID.ID)

	templateByKey := createTemplate(t, router, "template-delete-key")
	assertDelete(t, router, "/v1/templates/by-key/"+templateByKey.Key)

	templateForEnv := createTemplate(t, router, "environment-delete-source")
	env := createEnvironment(t, router, templateForEnv.Key)
	assertDelete(t, router, "/v1/environments/"+env.ID)
}

func newTestRouter(t *testing.T, logger *slog.Logger) http.Handler {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return api.NewRouter(db, logger)
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

func createEnvironment(t *testing.T, handler http.Handler, templateKey string) environment.Environment {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/environments", environment.CreateRequest{TemplateKey: templateKey})
	if res.Code != http.StatusOK {
		t.Fatalf("create environment status = %d, want %d", res.Code, http.StatusOK)
	}

	var created environment.Environment
	decode(t, res, &created)

	if created.Status != "pending" {
		t.Fatalf("created environment status = %q, want pending", created.Status)
	}

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

	var page services.Page[T]
	decode(t, res, &page)

	if len(page.Entries) != entries {
		t.Fatalf("list %s entries = %d, want %d", path, len(page.Entries), entries)
	}
}

func assertGet(t *testing.T, handler http.Handler, path, wantID string) {
	t.Helper()

	res := request(t, handler, http.MethodGet, path, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("get %s status = %d, want %d", path, res.Code, http.StatusOK)
	}

	var value struct {
		ID string `json:"id"`
	}
	decode(t, res, &value)

	if value.ID != wantID {
		t.Fatalf("get %s id = %q, want %q", path, value.ID, wantID)
	}
}

func request(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	return res
}

func decode(t *testing.T, res *httptest.ResponseRecorder, value any) {
	t.Helper()

	if err := json.NewDecoder(res.Body).Decode(value); err != nil {
		t.Fatalf("decode response %q: %v", res.Body.String(), err)
	}
}

func TestHealthRoute(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))
	res := request(t, router, http.MethodGet, "/v1/health", nil)

	if res.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", res.Code, http.StatusOK)
	}

	var body struct {
		Status string `json:"status"`
	}
	decode(t, res, &body)

	if body.Status != "ok" {
		t.Fatalf("health status body = %q, want ok", body.Status)
	}
}
