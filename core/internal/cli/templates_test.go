package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/template"
)

const (
	templateCreateTestConfig = `{"actions":{"init":[]}}`
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

		if err := json.NewEncoder(w).Encode(template.Metadata{ID: "tpl_keyed", Key: req.Key}); err != nil {
			t.Fatalf("encode create response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newTemplatesCreateCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--key", templateCreateTestKey, "--config", templateCreateTestConfig})

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

		if err := json.NewEncoder(w).Encode(template.Metadata{ID: "tpl_unkeyed"}); err != nil {
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
