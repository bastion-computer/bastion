package environments

import (
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

// AgentProxy proxies HTTP requests to an environment agent server.
func (h Handler) AgentProxy(c *gin.Context) {
	var (
		connection environment.AgentConnection
		err        error
	)

	if key := c.Param("key"); key != "" {
		connection, err = h.environments.AgentConnectionByKey(c.Request.Context(), key, c.Param("agent"))
	} else {
		connection, err = h.environments.AgentConnection(c.Request.Context(), c.Param("id"), c.Param("agent"))
	}

	if err != nil {
		_ = c.Error(err)
		c.JSON(handlers.ErrorStatus(err), gin.H{"error": err.Error()})

		return
	}

	proxyPath := c.Param("path")
	if proxyPath == "" {
		proxyPath = "/"
	}

	targetHost := net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = targetHost
			req.URL.Path = proxyPath
			req.URL.RawPath = ""
			req.Host = targetHost
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			_ = c.Error(err)
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(agentProxyResponseWriter{ResponseWriter: c.Writer}, c.Request)
}

type agentProxyResponseWriter struct {
	http.ResponseWriter
}

func (w agentProxyResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
