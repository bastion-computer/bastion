package cluster

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testWebSocketKey    = "dGhlIHNhbXBsZSBub25jZQ=="
	testWebSocketAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
)

type nodeUpgradeRequest struct {
	method     string
	requestURI string
	host       string
	testHeader string
}

func TestNodeReverseProxyForwardsWebSocketUpgrades(t *testing.T) {
	t.Parallel()

	gotRequest := make(chan nodeUpgradeRequest, 1)
	node := newNodeUpgradeServer(t, gotRequest)
	t.Cleanup(node.Close)

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy := newNodeReverseProxy(nil, node.URL+"/api?node=1", "/v1/environments/env_derivative/tunnels/frontend"+r.URL.Path)
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)

	conn, reader := openWebSocketUpgrade(t, proxy.URL, "/hmr?token=abc", http.Header{"X-Test-Proxy": []string{"yes"}})
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write websocket payload: %v", err)
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read websocket payload: %v", err)
	}

	if line != "node:ping\n" {
		t.Fatalf("websocket payload = %q, want node echo", line)
	}

	select {
	case got := <-gotRequest:
		wantURI := "/api/v1/environments/env_derivative/tunnels/frontend/hmr?node=1&token=abc"
		if got.method != http.MethodGet || got.requestURI != wantURI {
			t.Fatalf("node websocket request = %s %s, want GET %s", got.method, got.requestURI, wantURI)
		}

		if got.host != strings.TrimPrefix(node.URL, "http://") {
			t.Fatalf("node websocket host = %q, want node host", got.host)
		}

		if got.testHeader != "yes" {
			t.Fatalf("node websocket X-Test-Proxy = %q, want yes", got.testHeader)
		}
	case <-time.After(time.Second):
		t.Fatal("node was not called")
	}
}

func newNodeUpgradeServer(t *testing.T, gotRequest chan<- nodeUpgradeRequest) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest <- nodeUpgradeRequest{
			method:     r.Method,
			requestURI: r.RequestURI,
			host:       r.Host,
			testHeader: r.Header.Get("X-Test-Proxy"),
		}

		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			t.Errorf("node Upgrade = %q, want websocket", r.Header.Get("Upgrade"))
			return
		}

		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack node websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		key := r.Header.Get("Sec-WebSocket-Key")
		if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: " + websocketAccept(key) + "\r\n\r\n"); err != nil {
			t.Errorf("write node websocket response: %v", err)
			return
		}

		if err := rw.Flush(); err != nil {
			t.Errorf("flush node websocket response: %v", err)
			return
		}

		line, err := rw.ReadString('\n')
		if err != nil {
			t.Errorf("read node websocket payload: %v", err)
			return
		}

		if _, err := rw.WriteString("node:" + line); err != nil {
			t.Errorf("write node websocket payload: %v", err)
			return
		}

		if err := rw.Flush(); err != nil {
			t.Errorf("flush node websocket payload: %v", err)
		}
	}))
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
