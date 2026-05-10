package sandbox_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/services/sandbox"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func TestServiceCreatesListsPausesExecsAndRemovesSandbox(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	service := sandbox.NewService(db)
	ctx := context.Background()

	created := createSandboxFromTemplate(ctx, t, templates, service)
	assertSandboxList(ctx, t, service)
	assertSandboxPause(ctx, t, service, created.ID)
	assertSandboxExec(ctx, t, service, created.ID)
	assertSandboxRemove(ctx, t, service, created.ID)
}

func createSandboxFromTemplate(ctx context.Context, t *testing.T, templates *template.Service, service *sandbox.Service) sandbox.Sandbox {
	t.Helper()

	if _, err := templates.Create(ctx, template.CreateRequest{
		Key:    "dev-env",
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}

	created, err := service.Create(ctx, sandbox.CreateRequest{From: "template", Key: "dev-env"})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}

	if created.ID == "" || created.Status != "pending" || created.Source.Type != "template" {
		t.Fatalf("unexpected created sandbox: %#v", created)
	}

	return created
}

func assertSandboxList(ctx context.Context, t *testing.T, service *sandbox.Service) {
	t.Helper()

	page, err := service.List(ctx, 20, "")
	if err != nil {
		t.Fatalf("list sandboxes: %v", err)
	}

	if len(page.Entries) != 1 || page.Cursor != nil {
		t.Fatalf("unexpected sandboxes page: %#v", page)
	}
}

func assertSandboxPause(ctx context.Context, t *testing.T, service *sandbox.Service, id string) {
	t.Helper()

	paused, err := service.Pause(ctx, id)
	if err != nil {
		t.Fatalf("pause sandbox: %v", err)
	}

	if paused.Status != "paused" {
		t.Fatalf("paused sandbox status = %q, want paused", paused.Status)
	}
}

func assertSandboxExec(ctx context.Context, t *testing.T, service *sandbox.Service, id string) {
	t.Helper()

	response, err := service.Exec(ctx, id, []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("exec sandbox: %v", err)
	}

	if response.ID != id || response.Status != "not_implemented" || len(response.Command) != 2 {
		t.Fatalf("unexpected exec response: %#v", response)
	}
}

func assertSandboxRemove(ctx context.Context, t *testing.T, service *sandbox.Service, id string) {
	t.Helper()

	removed, err := service.Remove(ctx, id)
	if err != nil {
		t.Fatalf("remove sandbox: %v", err)
	}

	if removed.ID != id {
		t.Fatalf("removed sandbox id = %q, want %q", removed.ID, id)
	}
}

func openDB(t *testing.T) *database.Client {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}
