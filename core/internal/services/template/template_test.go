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

func TestServiceAcceptsActionTemplateConfigs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key    string
		config json.RawMessage
	}{
		{key: "run-actions", config: json.RawMessage(`{"actions":{"init":[{"run":"echo node setup"},{"run":"echo docker setup"}]}}`)},
		{key: "preset-actions", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"version":24}}]}}`)},
		{key: "resources", config: json.RawMessage(`{"resources":{"vcpu":3,"memory":4,"volume":5},"actions":{"init":[]}}`)},
		{key: "mise-preset-action", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_mise","with":{"version":"v2025.12.0"}}]}}`)},
	}

	for _, tc := range cases {
		db := openDB(t)
		service := template.NewService(db)
		ctx := context.Background()

		created, err := service.Create(ctx, template.CreateRequest{Key: tc.key, Config: tc.config})
		if err != nil {
			t.Fatalf("%s: create template: %v", tc.key, err)
		}

		got, err := service.Get(ctx, created.ID, "")
		if err != nil {
			t.Fatalf("%s: get template: %v", tc.key, err)
		}

		if string(got.Config) != string(tc.config) {
			t.Fatalf("%s: config = %s, want %s", tc.key, got.Config, tc.config)
		}
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
		{name: "invalid preset action name", config: json.RawMessage(`{"actions":{"init":[{"use":"example/action"}]}}`)},
		{name: "removed start action", config: json.RawMessage(`{"actions":{"init":[],"start":[{"run":"echo hi"}]}}`)},
		{name: "invalid action", config: json.RawMessage(`{"actions":{"init":[{"run":"echo hi","use":"example/action"}]}}`)},
		{name: "invalid with input name", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"node-version":24}}]}}`)},
		{name: "invalid with input value", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"version":{}}}]}}`)},
		{name: "unknown top-level property", config: json.RawMessage(`{"actions":{"init":[]},"legacy":{}}`)},
		{name: "non integer vcpu", config: json.RawMessage(`{"resources":{"vcpu":1.5},"actions":{"init":[]}}`)},
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
