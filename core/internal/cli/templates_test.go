package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/template"
)

const (
	templateCreateTestConfig = `{"agents":{"opencode":{}},"actions":{"init":[]}}`
	templateCreateTestKey    = "dev"
)

func TestTemplatesCreateCommandSendsOptionalKey(t *testing.T) {
	t.Parallel()

	gotReq := make(chan template.CreateRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/templates" {
			t.Fatalf("request = %s %s, want POST /v1/templates", r.Method, r.URL.Path)
		}

		var req template.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode create request: %v", err)
		}

		gotReq <- req

		if err := json.NewEncoder(w).Encode(template.CreateStreamEvent{Type: template.StreamEventResult, Template: &template.Metadata{ID: "tpl_keyed", Key: req.Key}}); err != nil {
			t.Fatalf("encode create response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newTemplatesCreateCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestKeyFlag, templateCreateTestKey, "--config", templateCreateTestConfig})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := <-gotReq
	if got.Key == nil || *got.Key != templateCreateTestKey || string(got.Config) != templateCreateTestConfig {
		t.Fatalf("create request = %#v, want keyed template config", got)
	}

	var created template.Metadata
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if created.ID != "tpl_keyed" || created.Key == nil || *created.Key != templateCreateTestKey {
		t.Fatalf("created output = %#v, want keyed template", created)
	}
}

func TestTemplatesCreateCommandOmitsAbsentKey(t *testing.T) {
	t.Parallel()

	gotReq := make(chan template.CreateRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/templates" {
			t.Fatalf("request = %s %s, want POST /v1/templates", r.Method, r.URL.Path)
		}

		var req template.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode create request: %v", err)
		}

		gotReq <- req

		if err := json.NewEncoder(w).Encode(template.CreateStreamEvent{Type: template.StreamEventResult, Template: &template.Metadata{ID: "tpl_unkeyed"}}); err != nil {
			t.Fatalf("encode create response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newTemplatesCreateCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", templateCreateTestConfig})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := <-gotReq
	if got.Key != nil || string(got.Config) != templateCreateTestConfig {
		t.Fatalf("create request = %#v, want unkeyed template config", got)
	}

	var created template.Metadata
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if created.ID != "tpl_unkeyed" || created.Key != nil {
		t.Fatalf("created output = %#v, want unkeyed template", created)
	}
}

func TestTemplatesExportCommandWritesArchive(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/templates/by-key/"+templateCreateTestKey+"/export" {
			t.Fatalf("request = %s %s, want GET template export by key", r.Method, r.URL.Path)
		}

		if r.Header.Get("Accept") != template.ArchiveContentType {
			t.Fatalf("Accept = %q, want %q", r.Header.Get("Accept"), template.ArchiveContentType)
		}

		_, _ = io.WriteString(w, "template-archive")
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newTemplatesExportCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestKeyFlag, templateCreateTestKey})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if stdout.String() != "template-archive" {
		t.Fatalf("stdout = %q, want archive bytes", stdout.String())
	}
}

//nolint:gocyclo // Verifies request construction, upload body, and CLI output in one command test.
func TestTemplatesImportCommandUploadsArchiveFile(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "template.tar.gz")
	if err := os.WriteFile(archivePath, []byte("template-archive"), 0o600); err != nil {
		t.Fatalf("write archive file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/templates/import" || r.URL.Query().Get("key") != templateCreateTestKey {
			t.Fatalf("request = %s %s?%s, want keyed template import", r.Method, r.URL.Path, r.URL.RawQuery)
		}

		if r.Header.Get("Content-Type") != template.ArchiveContentType {
			t.Fatalf("Content-Type = %q, want %q", r.Header.Get("Content-Type"), template.ArchiveContentType)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		if string(body) != "template-archive" {
			t.Fatalf("body = %q, want archive", body)
		}

		w.WriteHeader(http.StatusCreated)

		key := templateCreateTestKey
		if err := json.NewEncoder(w).Encode(template.Metadata{ID: "tpl_restored", Key: &key}); err != nil {
			t.Fatalf("encode import response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newTemplatesImportCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestKeyFlag, templateCreateTestKey, "--file", archivePath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var imported template.Metadata
	if err := json.NewDecoder(&stdout).Decode(&imported); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if imported.ID != "tpl_restored" || imported.Key == nil || *imported.Key != templateCreateTestKey {
		t.Fatalf("import output = %#v, want restored template", imported)
	}
}
