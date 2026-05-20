package environment_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	fc "github.com/bastion-computer/bastion/core/internal/firecracker"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
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
	orchestrator.vms[created.ID] = fc.VM{EnvironmentID: created.ID}

	_, err := service.Get(ctx, created.ID)
	if err == nil || !strings.Contains(err.Error(), "empty runtime state") {
		t.Fatalf("get environment error = %v, want empty runtime state error", err)
	}
}

func createEnvironmentFromTemplate(ctx context.Context, t *testing.T, templates *template.Service, service *environment.Service) environment.Environment {
	t.Helper()

	createdTemplate, err := templates.Create(ctx, template.CreateRequest{
		Key:    "dev-env",
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	created, err := service.Create(ctx, environment.CreateRequest{TemplateKey: "dev-env"})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if created.ID == "" || created.Status != "running" || created.TemplateID != createdTemplate.ID || created.SSHHost != "10.241.0.2" {
		t.Fatalf("unexpected created environment: %#v", created)
	}

	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal created environment: %v", err)
	}

	if strings.Contains(string(encoded), "vmId") || strings.Contains(string(encoded), "runtimeState") {
		t.Fatalf("created environment exposes internal runtime fields: %s", encoded)
	}

	return created
}

func assertEnvironmentList(ctx context.Context, t *testing.T, service *environment.Service) {
	t.Helper()

	page, err := service.List(ctx, 20, "")
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

	if got.ID != want.ID || got.TemplateID != want.TemplateID || got.SSHKeyPath != "/tmp/test.id_rsa" {
		t.Fatalf("unexpected environment: %#v", got)
	}
}

func assertEnvironmentRemove(ctx context.Context, t *testing.T, service *environment.Service, id string) {
	t.Helper()

	removed, err := service.Remove(ctx, id)
	if err != nil {
		t.Fatalf("remove environment: %v", err)
	}

	if removed.ID != id || removed.Status != "removed" || removed.SSHHost != "" {
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

type fakeOrchestrator struct {
	launches int
	removes  int
	vms      map[string]fc.VM
}

func newFakeOrchestrator() *fakeOrchestrator {
	return &fakeOrchestrator{vms: make(map[string]fc.VM)}
}

func (o *fakeOrchestrator) Launch(_ context.Context, req fc.LaunchRequest) (fc.VM, error) {
	o.launches++
	vm := fc.VM{
		EnvironmentID: req.EnvironmentID,
		VMID:          "vm-" + req.EnvironmentID,
		State:         fc.StateRunning,
		GuestIP:       "10.241.0.2",
		SSHUser:       fc.SSHUser,
		SSHPort:       fc.SSHPort,
		SSHKeyPath:    "/tmp/test.id_rsa",
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	o.vms[req.EnvironmentID] = vm

	return vm, nil
}

func (o *fakeOrchestrator) State(_ context.Context, environmentID string) (fc.VM, error) {
	vm, ok := o.vms[environmentID]
	if !ok {
		return fc.VM{EnvironmentID: environmentID, State: fc.StateStopped}, nil
	}

	return vm, nil
}

func (o *fakeOrchestrator) Remove(_ context.Context, environmentID string) (fc.VM, error) {
	o.removes++
	delete(o.vms, environmentID)

	return fc.VM{EnvironmentID: environmentID, State: fc.StateStopped}, nil
}
