//nolint:wsl_v5 // Tests keep server fixtures and command assertions close together.
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/base"
)

func TestBaseBuildCommandPassesForce(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/base/build" || r.URL.Query().Get("force") != "true" {
			t.Fatalf("request = %s %s?%s, want force base build", r.Method, r.URL.Path, r.URL.RawQuery)
		}

		if err := json.NewEncoder(w).Encode(base.StreamEvent{Type: base.StreamEventLog, Log: "booting base\n"}); err != nil {
			t.Fatalf("encode log event: %v", err)
		}

		if err := json.NewEncoder(w).Encode(base.StreamEvent{Type: base.StreamEventResult, Base: &base.Base{ContentAddress: "sha256:test", CreatedAt: cliTestNow, UpdatedAt: cliTestNow}}); err != nil {
			t.Fatalf("encode result event: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := newBaseBuildCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--force"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var built base.Base
	if err := json.NewDecoder(&stdout).Decode(&built); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if built.ContentAddress != "sha256:test" {
		t.Fatalf("built = %#v, want content address", built)
	}

	if stderr.String() != "booting base\n" {
		t.Fatalf("stderr = %q, want streamed logs", stderr.String())
	}
}

//nolint:gocyclo // Verifies request construction, upload body, progress, and export output in one command fixture.
func TestBaseImportAndExportCommandsUseArchivePaths(t *testing.T) {
	t.Parallel()

	archivePath := filepath.Join(t.TempDir(), "base.tar.zst")
	if err := os.WriteFile(archivePath, []byte("base-archive"), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/base/import" && r.URL.Query().Get("force") == "true":
			if r.Header.Get("Content-Type") != base.ArchiveContentType {
				t.Fatalf("Content-Type = %q, want %q", r.Header.Get("Content-Type"), base.ArchiveContentType)
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}

			if string(body) != "base-archive" {
				t.Fatalf("body = %q, want archive", body)
			}

			if err := json.NewEncoder(w).Encode(base.StreamEvent{Type: base.StreamEventResult, Base: &base.Base{ContentAddress: "sha256:imported", CreatedAt: cliTestNow, UpdatedAt: cliTestNow}}); err != nil {
				t.Fatalf("encode import response: %v", err)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/base/export":
			if r.Header.Get("Accept") != base.ArchiveContentType {
				t.Fatalf("Accept = %q, want %q", r.Header.Get("Accept"), base.ArchiveContentType)
			}

			_, _ = io.WriteString(w, "exported-base")
		default:
			t.Fatalf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	}))
	t.Cleanup(server.Close)

	var importStdout bytes.Buffer
	var importStderr bytes.Buffer
	importCmd := newBaseImportCommand(&rootOptions{apiURL: server.URL})
	importCmd.SetOut(&importStdout)
	importCmd.SetErr(&importStderr)
	importCmd.SetArgs([]string{"--file", archivePath, "--force"})

	if err := importCmd.Execute(); err != nil {
		t.Fatalf("import execute: %v", err)
	}

	if !strings.Contains(importStderr.String(), "bastion: importing base [") {
		t.Fatalf("import progress missing: %s", importStderr.String())
	}

	var imported base.Base
	if err := json.NewDecoder(&importStdout).Decode(&imported); err != nil {
		t.Fatalf("decode import stdout: %v", err)
	}

	if imported.ContentAddress != "sha256:imported" {
		t.Fatalf("imported = %#v, want imported base", imported)
	}

	var exportStdout bytes.Buffer
	var exportStderr bytes.Buffer
	exportCmd := newBaseExportCommand(&rootOptions{apiURL: server.URL})
	exportCmd.SetOut(&exportStdout)
	exportCmd.SetErr(&exportStderr)

	if err := exportCmd.Execute(); err != nil {
		t.Fatalf("export execute: %v", err)
	}

	if exportStdout.String() != "exported-base" {
		t.Fatalf("export stdout = %q, want archive", exportStdout.String())
	}

	if !strings.Contains(exportStderr.String(), "bastion: exporting base [") {
		t.Fatalf("export progress missing: %s", exportStderr.String())
	}
}
