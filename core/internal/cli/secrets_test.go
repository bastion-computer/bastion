package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
)

const (
	cliTestSecretID  = "sec_123"
	cliTestSecretKey = "api-token"
)

func TestSecretsCreateCommandSendsOptionalKeyAndValue(t *testing.T) {
	t.Parallel()

	gotReq := make(chan secret.CreateRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/secrets" {
			t.Fatalf("request = %s %s, want POST /v1/secrets", r.Method, r.URL.Path)
		}

		var req secret.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode create request: %v", err)
		}

		gotReq <- req

		w.WriteHeader(http.StatusCreated)

		if err := json.NewEncoder(w).Encode(secret.Metadata{ID: cliTestSecretID, Key: req.Key}); err != nil {
			t.Fatalf("encode create response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newSecretsCreateCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestKeyFlag, cliTestSecretKey, "--value", "secret-value"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := <-gotReq
	if got.Key == nil || *got.Key != cliTestSecretKey || got.Value != "secret-value" {
		t.Fatalf("create request = %#v, want keyed secret value", got)
	}

	var created secret.Metadata
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if created.ID != cliTestSecretID || created.Key == nil || *created.Key != cliTestSecretKey {
		t.Fatalf("created output = %#v, want keyed secret", created)
	}
}

func TestSecretsCreateCommandRequiresValueFlag(t *testing.T) {
	t.Parallel()

	cmd := newSecretsCreateCommand(&rootOptions{apiURL: "http://127.0.0.1"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestKeyFlag, cliTestSecretKey})

	if err := cmd.Execute(); err == nil {
		t.Fatal("execute error = nil, want missing --value error")
	}
}

func TestSecretsCommandsUseResourcePaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 4)
	secretKey := cliTestSecretKey
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.String())

		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == "/v1/secrets" {
				page := services.Page[secret.Metadata]{Entries: []secret.Metadata{{ID: cliTestSecretID}}}
				if err := json.NewEncoder(w).Encode(page); err != nil {
					t.Fatalf("encode list response: %v", err)
				}

				return
			}

			if err := json.NewEncoder(w).Encode(secret.Secret{ID: cliTestSecretID, Key: &secretKey, Value: "secret-value"}); err != nil {
				t.Fatalf("encode get response: %v", err)
			}
		case http.MethodDelete:
			if err := json.NewEncoder(w).Encode(secret.Metadata{ID: cliTestSecretID}); err != nil {
				t.Fatalf("encode remove response: %v", err)
			}
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	commands := []struct {
		cmd  *cobra.Command
		args []string
	}{
		{cmd: newSecretsListCommand(&rootOptions{apiURL: server.URL}), args: []string{"--limit", "5", "--cursor", cliTestNextCursor}},
		{cmd: newSecretsGetCommand(&rootOptions{apiURL: server.URL}), args: []string{cliTestKeyFlag, cliTestSecretKey}},
		{cmd: newSecretsRemoveCommand(&rootOptions{apiURL: server.URL}), args: []string{cliTestIDFlag, cliTestSecretID}},
	}

	for _, run := range commands {
		var stdout bytes.Buffer
		run.cmd.SetOut(&stdout)
		run.cmd.SetErr(&bytes.Buffer{})
		run.cmd.SetArgs(run.args)

		if err := run.cmd.Execute(); err != nil {
			t.Fatalf("execute %s: %v", run.cmd.Name(), err)
		}
	}

	want := []string{
		"GET /v1/secrets?cursor=next&limit=5",
		"GET /v1/secrets/by-key/api-token",
		"DELETE /v1/secrets/sec_123",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestRootSecretsUsesPersistedAPIURL(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/secrets" {
			t.Fatalf("request = %s %s, want GET /v1/secrets", r.Method, r.URL.Path)
		}

		page := services.Page[secret.Metadata]{Entries: []secret.Metadata{{ID: cliTestSecretID}}}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Fatalf("encode list response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: server.URL})

	var stdout bytes.Buffer

	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestDataDirFlag, dataDir, secretsUse, listUse})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var page services.Page[secret.Metadata]
	if err := json.NewDecoder(&stdout).Decode(&page); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if len(page.Entries) != 1 || page.Entries[0].ID != cliTestSecretID {
		t.Fatalf("list output = %#v, want test secret", page)
	}
}
