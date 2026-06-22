package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

const (
	cliTestTemplateKey    = "dev"
	cliTestEnvironmentKey = "dev-env"
	cliTestProdTag        = "prod"
	cliTestGPUTag         = "gpu"
	cliTestRunningStatus  = "running"
)

func TestEnvironmentCreateCommandSendsTags(t *testing.T) {
	t.Parallel()

	gotReq := make(chan environment.CreateRequest, 1)
	server := newEnvironmentCreateTestServer(t, gotReq)
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newEnvironmentCreateCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--template-key", cliTestTemplateKey, cliTestKeyFlag, cliTestEnvironmentKey, "-t", cliTestProdTag, "--tag", cliTestGPUTag})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	assertEnvironmentCreateRequest(t, <-gotReq)

	var created environment.Environment
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	assertCreatedEnvironmentOutput(t, created)
}

func TestEnvironmentGetCommandUsesID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments/"+cliTestEnvironmentID {
			t.Fatalf("request = %s %s, want GET /v1/environments/%s", r.Method, r.URL.Path, cliTestEnvironmentID)
		}

		if err := json.NewEncoder(w).Encode(environment.Environment{ID: cliTestEnvironmentID, Status: cliTestRunningStatus}); err != nil {
			t.Fatalf("encode get response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newEnvironmentGetCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestIDFlag, cliTestEnvironmentID})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var got environment.Environment
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if got.ID != cliTestEnvironmentID {
		t.Fatalf("get output = %#v, want environment ID %s", got, cliTestEnvironmentID)
	}
}

func TestEnvironmentGetCommandUsesKey(t *testing.T) {
	t.Parallel()

	key := cliTestEnvironmentKey

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments/by-key/"+cliTestEnvironmentKey {
			t.Fatalf("request = %s %s, want GET /v1/environments/by-key/%s", r.Method, r.URL.Path, cliTestEnvironmentKey)
		}

		if err := json.NewEncoder(w).Encode(environment.Environment{ID: "env_keyed", Key: &key, Status: cliTestRunningStatus}); err != nil {
			t.Fatalf("encode get response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newEnvironmentGetCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestKeyFlag, cliTestEnvironmentKey})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var got environment.Environment
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if got.ID != "env_keyed" || got.Key == nil || *got.Key != cliTestEnvironmentKey {
		t.Fatalf("get output = %#v, want keyed environment", got)
	}
}

func TestEnvironmentRemoveCommandUsesID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/environments/"+cliTestEnvironmentID {
			t.Fatalf("request = %s %s, want DELETE /v1/environments/%s", r.Method, r.URL.Path, cliTestEnvironmentID)
		}

		if err := json.NewEncoder(w).Encode(environment.Environment{ID: cliTestEnvironmentID, Status: "removed"}); err != nil {
			t.Fatalf("encode remove response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newEnvironmentRemoveCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestIDFlag, cliTestEnvironmentID})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var got environment.Environment
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if got.ID != cliTestEnvironmentID || got.Status != "removed" {
		t.Fatalf("remove output = %#v, want removed environment", got)
	}
}

func TestEnvironmentListCommandSendsTagFilters(t *testing.T) {
	t.Parallel()

	gotTags := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != cliTestEnvironmentsPath {
			t.Fatalf("request = %s %s, want GET %s", r.Method, r.URL.Path, cliTestEnvironmentsPath)
		}

		query := r.URL.Query()
		if query.Get("limit") != "5" || query.Get("cursor") != cliTestNextCursor {
			t.Fatalf("query = %v, want limit and cursor", query)
		}

		gotTags <- query["tag"]

		page := services.Page[environment.Environment]{
			Entries: []environment.Environment{{ID: "env_tagged", Status: cliTestRunningStatus, Tags: []string{cliTestProdTag, cliTestGPUTag}}},
		}

		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Fatalf("encode list response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newEnvironmentListCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestLimitFlag, "5", cliTestCursorFlag, cliTestNextCursor, "-t", cliTestProdTag, "--tag", cliTestGPUTag})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if tags := <-gotTags; !slices.Equal(tags, []string{cliTestProdTag, cliTestGPUTag}) {
		t.Fatalf("tag filters = %#v, want prod/gpu", tags)
	}

	var page services.Page[environment.Environment]
	if err := json.NewDecoder(&stdout).Decode(&page); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if len(page.Entries) != 1 || !slices.Equal(page.Entries[0].Tags, []string{cliTestProdTag, cliTestGPUTag}) {
		t.Fatalf("list output = %#v, want tagged environment", page)
	}
}

func TestEnvironmentTunnelsCommandPrintsTunnelURLs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/environments/"+cliTestEnvironmentID+"/tunnels" {
			t.Fatalf("request = %s %s, want GET /api/v1/environments/%s/tunnels", r.Method, r.URL.Path, cliTestEnvironmentID)
		}

		if err := json.NewEncoder(w).Encode(environment.Tunnels{Entries: []environment.Tunnel{{Name: cliTestTunnelName, Port: 3000}}}); err != nil {
			t.Fatalf("encode tunnels response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newEnvironmentTunnelsCommand(&rootOptions{apiURL: server.URL + "/api/"})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestIDFlag, cliTestEnvironmentID})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var got environment.Tunnels
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	wantURL := server.URL + "/api/v1/environments/" + cliTestEnvironmentID + "/tunnels/" + cliTestTunnelName
	if len(got.Entries) != 1 || got.Entries[0].URL != wantURL {
		t.Fatalf("tunnels output = %#v, want URL %s", got, wantURL)
	}
}

func TestEnvironmentTunnelURLIncludesNamespace(t *testing.T) {
	t.Parallel()

	got := environmentTunnelURL("http://localhost:3150/api/", "env_123", "", "frontend", "ns_123", "")
	want := "http://localhost:3150/api/v1/environments/env_123/tunnels/frontend?namespace-id=ns_123"

	if got != want {
		t.Fatalf("tunnel URL = %q, want %q", got, want)
	}

	got = environmentTunnelURL("http://localhost:3150", "", "dev/env", "frontend", "", "team-a")
	want = "http://localhost:3150/v1/environments/by-key/dev%2Fenv/tunnels/frontend?namespace-key=team-a"

	if got != want {
		t.Fatalf("keyed tunnel URL = %q, want %q", got, want)
	}
}

func TestRootEnvironmentTunnelsUsesPersistedAPIURL(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments/by-key/"+cliTestEnvironmentKey+"/tunnels" {
			t.Fatalf("request = %s %s, want by-key tunnels", r.Method, r.URL.Path)
		}

		if err := json.NewEncoder(w).Encode(environment.Tunnels{Entries: []environment.Tunnel{{Name: cliTestTunnelName, Port: 3000}}}); err != nil {
			t.Fatalf("encode tunnels response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: server.URL})

	var stdout bytes.Buffer

	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestDataDirFlag, dataDir, environmentUse, "tunnels", cliTestKeyFlag, cliTestEnvironmentKey})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var got environment.Tunnels
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	wantURL := server.URL + "/v1/environments/by-key/" + cliTestEnvironmentKey + "/tunnels/" + cliTestTunnelName
	if len(got.Entries) != 1 || got.Entries[0].URL != wantURL {
		t.Fatalf("tunnels output = %#v, want persisted API URL %s", got, wantURL)
	}
}

func newEnvironmentCreateTestServer(t *testing.T, gotReq chan<- environment.CreateRequest) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != cliTestEnvironmentsPath {
			t.Fatalf("request = %s %s, want POST %s", r.Method, r.URL.Path, cliTestEnvironmentsPath)
		}

		var req environment.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode create request: %v", err)
		}

		gotReq <- req

		w.Header().Set("Content-Type", "application/x-ndjson")

		if err := json.NewEncoder(w).Encode(environment.CreateStreamEvent{
			Type:        environment.StreamEventResult,
			Environment: &environment.Environment{ID: "env_tagged", Key: req.Key, Status: cliTestRunningStatus, Tags: req.Tags},
		}); err != nil {
			t.Fatalf("encode create stream: %v", err)
		}
	}))
}

func assertEnvironmentCreateRequest(t *testing.T, got environment.CreateRequest) {
	t.Helper()

	if got.TemplateKey != cliTestTemplateKey || got.Key == nil || *got.Key != cliTestEnvironmentKey || !slices.Equal(got.Tags, []string{cliTestProdTag, cliTestGPUTag}) {
		t.Fatalf("create request = %#v, want template dev with key and prod/gpu tags", got)
	}
}

func assertCreatedEnvironmentOutput(t *testing.T, created environment.Environment) {
	t.Helper()

	if created.ID != "env_tagged" || created.Key == nil || *created.Key != cliTestEnvironmentKey || !slices.Equal(created.Tags, []string{cliTestProdTag, cliTestGPUTag}) {
		t.Fatalf("created output = %#v, want tagged environment", created)
	}
}
