//go:build linux

// Package main runs the Bastion guest-side vsock HTTP proxy.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/bastion-computer/bastion/core/internal/tunnel"
)

const healthPath = "/_bastion/health"

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
	fd, sockaddr, err := unix.Accept4(l.fd, unix.SOCK_CLOEXEC)
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

	return &vsockConn{file: os.NewFile(uintptr(fd), "bastion-vsock"), local: l.addr, remote: remote}, nil
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
	file   *os.File
	local  vsockAddr
	remote vsockAddr
}

func (c *vsockConn) Read(p []byte) (int, error) {
	return c.file.Read(p)
}

func (c *vsockConn) Write(p []byte) (int, error) {
	return c.file.Write(p)
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

func (c *vsockConn) SetDeadline(time.Time) error {
	return nil
}

func (c *vsockConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *vsockConn) SetWriteDeadline(time.Time) error {
	return nil
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
