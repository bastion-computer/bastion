package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/sshtunnel"
)

func TestSSHCommandUsesAPIManagedSSH(t *testing.T) {
	t.Parallel()

	gotReq := make(chan sshtunnel.Request, 1)
	server := newSSHCommandTestServer(t, gotReq)
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newSSHCommand(&rootOptions{apiURL: server.URL})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"env_123", "true"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := <-gotReq
	if len(got.Command) != 1 || got.Command[0] != "true" || got.PTY {
		t.Fatalf("SSH request = %#v, want command true without pty", got)
	}

	if stdout.String() != "ok\n" {
		t.Fatalf("stdout = %q, want ok", stdout.String())
	}
}

func TestReadSSHOutputReturnsRemoteExitStatus(t *testing.T) {
	t.Parallel()

	var stream bytes.Buffer

	payload, err := json.Marshal(sshtunnel.ExitStatus{Code: 42})
	if err != nil {
		t.Fatalf("marshal exit status: %v", err)
	}

	if err := sshtunnel.WriteFrame(&stream, sshtunnel.FrameExit, payload); err != nil {
		t.Fatalf("write exit frame: %v", err)
	}

	err = readSSHOutput(&stream, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "42") {
		t.Fatalf("read SSH output error = %v, want remote exit status", err)
	}
}

func newSSHCommandTestServer(t *testing.T, gotReq chan<- sshtunnel.Request) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSSHCommandRequest(t, r)

		gotReq <- decodeSSHCommandRequest(t, r)

		conn := hijackSSHCommandResponse(t, w)
		defer func() { _ = conn.Close() }()

		writeSSHCommandResponse(t, conn)
	}))
}

func assertSSHCommandRequest(t *testing.T, r *http.Request) {
	t.Helper()

	if r.Method != http.MethodPost || r.URL.Path != "/v1/environments/env_123/ssh" {
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
	}
}

func decodeSSHCommandRequest(t *testing.T, r *http.Request) sshtunnel.Request {
	t.Helper()

	var req sshtunnel.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Fatalf("decode SSH request: %v", err)
	}

	return req
}

func hijackSSHCommandResponse(t *testing.T, w http.ResponseWriter) net.Conn {
	t.Helper()

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		t.Fatal("response writer does not support hijacking")
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		t.Fatalf("hijack: %v", err)
	}

	if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + sshtunnel.Protocol + "\r\n\r\n"); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}

	if err := rw.Flush(); err != nil {
		t.Fatalf("flush upgrade: %v", err)
	}

	return conn
}

func writeSSHCommandResponse(t *testing.T, conn net.Conn) {
	t.Helper()

	if err := sshtunnel.WriteFrame(conn, sshtunnel.FrameStdout, []byte("ok\n")); err != nil {
		t.Fatalf("write stdout frame: %v", err)
	}

	exitPayload, err := json.Marshal(sshtunnel.ExitStatus{})
	if err != nil {
		t.Fatalf("marshal exit status: %v", err)
	}

	if err := sshtunnel.WriteFrame(conn, sshtunnel.FrameExit, exitPayload); err != nil {
		t.Fatalf("write exit frame: %v", err)
	}
}
