package environments

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"

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

	proxy := newTunnelProxy(c, vsockSocketPath, targetPort, proxyPath)
	proxy.ServeHTTP(agentProxyResponseWriter{ResponseWriter: c.Writer}, c.Request)
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

func (w agentProxyResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
