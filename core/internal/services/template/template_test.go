package template_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		Key:    new("dev-env"),
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	if created.ID == "" {
		t.Fatalf("unexpected created template: %#v", created)
	}

	requireTemplateKey(t, created.Key, "dev-env")

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

	requireTemplateKey(t, got.Key, "dev-env")

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

func TestServiceCreatesTemplatesWithOptionalKeys(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := template.NewService(db)
	ctx := context.Background()
	config := json.RawMessage(`{"actions":{"init":[]}}`)

	keyed, err := service.Create(ctx, template.CreateRequest{Key: new("keyed-template"), Config: config})
	if err != nil {
		t.Fatalf("create keyed template: %v", err)
	}

	requireTemplateKey(t, keyed.Key, "keyed-template")

	unkeyedOne, err := service.Create(ctx, template.CreateRequest{Config: config})
	if err != nil {
		t.Fatalf("create first unkeyed template: %v", err)
	}

	unkeyedTwo, err := service.Create(ctx, template.CreateRequest{Config: config})
	if err != nil {
		t.Fatalf("create second unkeyed template: %v", err)
	}

	requireNoTemplateKey(t, unkeyedOne.Key)
	requireNoTemplateKey(t, unkeyedTwo.Key)

	got, err := service.Get(ctx, unkeyedOne.ID, "")
	if err != nil {
		t.Fatalf("get unkeyed template: %v", err)
	}

	requireNoTemplateKey(t, got.Key)

	encoded, err := json.Marshal(got.Metadata())
	if err != nil {
		t.Fatalf("marshal unkeyed template metadata: %v", err)
	}

	requireNoKeyJSON(t, encoded, "unkeyed template metadata")
}

func TestServiceRejectsDuplicateAndBlankTemplateKeys(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := template.NewService(db)
	ctx := context.Background()
	config := json.RawMessage(`{"actions":{"init":[]}}`)

	if _, err := service.Create(ctx, template.CreateRequest{Key: new("keyed-template"), Config: config}); err != nil {
		t.Fatalf("create keyed template: %v", err)
	}

	if _, err := service.Create(ctx, template.CreateRequest{Key: new("keyed-template"), Config: config}); !errors.Is(err, failure.ErrConflict) {
		t.Fatalf("create duplicate keyed template error = %v, want conflict", err)
	}

	if _, err := service.Create(ctx, template.CreateRequest{Key: new(""), Config: config}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("create blank-key template error = %v, want invalid", err)
	}
}

func TestServiceAcceptsActionTemplateConfigs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key    string
		config json.RawMessage
	}{
		{key: "run-actions", config: json.RawMessage(`{"actions":{"init":[{"run":"echo node setup"},{"run":"echo docker setup"}]}}`)},
		{key: "working-directory-run-action", config: json.RawMessage(`{"actions":{"init":[{"run":"pwd","working_directory":"/workspace/project"}]}}`)},
		{key: "preset-actions", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"version":24}}]}}`)},
		{key: "resources", config: json.RawMessage(`{"resources":{"vcpu":3,"memory":4,"volume":5},"actions":{"init":[]}}`)},
		{key: "mise-preset-action", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_mise","with":{"version":"v2025.12.0"}}]}}`)},
		{key: "github-cli-preset-action", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_github_cli","with":{"token":"test-token","hostname":"github.com","git_protocol":"https"}}]}}`)},
		{key: "opencode-preset-action", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_opencode","with":{"provider":"anthropic","model":"anthropic/claude-sonnet-4-5","api_key":"test-key","share":"disabled","permission":"allow"}}]}}`)},
		{key: "default-ssh-directory-preset-action", config: json.RawMessage(`{"actions":{"init":[{"use":"set_default_ssh_directory","with":{"path":"/workspace/bastion"}}]}}`)},
		{key: "queue-function", config: json.RawMessage(`{"functions":{"linear_task":{"trigger":{"type":"queue","key":"linear-task-queue"},"with":{"apiKey":"test-key","retries":3,"dryRun":true}}},"actions":{"init":[]}}`)},
	}

	for _, tc := range cases {
		db := openDB(t)
		service := template.NewService(db)
		ctx := context.Background()

		created, err := service.Create(ctx, template.CreateRequest{Key: new(tc.key), Config: tc.config})
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
		{name: "empty working directory", config: json.RawMessage(`{"actions":{"init":[{"run":"pwd","working_directory":""}]}}`)},
		{name: "working directory on preset action", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_node","working_directory":"/workspace"}]}}`)},
		{name: "invalid with input name", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"node-version":24}}]}}`)},
		{name: "invalid with input value", config: json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"version":{}}}]}}`)},
		{name: "unknown top-level property", config: json.RawMessage(`{"actions":{"init":[]},"legacy":{}}`)},
		{name: "non integer vcpu", config: json.RawMessage(`{"resources":{"vcpu":1.5},"actions":{"init":[]}}`)},
		{name: "invalid function name", config: json.RawMessage(`{"functions":{"linear/task":{"trigger":{"type":"queue","key":"jobs"}}},"actions":{"init":[]}}`)},
		{name: "invalid function trigger type", config: json.RawMessage(`{"functions":{"linear_task":{"trigger":{"type":"schedule","key":"jobs"}}},"actions":{"init":[]}}`)},
		{name: "function trigger id and key", config: json.RawMessage(`{"functions":{"linear_task":{"trigger":{"type":"queue","id":"que_test","key":"jobs"}}},"actions":{"init":[]}}`)},
		{name: "function trigger missing id key", config: json.RawMessage(`{"functions":{"linear_task":{"trigger":{"type":"queue"}}},"actions":{"init":[]}}`)},
	}

	for i, tc := range cases {
		_, err := service.Create(ctx, template.CreateRequest{
			Key:    new(fmt.Sprintf("dev-env-%d", i)),
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
		Key:    new("dev-env"),
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

func requireTemplateKey(t *testing.T, got *string, want string) {
	t.Helper()

	if got == nil || *got != want {
		t.Fatalf("template key = %#v, want %q", got, want)
	}
}

func requireNoTemplateKey(t *testing.T, got *string) {
	t.Helper()

	if got != nil {
		t.Fatalf("template key = %#v, want nil", got)
	}
}

func requireNoKeyJSON(t *testing.T, encoded []byte, label string) {
	t.Helper()

	if strings.Contains(string(encoded), `"key"`) {
		t.Fatalf("%s JSON = %s, want omitted key", label, encoded)
	}
}
