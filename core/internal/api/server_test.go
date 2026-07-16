package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/core/internal/api"
	hostclient "github.com/bastion-computer/bastion/core/internal/client"
	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/internal/services/utilization"
	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
)

const (
	apiTestProdTag = "prod"
	apiTestGPUTag  = "gpu"
	apiTestCPUTag  = "cpu"
	apiTestGiB     = int64(1 << 30)

	testWebSocketKey    = "dGhlIHNhbXBsZSBub25jZQ=="
	testWebSocketAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
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

func TestUtilizationRoute(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithUtilizationHostCapacity(func(context.Context) (utilization.HostCapacity, error) {
		return utilization.HostCapacity{VCPU: 12, MemoryBytes: 32 * apiTestGiB, VolumeBytes: 100 * apiTestGiB}, nil
	}))

	template := createTemplateWithConfig(t, router, "utilization-template", json.RawMessage(`{"agents":{"opencode":{}},"resources":{"vcpu":3,"memory":4,"volume":5},"actions":{"init":[]}}`))
	createEnvironment(t, router, requireStringPtr(t, template.Key))

	res := request(t, router, http.MethodGet, "/v1/utilization", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("utilization status = %d, want %d", res.Code, http.StatusOK)
	}

	var got utilization.Utilization
	decode(t, res, &got)

	want := utilization.Utilization{
		VCPU:   utilization.Resource{Total: 12, Used: 3, Available: 9},
		Memory: utilization.Resource{Total: 32 * apiTestGiB, Used: 4 * apiTestGiB, Available: 28 * apiTestGiB},
		Volume: utilization.Resource{Total: 100 * apiTestGiB, Used: 5 * apiTestGiB, Available: 95 * apiTestGiB},
	}
	if got != want {
		t.Fatalf("utilization = %#v, want %#v", got, want)
	}
}

func TestCreateTemplateRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	res := request(t, router, http.MethodPost, "/v1/templates", template.CreateRequest{
		Key:    new("invalid-template"),
		Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]},"networkRules":{}}`),
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create invalid template status = %d, want streaming %d", res.Code, http.StatusOK)
	}

	var event template.CreateStreamEvent
	decode(t, res, &event)

	if event.Type != template.StreamEventError || event.Status != http.StatusBadRequest {
		t.Fatalf("create invalid template event = %#v, want bad request error event", event)
	}

	res = request(t, router, http.MethodGet, "/v1/templates/by-key/invalid-template", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("get invalid template status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestDocumentedTemplateExamplesCreateTemplates(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))
	createSecret(t, router, "OPENAI_API_KEY", "test-openai-key")
	createSecret(t, router, "GITHUB_TOKEN", "test-github-token")

	cases := []struct {
		name string
		key  string
		path string
	}{
		{name: "get started", key: "docs-get-started", path: "../../../docs/src/content/docs/tutorials/get-started.md"},
		{name: "parallel agents", key: "docs-parallel-agents", path: "../../../docs/src/content/docs/tutorials/run-parallel-agents.md"},
	}

	for _, tc := range cases {
		res := request(t, router, http.MethodPost, "/v1/templates", template.CreateRequest{
			Key:    new(tc.key),
			Config: documentedTemplateConfig(t, tc.path),
		})
		if res.Code != http.StatusOK {
			t.Fatalf("create %s documented template status = %d, want %d; body: %s", tc.name, res.Code, http.StatusOK, res.Body.String())
		}
	}
}

func TestTemplateAndEnvironmentOptionalKeys(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))

	res := request(t, router, http.MethodPost, "/v1/templates", template.CreateRequest{Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)})
	if res.Code != http.StatusOK {
		t.Fatalf("create unkeyed template status = %d, want %d", res.Code, http.StatusOK)
	}

	if strings.Contains(res.Body.String(), `"key"`) {
		t.Fatalf("unkeyed template response includes key: %s", res.Body.String())
	}

	unkeyedTemplate := decodeTemplateCreateResult(t, res)

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

func TestSecretRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler))
	key := "api-secret"

	created := createSecretRoute(t, router, key, "secret-value")
	assertSecretListRoute(t, router, created.ID, "secret-value")
	assertSecretGetRoutes(t, router, created.ID, key, "secret-value")
	assertSecretDeleteRoute(t, router, created.ID, key, "secret-value")
}

func createSecretRoute(t *testing.T, router http.Handler, key, value string) secret.Metadata {
	t.Helper()

	res := request(t, router, http.MethodPost, "/v1/secrets", secret.CreateRequest{Key: &key, Value: value})
	if res.Code != http.StatusCreated {
		t.Fatalf("create secret status = %d, want %d; body: %s", res.Code, http.StatusCreated, res.Body.String())
	}

	var created secret.Metadata
	decode(t, res, &created)

	if !strings.HasPrefix(created.ID, "sec_") || created.Key == nil || *created.Key != key {
		t.Fatalf("created secret = %#v, want sec_ id and key", created)
	}

	if strings.Contains(res.Body.String(), value) || strings.Contains(res.Body.String(), `"value"`) {
		t.Fatalf("create secret response leaked value: %s", res.Body.String())
	}

	return created
}

func assertSecretListRoute(t *testing.T, router http.Handler, secretID, value string) {
	t.Helper()

	res := request(t, router, http.MethodGet, "/v1/secrets", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list secrets status = %d, want %d", res.Code, http.StatusOK)
	}

	var page services.Page[secret.Metadata]
	decode(t, res, &page)

	if len(page.Entries) != 1 || page.Entries[0].ID != secretID {
		t.Fatalf("secret page = %#v, want created secret metadata", page)
	}

	if strings.Contains(res.Body.String(), value) || strings.Contains(res.Body.String(), `"value"`) {
		t.Fatalf("list secrets response leaked value: %s", res.Body.String())
	}
}

func assertSecretGetRoutes(t *testing.T, router http.Handler, secretID, key, value string) {
	t.Helper()

	for _, path := range []string{"/v1/secrets/" + secretID, "/v1/secrets/by-key/" + key} {
		res := request(t, router, http.MethodGet, path, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("get secret %s status = %d, want %d", path, res.Code, http.StatusOK)
		}

		var got secret.Secret
		decode(t, res, &got)

		if got.ID != secretID || got.Value != value {
			t.Fatalf("get secret %s = %#v, want value", path, got)
		}
	}
}

func assertSecretDeleteRoute(t *testing.T, router http.Handler, secretID, key, value string) {
	t.Helper()

	res := request(t, router, http.MethodDelete, "/v1/secrets/by-key/"+key, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("delete secret status = %d, want %d", res.Code, http.StatusOK)
	}

	var removed secret.Metadata
	decode(t, res, &removed)

	if removed.ID != secretID {
		t.Fatalf("removed secret = %#v, want %s", removed, secretID)
	}

	if strings.Contains(res.Body.String(), value) || strings.Contains(res.Body.String(), `"value"`) {
		t.Fatalf("delete secret response leaked value: %s", res.Body.String())
	}

	res = request(t, router, http.MethodGet, "/v1/secrets/"+secretID, nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("get deleted secret status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

//nolint:gocyclo // Verifies template import/export routes and archive forwarding in one fixture.
func TestTemplateImportExportRoutes(t *testing.T) {
	t.Parallel()

	orchestrator := &templateArchiveOrchestrator{importConfig: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)}
	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithTemplateOrchestrator(orchestrator))
	source := createTemplate(t, router, "template-export-source")

	res := request(t, router, http.MethodGet, "/v1/templates/by-key/template-export-source/export", nil)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "template-archive" {
		t.Fatalf("export by key response = status %d body %q, want archive", res.Code, res.Body.String())
	}

	if got := res.Header().Get("Content-Type"); got != ch.TemplateArchiveContentType {
		t.Fatalf("export content type = %q, want %q", got, ch.TemplateArchiveContentType)
	}

	res = request(t, router, http.MethodGet, "/v1/templates/"+source.ID+"/export", nil)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "template-archive" {
		t.Fatalf("export by id response = status %d body %q, want archive", res.Code, res.Body.String())
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/templates/import?key=template-import-restored", strings.NewReader("template-archive"))
	req.Header.Set("Content-Type", ch.TemplateArchiveContentType)

	res = httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("import template status = %d, want %d; body: %s", res.Code, http.StatusCreated, res.Body.String())
	}

	var imported template.Metadata
	decode(t, res, &imported)

	if imported.ID == "" || imported.ID == source.ID || imported.Key == nil || *imported.Key != "template-import-restored" {
		t.Fatalf("imported template = %#v, want new keyed template", imported)
	}

	assertGet(t, router, "/v1/templates/by-key/template-import-restored", imported.ID)

	if len(orchestrator.importedArchives) != 1 || string(orchestrator.importedArchives[0]) != "template-archive" {
		t.Fatalf("imported archives = %q, want archive", orchestrator.importedArchives)
	}

	if len(orchestrator.importSizes) != 1 || orchestrator.importSizes[0] != int64(len("template-archive")) {
		t.Fatalf("import sizes = %#v, want archive size", orchestrator.importSizes)
	}
}

func TestTemplateImportRejectsInvalidArchive(t *testing.T) {
	t.Parallel()

	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithTemplateOrchestrator(&templateArchiveOrchestrator{importErr: ch.ErrInvalidTemplateArchive}))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/v1/templates/import", strings.NewReader(""))
	req.Header.Set("Content-Type", ch.TemplateArchiveContentType)

	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("import invalid archive status = %d, want %d; body: %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
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

func TestAgentProxyRouteForwardsToOpenCode(t *testing.T) {
	t.Parallel()

	const port = 4097

	socketPath := newGuestProxySocket(t, func(r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/global/health" || r.URL.RawQuery != "check=1" {
			t.Fatalf("proxied request = %s %s?%s, want GET /global/health?check=1", r.Method, r.URL.Path, r.URL.RawQuery)
		}

		if r.Header.Get("X-Bastion-Tunnel-Port") != strconv.Itoa(port) {
			t.Fatalf("tunnel port header = %q, want %d", r.Header.Get("X-Bastion-Tunnel-Port"), port)
		}

		if r.Header.Get("X-Test-Proxy") != "yes" {
			t.Fatalf("proxied header X-Test-Proxy = %q, want yes", r.Header.Get("X-Test-Proxy"))
		}
	}, map[string]string{"X-Agent": "opencode"}, `{"healthy":true}`)

	orchestrator := &agentProxyOrchestrator{vms: make(map[string]ch.VM), vsockSocketPath: socketPath}

	var logs bytes.Buffer

	router := newTestRouter(t, slog.New(slog.NewTextHandler(&logs, nil)), api.WithEnvironmentOrchestrator(orchestrator))
	template := createTemplateWithConfig(t, router, "agent-proxy-template", json.RawMessage(fmt.Sprintf(`{"agents":{"opencode":{"config":{"server":{"port":%d}}}},"actions":{"init":[]}}`, port)))
	envKey := "agent-proxy-environment"
	env := createEnvironmentFromRequest(t, router, environment.CreateRequest{Key: new(envKey), TemplateKey: requireStringPtr(t, template.Key)})

	paths := []string{
		"/v1/environments/" + env.ID + "/agents/opencode/global/health?check=1",
		"/v1/environments/by-key/" + envKey + "/agents/opencode/global/health?check=1",
	}

	for _, path := range paths {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		req.Header.Set("X-Test-Proxy", "yes")

		res := httptest.NewRecorder()

		router.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("agent proxy %s status = %d, want %d; body: %s; logs: %s", path, res.Code, http.StatusOK, res.Body.String(), logs.String())
		}

		if res.Header().Get("X-Agent") != "opencode" {
			t.Fatalf("agent proxy %s X-Agent header = %q, want opencode", path, res.Header().Get("X-Agent"))
		}

		if strings.TrimSpace(res.Body.String()) != `{"healthy":true}` {
			t.Fatalf("agent proxy %s body = %q, want health JSON", path, res.Body.String())
		}
	}
}

func TestTunnelProxyRouteForwardsRegisteredTunnel(t *testing.T) {
	t.Parallel()

	socketPath := newGuestProxySocket(t, func(r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/preview" || r.URL.RawQuery != "mode=dev" {
			t.Fatalf("proxied tunnel request = %s %s?%s, want POST /preview?mode=dev", r.Method, r.URL.Path, r.URL.RawQuery)
		}

		if r.Header.Get("X-Bastion-Tunnel-Port") != "3000" {
			t.Fatalf("tunnel port header = %q, want 3000", r.Header.Get("X-Bastion-Tunnel-Port"))
		}
	}, map[string]string{"X-Tunnel": "frontend"}, `preview ok`)

	orchestrator := &agentProxyOrchestrator{vms: make(map[string]ch.VM), vsockSocketPath: socketPath}
	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithEnvironmentOrchestrator(orchestrator))
	template := createTemplateWithConfig(t, router, "tunnel-proxy-template", json.RawMessage(`{"agents":{"opencode":{}},"tunnels":{"frontend":3000},"actions":{"init":[]}}`))
	envKey := "tunnel-proxy-environment"
	env := createEnvironmentFromRequest(t, router, environment.CreateRequest{Key: new(envKey), TemplateKey: requireStringPtr(t, template.Key)})

	paths := []string{
		"/v1/environments/" + env.ID + "/tunnels/frontend/preview?mode=dev",
		"/v1/environments/by-key/" + envKey + "/tunnels/frontend/preview?mode=dev",
	}

	for _, path := range paths {
		res := request(t, router, http.MethodPost, path, strings.NewReader("body"))
		if res.Code != http.StatusOK {
			t.Fatalf("tunnel proxy %s status = %d, want %d; body: %s", path, res.Code, http.StatusOK, res.Body.String())
		}

		if res.Header().Get("X-Tunnel") != "frontend" {
			t.Fatalf("tunnel proxy %s X-Tunnel header = %q, want frontend", path, res.Header().Get("X-Tunnel"))
		}

		if strings.TrimSpace(res.Body.String()) != "preview ok" {
			t.Fatalf("tunnel proxy %s body = %q, want preview ok", path, res.Body.String())
		}
	}
}

func TestTunnelProxyRouteForwardsUpgradedSSH(t *testing.T) {
	t.Parallel()

	socketPath := newGuestProxyUpgradeSocket(t, func(r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/environments/env_inner/ssh" {
			t.Fatalf("proxied upgrade request = %s %s, want POST /v1/environments/env_inner/ssh", r.Method, r.URL.Path)
		}

		if r.Header.Get("X-Bastion-Tunnel-Port") != "4148" {
			t.Fatalf("tunnel port header = %q, want 4148", r.Header.Get("X-Bastion-Tunnel-Port"))
		}

		if r.Header.Get("Upgrade") != sshtunnel.Protocol {
			t.Fatalf("upgrade header = %q, want %q", r.Header.Get("Upgrade"), sshtunnel.Protocol)
		}
	})

	orchestrator := &agentProxyOrchestrator{vms: make(map[string]ch.VM), vsockSocketPath: socketPath}
	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithEnvironmentOrchestrator(orchestrator))
	template := createTemplateWithConfig(t, router, "tunnel-upgrade-template", json.RawMessage(`{"agents":{"opencode":{}},"tunnels":{"nodeapi":4148},"actions":{"init":[]}}`))
	env := createEnvironment(t, router, requireStringPtr(t, template.Key))

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	stream, err := hostclient.New(server.URL+"/v1/environments/"+env.ID+"/tunnels/nodeapi").OpenSSH(context.Background(), "env_inner", sshtunnel.Request{Command: []string{"true"}})
	if err != nil {
		t.Fatalf("open nested SSH stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	frameType, _, err := sshtunnel.ReadFrame(stream)
	if err != nil {
		t.Fatalf("read nested SSH frame: %v", err)
	}

	if frameType != sshtunnel.FrameExit {
		t.Fatalf("frame type = %d, want exit", frameType)
	}
}

func TestTunnelProxyRouteForwardsWebSocketUpgrades(t *testing.T) {
	t.Parallel()

	socketPath := newGuestProxyWebSocketSocket(t, func(r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/hmr" || r.URL.RawQuery != "token=abc" {
			t.Fatalf("proxied websocket request = %s %s?%s, want GET /hmr?token=abc", r.Method, r.URL.Path, r.URL.RawQuery)
		}

		if r.Header.Get("X-Bastion-Tunnel-Port") != "3000" {
			t.Fatalf("tunnel port header = %q, want 3000", r.Header.Get("X-Bastion-Tunnel-Port"))
		}

		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			t.Fatalf("upgrade header = %q, want websocket", r.Header.Get("Upgrade"))
		}
	})

	orchestrator := &agentProxyOrchestrator{vms: make(map[string]ch.VM), vsockSocketPath: socketPath}
	router := newTestRouter(t, slog.New(slog.DiscardHandler), api.WithEnvironmentOrchestrator(orchestrator))
	template := createTemplateWithConfig(t, router, "tunnel-websocket-template", json.RawMessage(`{"agents":{"opencode":{}},"tunnels":{"frontend":3000},"actions":{"init":[]}}`))
	env := createEnvironment(t, router, requireStringPtr(t, template.Key))

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	conn, reader := openWebSocketUpgrade(t, server.URL, "/v1/environments/"+env.ID+"/tunnels/frontend/hmr?token=abc", nil)
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write websocket payload: %v", err)
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read websocket payload: %v", err)
	}

	if line != "guest:ping\n" {
		t.Fatalf("websocket payload = %q, want guest echo", line)
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

	return createTemplateWithConfig(t, handler, key, json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`))
}

func createSecret(t *testing.T, handler http.Handler, key, value string) secret.Metadata {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/secrets", secret.CreateRequest{Key: &key, Value: value})
	if res.Code != http.StatusCreated {
		t.Fatalf("create secret status = %d, want %d", res.Code, http.StatusCreated)
	}

	var created secret.Metadata
	decode(t, res, &created)

	return created
}

func createTemplateWithConfig(t *testing.T, handler http.Handler, key string, config json.RawMessage) template.Metadata {
	t.Helper()

	res := request(t, handler, http.MethodPost, "/v1/templates", template.CreateRequest{
		Key:    new(key),
		Config: config,
	})
	if res.Code != http.StatusOK {
		t.Fatalf("create template status = %d, want %d", res.Code, http.StatusOK)
	}

	created := decodeTemplateCreateResult(t, res)

	return created
}

func newGuestProxySocket(t *testing.T, assertRequest func(*http.Request), headers map[string]string, body string) string {
	t.Helper()

	socketPath := filepath.Join(t.TempDir(), "guest-proxy.sock")

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socketPath)
	if err != nil {
		t.Fatalf("listen on guest proxy socket: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go handleGuestProxySocket(t, conn, assertRequest, headers, body)
		}
	}()

	return socketPath
}

func newGuestProxyUpgradeSocket(t *testing.T, assertRequest func(*http.Request)) string {
	t.Helper()

	return newGuestProxyRawSocket(t, "guest-proxy-upgrade.sock", func(conn net.Conn) {
		handleGuestProxyUpgradeSocket(t, conn, assertRequest)
	})
}

func newGuestProxyWebSocketSocket(t *testing.T, assertRequest func(*http.Request)) string {
	t.Helper()

	return newGuestProxyRawSocket(t, "guest-proxy-websocket.sock", func(conn net.Conn) {
		handleGuestProxyWebSocketSocket(t, conn, assertRequest)
	})
}

func newGuestProxyRawSocket(t *testing.T, name string, handle func(net.Conn)) string {
	t.Helper()

	socketPath := filepath.Join(t.TempDir(), name)

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", socketPath)
	if err != nil {
		t.Fatalf("listen on guest proxy socket: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			go handle(conn)
		}
	}()

	return socketPath
}

func handleGuestProxyWebSocketSocket(t *testing.T, conn net.Conn, assertRequest func(*http.Request)) {
	t.Helper()

	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Errorf("set guest proxy websocket deadline: %v", err)
		return
	}

	reader := bufio.NewReader(conn)

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Errorf("read vsock connect line: %v", err)
		return
	}

	wantConnect := fmt.Sprintf("CONNECT %d\n", ch.GuestProxyVsockPort)
	if line != wantConnect {
		t.Errorf("vsock connect line = %q, want %q", line, wantConnect)
		return
	}

	if _, err := conn.Write([]byte("OK 1073741824\n")); err != nil {
		t.Errorf("write vsock connect ack: %v", err)
		return
	}

	req, err := http.ReadRequest(reader)
	if err != nil {
		t.Errorf("read proxied HTTP request: %v", err)
		return
	}
	defer func() { _ = req.Body.Close() }()

	assertRequest(req)
	_, _ = io.Copy(io.Discard, req.Body)

	key := req.Header.Get("Sec-WebSocket-Key")

	response := "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: " + websocketAccept(key) + "\r\n\r\n"
	if _, err := conn.Write([]byte(response)); err != nil {
		t.Errorf("write websocket response: %v", err)
		return
	}

	payload, err := reader.ReadString('\n')
	if err != nil {
		t.Errorf("read websocket payload: %v", err)
		return
	}

	if _, err := conn.Write([]byte("guest:" + payload)); err != nil {
		t.Errorf("write websocket payload: %v", err)
	}
}

func handleGuestProxyUpgradeSocket(t *testing.T, conn net.Conn, assertRequest func(*http.Request)) {
	t.Helper()

	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Errorf("read vsock connect line: %v", err)
		return
	}

	wantConnect := fmt.Sprintf("CONNECT %d\n", ch.GuestProxyVsockPort)
	if line != wantConnect {
		t.Errorf("vsock connect line = %q, want %q", line, wantConnect)
		return
	}

	if _, err := conn.Write([]byte("OK 1073741824\n")); err != nil {
		t.Errorf("write vsock connect ack: %v", err)
		return
	}

	req, err := http.ReadRequest(reader)
	if err != nil {
		t.Errorf("read proxied HTTP request: %v", err)
		return
	}
	defer func() { _ = req.Body.Close() }()

	assertRequest(req)
	_, _ = io.Copy(io.Discard, req.Body)

	response := "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + sshtunnel.Protocol + "\r\n\r\n"
	if _, err := conn.Write([]byte(response)); err != nil {
		t.Errorf("write upgrade response: %v", err)
		return
	}

	payload, err := json.Marshal(sshtunnel.ExitStatus{})
	if err != nil {
		t.Errorf("marshal exit status: %v", err)
		return
	}

	if err := sshtunnel.WriteFrame(conn, sshtunnel.FrameExit, payload); err != nil {
		t.Errorf("write exit frame: %v", err)
	}
}

func handleGuestProxySocket(t *testing.T, conn net.Conn, assertRequest func(*http.Request), headers map[string]string, body string) {
	t.Helper()

	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Errorf("read vsock connect line: %v", err)
		return
	}

	wantConnect := fmt.Sprintf("CONNECT %d\n", ch.GuestProxyVsockPort)
	if line != wantConnect {
		t.Errorf("vsock connect line = %q, want %q", line, wantConnect)
		return
	}

	if _, err := conn.Write([]byte("OK 1073741824\n")); err != nil {
		t.Errorf("write vsock connect ack: %v", err)
		return
	}

	req, err := http.ReadRequest(reader)
	if err != nil {
		t.Errorf("read proxied HTTP request: %v", err)
		return
	}
	defer func() { _ = req.Body.Close() }()

	assertRequest(req)
	_, _ = io.Copy(io.Discard, req.Body)

	var response strings.Builder
	response.WriteString("HTTP/1.1 200 OK\r\n")

	for name, value := range headers {
		response.WriteString(name + ": " + value + "\r\n")
	}

	response.WriteString("Content-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n")
	response.WriteString(body)

	_, _ = conn.Write([]byte(response.String()))
}

func decodeTemplateCreateResult(t *testing.T, res *httptest.ResponseRecorder) template.Metadata {
	t.Helper()

	var event template.CreateStreamEvent
	decode(t, res, &event)

	if event.Type != template.StreamEventResult || event.Template == nil {
		t.Fatalf("create template event = %#v, want result", event)
	}

	return *event.Template
}

func documentedTemplateConfig(t *testing.T, path string) json.RawMessage {
	t.Helper()

	contents, err := os.ReadFile(path) //nolint:gosec // Test reads tracked documentation examples.
	if err != nil {
		t.Fatalf("read documented template example %s: %v", path, err)
	}

	const marker = "<<'JSON'\n"

	_, rest, ok := strings.Cut(string(contents), marker)
	if !ok {
		t.Fatalf("documented template example %s missing %q", path, marker)
	}

	config, _, ok := strings.Cut(rest, "\n   JSON\n")
	if !ok {
		t.Fatalf("documented template example %s missing closing heredoc delimiter", path)
	}

	return json.RawMessage(config)
}

//nolint:wsl_v5 // Raw socket handshake tests keep cleanup next to each failing operation.
func openWebSocketUpgrade(t *testing.T, serverURL, requestURI string, headers http.Header) (net.Conn, *bufio.Reader) {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse websocket server URL: %v", err)
	}

	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", parsed.Host)
	if err != nil {
		t.Fatalf("dial websocket server: %v", err)
	}

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		_ = conn.Close()
		t.Fatalf("set websocket deadline: %v", err)
	}

	key := testWebSocketKey
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", requestURI, parsed.Host, key); err != nil {
		_ = conn.Close()
		t.Fatalf("write websocket request: %v", err)
	}

	for name, values := range headers {
		for _, value := range values {
			if _, err := fmt.Fprintf(conn, "%s: %s\r\n", name, value); err != nil {
				_ = conn.Close()
				t.Fatalf("write websocket request header: %v", err)
			}
		}
	}

	if _, err := fmt.Fprint(conn, "\r\n"); err != nil {
		_ = conn.Close()
		t.Fatalf("finish websocket request: %v", err)
	}

	reader := bufio.NewReader(conn)
	//nolint:bodyclose // The caller owns the upgraded raw connection.
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read websocket response: %v", err)
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		t.Fatalf("websocket status = %d, want %d", res.StatusCode, http.StatusSwitchingProtocols)
	}

	if got := res.Header.Get("Upgrade"); !strings.EqualFold(got, "websocket") {
		_ = conn.Close()
		t.Fatalf("websocket Upgrade = %q, want websocket", got)
	}

	if got := res.Header.Get("Sec-WebSocket-Accept"); got != websocketAccept(key) {
		_ = conn.Close()
		t.Fatalf("websocket accept = %q, want valid accept", got)
	}

	return conn, reader
}

func websocketAccept(key string) string {
	if key != testWebSocketKey {
		return ""
	}

	return testWebSocketAccept
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

type templateArchiveOrchestrator struct {
	prepared         []ch.Template
	removed          []string
	importConfig     json.RawMessage
	importErr        error
	importedArchives [][]byte
	importSizes      []int64
}

func (o *templateArchiveOrchestrator) PrepareTemplate(_ context.Context, req ch.PrepareTemplateRequest) (ch.PreparedTemplate, error) {
	o.prepared = append(o.prepared, req.Template)

	return ch.PreparedTemplate{TemplateID: req.Template.ID, BaseContentAddress: "sha256:test-base"}, nil
}

func (o *templateArchiveOrchestrator) RemoveTemplate(_ context.Context, templateID string) (ch.PreparedTemplate, error) {
	o.removed = append(o.removed, templateID)

	return ch.PreparedTemplate{TemplateID: templateID}, nil
}

func (o *templateArchiveOrchestrator) ExportTemplate(_ context.Context, req ch.ExportTemplateRequest) error {
	_, err := io.WriteString(req.Writer, "template-archive")

	return err
}

func (o *templateArchiveOrchestrator) ImportTemplate(_ context.Context, req ch.ImportTemplateRequest) (ch.ImportedTemplate, error) {
	contents, err := io.ReadAll(req.Reader)
	if err != nil {
		return ch.ImportedTemplate{}, err
	}

	if o.importErr != nil {
		return ch.ImportedTemplate{}, o.importErr
	}

	o.importedArchives = append(o.importedArchives, contents)
	o.importSizes = append(o.importSizes, req.ContentLength)

	return ch.ImportedTemplate{Template: ch.Template{ID: req.TemplateID, Config: append(json.RawMessage(nil), o.importConfig...), BaseContentAddress: "sha256:test-base"}}, nil
}

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

type agentProxyOrchestrator struct {
	vms             map[string]ch.VM
	vsockSocketPath string
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

func (o *agentProxyOrchestrator) Launch(_ context.Context, req ch.LaunchRequest) (ch.VM, error) {
	vm := ch.VM{
		EnvironmentID:   req.EnvironmentID,
		VMID:            "vm-" + req.EnvironmentID,
		State:           ch.StateRunning,
		GuestIP:         "127.0.0.1",
		VsockSocketPath: o.vsockSocketPath,
		SSHUser:         ch.SSHUser,
		SSHPort:         ch.SSHPort,
		SSHKeyPath:      "/tmp/test.id_rsa",
	}
	o.vms[req.EnvironmentID] = vm

	return vm, nil
}

func (o *agentProxyOrchestrator) State(_ context.Context, environmentID string) (ch.VM, error) {
	vm, ok := o.vms[environmentID]
	if !ok {
		return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped}, nil
	}

	return vm, nil
}

func (o *agentProxyOrchestrator) Remove(_ context.Context, environmentID string) (ch.VM, error) {
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
