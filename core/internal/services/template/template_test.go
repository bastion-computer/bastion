package template_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func TestServiceCreatesListsGetsAndRemovesTemplate(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := template.New(db)
	ctx := context.Background()

	created, err := service.Create(ctx, template.CreateRequest{
		Key:    "dev-env",
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	if created.ID == "" || created.Key != "dev-env" {
		t.Fatalf("unexpected created template: %#v", created)
	}

	page, err := service.List(ctx, 20, "")
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}

	if len(page.Entries) != 1 || page.Cursor != nil {
		t.Fatalf("unexpected templates page: %#v", page)
	}

	got, err := service.Get(ctx, "", "dev-env")
	if err != nil {
		t.Fatalf("get template: %v", err)
	}

	if got.ID != created.ID || string(got.Config) != `{"actions":{"init":[]}}` {
		t.Fatalf("unexpected template: %#v", got)
	}

	removed, err := service.Remove(ctx, created.ID, "")
	if err != nil {
		t.Fatalf("remove template: %v", err)
	}

	if removed.ID != created.ID {
		t.Fatalf("removed template id = %q, want %q", removed.ID, created.ID)
	}

	if _, err := service.Get(ctx, created.ID, ""); !errors.Is(err, failure.ErrNotFound) {
		t.Fatalf("get removed template error = %v, want not found", err)
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
