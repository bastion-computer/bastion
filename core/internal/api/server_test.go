package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/api"
	hostclient "github.com/bastion-computer/bastion/core/internal/client"
	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/internal/sshtunnel"
)

const (
	apiTestProdTag = "prod"
	apiTestGPUTag  = "gpu"
	apiTestCPUTag  = "cpu"
)

func TestListRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	templateOne := createTemplate(t, router, "template-list-1")
	templateTwo := createTemplate(t, router, "template-list-2")

	createEnvironment(t, router, requireStringPtr(t, templateOne.Key))
	createEnvironment(t, router, requireStringPtr(t, templateTwo.Key))

	assertList[template.Metadata](t, router, "/v1/templates", 2)
	assertList[environment.Environment](t, router, "/v1/environments", 2)
}

func TestCreateTemplateRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	res := request(t, router, http.MethodPost, "/v1/templates", template.CreateRequest{
		Key:    new("invalid-template"),
		Config: json.RawMessage(`{"actions":{"init":[]},"networkRules":{}}`),
	})
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create invalid template status = %d, want %d", res.Code, http.StatusBadRequest)
	}

	res = request(t, router, http.MethodGet, "/v1/templates/by-key/invalid-template", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("get invalid template status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestTemplateAndEnvironmentOptionalKeys(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	res := request(t, router, http.MethodPost, "/v1/templates", template.CreateRequest{Config: json.RawMessage(`{"actions":{"init":[]}}`)})
	if res.Code != http.StatusOK {
		t.Fatalf("create unkeyed template status = %d, want %d", res.Code, http.StatusOK)
	}

	if strings.Contains(res.Body.String(), `"key"`) {
		t.Fatalf("unkeyed template response includes key: %s", res.Body.String())
	}

	var unkeyedTemplate template.Metadata
	decode(t, res, &unkeyedTemplate)

	if unkeyedTemplate.ID == "" || unkeyedTemplate.Key != nil {
		t.Fatalf("unkeyed template = %#v, want id without key", unkeyedTemplate)
	}

	keyedEnv := createEnvironmentFromRequest(t, router, environment.CreateRequest{Key: new("api-environment-key"), TemplateID: unkeyedTemplate.ID})
	if keyedEnv.Key == nil || *keyedEnv.Key != "api-environment-key" {
		t.Fatalf("keyed environment key = %#v, want api-environment-key", keyedEnv.Key)
	}

	assertGet(t, router, "/v1/environments/by-key/api-environment-key", keyedEnv.ID)

	unkeyedEnv := createEnvironmentFromRequest(t, router, environment.CreateRequest{TemplateID: unkeyedTemplate.ID})
	if unkeyedEnv.Key != nil {
		t.Fatalf("unkeyed environment key = %#v, want nil", unkeyedEnv.Key)
	}

	encoded, err := json.Marshal(unkeyedEnv)
	if err != nil {
		t.Fatalf("marshal unkeyed environment: %v", err)
	}

	if strings.Contains(string(encoded), `"key"`) {
		t.Fatalf("unkeyed environment JSON includes key: %s", encoded)
	}

	assertDelete(t, router, "/v1/environments/by-key/api-environment-key")
}

func TestCreateEnvironmentForwardsFailedDependency(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithEnvironmentOrchestrator(failedDependencyOrchestrator{}))
	template := createTemplate(t, router, "failed-dependency-template")

	res := request(t, router, http.MethodPost, "/v1/environments", environment.CreateRequest{TemplateKey: requireStringPtr(t, template.Key)})
	if res.Code != http.StatusOK {
		t.Fatalf("create environment status = %d, want streaming %d", res.Code, http.StatusOK)
	}

	var event environment.CreateStreamEvent
	decode(t, res, &event)

	if event.Type != environment.StreamEventError || event.Status != http.StatusFailedDependency || event.Error == "" {
		t.Fatalf("create environment event = %#v, want failed dependency error event", event)
	}
}

func TestEnvironmentTagsCreateGetAndListRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))
	template := createTemplate(t, router, "tagged-environment-template")

	prodGPU := createEnvironment(t, router, requireStringPtr(t, template.Key), apiTestProdTag, apiTestGPUTag)
	prodCPU := createEnvironment(t, router, requireStringPtr(t, template.Key), apiTestProdTag, apiTestCPUTag)

	res := request(t, router, http.MethodGet, "/v1/environments/"+prodGPU.ID, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("get tagged environment status = %d, want %d", res.Code, http.StatusOK)
	}

	var got environment.Environment
	decode(t, res, &got)

	if !slices.Equal(got.Tags, []string{apiTestProdTag, apiTestGPUTag}) {
		t.Fatalf("get tagged environment tags = %#v, want prod/gpu", got.Tags)
	}

	res = request(t, router, http.MethodGet, "/v1/environments?tag="+apiTestProdTag+"&tag="+apiTestGPUTag, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list tagged environments status = %d, want %d", res.Code, http.StatusOK)
	}

	var gpuPage services.Page[environment.Environment]
	decode(t, res, &gpuPage)

	if len(gpuPage.Entries) != 1 || gpuPage.Entries[0].ID != prodGPU.ID || !slices.Equal(gpuPage.Entries[0].Tags, prodGPU.Tags) {
		t.Fatalf("tag-filtered environments = %#v, want only %#v", gpuPage, prodGPU)
	}

	res = request(t, router, http.MethodGet, "/v1/environments?tag="+apiTestProdTag, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list prod environments status = %d, want %d", res.Code, http.StatusOK)
	}

	var prodPage services.Page[environment.Environment]
	decode(t, res, &prodPage)

	if len(prodPage.Entries) != 2 {
		t.Fatalf("prod environments = %#v, want 2 entries", prodPage)
	}

	prodIDs := []string{prodPage.Entries[0].ID, prodPage.Entries[1].ID}
	if !slices.Contains(prodIDs, prodGPU.ID) || !slices.Contains(prodIDs, prodCPU.ID) {
		t.Fatalf("prod environment ids = %#v, want %#v and %#v", prodIDs, prodGPU.ID, prodCPU.ID)
	}
}

func TestGetRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	templateByID := createTemplate(t, router, "template-get-id")
	assertGet(t, router, "/v1/templates/"+templateByID.ID, templateByID.ID)

	templateByKey := createTemplate(t, router, "template-get-key")
	assertGet(t, router, "/v1/templates/by-key/"+requireStringPtr(t, templateByKey.Key), templateByKey.ID)

	env := createEnvironment(t, router, requireStringPtr(t, templateByID.Key))
	assertGet(t, router, "/v1/environments/"+env.ID, env.ID)
}

func TestDeleteRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	templateByID := createTemplate(t, router, "template-delete-id")
	assertDelete(t, router, "/v1/templates/"+templateByID.ID)

	templateByKey := createTemplate(t, router, "template-delete-key")
	assertDelete(t, router, "/v1/templates/by-key/"+requireStringPtr(t, templateByKey.Key))

	templateForEnv := createTemplate(t, router, "environment-delete-source")
	env := createEnvironment(t, router, requireStringPtr(t, templateForEnv.Key))
	assertDelete(t, router, "/v1/environments/"+env.ID)
}

func TestSSHRouteUpgradesAndRunsSSHRunner(t *testing.T) {
	t.Parallel()

	orchestrator := &sshRouteOrchestrator{vms: make(map[string]ch.VM)}
	runnerCalled := make(chan struct {
		connection environment.SSHConnection
		req        sshtunnel.Request
	}, 1)

	router := newTestRouter(t, slog.New(slog.DiscardHandler),
		api.WithEnvironmentOrchestrator(orchestrator),
		api.WithEnvironmentSSHRunner(func(_ context.Context, stream io.ReadWriteCloser, connection environment.SSHConnection, req sshtunnel.Request) error {
			runnerCalled <- struct {
				connection environment.SSHConnection
				req        sshtunnel.Request
			}{connection: connection, req: req}

			payload, err := json.Marshal(sshtunnel.ExitStatus{})
			if err != nil {
				return err
			}

			return sshtunnel.WriteFrame(stream, sshtunnel.FrameExit, payload)
		}),
	)
	template := createTemplate(t, router, "ssh-route-template")
	env := createEnvironment(t, router, requireStringPtr(t, template.Key))

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	stream, err := hostclient.New(server.URL).OpenSSH(context.Background(), env.ID, sshtunnel.Request{Command: []string{"true"}})
	if err != nil {
		t.Fatalf("open SSH stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	frameType, payload, err := sshtunnel.ReadFrame(stream)
	if err != nil {
		t.Fatalf("read SSH frame: %v", err)
	}

	if frameType != sshtunnel.FrameExit {
		t.Fatalf("frame type = %d, want exit", frameType)
	}

	var status sshtunnel.ExitStatus
	if err := json.Unmarshal(payload, &status); err != nil {
		t.Fatalf("decode exit status: %v", err)
	}

	if status.Code != 0 {
		t.Fatalf("exit status = %d, want 0", status.Code)
	}

	got := <-runnerCalled
	if got.connection.Host != "10.241.0.2" || got.connection.KeyPath != "/tmp/test.id_rsa" {
		t.Fatalf("SSH connection = %#v, want private metadata", got.connection)
	}

	if len(got.req.Command) != 1 || got.req.Command[0] != "true" {
		t.Fatalf("SSH request = %#v, want true command", got.req)
	}
}

func newTestRouter(t *testing.T, logger *slog.Logger, opts ...api.RouterOption) http.Handler {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return api.NewRouter(db, logger, opts...)
}

func createTemplate(t *testing.T, handler http.Handler, key string) template.Metadata {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/templates", template.CreateRequest{
		Key:    new(key),
		Config: json.RawMessage(`{"actions":{"init":[]}}`),
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create template status = %d, want %d", res.Code, http.StatusOK)
	}

	var created template.Metadata
	decode(t, res, &created)

	return created
}

func createEnvironment(t *testing.T, handler http.Handler, templateKey string, tags ...string) environment.Environment {
	t.Helper()

	return createEnvironmentFromRequest(t, handler, environment.CreateRequest{TemplateKey: templateKey, Tags: tags})
}

func createEnvironmentFromRequest(t *testing.T, handler http.Handler, req environment.CreateRequest) environment.Environment {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/environments", req)
	if res.Code != http.StatusOK {
		t.Fatalf("create environment status = %d, want %d", res.Code, http.StatusOK)
	}

	var event environment.CreateStreamEvent
	decode(t, res, &event)

	if event.Type != environment.StreamEventResult || event.Environment == nil {
		t.Fatalf("create environment event = %#v, want result", event)
	}

	created := *event.Environment

	if created.Status != "running" || !slices.Equal(created.Tags, req.Tags) {
		t.Fatalf("created environment = %#v, want running with tags %#v", created, req.Tags)
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

func requireStringPtr(t *testing.T, value *string) string {
	t.Helper()

	if value == nil {
		t.Fatal("string pointer is nil")
	}

	return *value
}

type failedDependencyOrchestrator struct{}

func (failedDependencyOrchestrator) Launch(_ context.Context, req ch.LaunchRequest) (ch.VM, error) {
	return ch.VM{
		EnvironmentID: req.EnvironmentID,
		VMID:          "vm-" + req.EnvironmentID,
		State:         ch.StateError,
		LastError:     "init action 2 failed",
	}, fmt.Errorf("%w: bastiond returned 424 Failed Dependency: init action 2 failed", failure.ErrFailedDependency)
}

func (failedDependencyOrchestrator) State(_ context.Context, environmentID string) (ch.VM, error) {
	return ch.VM{EnvironmentID: environmentID, State: ch.StateError, LastError: "init action 2 failed"}, nil
}

func (failedDependencyOrchestrator) Remove(_ context.Context, environmentID string) (ch.VM, error) {
	return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped}, nil
}

type sshRouteOrchestrator struct {
	vms map[string]ch.VM
}

func (o *sshRouteOrchestrator) Launch(_ context.Context, req ch.LaunchRequest) (ch.VM, error) {
	vm := ch.VM{
		EnvironmentID: req.EnvironmentID,
		VMID:          "vm-" + req.EnvironmentID,
		State:         ch.StateRunning,
		GuestIP:       "10.241.0.2",
		SSHUser:       ch.SSHUser,
		SSHPort:       ch.SSHPort,
		SSHKeyPath:    "/tmp/test.id_rsa",
	}
	o.vms[req.EnvironmentID] = vm

	return vm, nil
}

func (o *sshRouteOrchestrator) State(_ context.Context, environmentID string) (ch.VM, error) {
	vm, ok := o.vms[environmentID]
	if !ok {
		return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped}, nil
	}

	return vm, nil
}

func (o *sshRouteOrchestrator) Remove(_ context.Context, environmentID string) (ch.VM, error) {
	delete(o.vms, environmentID)

	return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped}, nil
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
