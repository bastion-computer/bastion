//go:build linux

// Package main runs the Bastion guest-side vsock HTTP proxy.
//
//nolint:wsl_v5 // Low-level upgrade proxying keeps cleanup/error handling close to the operations it protects.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/bastion-computer/bastion/core/internal/tunnel"
)

const healthPath = "/_bastion/health"

const vsockPollInterval = 100 * time.Millisecond

func main() {
	port := flag.Int("port", tunnel.GuestProxyVsockPort, "guest vsock listen port")

	flag.Parse()

	if *port < 1 || *port > 65535 {
		log.Fatalf("invalid vsock port %d", *port)
	}

	listenPort := uint32(*port) //nolint:gosec // Range checked immediately above.

	listener, err := listenVsock(listenPort)
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Handler:           http.HandlerFunc(handleProxy),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("bastion guest proxy listening on vsock port %d", *port)

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		_ = listener.Close()

		log.Fatal(err)
	}

	_ = listener.Close()
}

func handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == healthPath && r.Header.Get(tunnel.TargetPortHeader) == "" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))

		return
	}

	port, err := targetPort(r.Header.Get(tunnel.TargetPortHeader))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	if upgradeType(r.Header) != "" {
		proxyUpgrade(w, r, port)

		return
	}

	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("localhost", strconv.Itoa(port))}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(target)
			req.Out.Header.Del(tunnel.TargetPortHeader)
		},
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, r)
}

func proxyUpgrade(w http.ResponseWriter, r *http.Request, port int) {
	targetConn, err := (&net.Dialer{}).DialContext(r.Context(), "tcp", net.JoinHostPort("localhost", strconv.Itoa(port)))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)

		return
	}

	outReq := r.Clone(r.Context())
	outReq.URL = &url.URL{Scheme: "http", Host: net.JoinHostPort("localhost", strconv.Itoa(port)), Path: r.URL.Path, RawQuery: r.URL.RawQuery}
	outReq.RequestURI = ""
	outReq.Header = r.Header.Clone()
	outReq.Header.Del(tunnel.TargetPortHeader)

	if err := outReq.Write(targetConn); err != nil {
		_ = targetConn.Close()
		http.Error(w, err.Error(), http.StatusBadGateway)

		return
	}

	backendReader := bufio.NewReader(targetConn)
	res, err := http.ReadResponse(backendReader, outReq)
	if err != nil {
		_ = targetConn.Close()
		http.Error(w, err.Error(), http.StatusBadGateway)

		return
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		defer func() { _ = targetConn.Close() }()
		defer func() { _ = res.Body.Close() }()
		copyResponse(w, res)

		return
	}

	clientConn, clientRW, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = targetConn.Close()
		_ = res.Body.Close()

		return
	}

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = targetConn.Close() }()

	res.Body = nil
	if err := res.Write(clientRW); err != nil {
		return
	}

	if err := clientRW.Flush(); err != nil {
		return
	}

	backend := bufferedReadWriteCloser{Conn: targetConn, reader: backendReader}
	proxyRawUpgrade(clientConn, backend)
}

func upgradeType(header http.Header) string {
	if !headerHasToken(header, "Connection", "Upgrade") {
		return ""
	}

	return header.Get("Upgrade")
}

func headerHasToken(header http.Header, key, token string) bool {
	for _, value := range header.Values(key) {
		for part := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}

	return false
}

func copyResponse(w http.ResponseWriter, res *http.Response) {
	for key, values := range res.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

func proxyRawUpgrade(client, backend io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	copyStream := func(dst io.WriteCloser, src io.Reader) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		done <- struct{}{}
	}

	go copyStream(backend, client)
	go copyStream(client, backend)
	<-done
}

func targetPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid %s", tunnel.TargetPortHeader)
	}

	return port, nil
}

func listenVsock(port uint32) (net.Listener, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("create vsock socket: %w", err)
	}

	listener := &vsockListener{fd: fd, addr: vsockAddr{cid: uint32(unix.VMADDR_CID_ANY), port: port}}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: uint32(unix.VMADDR_CID_ANY), Port: port}); err != nil {
		_ = listener.Close()

		return nil, fmt.Errorf("bind vsock port %d: %w", port, err)
	}

	if err := unix.Listen(fd, 128); err != nil {
		_ = listener.Close()

		return nil, fmt.Errorf("listen on vsock port %d: %w", port, err)
	}

	return listener, nil
}

type vsockListener struct {
	fd   int
	addr vsockAddr
	once sync.Once
}

func (l *vsockListener) Accept() (net.Conn, error) {
	fd, sockaddr, err := unix.Accept4(l.fd, unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK)
	if err != nil {
		if errors.Is(err, unix.EBADF) || errors.Is(err, unix.EINVAL) {
			return nil, net.ErrClosed
		}

		return nil, err
	}

	remote := vsockAddr{}
	if vm, ok := sockaddr.(*unix.SockaddrVM); ok {
		remote = vsockAddr{cid: vm.CID, port: vm.Port}
	}

	return &vsockConn{fd: fd, file: os.NewFile(uintptr(fd), "bastion-vsock"), local: l.addr, remote: remote}, nil
}

func (l *vsockListener) Close() error {
	var err error

	l.once.Do(func() { err = unix.Close(l.fd) })

	return err
}

func (l *vsockListener) Addr() net.Addr {
	return l.addr
}

type vsockConn struct {
	fd     int
	file   *os.File
	local  vsockAddr
	remote vsockAddr

	readMu  sync.Mutex
	writeMu sync.Mutex

	deadlineMu    sync.RWMutex
	readDeadline  time.Time
	writeDeadline time.Time
}

type bufferedReadWriteCloser struct {
	net.Conn
	reader *bufio.Reader
}

func (c bufferedReadWriteCloser) Read(p []byte) (int, error) {
	if c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}

	return c.Conn.Read(p)
}

func (c *vsockConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	c.readMu.Lock()
	defer c.readMu.Unlock()

	for {
		n, err := unix.Read(c.fd, p)
		if err == nil {
			if n == 0 {
				return 0, io.EOF
			}

			return n, nil
		}

		if errors.Is(err, unix.EINTR) {
			continue
		}

		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
			return 0, os.NewSyscallError("read", err)
		}

		if err := c.wait(unix.POLLIN, c.readDeadlineValue); err != nil {
			return 0, err
		}
	}
}

func (c *vsockConn) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	written := 0
	for written < len(p) {
		n, err := unix.Write(c.fd, p[written:])
		if n > 0 {
			written += n
		}

		if err == nil {
			continue
		}

		if errors.Is(err, unix.EINTR) {
			continue
		}

		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EWOULDBLOCK) {
			return written, os.NewSyscallError("write", err)
		}

		if err := c.wait(unix.POLLOUT, c.writeDeadlineValue); err != nil {
			return written, err
		}
	}

	return written, nil
}

func (c *vsockConn) Close() error {
	return c.file.Close()
}

func (c *vsockConn) LocalAddr() net.Addr {
	return c.local
}

func (c *vsockConn) RemoteAddr() net.Addr {
	return c.remote
}

func (c *vsockConn) SetDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.writeDeadline = t
	c.deadlineMu.Unlock()

	return nil
}

func (c *vsockConn) SetReadDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.deadlineMu.Unlock()

	return nil
}

func (c *vsockConn) SetWriteDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.writeDeadline = t
	c.deadlineMu.Unlock()

	return nil
}

func (c *vsockConn) wait(events int16, deadline func() time.Time) error {
	for {
		timeout, expired := pollTimeout(deadline())
		if expired {
			return os.ErrDeadlineExceeded
		}

		fd := int32(c.fd) //nolint:gosec // File descriptors are kernel-assigned small non-negative integers.
		fds := []unix.PollFd{{Fd: fd, Events: events}}
		n, err := unix.Poll(fds, timeout)
		if errors.Is(err, unix.EINTR) {
			continue
		}

		if err != nil {
			return os.NewSyscallError("poll", err)
		}

		if n > 0 && fds[0].Revents&(events|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return nil
		}
	}
}

func (c *vsockConn) readDeadlineValue() time.Time {
	c.deadlineMu.RLock()
	defer c.deadlineMu.RUnlock()

	return c.readDeadline
}

func (c *vsockConn) writeDeadlineValue() time.Time {
	c.deadlineMu.RLock()
	defer c.deadlineMu.RUnlock()

	return c.writeDeadline
}

func pollTimeout(deadline time.Time) (int, bool) {
	if deadline.IsZero() {
		return int(vsockPollInterval / time.Millisecond), false
	}

	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, true
	}

	if remaining > vsockPollInterval {
		remaining = vsockPollInterval
	}

	return int((remaining + time.Millisecond - 1) / time.Millisecond), false
}

type vsockAddr struct {
	cid  uint32
	port uint32
}

func (a vsockAddr) Network() string {
	return "vsock"
}

func (a vsockAddr) String() string {
	return fmt.Sprintf("%d:%d", a.cid, a.port)
}
