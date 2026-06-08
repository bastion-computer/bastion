package environment_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

const (
	testTemplateKey = "dev-env"
	testGuestIP     = "10.241.0.2"
	testVMTime      = "2026-01-01T00:00:00Z"
	testProdTag     = "prod"
	testGPUTag      = "gpu"
	testCPUTag      = "cpu"
)

func TestServiceCreatesListsGetsAndRemovesEnvironment(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	orchestrator := newFakeOrchestrator()
	service := environment.NewService(db, environment.WithOrchestrator(orchestrator))
	ctx := context.Background()

	created := createEnvironmentFromTemplate(ctx, t, templates, service)
	assertEnvironmentList(ctx, t, service)
	assertEnvironmentGet(ctx, t, service, created)
	assertEnvironmentRemove(ctx, t, service, created.ID)

	if orchestrator.launches != 1 || orchestrator.removes != 1 {
		t.Fatalf("orchestration launches/removes = %d/%d, want 1/1", orchestrator.launches, orchestrator.removes)
	}
}

func TestServiceReconcileRejectsEmptyRuntimeState(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	orchestrator := newFakeOrchestrator()
	service := environment.NewService(db, environment.WithOrchestrator(orchestrator))
	ctx := context.Background()

	created := createEnvironmentFromTemplate(ctx, t, templates, service)
	orchestrator.vms[created.ID] = ch.VM{EnvironmentID: created.ID}

	_, err := service.Get(ctx, created.ID)
	if err == nil || !strings.Contains(err.Error(), "empty runtime state") {
		t.Fatalf("get environment error = %v, want empty runtime state error", err)
	}
}

func TestServicePersistsLaunchVMFailure(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	orchestrator := &failingLaunchOrchestrator{err: errors.New("init action 2 failed")}
	service := environment.NewService(db, environment.WithOrchestrator(orchestrator))
	ctx := context.Background()

	if _, err := templates.Create(ctx, template.CreateRequest{
		Key:    new(testTemplateKey),
		Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"false"}]}}`),
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}

	_, err := service.Create(ctx, environment.CreateRequest{TemplateKey: testTemplateKey})
	if err == nil || !strings.Contains(err.Error(), orchestrator.err.Error()) {
		t.Fatalf("create environment error = %v, want launch failure", err)
	}

	page, err := service.List(ctx, 20, "", nil)
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}

	if len(page.Entries) != 1 {
		t.Fatalf("environment count = %d, want 1", len(page.Entries))
	}

	got := page.Entries[0]
	if got.Status != ch.StateError || got.LastError != orchestrator.err.Error() {
		t.Fatalf("failed environment = %#v, want persisted vm failure", got)
	}
}

func TestServicePreservesTemplateResourcesForOrchestration(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	orchestrator := newFakeOrchestrator()
	service := environment.NewService(db, environment.WithOrchestrator(orchestrator))
	ctx := context.Background()

	if _, err := templates.Create(ctx, template.CreateRequest{
		Key:    new("resource-template"),
		Config: json.RawMessage(`{"agents":{"opencode":{}},"resources":{"vcpu":3,"memory":4,"volume":5},"actions":{"init":[]}}`),
	}); err != nil {
		t.Fatalf("create template: %v", err)
	}

	if _, err := service.Create(ctx, environment.CreateRequest{TemplateKey: "resource-template"}); err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if len(orchestrator.templates) != 1 {
		t.Fatalf("launch templates = %d, want 1", len(orchestrator.templates))
	}

	var launched struct {
		Resources struct {
			VCPU   int `json:"vcpu"`
			Memory int `json:"memory"`
			Volume int `json:"volume"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(orchestrator.templates[0].Config, &launched); err != nil {
		t.Fatalf("unmarshal launched template config: %v", err)
	}

	if launched.Resources.VCPU != 3 || launched.Resources.Memory != 4 || launched.Resources.Volume != 5 {
		t.Fatalf("launched resources = %#v, want template resources", launched.Resources)
	}
}

func TestServiceCreatesAndFiltersEnvironmentTags(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	service := environment.NewService(db, environment.WithOrchestrator(newFakeOrchestrator()))
	ctx := context.Background()

	createdTemplate := createTaggedTestTemplate(ctx, t, templates)
	prodGPU := createTaggedEnvironment(ctx, t, service, requireStringPtr(t, createdTemplate.Key), testProdTag, testGPUTag)
	createTaggedEnvironment(ctx, t, service, requireStringPtr(t, createdTemplate.Key), testProdTag, testCPUTag)

	assertStoredEnvironmentTags(ctx, t, service, prodGPU.ID, testProdTag, testGPUTag)
	assertEnvironmentListCount(ctx, t, service, []string{testProdTag}, 2)
	assertEnvironmentListOnly(ctx, t, service, []string{testProdTag, testGPUTag}, prodGPU)
	assertEnvironmentListCount(ctx, t, service, []string{"missing"}, 0)
}

func TestServiceCreatesAndGetsEnvironmentWithKey(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	service := environment.NewService(db, environment.WithOrchestrator(newFakeOrchestrator()))
	ctx := context.Background()
	createdTemplate := createUnkeyedTestTemplate(ctx, t, templates)

	keyed, err := service.Create(ctx, environment.CreateRequest{Key: new("keyed-environment"), TemplateID: createdTemplate.ID})
	if err != nil {
		t.Fatalf("create keyed environment: %v", err)
	}

	if keyed.Key == nil || *keyed.Key != "keyed-environment" {
		t.Fatalf("keyed environment key = %#v, want keyed-environment", keyed.Key)
	}

	gotByKey, err := service.GetByKey(ctx, "keyed-environment")
	if err != nil {
		t.Fatalf("get keyed environment: %v", err)
	}

	if gotByKey.ID != keyed.ID || gotByKey.Key == nil || *gotByKey.Key != "keyed-environment" {
		t.Fatalf("environment by key = %#v, want %#v", gotByKey, keyed)
	}

	removed, err := service.RemoveByKey(ctx, "keyed-environment")
	if err != nil {
		t.Fatalf("remove keyed environment: %v", err)
	}

	if removed.ID != keyed.ID || removed.Key == nil || *removed.Key != "keyed-environment" {
		t.Fatalf("removed keyed environment = %#v, want %#v", removed, keyed)
	}
}

func TestServiceCreatesEnvironmentsWithoutKeys(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	service := environment.NewService(db, environment.WithOrchestrator(newFakeOrchestrator()))
	ctx := context.Background()
	createdTemplate := createUnkeyedTestTemplate(ctx, t, templates)

	unkeyedOne, err := service.Create(ctx, environment.CreateRequest{TemplateID: createdTemplate.ID})
	if err != nil {
		t.Fatalf("create first unkeyed environment: %v", err)
	}

	unkeyedTwo, err := service.Create(ctx, environment.CreateRequest{TemplateID: createdTemplate.ID})
	if err != nil {
		t.Fatalf("create second unkeyed environment: %v", err)
	}

	if unkeyedOne.Key != nil || unkeyedTwo.Key != nil {
		t.Fatalf("unkeyed environment keys = %#v/%#v, want nil", unkeyedOne.Key, unkeyedTwo.Key)
	}

	encoded, err := json.Marshal(unkeyedOne)
	if err != nil {
		t.Fatalf("marshal unkeyed environment: %v", err)
	}

	if strings.Contains(string(encoded), `"key"`) {
		t.Fatalf("unkeyed environment JSON = %s, want omitted key", encoded)
	}
}

func TestServiceRejectsDuplicateAndBlankEnvironmentKeys(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	templates := template.NewService(db)
	service := environment.NewService(db, environment.WithOrchestrator(newFakeOrchestrator()))
	ctx := context.Background()
	createdTemplate := createUnkeyedTestTemplate(ctx, t, templates)

	if _, err := service.Create(ctx, environment.CreateRequest{Key: new("keyed-environment"), TemplateID: createdTemplate.ID}); err != nil {
		t.Fatalf("create keyed environment: %v", err)
	}

	if _, err := service.Create(ctx, environment.CreateRequest{Key: new("keyed-environment"), TemplateID: createdTemplate.ID}); !errors.Is(err, failure.ErrConflict) {
		t.Fatalf("create duplicate keyed environment error = %v, want conflict", err)
	}

	if _, err := service.Create(ctx, environment.CreateRequest{Key: new(""), TemplateID: createdTemplate.ID}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("create blank-key environment error = %v, want invalid", err)
	}
}

func createTaggedTestTemplate(ctx context.Context, t *testing.T, templates *template.Service) template.Metadata {
	t.Helper()

	createdTemplate, err := templates.Create(ctx, template.CreateRequest{
		Key:    new(testTemplateKey),
		Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	return createdTemplate
}

func createUnkeyedTestTemplate(ctx context.Context, t *testing.T, templates *template.Service) template.Metadata {
	t.Helper()

	createdTemplate, err := templates.Create(ctx, template.CreateRequest{Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)})
	if err != nil {
		t.Fatalf("create unkeyed template: %v", err)
	}

	return createdTemplate
}

func createTaggedEnvironment(ctx context.Context, t *testing.T, service *environment.Service, templateKey string, tags ...string) environment.Environment {
	t.Helper()

	created, err := service.Create(ctx, environment.CreateRequest{TemplateKey: templateKey, Tags: tags})
	if err != nil {
		t.Fatalf("create tagged environment: %v", err)
	}

	if !slices.Equal(created.Tags, tags) {
		t.Fatalf("created environment tags = %#v, want %#v", created.Tags, tags)
	}

	return created
}

func assertStoredEnvironmentTags(ctx context.Context, t *testing.T, service *environment.Service, environmentID string, tags ...string) {
	t.Helper()

	got, err := service.Get(ctx, environmentID)
	if err != nil {
		t.Fatalf("get tagged environment: %v", err)
	}

	if !slices.Equal(got.Tags, tags) {
		t.Fatalf("environment tags = %#v, want %#v", got.Tags, tags)
	}
}

func assertEnvironmentListCount(ctx context.Context, t *testing.T, service *environment.Service, tags []string, want int) {
	t.Helper()

	page, err := service.List(ctx, 20, "", tags)
	if err != nil {
		t.Fatalf("list environments by tags %#v: %v", tags, err)
	}

	if len(page.Entries) != want {
		t.Fatalf("tagged environment count = %d, want %d for %#v", len(page.Entries), want, tags)
	}
}

func assertEnvironmentListOnly(ctx context.Context, t *testing.T, service *environment.Service, tags []string, want environment.Environment) {
	t.Helper()

	page, err := service.List(ctx, 20, "", tags)
	if err != nil {
		t.Fatalf("list environments by tags %#v: %v", tags, err)
	}

	if len(page.Entries) != 1 || page.Entries[0].ID != want.ID || !slices.Equal(page.Entries[0].Tags, want.Tags) {
		t.Fatalf("tagged environment page = %#v, want only %#v", page, want)
	}
}

func createEnvironmentFromTemplate(ctx context.Context, t *testing.T, templates *template.Service, service *environment.Service) environment.Environment {
	t.Helper()

	createdTemplate, err := templates.Create(ctx, template.CreateRequest{
		Key:    new(testTemplateKey),
		Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	created, err := service.Create(ctx, environment.CreateRequest{TemplateKey: testTemplateKey})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if created.ID == "" || created.Status != "running" || created.TemplateID != createdTemplate.ID || created.Tags == nil || len(created.Tags) != 0 {
		t.Fatalf("unexpected created environment: %#v", created)
	}

	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal created environment: %v", err)
	}

	if strings.Contains(string(encoded), "vmId") || strings.Contains(string(encoded), "runtimeState") || strings.Contains(string(encoded), "sshHost") || strings.Contains(string(encoded), "sshKeyPath") {
		t.Fatalf("created environment exposes internal runtime fields: %s", encoded)
	}

	return created
}

func assertEnvironmentList(ctx context.Context, t *testing.T, service *environment.Service) {
	t.Helper()

	page, err := service.List(ctx, 20, "", nil)
	if err != nil {
		t.Fatalf("list environments: %v", err)
	}

	if len(page.Entries) != 1 || page.Cursor != nil {
		t.Fatalf("unexpected environments page: %#v", page)
	}
}

func assertEnvironmentGet(ctx context.Context, t *testing.T, service *environment.Service, want environment.Environment) {
	t.Helper()

	got, err := service.Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("get environment: %v", err)
	}

	if got.ID != want.ID || got.TemplateID != want.TemplateID {
		t.Fatalf("unexpected environment: %#v", got)
	}

	connection, err := service.SSHConnection(ctx, want.ID)
	if err != nil {
		t.Fatalf("get environment SSH connection: %v", err)
	}

	if connection.Host != testGuestIP || connection.KeyPath != "/tmp/test.id_rsa" {
		t.Fatalf("unexpected SSH connection metadata: %#v", connection)
	}
}

func assertEnvironmentRemove(ctx context.Context, t *testing.T, service *environment.Service, id string) {
	t.Helper()

	removed, err := service.Remove(ctx, id)
	if err != nil {
		t.Fatalf("remove environment: %v", err)
	}

	if removed.ID != id || removed.Status != "removed" {
		t.Fatalf("unexpected removed environment: %#v", removed)
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

func requireStringPtr(t *testing.T, value *string) string {
	t.Helper()

	if value == nil {
		t.Fatal("string pointer is nil")
	}

	return *value
}

type fakeOrchestrator struct {
	launches  int
	removes   int
	templates []ch.Template
	vms       map[string]ch.VM
}

func newFakeOrchestrator() *fakeOrchestrator {
	return &fakeOrchestrator{vms: make(map[string]ch.VM)}
}

func (o *fakeOrchestrator) Launch(_ context.Context, req ch.LaunchRequest) (ch.VM, error) {
	o.launches++
	o.templates = append(o.templates, ch.Template{ID: req.Template.ID, Key: req.Template.Key, Config: append(json.RawMessage(nil), req.Template.Config...)})

	vm := ch.VM{
		EnvironmentID: req.EnvironmentID,
		VMID:          "vm-" + req.EnvironmentID,
		State:         ch.StateRunning,
		GuestIP:       testGuestIP,
		SSHUser:       ch.SSHUser,
		SSHPort:       ch.SSHPort,
		SSHKeyPath:    "/tmp/test.id_rsa",
		CreatedAt:     testVMTime,
		UpdatedAt:     testVMTime,
	}
	o.vms[req.EnvironmentID] = vm

	return vm, nil
}

func (o *fakeOrchestrator) State(_ context.Context, environmentID string) (ch.VM, error) {
	vm, ok := o.vms[environmentID]
	if !ok {
		return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped}, nil
	}

	return vm, nil
}

func (o *fakeOrchestrator) Remove(_ context.Context, environmentID string) (ch.VM, error) {
	o.removes++
	delete(o.vms, environmentID)

	return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped}, nil
}

type failingLaunchOrchestrator struct {
	err error
	vm  ch.VM
}

func (o *failingLaunchOrchestrator) Launch(_ context.Context, req ch.LaunchRequest) (ch.VM, error) {
	o.vm = ch.VM{
		EnvironmentID: req.EnvironmentID,
		VMID:          "vm-" + req.EnvironmentID,
		State:         ch.StateError,
		GuestIP:       testGuestIP,
		SSHUser:       ch.SSHUser,
		SSHPort:       ch.SSHPort,
		SSHKeyPath:    "/tmp/test.id_rsa",
		CreatedAt:     testVMTime,
		UpdatedAt:     testVMTime,
		LastError:     o.err.Error(),
	}

	return o.vm, o.err
}

func (o *failingLaunchOrchestrator) State(_ context.Context, _ string) (ch.VM, error) {
	return o.vm, nil
}

func (o *failingLaunchOrchestrator) Remove(_ context.Context, environmentID string) (ch.VM, error) {
	return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped}, nil
}
