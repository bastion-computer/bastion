package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

const cliTestRemoteAPIURL = "https://bastion.example/api"

func TestRootCommandIncludesClient(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	for _, subcommand := range cmd.Commands() {
		if subcommand.Name() == clientUse && !subcommand.Hidden {
			return
		}
	}

	t.Fatal("root command is missing visible client subcommand")
}

func TestClientSetAPIURLPersistsOverride(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	dataDir := t.TempDir()
	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{clientUse, cliTestDataDirFlag, dataDir, setUse, rootFlagAPIURL, cliTestRemoteAPIURL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := readClientConfigFile(t, dataDir)
	if got.APIURL != cliTestRemoteAPIURL {
		t.Fatalf("apiUrl = %q, want %q", got.APIURL, cliTestRemoteAPIURL)
	}
}

func TestClientRemoveAPIURLClearsOverride(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: cliTestRemoteAPIURL})

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{clientUse, cliTestDataDirFlag, dataDir, removeUse, rootFlagAPIURL})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	contents, err := os.ReadFile(clientConfigTestPath(dataDir))
	if os.IsNotExist(err) {
		return
	}

	if err != nil {
		t.Fatalf("read client config: %v", err)
	}

	var got testClientConfig
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode client config: %v", err)
	}

	if got.APIURL != "" {
		t.Fatalf("apiUrl = %q, want empty", got.APIURL)
	}
}

func TestClientConfigShowsPersistedAPIURL(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: cliTestRemoteAPIURL})

	var stdout bytes.Buffer

	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{clientUse, cliTestDataDirFlag, dataDir, rootOptionSourceConfig})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var got struct {
		DataDir string `json:"dataDir"`
		APIURL  struct {
			Value  string `json:"value"`
			Source string `json:"source"`
		} `json:"apiUrl"`
	}
	if err := json.NewDecoder(&stdout).Decode(&got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if got.DataDir != dataDir {
		t.Fatalf("dataDir = %q, want %q", got.DataDir, dataDir)
	}

	if got.APIURL.Value != cliTestRemoteAPIURL || got.APIURL.Source != "config" {
		t.Fatalf("apiUrl = %#v, want config value %q", got.APIURL, cliTestRemoteAPIURL)
	}
}

func TestClientConfigRejectsInvalidAPIURL(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{clientUse, cliTestDataDirFlag, t.TempDir(), setUse, rootFlagAPIURL, "localhost:3148"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("execute error = nil, want invalid API URL")
	}
}

func TestRootCommandUsesPersistedAPIURL(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	server := newEnvironmentListAPIURLTestServer(t)
	t.Cleanup(server.Close)

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: server.URL})

	runRootEnvironmentListCommand(t, []string{cliTestDataDirFlag, dataDir, environmentUse, listUse})
}

func TestRootCommandEnvironmentAPIURLOverridesPersistedAPIURL(t *testing.T) {
	t.Setenv("BASTION_DATA_DIR", "")

	badServer := newUnexpectedAPIURLTestServer(t)
	t.Cleanup(badServer.Close)

	server := newEnvironmentListAPIURLTestServer(t)
	t.Cleanup(server.Close)
	t.Setenv("BASTION_API_URL", server.URL)

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: badServer.URL})

	runRootEnvironmentListCommand(t, []string{cliTestDataDirFlag, dataDir, environmentUse, listUse})
}

func TestRootCommandFlagAPIURLOverridesEnvironmentAndPersistedAPIURL(t *testing.T) {
	badServer := newUnexpectedAPIURLTestServer(t)
	t.Cleanup(badServer.Close)
	t.Setenv("BASTION_API_URL", badServer.URL)
	t.Setenv("BASTION_DATA_DIR", "")

	server := newEnvironmentListAPIURLTestServer(t)
	t.Cleanup(server.Close)

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: badServer.URL})

	runRootEnvironmentListCommand(t, []string{cliTestDataDirFlag, dataDir, "--" + rootFlagAPIURL, server.URL, environmentUse, listUse})
}

func TestRootCommandDataDirEnvironmentLocatesPersistedAPIURL(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")

	server := newEnvironmentListAPIURLTestServer(t)
	t.Cleanup(server.Close)

	dataDir := t.TempDir()
	t.Setenv("BASTION_DATA_DIR", dataDir)
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: server.URL})

	runRootEnvironmentListCommand(t, []string{environmentUse, listUse})
}

type testClientConfig struct {
	APIURL string `json:"apiUrl,omitempty"`
}

func clientConfigTestPath(dataDir string) string {
	return filepath.Join(dataDir, "client.json")
}

func readClientConfigFile(t *testing.T, dataDir string) testClientConfig {
	t.Helper()

	contents, err := os.ReadFile(clientConfigTestPath(dataDir))
	if err != nil {
		t.Fatalf("read client config: %v", err)
	}

	var got testClientConfig
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode client config: %v", err)
	}

	return got
}

func writeClientConfigFile(t *testing.T, dataDir string, cfg testClientConfig) {
	t.Helper()

	contents, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("encode client config: %v", err)
	}

	if err := os.WriteFile(clientConfigTestPath(dataDir), contents, 0o600); err != nil {
		t.Fatalf("write client config: %v", err)
	}
}

func newEnvironmentListAPIURLTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != cliTestEnvironmentsPath {
			t.Fatalf("request = %s %s, want GET %s", r.Method, r.URL.Path, cliTestEnvironmentsPath)
		}

		page := services.Page[environment.Environment]{Entries: []environment.Environment{{ID: cliTestEnvironmentID, Status: cliTestRunningStatus}}}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Fatalf("encode list response: %v", err)
		}
	}))
}

func newUnexpectedAPIURLTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request to overridden API URL: %s %s", r.Method, r.URL.Path)
	}))
}

func runRootEnvironmentListCommand(t *testing.T, args []string) {
	t.Helper()

	var stdout bytes.Buffer

	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var page services.Page[environment.Environment]
	if err := json.NewDecoder(&stdout).Decode(&page); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if len(page.Entries) != 1 || page.Entries[0].ID != cliTestEnvironmentID {
		t.Fatalf("list output = %#v, want test environment", page)
	}
}
