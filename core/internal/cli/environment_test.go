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
	cliTestProdTag = "prod"
	cliTestGPUTag  = "gpu"
)

func TestEnvironmentCreateCommandSendsTags(t *testing.T) {
	t.Parallel()

	gotReq := make(chan environment.CreateRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/environments" {
			t.Fatalf("request = %s %s, want POST /v1/environments", r.Method, r.URL.Path)
		}

		var req environment.CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode create request: %v", err)
		}

		gotReq <- req

		w.Header().Set("Content-Type", "application/x-ndjson")

		if err := json.NewEncoder(w).Encode(environment.CreateStreamEvent{
			Type:        environment.StreamEventResult,
			Environment: &environment.Environment{ID: "env_tagged", Status: "running", Tags: req.Tags},
		}); err != nil {
			t.Fatalf("encode create stream: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newEnvironmentCreateCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--template", "dev", "-t", cliTestProdTag, "--tag", cliTestGPUTag})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := <-gotReq
	if got.TemplateKey != "dev" || !slices.Equal(got.Tags, []string{cliTestProdTag, cliTestGPUTag}) {
		t.Fatalf("create request = %#v, want template dev with prod/gpu tags", got)
	}

	var created environment.Environment
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if created.ID != "env_tagged" || !slices.Equal(created.Tags, []string{cliTestProdTag, cliTestGPUTag}) {
		t.Fatalf("created output = %#v, want tagged environment", created)
	}
}

func TestEnvironmentListCommandSendsTagFilters(t *testing.T) {
	t.Parallel()

	gotTags := make(chan []string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments" {
			t.Fatalf("request = %s %s, want GET /v1/environments", r.Method, r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("limit") != "5" || query.Get("cursor") != "next" {
			t.Fatalf("query = %v, want limit and cursor", query)
		}

		gotTags <- query["tag"]

		page := services.Page[environment.Environment]{
			Entries: []environment.Environment{{ID: "env_tagged", Status: "running", Tags: []string{cliTestProdTag, cliTestGPUTag}}},
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
	cmd.SetArgs([]string{"--limit", "5", "--cursor", "next", "-t", cliTestProdTag, "--tag", cliTestGPUTag})

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
