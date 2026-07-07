//nolint:wsl_v5 // Tests keep request setup and assertions close together.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/base"
)

const clientBaseTestNow = "now"

func TestBuildBaseStreamsLogsAndForceFlag(t *testing.T) {
	t.Parallel()

	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	if err := encoder.Encode(base.StreamEvent{Type: base.StreamEventLog, Log: "building base\n"}); err != nil {
		t.Fatalf("encode log event: %v", err)
	}

	if err := encoder.Encode(base.StreamEvent{Type: base.StreamEventResult, Base: &base.Base{ContentAddress: "sha256:test", CreatedAt: clientBaseTestNow, UpdatedAt: clientBaseTestNow}}); err != nil {
		t.Fatalf("encode result event: %v", err)
	}

	client := &Client{
		baseURL: clientTestBaseURL,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.Path != "/v1/base/build" || req.URL.Query().Get("force") != "true" {
				t.Fatalf("request = %s %s?%s, want force base build", req.Method, req.URL.Path, req.URL.RawQuery)
			}

			if req.Header.Get("Accept") != "application/x-ndjson" {
				t.Fatalf("Accept = %q, want application/x-ndjson", req.Header.Get("Accept"))
			}

			return &http.Response{StatusCode: http.StatusOK, Status: clientTestOKStatus, Body: io.NopCloser(bytes.NewReader(body.Bytes()))}, nil
		})},
	}

	var logs bytes.Buffer
	built, err := client.BuildBase(context.Background(), base.BuildRequest{Force: true, Logs: &logs})
	if err != nil {
		t.Fatalf("build base: %v", err)
	}

	if built.ContentAddress != "sha256:test" {
		t.Fatalf("built base = %#v, want content address", built)
	}

	if logs.String() != "building base\n" {
		t.Fatalf("logs = %q, want streamed log", logs.String())
	}
}

//nolint:gocyclo // Verifies get/export/import path behavior in one table-like HTTP fixture.
func TestBaseGetImportExportUseGlobalPath(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 3)
	client := &Client{
		baseURL:      clientTestBaseURL,
		namespaceID:  "ns_ignored",
		namespaceKey: "",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.Method+" "+req.URL.String())

			switch {
			case req.Method == http.MethodGet && req.URL.Path == "/v1/base":
				return clientJSONResponse(http.StatusOK, clientTestOKStatus, `{"contentAddress":"sha256:test","createdAt":"`+clientBaseTestNow+`","updatedAt":"`+clientBaseTestNow+`"}`), nil
			case req.Method == http.MethodGet && req.URL.Path == "/v1/base/export":
				if req.Header.Get("Accept") != base.ArchiveContentType {
					t.Fatalf("Accept = %q, want %q", req.Header.Get("Accept"), base.ArchiveContentType)
				}

				return &http.Response{StatusCode: http.StatusOK, Status: clientTestOKStatus, Body: io.NopCloser(bytes.NewBufferString("base-archive"))}, nil
			case req.Method == http.MethodPost && req.URL.Path == "/v1/base/import" && req.URL.Query().Get("force") == "true":
				if req.Header.Get("Content-Type") != base.ArchiveContentType {
					t.Fatalf("Content-Type = %q, want %q", req.Header.Get("Content-Type"), base.ArchiveContentType)
				}

				var response bytes.Buffer
				if err := json.NewEncoder(&response).Encode(base.StreamEvent{Type: base.StreamEventResult, Base: &base.Base{ContentAddress: "sha256:test", CreatedAt: clientBaseTestNow, UpdatedAt: clientBaseTestNow}}); err != nil {
					t.Fatalf("encode import stream: %v", err)
				}

				return &http.Response{StatusCode: http.StatusOK, Status: clientTestOKStatus, Body: io.NopCloser(&response)}, nil
			default:
				t.Fatalf("unexpected request %s %s?%s", req.Method, req.URL.Path, req.URL.RawQuery)

				return nil, nil
			}
		})},
	}

	if _, err := client.GetBase(context.Background()); err != nil {
		t.Fatalf("get base: %v", err)
	}

	var exported bytes.Buffer
	if err := client.ExportBase(context.Background(), &exported); err != nil {
		t.Fatalf("export base: %v", err)
	}

	if exported.String() != "base-archive" {
		t.Fatalf("exported = %q, want archive", exported.String())
	}

	if _, err := client.ImportBase(context.Background(), base.ImportRequest{Force: true, Archive: strings.NewReader("base-archive"), ArchiveSize: int64(len("base-archive"))}); err != nil {
		t.Fatalf("import base: %v", err)
	}

	want := []string{
		"GET http://bastion.test/v1/base",
		"GET http://bastion.test/v1/base/export",
		"POST http://bastion.test/v1/base/import?force=true",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}
