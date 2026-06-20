//nolint:goconst,gocyclo,wsl_v5 // Namespaced tests use table-like fake node handlers with explicit fixtures.
package clusterapi_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/clusterapi"
	"github.com/bastion-computer/bastion/core/internal/services/cluster"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/klauspost/compress/zstd"
)

func TestNamespacedTemplateCreateExportsAndCleansDerivative(t *testing.T) {
	t.Parallel()

	var removedSecrets, removedTemplates atomic.Int64
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/secrets":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(secret.Metadata{ID: "sec_derivative"})
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/secrets/sec_derivative":
			removedSecrets.Add(1)
			_ = json.NewEncoder(w).Encode(secret.Metadata{ID: "sec_derivative"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/templates":
			var req template.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode derivative template request: %v", err)
			}

			if !strings.Contains(string(req.Config), "${{ secret.sec_derivative }}") || strings.Contains(string(req.Config), "API_TOKEN") {
				t.Fatalf("derivative config = %s, want derivative secret reference", req.Config)
			}

			streamTemplateResult(t, w, template.Metadata{ID: "tpl_derivative"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/templates/tpl_derivative/export":
			_, _ = w.Write([]byte("template-archive"))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/templates/tpl_derivative":
			removedTemplates.Add(1)
			_ = json.NewEncoder(w).Encode(template.Template{ID: "tpl_derivative"})
		default:
			t.Fatalf("unexpected node request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(node.Close)

	store := newNamespacedStore(t, node.URL)
	router := newTestRouterWithStore(store)

	createSecretRes := request(t, router, http.MethodPost, "/v1/namespaces/team-a/secrets", secret.CreateRequest{Key: new("API_TOKEN"), Value: "source-secret"})
	if createSecretRes.Code != http.StatusCreated {
		t.Fatalf("create source secret status = %d, want %d; body: %s", createSecretRes.Code, http.StatusCreated, createSecretRes.Body.String())
	}

	originalConfig := json.RawMessage(`{"agents":{"opencode":{"auth":{"anthropic":{"type":"api","key":"${{ secret.API_TOKEN }}"}}}},"actions":{"init":[]}}`)
	createTemplateRes := request(t, router, http.MethodPost, "/v1/namespaces/team-a/templates", template.CreateRequest{Key: new("dev"), Config: originalConfig})
	if createTemplateRes.Code != http.StatusOK {
		t.Fatalf("create source template status = %d, want streaming %d; body: %s", createTemplateRes.Code, http.StatusOK, createTemplateRes.Body.String())
	}

	var event template.CreateStreamEvent
	decode(t, createTemplateRes, &event)
	if event.Type != template.StreamEventResult || event.Template == nil || event.Template.ID == "tpl_derivative" || event.Template.Key == nil || *event.Template.Key != "dev" {
		t.Fatalf("create template event = %#v, want source template result", event)
	}

	getTemplateRes := request(t, router, http.MethodGet, "/v1/namespaces/team-a/templates/by-key/dev", nil)
	if getTemplateRes.Code != http.StatusOK {
		t.Fatalf("get source template status = %d, want %d", getTemplateRes.Code, http.StatusOK)
	}

	var got template.Template
	decode(t, getTemplateRes, &got)
	if string(got.Config) != string(originalConfig) {
		t.Fatalf("stored config = %s, want original %s", got.Config, originalConfig)
	}

	if removedSecrets.Load() != 1 || removedTemplates.Load() != 1 {
		t.Fatalf("cleanup counts = secrets %d templates %d, want 1/1", removedSecrets.Load(), removedTemplates.Load())
	}
}

func TestNamespacedEnvironmentCreateImportsTemplateAndReturnsSourceIDs(t *testing.T) {
	t.Parallel()

	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/templates/import":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read template import body: %v", err)
			}

			if string(body) != "template-archive" {
				t.Fatalf("import archive = %q, want template-archive", body)
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(template.Metadata{ID: "tpl_imported"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/environments":
			var req environment.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode derivative environment request: %v", err)
			}

			if req.TemplateID != "tpl_imported" || req.TemplateKey != "" {
				t.Fatalf("environment request = %#v, want derivative template ID", req)
			}

			streamEnvironmentResult(t, w, environment.Environment{ID: "env_derivative", Status: "running", TemplateID: "tpl_imported", Tags: req.Tags, CreatedAt: "node-created", UpdatedAt: "node-updated"})
		default:
			t.Fatalf("unexpected node request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(node.Close)

	store := newNamespacedStore(t, node.URL)
	archiveStore := clusterapi.NewMemoryArchiveStore()
	if err := archiveStore.Put(context.Background(), "templates/tpl_source.tar.zst", []byte("template-archive")); err != nil {
		t.Fatalf("put template archive: %v", err)
	}

	namespace, err := store.ResolveNamespace(context.Background(), "team-a")
	if err != nil {
		t.Fatalf("resolve namespace: %v", err)
	}

	if _, err := store.CreateTemplate(context.Background(), namespace.ID, template.Template{ID: "tpl_source", Key: new("dev"), Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`), CreatedAt: "source-created"}, "templates/tpl_source.tar.zst"); err != nil {
		t.Fatalf("create source template: %v", err)
	}

	router := clusterapi.NewRouter(store, nil, clusterapi.WithArchiveStore(archiveStore))
	createRes := request(t, router, http.MethodPost, "/v1/namespaces/team-a/environments", environment.CreateRequest{Key: new("review"), TemplateKey: "dev", Tags: []string{"repo:bastion"}})
	if createRes.Code != http.StatusOK {
		t.Fatalf("create source environment status = %d, want streaming %d; body: %s", createRes.Code, http.StatusOK, createRes.Body.String())
	}

	var event environment.CreateStreamEvent
	decode(t, createRes, &event)
	if event.Type != environment.StreamEventResult || event.Environment == nil || event.Environment.ID == "env_derivative" || event.Environment.TemplateID != "tpl_source" || event.Environment.Key == nil || *event.Environment.Key != "review" {
		t.Fatalf("create environment event = %#v, want source environment result", event)
	}
}

func TestNamespacedEnvironmentCreateRewritesImportedTemplateSecretReferences(t *testing.T) {
	t.Parallel()

	var createdSecrets atomic.Int64
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/secrets":
			var req secret.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode derivative secret request: %v", err)
			}

			if req.Value != "source-secret" {
				t.Fatalf("derivative secret value = %q, want source-secret", req.Value)
			}

			createdSecrets.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(secret.Metadata{ID: "sec_node_b"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/templates/import":
			config := readTemplateArchiveConfig(t, r.Body)
			if !strings.Contains(string(config), "${{ secret.sec_node_b }}") || strings.Contains(string(config), "sec_node_a") || strings.Contains(string(config), "API_TOKEN") {
				t.Fatalf("imported template config = %s, want node B derivative secret only", config)
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(template.Metadata{ID: "tpl_imported"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/environments":
			streamEnvironmentResult(t, w, environment.Environment{ID: "env_derivative", Status: "running", TemplateID: "tpl_imported", CreatedAt: "node-created", UpdatedAt: "node-updated"})
		default:
			t.Fatalf("unexpected node request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(node.Close)

	store := newNamespacedStore(t, node.URL)
	archiveStore := clusterapi.NewMemoryArchiveStore()
	archive := makeTemplateArchive(t, json.RawMessage(`{"agents":{"opencode":{"auth":{"anthropic":{"type":"api","key":"${{ secret.sec_node_a }}"}}}},"actions":{"init":[]}}`))
	if err := archiveStore.Put(context.Background(), "templates/tpl_source.tar.zst", archive); err != nil {
		t.Fatalf("put template archive: %v", err)
	}

	namespace, err := store.ResolveNamespace(context.Background(), "team-a")
	if err != nil {
		t.Fatalf("resolve namespace: %v", err)
	}

	secretKey := "API_TOKEN"
	if _, err := store.CreateSecret(context.Background(), namespace.ID, secret.CreateRequest{Key: &secretKey, Value: "source-secret"}); err != nil {
		t.Fatalf("create source secret: %v", err)
	}

	sourceConfig := json.RawMessage(`{"agents":{"opencode":{"auth":{"anthropic":{"type":"api","key":"${{ secret.API_TOKEN }}"}}}},"actions":{"init":[]}}`)
	if _, err := store.CreateTemplate(context.Background(), namespace.ID, template.Template{ID: "tpl_source", Key: new("dev"), Config: sourceConfig, CreatedAt: "source-created"}, "templates/tpl_source.tar.zst"); err != nil {
		t.Fatalf("create source template: %v", err)
	}

	router := clusterapi.NewRouter(store, nil, clusterapi.WithArchiveStore(archiveStore))
	createRes := request(t, router, http.MethodPost, "/v1/namespaces/team-a/environments", environment.CreateRequest{TemplateKey: "dev"})
	if createRes.Code != http.StatusOK {
		t.Fatalf("create source environment status = %d, want streaming %d; body: %s", createRes.Code, http.StatusOK, createRes.Body.String())
	}

	if createdSecrets.Load() != 1 {
		t.Fatalf("created derivative secrets = %d, want 1", createdSecrets.Load())
	}
}

func TestNamespacedEnvironmentAgentProxyForwardsToOwningNode(t *testing.T) {
	t.Parallel()

	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RequestURI() != "/v1/environments/env_derivative/agents/opencode/api?trace=1" {
			t.Fatalf("node request = %s %s, want derivative agent proxy path", r.Method, r.URL.RequestURI())
		}

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("proxied"))
	}))
	t.Cleanup(node.Close)

	store := newNamespacedStore(t, node.URL)
	namespace, err := store.ResolveNamespace(context.Background(), "team-a")
	if err != nil {
		t.Fatalf("resolve namespace: %v", err)
	}

	nodes, err := store.ListNodes(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	if len(nodes.Entries) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes.Entries))
	}

	key := "review"
	if _, err := store.CreateEnvironment(context.Background(), namespace.ID, clusterapi.EnvironmentRecord{
		Environment:             environment.Environment{ID: "env_source", Key: &key, Status: "running", TemplateID: "tpl_source", CreatedAt: "created", UpdatedAt: "updated"},
		NodeID:                  nodes.Entries[0].ID,
		DerivativeTemplateID:    "tpl_derivative",
		DerivativeEnvironmentID: "env_derivative",
	}); err != nil {
		t.Fatalf("create source environment: %v", err)
	}

	router := clusterapi.NewRouter(store, nil)
	res := request(t, router, http.MethodGet, "/v1/namespaces/team-a/environments/by-key/review/agents/opencode/api?trace=1", nil)
	if res.Code != http.StatusAccepted || res.Body.String() != "proxied" {
		t.Fatalf("proxy response = %d %q, want 202 proxied", res.Code, res.Body.String())
	}
}

func newNamespacedStore(t *testing.T, nodeURL string) *clusterapi.MemoryStore {
	t.Helper()

	store := clusterapi.NewMemoryStore()
	if _, err := store.CreateNode(context.Background(), cluster.CreateNodeRequest{APIURL: nodeURL}); err != nil {
		t.Fatalf("create test node: %v", err)
	}

	key := "team-a"
	if _, err := store.CreateNamespace(context.Background(), cluster.CreateNamespaceRequest{Key: &key}); err != nil {
		t.Fatalf("create test namespace: %v", err)
	}

	return store
}

func newTestRouterWithStore(store *clusterapi.MemoryStore) http.Handler {
	return clusterapi.NewRouter(store, nil)
}

func streamTemplateResult(t *testing.T, w http.ResponseWriter, metadata template.Metadata) {
	t.Helper()

	w.Header().Set("Content-Type", "application/x-ndjson")
	_ = json.NewEncoder(w).Encode(template.CreateStreamEvent{Type: template.StreamEventResult, Template: &metadata})
}

func streamEnvironmentResult(t *testing.T, w http.ResponseWriter, env environment.Environment) {
	t.Helper()

	w.Header().Set("Content-Type", "application/x-ndjson")
	_ = json.NewEncoder(w).Encode(environment.CreateStreamEvent{Type: environment.StreamEventResult, Environment: &env})
}

func makeTemplateArchive(t *testing.T, config json.RawMessage) []byte {
	t.Helper()

	var out bytes.Buffer
	zstdWriter, err := zstd.NewWriter(&out, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		t.Fatalf("create archive compressor: %v", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)
	manifest := map[string]any{
		"format": "bastion-template-v1",
		"template": map[string]any{
			"id":     "tpl_archived",
			"config": config,
		},
	}
	contents, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode archive manifest: %v", err)
	}
	contents = append(contents, '\n')

	if err := tarWriter.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(contents))}); err != nil {
		t.Fatalf("write archive manifest header: %v", err)
	}
	if _, err := tarWriter.Write(contents); err != nil {
		t.Fatalf("write archive manifest: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close archive tar: %v", err)
	}
	if err := zstdWriter.Close(); err != nil {
		t.Fatalf("close archive compressor: %v", err)
	}

	return out.Bytes()
}

func readTemplateArchiveConfig(t *testing.T, archive io.Reader) json.RawMessage {
	t.Helper()

	zstdReader, err := zstd.NewReader(archive)
	if err != nil {
		t.Fatalf("create archive reader: %v", err)
	}
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read archive entry: %v", err)
		}
		if header.Name != "manifest.json" {
			continue
		}

		var manifest struct {
			Template struct {
				Config json.RawMessage `json:"config"`
			} `json:"template"`
		}
		if err := json.NewDecoder(tarReader).Decode(&manifest); err != nil {
			t.Fatalf("decode archive manifest: %v", err)
		}

		return manifest.Template.Config
	}

	t.Fatal("archive missing manifest")
	return nil
}
