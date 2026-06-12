package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	proxyTestRequestBody  = "request body"
	proxyTestResponseBody = "proxied response"
)

type proxyUpstreamRequest struct {
	method     string
	requestURI string
	host       string
	body       string
	testHeader string
}

func TestRootCommandIncludesProxy(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	for _, subcommand := range cmd.Commands() {
		if subcommand.Name() == proxyUse && !subcommand.Hidden {
			return
		}
	}

	t.Fatal("root command is missing visible proxy subcommand")
}

func TestProxyCommandValidatesRequiredFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantError string
	}{
		{
			name:      "environment reference",
			args:      []string{"--name", cliTestTunnelName},
			wantError: "specify exactly one of --env-id or --env-key",
		},
		{
			name:      "name",
			args:      []string{"--env-id", cliTestEnvironmentID},
			wantError: "--name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := newProxyCommandWithRunner(&rootOptions{apiURL: "http://localhost:3148"}, func(context.Context, io.Writer, proxyOptions) error {
				t.Fatal("proxy runner should not be called")

				return nil
			})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("execute error = %v, want %s", err, tt.wantError)
			}
		})
	}
}

func TestProxyCommandPassesOptions(t *testing.T) {
	t.Parallel()

	gotOptions := make(chan proxyOptions, 1)
	cmd := newProxyCommandWithRunner(&rootOptions{apiURL: "http://localhost:3148/api/"}, func(_ context.Context, _ io.Writer, opts proxyOptions) error {
		gotOptions <- opts

		return nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--env-key", "feature/dev", "--name", cliTestTunnelName, "--port", "43210"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case got := <-gotOptions:
		if got.apiURL != "http://localhost:3148/api/" || got.environmentKey != "feature/dev" || got.name != cliTestTunnelName || got.port != 43210 {
			t.Fatalf("proxy options = %#v, want keyed frontend proxy on port 43210", got)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy runner was not called")
	}
}

func TestProxyHandlerForwardsRequestsToTunnelURL(t *testing.T) {
	t.Parallel()

	gotRequest := make(chan proxyUpstreamRequest, 1)
	upstream := newProxyForwardingUpstream(t, gotRequest)
	t.Cleanup(upstream.Close)

	var logs bytes.Buffer

	proxy := httptest.NewServer(newProxyHandler(mustParseProxyTarget(t, environmentTunnelURL(upstream.URL+"/api/", cliTestEnvironmentID, "", cliTestTunnelName)), &logs))
	t.Cleanup(proxy.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, proxy.URL+"/assets/app.js?mode=dev", strings.NewReader(proxyTestRequestBody))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req.Header.Set("X-Test-Proxy", "yes")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("call proxy: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	assertProxyResponse(t, res)

	select {
	case got := <-gotRequest:
		assertProxyUpstreamRequest(t, got, strings.TrimPrefix(upstream.URL, "http://"))
	case <-time.After(time.Second):
		t.Fatal("upstream was not called")
	}

	if !strings.Contains(logs.String(), "POST /assets/app.js?mode=dev -> 201") {
		t.Fatalf("proxy logs = %q, want request log", logs.String())
	}
}

func newProxyForwardingUpstream(t *testing.T, gotRequest chan<- proxyUpstreamRequest) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}

		gotRequest <- proxyUpstreamRequest{
			method:     r.Method,
			requestURI: r.RequestURI,
			host:       r.Host,
			body:       string(body),
			testHeader: r.Header.Get("X-Test-Proxy"),
		}

		w.Header().Set("X-Upstream", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(proxyTestResponseBody))
	}))
}

func assertProxyUpstreamRequest(t *testing.T, got proxyUpstreamRequest, wantHost string) {
	t.Helper()

	wantURI := "/api/v1/environments/" + cliTestEnvironmentID + "/tunnel/" + cliTestTunnelName + "/assets/app.js?mode=dev"
	if got.method != http.MethodPost || got.requestURI != wantURI {
		t.Fatalf("upstream request = %s %s, want POST %s", got.method, got.requestURI, wantURI)
	}

	if got.host != wantHost {
		t.Fatalf("upstream host = %q, want API host", got.host)
	}

	if got.body != proxyTestRequestBody {
		t.Fatalf("upstream body = %q, want %s", got.body, proxyTestRequestBody)
	}

	if got.testHeader != "yes" {
		t.Fatalf("upstream X-Test-Proxy = %q, want yes", got.testHeader)
	}
}

func assertProxyResponse(t *testing.T, res *http.Response) {
	t.Helper()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("proxy status = %d, want %d", res.StatusCode, http.StatusCreated)
	}

	if res.Header.Get("X-Upstream") != "ok" {
		t.Fatalf("proxy X-Upstream = %q, want ok", res.Header.Get("X-Upstream"))
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read proxy body: %v", err)
	}

	if string(body) != proxyTestResponseBody {
		t.Fatalf("proxy body = %q, want %s", body, proxyTestResponseBody)
	}
}

func mustParseProxyTarget(t *testing.T, value string) *url.URL {
	t.Helper()

	parsed, err := parseProxyTarget(value)
	if err != nil {
		t.Fatalf("parse proxy target: %v", err)
	}

	return parsed
}
