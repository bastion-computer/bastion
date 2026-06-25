package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	proxyTestRequestBody  = "request body"
	proxyTestResponseBody = "proxied response"
	proxyNameFlag         = "--name"
	testWebSocketKey      = "dGhlIHNhbXBsZSBub25jZQ=="
	testWebSocketAccept   = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
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
			args:      []string{proxyNameFlag, cliTestTunnelName},
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
	cmd.SetArgs([]string{"--env-key", "feature/dev", proxyNameFlag, cliTestTunnelName, "--host", "0.0.0.0", "--port", "43210"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case got := <-gotOptions:
		if got.apiURL != "http://localhost:3148/api/" || got.environmentKey != "feature/dev" || got.name != cliTestTunnelName || got.host != "0.0.0.0" || got.port != 43210 || !got.portSet {
			t.Fatalf("proxy options = %#v, want keyed frontend proxy on 0.0.0.0:43210", got)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy runner was not called")
	}
}

func TestProxyCommandMarksExplicitPortZero(t *testing.T) {
	t.Parallel()

	gotOptions := make(chan proxyOptions, 1)
	cmd := newProxyCommandWithRunner(&rootOptions{apiURL: "http://localhost:3148"}, func(_ context.Context, _ io.Writer, opts proxyOptions) error {
		gotOptions <- opts

		return nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--env-id", cliTestEnvironmentID, proxyNameFlag, cliTestTunnelName, "--port", "0"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case got := <-gotOptions:
		if got.port != 0 || !got.portSet {
			t.Fatalf("proxy port options = port %d, set %t; want explicit random port", got.port, got.portSet)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy runner was not called")
	}
}

func TestProxyCommandDefaultsToLocalhost(t *testing.T) {
	t.Parallel()

	gotOptions := make(chan proxyOptions, 1)
	cmd := newProxyCommandWithRunner(&rootOptions{apiURL: "http://localhost:3148"}, func(_ context.Context, _ io.Writer, opts proxyOptions) error {
		gotOptions <- opts

		return nil
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--env-id", cliTestEnvironmentID, proxyNameFlag, cliTestTunnelName})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case got := <-gotOptions:
		if got.host != "localhost" {
			t.Fatalf("proxy host = %q, want localhost", got.host)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy runner was not called")
	}
}

func TestRunProxyPrintsLocalhostURL(t *testing.T) {
	t.Parallel()

	server := newProxyTunnelValidationServer(t)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stderr bytes.Buffer

	errCh := make(chan error, 1)

	go func() {
		errCh <- runProxy(ctx, &stderr, proxyOptions{
			apiURL:        server.URL,
			environmentID: cliTestEnvironmentID,
			name:          cliTestTunnelName,
			host:          "localhost",
			portSet:       true,
		})
	}()

	localURL := waitForProxyListeningURL(t, &stderr)
	if !strings.HasPrefix(localURL, "http://localhost:") {
		t.Fatalf("proxy URL = %q, want localhost URL", localURL)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run proxy: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop after context cancellation")
	}
}

func TestRunProxyDefaultsToTunnelPortWhenPortOmitted(t *testing.T) {
	t.Parallel()

	tunnelPort := unusedTCPPort(t, "127.0.0.1")
	server := newProxyTunnelValidationServerWithPort(t, tunnelPort)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stderr bytes.Buffer

	errCh := make(chan error, 1)

	go func() {
		errCh <- runProxy(ctx, &stderr, proxyOptions{
			apiURL:        server.URL,
			environmentID: cliTestEnvironmentID,
			name:          cliTestTunnelName,
			host:          "127.0.0.1",
		})
	}()

	localURL := waitForProxyListeningURL(t, &stderr)
	assertProxyURLPort(t, localURL, tunnelPort)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run proxy: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop after context cancellation")
	}
}

func TestRunProxyFallsBackToRandomPortWhenTunnelPortUnavailable(t *testing.T) {
	t.Parallel()

	occupied := occupyTCPPort(t, "127.0.0.1")
	defer func() { _ = occupied.Close() }()

	tunnelPort := tcpListenerPort(t, occupied)
	server := newProxyTunnelValidationServerWithPort(t, tunnelPort)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stderr bytes.Buffer

	errCh := make(chan error, 1)

	go func() {
		errCh <- runProxy(ctx, &stderr, proxyOptions{
			apiURL:        server.URL,
			environmentID: cliTestEnvironmentID,
			name:          cliTestTunnelName,
			host:          "127.0.0.1",
		})
	}()

	localURL := waitForProxyListeningURL(t, &stderr)
	if got := proxyURLPort(t, localURL); got == tunnelPort {
		t.Fatalf("proxy port = %d, want fallback away from occupied tunnel port", got)
	}

	wantLog := fmt.Sprintf("proxy port %d is unavailable; using a random port instead", tunnelPort)
	if !strings.Contains(stderr.String(), wantLog) {
		t.Fatalf("proxy logs = %q, want fallback log %q", stderr.String(), wantLog)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run proxy: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop after context cancellation")
	}
}

func TestRunProxyExplicitPortZeroKeepsRandomPortBehavior(t *testing.T) {
	t.Parallel()

	occupied := occupyTCPPort(t, "127.0.0.1")
	defer func() { _ = occupied.Close() }()

	tunnelPort := tcpListenerPort(t, occupied)
	server := newProxyTunnelValidationServerWithPort(t, tunnelPort)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stderr bytes.Buffer

	errCh := make(chan error, 1)

	go func() {
		errCh <- runProxy(ctx, &stderr, proxyOptions{
			apiURL:        server.URL,
			environmentID: cliTestEnvironmentID,
			name:          cliTestTunnelName,
			host:          "127.0.0.1",
			port:          0,
			portSet:       true,
		})
	}()

	localURL := waitForProxyListeningURL(t, &stderr)
	if got := proxyURLPort(t, localURL); got == tunnelPort {
		t.Fatalf("proxy port = %d, want explicit random port away from occupied tunnel port", got)
	}

	if strings.Contains(stderr.String(), "using a random port instead") {
		t.Fatalf("proxy logs = %q, want no tunnel-port fallback log for explicit --port 0", stderr.String())
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run proxy: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop after context cancellation")
	}
}

func TestProxyHandlerForwardsRequestsToTunnelURL(t *testing.T) {
	t.Parallel()

	gotRequest := make(chan proxyUpstreamRequest, 1)
	upstream := newProxyForwardingUpstream(t, gotRequest)
	t.Cleanup(upstream.Close)

	var logs bytes.Buffer

	proxy := httptest.NewServer(newProxyHandler(mustParseProxyTarget(t, environmentTunnelURL(upstream.URL+"/api/", cliTestEnvironmentID, "", cliTestTunnelName, "ns_123", "")), &logs))
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

func TestProxyHandlerForwardsWebSocketUpgradesToTunnelURL(t *testing.T) {
	t.Parallel()

	gotRequest := make(chan proxyUpstreamRequest, 1)
	upstream := newProxyUpgradeUpstream(t, gotRequest)
	t.Cleanup(upstream.Close)

	var logs bytes.Buffer

	proxy := httptest.NewServer(newProxyHandler(mustParseProxyTarget(t, environmentTunnelURL(upstream.URL+"/api/", cliTestEnvironmentID, "", cliTestTunnelName, "ns_123", "")), &logs))
	t.Cleanup(proxy.Close)

	conn, reader := openWebSocketUpgrade(t, proxy.URL, "/hmr?token=abc", http.Header{"X-Test-Proxy": []string{"yes"}})
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write upgraded payload: %v", err)
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read upgraded payload: %v", err)
	}

	if line != "echo:ping\n" {
		t.Fatalf("upgraded payload = %q, want echo", line)
	}

	_ = conn.Close()

	select {
	case got := <-gotRequest:
		assertProxyWebSocketUpstreamRequest(t, got, strings.TrimPrefix(upstream.URL, "http://"))
	case <-time.After(time.Second):
		t.Fatal("upstream was not called")
	}

	if !waitForProxyLog(&logs, "GET /hmr?token=abc -> 101") {
		t.Fatalf("proxy logs = %q, want websocket upgrade log", logs.String())
	}
}

func waitForProxyLog(logs *bytes.Buffer, want string) bool {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), want) {
			return true
		}

		time.Sleep(10 * time.Millisecond)
	}

	return false
}

func waitForProxyListeningURL(t *testing.T, logs *bytes.Buffer) string {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for line := range strings.SplitSeq(logs.String(), "\n") {
			if localURL, ok := strings.CutPrefix(line, "proxy listening on "); ok {
				return localURL
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("proxy did not report a local URL: %s", logs.String())

	return ""
}

func newProxyTunnelValidationServer(t *testing.T) *httptest.Server {
	t.Helper()

	return newProxyTunnelValidationServerWithPort(t, 3000)
}

func newProxyTunnelValidationServerWithPort(t *testing.T, port int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/environments/"+cliTestEnvironmentID+"/tunnels" {
			t.Fatalf("request = %s %s, want GET environment tunnels", r.Method, r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"entries":[{"name":%q,"port":%d}]}`, cliTestTunnelName, port)
	}))
}

func unusedTCPPort(t *testing.T, host string) int {
	t.Helper()

	listener := occupyTCPPort(t, host)
	defer func() { _ = listener.Close() }()

	return tcpListenerPort(t, listener)
}

func occupyTCPPort(t *testing.T, host string) net.Listener {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatalf("listen on free TCP port: %v", err)
	}

	return listener
}

func tcpListenerPort(t *testing.T, listener net.Listener) int {
	t.Helper()

	_, portValue, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}

	port, err := strconv.Atoi(portValue)
	if err != nil {
		t.Fatalf("parse listener port: %v", err)
	}

	return port
}

func assertProxyURLPort(t *testing.T, value string, want int) {
	t.Helper()

	if got := proxyURLPort(t, value); got != want {
		t.Fatalf("proxy port = %d, want %d", got, want)
	}
}

func proxyURLPort(t *testing.T, value string) int {
	t.Helper()

	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse proxy URL port: %v", err)
	}

	return port
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

func newProxyUpgradeUpstream(t *testing.T, gotRequest chan<- proxyUpstreamRequest) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest <- proxyUpstreamRequest{
			method:     r.Method,
			requestURI: r.RequestURI,
			host:       r.Host,
			testHeader: r.Header.Get("X-Test-Proxy"),
		}

		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			t.Errorf("upstream Upgrade = %q, want websocket", r.Header.Get("Upgrade"))
			return
		}

		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack upstream websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		key := r.Header.Get("Sec-WebSocket-Key")
		if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: " + websocketAccept(key) + "\r\n\r\n"); err != nil {
			t.Errorf("write upstream websocket response: %v", err)
			return
		}

		if err := rw.Flush(); err != nil {
			t.Errorf("flush upstream websocket response: %v", err)
			return
		}

		line, err := rw.ReadString('\n')
		if err != nil {
			t.Errorf("read upstream websocket payload: %v", err)
			return
		}

		if _, err := rw.WriteString("echo:" + line); err != nil {
			t.Errorf("write upstream websocket payload: %v", err)
			return
		}

		if err := rw.Flush(); err != nil {
			t.Errorf("flush upstream websocket payload: %v", err)
		}
	}))
}

func assertProxyUpstreamRequest(t *testing.T, got proxyUpstreamRequest, wantHost string) {
	t.Helper()

	wantURI := "/api/v1/environments/" + cliTestEnvironmentID + "/tunnels/" + cliTestTunnelName + "/assets/app.js?namespace-id=ns_123&mode=dev"
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

func assertProxyWebSocketUpstreamRequest(t *testing.T, got proxyUpstreamRequest, wantHost string) {
	t.Helper()

	wantURI := "/api/v1/environments/" + cliTestEnvironmentID + "/tunnels/" + cliTestTunnelName + "/hmr?namespace-id=ns_123&token=abc"
	if got.method != http.MethodGet || got.requestURI != wantURI {
		t.Fatalf("upstream websocket request = %s %s, want GET %s", got.method, got.requestURI, wantURI)
	}

	if got.host != wantHost {
		t.Fatalf("upstream websocket host = %q, want API host", got.host)
	}

	if got.testHeader != "yes" {
		t.Fatalf("upstream websocket X-Test-Proxy = %q, want yes", got.testHeader)
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

//nolint:wsl_v5 // Raw socket handshake tests keep cleanup next to each failing operation.
func openWebSocketUpgrade(t *testing.T, serverURL, requestURI string, headers http.Header) (net.Conn, *bufio.Reader) {
	t.Helper()

	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse websocket server URL: %v", err)
	}

	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", parsed.Host)
	if err != nil {
		t.Fatalf("dial websocket server: %v", err)
	}

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		_ = conn.Close()
		t.Fatalf("set websocket deadline: %v", err)
	}

	key := testWebSocketKey
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n", requestURI, parsed.Host, key); err != nil {
		_ = conn.Close()
		t.Fatalf("write websocket request: %v", err)
	}

	for name, values := range headers {
		for _, value := range values {
			if _, err := fmt.Fprintf(conn, "%s: %s\r\n", name, value); err != nil {
				_ = conn.Close()
				t.Fatalf("write websocket request header: %v", err)
			}
		}
	}

	if _, err := fmt.Fprint(conn, "\r\n"); err != nil {
		_ = conn.Close()
		t.Fatalf("finish websocket request: %v", err)
	}

	reader := bufio.NewReader(conn)
	//nolint:bodyclose // The caller owns the upgraded raw connection.
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read websocket response: %v", err)
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		_ = conn.Close()
		t.Fatalf("websocket status = %d, want %d", res.StatusCode, http.StatusSwitchingProtocols)
	}

	if got := res.Header.Get("Upgrade"); !strings.EqualFold(got, "websocket") {
		_ = conn.Close()
		t.Fatalf("websocket Upgrade = %q, want websocket", got)
	}

	if got := res.Header.Get("Sec-WebSocket-Accept"); got != websocketAccept(key) {
		_ = conn.Close()
		t.Fatalf("websocket accept = %q, want valid accept", got)
	}

	return conn, reader
}

func websocketAccept(key string) string {
	if key != testWebSocketKey {
		return ""
	}

	return testWebSocketAccept
}
