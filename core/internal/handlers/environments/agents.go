//nolint:wsl_v5 // Upgrade proxying keeps response/cleanup steps adjacent to preserve stream-handling readability.
package environments

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/tunnel"
)

// AgentProxy proxies HTTP requests to an environment agent server.
func (h Handler) AgentProxy(c *gin.Context) {
	agentName := c.Param("agent")
	h.proxyConnection(c, func(ctx context.Context, key string) (environment.TunnelConnection, error) {
		connection, err := h.environments.AgentConnectionByKey(ctx, key, agentName)

		return environment.TunnelConnection(connection), err
	}, func(ctx context.Context, id string) (environment.TunnelConnection, error) {
		connection, err := h.environments.AgentConnection(ctx, id, agentName)

		return environment.TunnelConnection(connection), err
	})
}

type proxyConnectionResolver func(context.Context, string) (environment.TunnelConnection, error)

func (h Handler) proxyConnection(c *gin.Context, byKey, byID proxyConnectionResolver) {
	var (
		connection environment.TunnelConnection
		err        error
	)

	if key := c.Param("key"); key != "" {
		connection, err = byKey(c.Request.Context(), key)
	} else {
		connection, err = byID(c.Request.Context(), c.Param("id"))
	}

	if err != nil {
		_ = c.Error(err)
		c.JSON(handlers.ErrorStatus(err), gin.H{errorResponseKey: err.Error()})

		return
	}

	serveTunnelProxy(c, connection.VsockSocketPath, connection.Port)
}

func serveTunnelProxy(c *gin.Context, vsockSocketPath string, targetPort int) {
	proxyPath := c.Param("path")
	if proxyPath == "" {
		proxyPath = "/"
	}

	if upgradeType(c.Request.Header) != "" {
		serveTunnelUpgrade(c, vsockSocketPath, targetPort, proxyPath)

		return
	}

	proxy := newTunnelProxy(c, vsockSocketPath, targetPort, proxyPath)
	proxy.ServeHTTP(agentProxyResponseWriter{ResponseWriter: c.Writer}, c.Request)
}

func serveTunnelUpgrade(c *gin.Context, vsockSocketPath string, targetPort int, proxyPath string) {
	backendConn, err := tunnel.DialGuestProxy(c.Request.Context(), vsockSocketPath)
	if err != nil {
		_ = c.Error(err)
		http.Error(c.Writer, err.Error(), http.StatusBadGateway)

		return
	}

	outReq := c.Request.Clone(c.Request.Context())
	outReq.URL.Scheme = "http"
	outReq.URL.Host = "bastion-guest-proxy"
	outReq.URL.Path = proxyPath
	outReq.URL.RawPath = ""
	outReq.RequestURI = ""
	outReq.Header = c.Request.Header.Clone()
	outReq.Header.Del(tunnel.TargetPortHeader)
	outReq.Header.Set(tunnel.TargetPortHeader, strconv.Itoa(targetPort))

	if err := outReq.Write(backendConn); err != nil {
		_ = backendConn.Close()
		_ = c.Error(err)
		http.Error(c.Writer, err.Error(), http.StatusBadGateway)

		return
	}

	backendReader := bufio.NewReader(backendConn)
	res, err := http.ReadResponse(backendReader, outReq)
	if err != nil {
		_ = backendConn.Close()
		_ = c.Error(err)
		http.Error(c.Writer, err.Error(), http.StatusBadGateway)

		return
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		defer func() { _ = backendConn.Close() }()
		defer func() { _ = res.Body.Close() }()
		copyTunnelResponse(c.Writer, res)

		return
	}

	clientConn, clientRW, err := http.NewResponseController(c.Writer).Hijack()
	if err != nil {
		_ = backendConn.Close()
		_ = res.Body.Close()
		_ = c.Error(err)

		return
	}

	defer func() { _ = clientConn.Close() }()
	defer func() { _ = backendConn.Close() }()

	res.Body = nil
	if err := res.Write(clientRW); err != nil {
		_ = c.Error(err)

		return
	}

	if err := clientRW.Flush(); err != nil {
		_ = c.Error(err)

		return
	}

	proxyRawTunnel(clientConn, bufferedTunnelConn{Conn: backendConn, reader: backendReader})
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

func copyTunnelResponse(w http.ResponseWriter, res *http.Response) {
	for key, values := range res.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

func proxyRawTunnel(client, backend io.ReadWriteCloser) {
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

type bufferedTunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c bufferedTunnelConn) Read(p []byte) (int, error) {
	if c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}

	return c.Conn.Read(p)
}

func newTunnelProxy(c *gin.Context, vsockSocketPath string, targetPort int, proxyPath string) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "bastion-guest-proxy"
			req.URL.Path = proxyPath
			req.URL.RawPath = ""
			req.Header.Del(tunnel.TargetPortHeader)
			req.Header.Set(tunnel.TargetPortHeader, strconv.Itoa(targetPort))
		},
		Transport: tunnelProxyTransport(vsockSocketPath),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			_ = c.Error(err)
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
	}

	return proxy
}

func tunnelProxyTransport(vsockSocketPath string) *http.Transport {
	return &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return tunnel.DialGuestProxy(ctx, vsockSocketPath)
	}}
}

type agentProxyResponseWriter struct {
	http.ResponseWriter
}

func (w agentProxyResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w agentProxyResponseWriter) Flush() {
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w agentProxyResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
