package checkpoint_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services/checkpoint"
	"github.com/bastion-computer/bastion/core/internal/services/sandbox"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func TestServiceCreatesListsGetsAndRemovesCheckpoint(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	sandboxes := sandbox.NewService(db)
	service := checkpoint.NewService(db)
	ctx := context.Background()

	createdSandbox := createCheckpointSandbox(ctx, t, templates, sandboxes)
	assertCheckpointBeforePauseInvalid(ctx, t, service, createdSandbox.ID)
	pauseCheckpointSandbox(ctx, t, sandboxes, createdSandbox.ID)
	created := createCheckpoint(ctx, t, service, createdSandbox.ID)
	assertCheckpointList(ctx, t, service)
	assertCheckpointGet(ctx, t, service, created.ID)
	assertCheckpointRemove(ctx, t, service, created.ID)
}

func createCheckpointSandbox(ctx context.Context, t *testing.T, templates *template.Service, sandboxes *sandbox.Service) sandbox.Sandbox {
	t.Helper()

	if _, err := templates.Create(ctx, template.CreateRequest{
		Key:    "dev-env",
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}

	createdSandbox, err := sandboxes.Create(ctx, sandbox.CreateRequest{From: "template", Key: "dev-env"})
	if err != nil {
		t.Fatalf("create sandbox: %v", err)
	}

	return createdSandbox
}

func assertCheckpointBeforePauseInvalid(ctx context.Context, t *testing.T, service *checkpoint.Service, sandboxID string) {
	t.Helper()

	if _, err := service.Create(ctx, checkpoint.CreateRequest{Key: "before-pause", SandboxID: sandboxID}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("create checkpoint before pause error = %v, want invalid", err)
	}
}

func pauseCheckpointSandbox(ctx context.Context, t *testing.T, sandboxes *sandbox.Service, sandboxID string) {
	t.Helper()

	if _, err := sandboxes.Pause(ctx, sandboxID); err != nil {
		t.Fatalf("pause sandbox: %v", err)
	}
}

func createCheckpoint(ctx context.Context, t *testing.T, service *checkpoint.Service, sandboxID string) checkpoint.Checkpoint {
	t.Helper()

	created, err := service.Create(ctx, checkpoint.CreateRequest{Key: "base", SandboxID: sandboxID})
	if err != nil {
		t.Fatalf("create checkpoint: %v", err)
	}

	if created.ID == "" || created.Key != "base" || created.Source.ID != sandboxID {
		t.Fatalf("unexpected created checkpoint: %#v", created)
	}

	return created
}

func assertCheckpointList(ctx context.Context, t *testing.T, service *checkpoint.Service) {
	t.Helper()

	page, err := service.List(ctx, 20, "")
	if err != nil {
		t.Fatalf("list checkpoints: %v", err)
	}

	if len(page.Entries) != 1 || page.Cursor != nil {
		t.Fatalf("unexpected checkpoints page: %#v", page)
	}
}

func assertCheckpointGet(ctx context.Context, t *testing.T, service *checkpoint.Service, wantID string) {
	t.Helper()

	got, err := service.Get(ctx, "", "base")
	if err != nil {
		t.Fatalf("get checkpoint: %v", err)
	}

	if got.ID != wantID || got.Source.Type != "sandbox" {
		t.Fatalf("unexpected checkpoint: %#v", got)
	}
}

func assertCheckpointRemove(ctx context.Context, t *testing.T, service *checkpoint.Service, id string) {
	t.Helper()

	removed, err := service.Remove(ctx, id, "")
	if err != nil {
		t.Fatalf("remove checkpoint: %v", err)
	}

	if removed.ID != id {
		t.Fatalf("removed checkpoint id = %q, want %q", removed.ID, id)
	}

	if _, err := service.Get(ctx, id, ""); !errors.Is(err, failure.ErrNotFound) {
		t.Fatalf("get removed checkpoint error = %v, want not found", err)
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
