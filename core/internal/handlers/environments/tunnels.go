package environments

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

// Tunnels handles environment tunnel metadata lookup requests.
func (h Handler) Tunnels(c *gin.Context) {
	var (
		tunnels environment.Tunnels
		err     error
	)

	if key := c.Param("key"); key != "" {
		tunnels, err = h.environments.TunnelsByKey(c.Request.Context(), key)
	} else {
		tunnels, err = h.environments.Tunnels(c.Request.Context(), c.Param("id"))
	}

	handlers.Respond(c, tunnels, err, http.StatusOK)
}

// TunnelProxy proxies HTTP requests to a registered environment tunnel.
func (h Handler) TunnelProxy(c *gin.Context) {
	tunnelName := c.Param("name")
	h.proxyConnection(c, func(ctx context.Context, key string) (environment.TunnelConnection, error) {
		return h.environments.TunnelConnectionByKey(ctx, key, tunnelName)
	}, func(ctx context.Context, id string) (environment.TunnelConnection, error) {
		return h.environments.TunnelConnection(ctx, id, tunnelName)
	})
}
