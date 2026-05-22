package template_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func TestServiceCreatesListsGetsAndRemovesTemplate(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := template.NewService(db)
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

func TestServiceAcceptsRunActionTemplateConfig(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := template.NewService(db)
	ctx := context.Background()
	config := json.RawMessage(`{"actions":{"init":[{"run":"echo node setup"},{"run":"echo docker setup"}]}}`)

	created, err := service.Create(ctx, template.CreateRequest{Key: "run-actions", Config: config})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	got, err := service.Get(ctx, created.ID, "")
	if err != nil {
		t.Fatalf("get template: %v", err)
	}

	if string(got.Config) != string(config) {
		t.Fatalf("config = %s, want %s", got.Config, config)
	}
}

func TestServiceRejectsInvalidTemplateConfig(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := template.NewService(db)
	ctx := context.Background()

	cases := []struct {
		name   string
		config json.RawMessage
	}{
		{name: "invalid json", config: json.RawMessage(`{`)},
		{name: "missing actions", config: json.RawMessage(`{}`)},
		{name: "removed delegate commands", config: json.RawMessage(`{"actions":{"init":[]},"delegateCommands":{}}`)},
		{name: "removed network rules", config: json.RawMessage(`{"actions":{"init":[]},"networkRules":{}}`)},
		{name: "removed external action", config: json.RawMessage(`{"actions":{"init":[{"use":"example/action"}]}}`)},
		{name: "removed start action", config: json.RawMessage(`{"actions":{"init":[],"start":[{"run":"echo hi"}]}}`)},
		{name: "invalid action", config: json.RawMessage(`{"actions":{"init":[{"run":"echo hi","use":"example/action"}]}}`)},
		{name: "non string env", config: json.RawMessage(`{"actions":{"init":[]},"env":{"PORT":3000}}`)},
	}

	for i, tc := range cases {
		_, err := service.Create(ctx, template.CreateRequest{
			Key:    fmt.Sprintf("dev-env-%d", i),
			Config: tc.config,
		})
		if !errors.Is(err, failure.ErrInvalid) {
			t.Fatalf("%s: create template error = %v, want invalid", tc.name, err)
		}
	}

	page, err := service.List(ctx, 20, "")
	if err != nil {
		t.Fatalf("list templates: %v", err)
	}

	if len(page.Entries) != 0 {
		t.Fatalf("template count = %d, want 0", len(page.Entries))
	}
}

func TestServiceRejectsRemovingTemplateInUseByEnvironment(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	environments := environment.NewService(db)
	ctx := context.Background()

	created, err := templates.Create(ctx, template.CreateRequest{
		Key:    "dev-env",
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	if _, err := environments.Create(ctx, environment.CreateRequest{TemplateID: created.ID}); err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if _, err := templates.Remove(ctx, created.ID, ""); !errors.Is(err, failure.ErrConflict) {
		t.Fatalf("remove template error = %v, want conflict", err)
	}

	if _, err := templates.Get(ctx, created.ID, ""); err != nil {
		t.Fatalf("get template after rejected remove: %v", err)
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
