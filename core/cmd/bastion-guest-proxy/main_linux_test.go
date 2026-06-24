//go:build linux

//nolint:wsl_v5 // These tests model raw socket handshakes where adjacent read/write steps are clearer together.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/bastion-computer/bastion/core/internal/tunnel"
	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
)

const (
	testWebSocketKey    = "dGhlIHNhbXBsZSBub25jZQ=="
	testWebSocketAccept = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
)

//nolint:gocyclo // Exercises the full HTTP upgrade forwarding contract in one fixture.
func TestGuestProxyForwardsUpgradeResponses(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/environments/env_inner/ssh" {
			t.Fatalf("backend request = %s %s, want POST /v1/environments/env_inner/ssh", r.Method, r.URL.Path)
		}

		if r.Header.Get("Upgrade") != sshtunnel.Protocol {
			t.Fatalf("backend upgrade = %q, want %q", r.Header.Get("Upgrade"), sshtunnel.Protocol)
		}

		conn := hijackTestConnection(t, w)
		defer func() { _ = conn.Close() }()

		payload, err := json.Marshal(sshtunnel.ExitStatus{})
		if err != nil {
			t.Fatalf("marshal exit status: %v", err)
		}

		if err := sshtunnel.WriteFrame(conn, sshtunnel.FrameExit, payload); err != nil {
			t.Fatalf("write exit frame: %v", err)
		}
	}))
	t.Cleanup(backend.Close)

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	_, portValue, err := net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatalf("split backend host: %v", err)
	}

	proxy := httptest.NewServer(http.HandlerFunc(handleProxy))
	t.Cleanup(proxy.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, proxy.URL+"/v1/environments/env_inner/ssh", bytes.NewReader([]byte(`{"command":["true"]}`)))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req.Header.Set(tunnel.TargetPortHeader, portValue)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", sshtunnel.Protocol)

	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatalf("call guest proxy: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %s, want 101; body: %s", res.Status, body)
	}

	reader := make(chan byte, 1)
	errCh := make(chan error, 1)
	go func() {
		frameType, _, err := sshtunnel.ReadFrame(res.Body)
		if err != nil {
			errCh <- err

			return
		}

		reader <- frameType
	}()

	select {
	case frameType := <-reader:
		if frameType != sshtunnel.FrameExit {
			t.Fatalf("frame type = %d, want exit", frameType)
		}
	case err := <-errCh:
		t.Fatalf("read frame: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for proxied upgrade frame")
	}
}

func TestGuestProxyVsockUpgradeHijackDoesNotHang(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Fatalf("read backend request body: %v", err)
		}

		conn := hijackTestConnection(t, w)
		defer func() { _ = conn.Close() }()

		payload, err := json.Marshal(sshtunnel.ExitStatus{})
		if err != nil {
			t.Fatalf("marshal exit status: %v", err)
		}

		if err := sshtunnel.WriteFrame(conn, sshtunnel.FrameExit, payload); err != nil {
			t.Fatalf("write exit frame: %v", err)
		}
	}))
	t.Cleanup(backend.Close)

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	_, portValue, err := net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatalf("split backend host: %v", err)
	}

	serverConn, clientConn := newVsockConnPair(t)
	listener := newSingleConnListener(serverConn)
	server := &http.Server{Handler: http.HandlerFunc(handleProxy), ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	t.Cleanup(func() {
		_ = listener.Close()
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("serve guest proxy: %v", err)
		}
	})

	body := []byte(`{"command":["true"]}`)
	request := fmt.Sprintf("POST /v1/environments/env_inner/ssh HTTP/1.1\r\nHost: bastion-guest-proxy\r\n%s: %s\r\nConnection: Upgrade\r\nUpgrade: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		tunnel.TargetPortHeader,
		portValue,
		sshtunnel.Protocol,
		len(body),
		body,
	)
	if _, err := clientConn.Write([]byte(request)); err != nil {
		t.Fatalf("write guest proxy request: %v", err)
	}

	reader := bufio.NewReader(clientConn)
	res := readUpgradeResponse(t, reader)
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %s, want 101", res.Status)
	}

	frameType := readFrameType(t, reader)
	if frameType != sshtunnel.FrameExit {
		t.Fatalf("frame type = %d, want exit", frameType)
	}
}

//nolint:gocyclo // Exercises the full WebSocket upgrade host/payload contract in one fixture.
func TestGuestProxyWebSocketUpgradeUsesTargetHost(t *testing.T) {
	t.Parallel()

	var portValue string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantHost := net.JoinHostPort("localhost", portValue)
		if r.Host != wantHost {
			http.Error(w, fmt.Sprintf("backend host = %q, want %q", r.Host, wantHost), http.StatusBadRequest)

			return
		}

		if r.Method != http.MethodGet || r.URL.Path != "/hmr" || r.URL.RawQuery != "token=abc" {
			http.Error(w, fmt.Sprintf("backend request = %s %s?%s, want GET /hmr?token=abc", r.Method, r.URL.Path, r.URL.RawQuery), http.StatusBadRequest)

			return
		}

		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, fmt.Sprintf("backend upgrade = %q, want websocket", r.Header.Get("Upgrade")), http.StatusBadRequest)

			return
		}

		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack websocket response: %v", err)

			return
		}
		defer func() { _ = conn.Close() }()

		key := r.Header.Get("Sec-WebSocket-Key")
		if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: " + websocketAccept(key) + "\r\n\r\n"); err != nil {
			t.Errorf("write websocket response: %v", err)

			return
		}

		if err := rw.Flush(); err != nil {
			t.Errorf("flush websocket response: %v", err)

			return
		}

		line, err := rw.ReadString('\n')
		if err != nil {
			t.Errorf("read websocket payload: %v", err)

			return
		}

		if _, err := rw.WriteString("backend:" + line); err != nil {
			t.Errorf("write websocket payload: %v", err)

			return
		}

		if err := rw.Flush(); err != nil {
			t.Errorf("flush websocket payload: %v", err)
		}
	}))
	t.Cleanup(backend.Close)

	backendURL, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	_, portValue, err = net.SplitHostPort(backendURL.Host)
	if err != nil {
		t.Fatalf("split backend host: %v", err)
	}

	proxy := httptest.NewServer(http.HandlerFunc(handleProxy))
	t.Cleanup(proxy.Close)

	conn, reader := openGuestProxyWebSocket(t, proxy.URL, portValue)
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write websocket payload: %v", err)
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read websocket payload: %v", err)
	}

	if line != "backend:ping\n" {
		t.Fatalf("websocket payload = %q, want backend echo", line)
	}
}

func hijackTestConnection(t *testing.T, w http.ResponseWriter) net.Conn {
	t.Helper()

	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		t.Fatalf("hijack response: %v", err)
	}

	if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + sshtunnel.Protocol + "\r\n\r\n"); err != nil {
		t.Fatalf("write upgrade response: %v", err)
	}

	if err := rw.Flush(); err != nil {
		t.Fatalf("flush upgrade response: %v", err)
	}

	return conn
}

func readUpgradeResponse(t *testing.T, reader *bufio.Reader) *http.Response {
	t.Helper()

	type result struct {
		res *http.Response
		err error
	}

	done := make(chan result, 1)
	go func() {
		//nolint:bodyclose // The caller owns and closes the response returned through the channel.
		res, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
		done <- result{res: res, err: err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("read upgrade response: %v", got.err)
		}

		return got.res
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upgrade response")
	}

	return nil
}

func readFrameType(t *testing.T, reader *bufio.Reader) byte {
	t.Helper()

	type result struct {
		frameType byte
		err       error
	}

	done := make(chan result, 1)
	go func() {
		frameType, _, err := sshtunnel.ReadFrame(reader)
		done <- result{frameType: frameType, err: err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("read frame: %v", got.err)
		}

		return got.frameType
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upgrade frame")
	}

	return 0
}

func openGuestProxyWebSocket(t *testing.T, proxyURL, portValue string) (net.Conn, *bufio.Reader) {
	t.Helper()

	parsed, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatalf("parse guest proxy URL: %v", err)
	}

	conn, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", parsed.Host)
	if err != nil {
		t.Fatalf("dial guest proxy: %v", err)
	}

	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		_ = conn.Close()
		t.Fatalf("set guest proxy deadline: %v", err)
	}

	key := testWebSocketKey
	request := fmt.Sprintf("GET /hmr?token=abc HTTP/1.1\r\nHost: api.example\r\n%s: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", tunnel.TargetPortHeader, portValue, key)
	if _, err := conn.Write([]byte(request)); err != nil {
		_ = conn.Close()
		t.Fatalf("write guest proxy websocket request: %v", err)
	}

	reader := bufio.NewReader(conn)
	//nolint:bodyclose // The caller owns the upgraded raw connection.
	res, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read guest proxy websocket response: %v", err)
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(res.Body)
		_ = conn.Close()
		t.Fatalf("websocket status = %s, want 101; body: %s", res.Status, body)
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

func newVsockConnPair(t *testing.T) (*vsockConn, *vsockConn) {
	t.Helper()

	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("create socket pair: %v", err)
	}

	server := &vsockConn{fd: fds[0], file: os.NewFile(uintptr(fds[0]), "test-vsock-server")}
	client := &vsockConn{fd: fds[1], file: os.NewFile(uintptr(fds[1]), "test-vsock-client")}
	t.Cleanup(func() { _ = server.Close() })
	t.Cleanup(func() { _ = client.Close() })

	return server, client
}

type singleConnListener struct {
	conn   net.Conn
	once   sync.Once
	closed chan struct{}
}

func newSingleConnListener(conn net.Conn) *singleConnListener {
	return &singleConnListener{conn: conn, closed: make(chan struct{})}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return conn, nil
	}

	<-l.closed

	return nil, net.ErrClosed
}

func (l *singleConnListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}

	return nil
}

func (l *singleConnListener) Addr() net.Addr {
	return vsockAddr{cid: 1, port: 1}
}
