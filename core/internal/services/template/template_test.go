package template_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
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
		Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`),
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

	if got.ID != created.ID || !jsonEqual(got.Config, json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)) {
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
	config := json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)

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
	config := json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)

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
		{key: "run-actions", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"echo node setup"},{"run":"echo docker setup"}]}}`)},
		{key: "start-run-actions", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[],"start":[{"run":"echo env setup"},{"run":"echo env ready"}]}}`)},
		{key: "working-directory-run-action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"pwd","working_directory":"/workspace/project"}]}}`)},
		{key: "start-working-directory-run-action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[],"start":[{"run":"pwd","working_directory":"/workspace/project"}]}}`)},
		{key: "preset-actions", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_node","with":{"version":24}}]}}`)},
		{key: "start-preset-actions", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[],"start":[{"use":"setup_node","with":{"version":24}}]}}`)},
		{key: "resources", config: json.RawMessage(`{"agents":{"opencode":{}},"resources":{"vcpu":3,"memory":4,"volume":5},"actions":{"init":[]}}`)},
		{key: "tunnel", config: json.RawMessage(`{"agents":{"opencode":{}},"tunnel":{"frontend":3000,"backend":3001},"actions":{"init":[]}}`)},
		{key: "opencode-agent", config: json.RawMessage(`{"agents":{"opencode":{"working_directory":"/workspace/project","auth":{"anthropic":{"type":"api","key":"test-key"}},"config":{"model":"anthropic/claude-sonnet-4-5","permission":"allow"}}},"actions":{"init":[]}}`)},
		{key: "bun-preset-action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_bun","with":{"version":"bun-v1.3.3"}}]}}`)},
		{key: "mise-preset-action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_mise","with":{"version":"v2025.12.0"}}]}}`)},
		{key: "github-cli-preset-action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_github_cli","with":{"token":"test-token","hostname":"github.com","git_protocol":"https"}}]}}`)},
		{key: "docker-preset-action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_docker"}]}}`)},
		{key: "default-ssh-directory-preset-action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"set_default_ssh_directory","with":{"path":"/workspace/bastion"}}]}}`)},
		{key: "write-env-file-context", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"write_env_file","with":{"path":"/workspace/bastion"},"context":{"STRING":"value","NUMBER":42,"BOOLEAN":true,"OBJECT":{"nested":"value"},"ARRAY":[1,"two"],"NULL":null}}]}}`)},
		{key: "start-write-env-file-context", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[],"start":[{"use":"write_env_file","with":{"path":"/workspace/bastion"},"context":{"STRING":"value","OBJECT":{"nested":"value"}}}]}}`)},
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

		if !jsonEqual(got.Config, tc.config) {
			t.Fatalf("%s: config = %s, want %s", tc.key, got.Config, tc.config)
		}
	}
}

func TestServicePreparesResolvedTemplateEnvironmentVariables(t *testing.T) {
	const envName = "BASTION_TEMPLATE_SUBSTITUTION_TEST"

	t.Setenv(envName, "substituted-value")

	db := openDB(t)
	orchestrator := &fakeTemplateOrchestrator{}
	service := template.NewService(db, template.WithOrchestrator(orchestrator))
	ctx := context.Background()

	created, err := service.Create(ctx, template.CreateRequest{
		Key:    new("substitution-template"),
		Config: json.RawMessage(`{"agents":{"opencode":{"working_directory":"/workspace/${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }}","auth":{"anthropic":{"type":"api","key":"${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }}"}},"config":{"model":"anthropic/${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }}"}}},"actions":{"init":[{"run":"printf '${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }}'"},{"use":"write_env_file","with":{"path":"/workspace/${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }}"},"context":{"TOKEN":"${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }}","NESTED":{"name":"prefix-${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }}"}}}],"start":[{"run":"printf '${{ env.BASTION_TEMPLATE_SUBSTITUTION_TEST }} again'"}]}}`),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	if len(orchestrator.prepared) != 1 {
		t.Fatalf("prepared templates = %d, want 1", len(orchestrator.prepared))
	}

	requirePreparedSubstitutionConfig(t, orchestrator.prepared[0].Config)

	stored, err := service.Get(ctx, created.ID, "")
	if err != nil {
		t.Fatalf("get stored template: %v", err)
	}

	if !jsonEqual(stored.Config, orchestrator.prepared[0].Config) {
		t.Fatalf("stored config = %s, want prepared config %s", stored.Config, orchestrator.prepared[0].Config)
	}
}

type preparedSubstitutionConfig struct {
	Agents struct {
		OpenCode struct {
			WorkingDirectory string `json:"working_directory"`
			Auth             struct {
				Anthropic struct {
					Key string `json:"key"`
				} `json:"anthropic"`
			} `json:"auth"`
			Config struct {
				Model string `json:"model"`
			} `json:"config"`
		} `json:"opencode"`
	} `json:"agents"`
	Actions struct {
		Init []struct {
			Run  string `json:"run"`
			With struct {
				Path string `json:"path"`
			} `json:"with"`
			Context struct {
				Token  string `json:"TOKEN"`
				Nested struct {
					Name string `json:"name"`
				} `json:"NESTED"`
			} `json:"context"`
		} `json:"init"`
		Start []struct {
			Run string `json:"run"`
		} `json:"start"`
	} `json:"actions"`
}

func requirePreparedSubstitutionConfig(t *testing.T, config json.RawMessage) {
	t.Helper()

	var prepared preparedSubstitutionConfig
	if err := json.Unmarshal(config, &prepared); err != nil {
		t.Fatalf("unmarshal prepared config: %v", err)
	}

	if len(prepared.Actions.Init) != 2 || prepared.Actions.Init[0].Run != "printf 'substituted-value'" {
		t.Fatalf("prepared template config = %s, want substituted env values", config)
	}

	if prepared.Actions.Init[1].With.Path != "/workspace/substituted-value" || prepared.Actions.Init[1].Context.Token != "substituted-value" || prepared.Actions.Init[1].Context.Nested.Name != "prefix-substituted-value" {
		t.Fatalf("prepared template config = %s, want substituted use action context", config)
	}

	if prepared.Agents.OpenCode.WorkingDirectory != "/workspace/substituted-value" || prepared.Agents.OpenCode.Auth.Anthropic.Key != "substituted-value" || prepared.Agents.OpenCode.Config.Model != "anthropic/substituted-value" {
		t.Fatalf("prepared template config = %s, want substituted opencode agent values", config)
	}

	if len(prepared.Actions.Start) != 1 || prepared.Actions.Start[0].Run != "printf 'substituted-value again'" {
		t.Fatalf("prepared template config = %s, want substituted start env values", config)
	}
}

func TestServiceRejectsUnsetTemplateEnvironmentVariableBeforePrepare(t *testing.T) {
	t.Parallel()

	missingName := "BASTION_TEMPLATE_SUBSTITUTION_MISSING_TEST_73D4C05F5B2F4E2FA7D8C7D2"
	db := openDB(t)
	orchestrator := &fakeTemplateOrchestrator{}
	service := template.NewService(db, template.WithOrchestrator(orchestrator))
	ctx := context.Background()

	_, err := service.Create(ctx, template.CreateRequest{
		Key:    new("missing-substitution-template"),
		Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"echo ${{ env.BASTION_TEMPLATE_SUBSTITUTION_MISSING_TEST_73D4C05F5B2F4E2FA7D8C7D2 }}"}]}}`),
	})
	if !errors.Is(err, failure.ErrInvalid) || !strings.Contains(err.Error(), missingName) {
		t.Fatalf("create template error = %v, want invalid missing env var", err)
	}

	if len(orchestrator.prepared) != 0 {
		t.Fatalf("prepared templates = %d, want 0", len(orchestrator.prepared))
	}
}

func TestServiceRemovesPreparedTemplateArtifacts(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	orchestrator := &fakeTemplateOrchestrator{}
	service := template.NewService(db, template.WithOrchestrator(orchestrator))
	ctx := context.Background()

	created, err := service.Create(ctx, template.CreateRequest{Key: new("prepared-template"), Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	if _, err := service.Remove(ctx, created.ID, ""); err != nil {
		t.Fatalf("remove template: %v", err)
	}

	if len(orchestrator.removed) != 1 || orchestrator.removed[0] != created.ID {
		t.Fatalf("removed prepared templates = %#v, want %s", orchestrator.removed, created.ID)
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
		{name: "missing agents", config: json.RawMessage(`{"actions":{"init":[]}}`)},
		{name: "empty agents", config: json.RawMessage(`{"agents":{},"actions":{"init":[]}}`)},
		{name: "unknown agent", config: json.RawMessage(`{"agents":{"other":{}},"actions":{"init":[]}}`)},
		{name: "invalid opencode auth", config: json.RawMessage(`{"agents":{"opencode":{"auth":{"anthropic":{"type":"api"}}}},"actions":{"init":[]}}`)},
		{name: "missing actions", config: json.RawMessage(`{"agents":{"opencode":{}}}`)},
		{name: "schema metadata property", config: json.RawMessage(`{"agents":{"opencode":{}},"$schema":"https://bastion.computer/schemas/template.json","actions":{"init":[]}}`)},
		{name: "removed delegate commands", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]},"delegateCommands":{}}`)},
		{name: "removed network rules", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]},"networkRules":{}}`)},
		{name: "invalid preset action name", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"example/action"}]}}`)},
		{name: "invalid action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"echo hi","use":"example/action"}]}}`)},
		{name: "invalid start action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[],"start":[{"run":"echo hi","use":"example/action"}]}}`)},
		{name: "empty working directory", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"pwd","working_directory":""}]}}`)},
		{name: "working directory on preset action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_node","working_directory":"/workspace"}]}}`)},
		{name: "context on run action", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"echo hi","context":{"VALUE":"test"}}]}}`)},
		{name: "invalid with input name", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_node","with":{"node-version":24}}]}}`)},
		{name: "invalid with input value", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"use":"setup_node","with":{"version":{}}}]}}`)},
		{name: "unknown top-level property", config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]},"legacy":{}}`)},
		{name: "invalid tunnel name", config: json.RawMessage(`{"agents":{"opencode":{}},"tunnel":{"front/end":3000},"actions":{"init":[]}}`)},
		{name: "invalid tunnel port", config: json.RawMessage(`{"agents":{"opencode":{}},"tunnel":{"frontend":70000},"actions":{"init":[]}}`)},
		{name: "non integer vcpu", config: json.RawMessage(`{"agents":{"opencode":{}},"resources":{"vcpu":1.5},"actions":{"init":[]}}`)},
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
		Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`),
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

func jsonEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any

	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}

	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}

	return fmt.Sprintf("%#v", leftValue) == fmt.Sprintf("%#v", rightValue)
}

type fakeTemplateOrchestrator struct {
	prepared []ch.Template
	removed  []string
}

func (o *fakeTemplateOrchestrator) PrepareTemplate(_ context.Context, req ch.PrepareTemplateRequest) (ch.PreparedTemplate, error) {
	o.prepared = append(o.prepared, ch.Template{ID: req.Template.ID, Key: req.Template.Key, Config: append(json.RawMessage(nil), req.Template.Config...)})

	return ch.PreparedTemplate{TemplateID: req.Template.ID}, nil
}

func (o *fakeTemplateOrchestrator) RemoveTemplate(_ context.Context, templateID string) (ch.PreparedTemplate, error) {
	o.removed = append(o.removed, templateID)

	return ch.PreparedTemplate{TemplateID: templateID}, nil
}
