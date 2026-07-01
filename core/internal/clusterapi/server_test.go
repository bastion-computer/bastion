package clusterapi

import (
	"log/slog"
	"net/http"
	"testing"
)

func TestNewRouterRegistersNamespacedResourceRoutes(t *testing.T) {
	t.Parallel()

	router := NewRouter(nil, slog.New(slog.DiscardHandler))

	routes := map[string]bool{}
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	want := []string{
		http.MethodPost + " /v1/namespaces/:namespaceID/secrets",
		http.MethodGet + " /v1/namespaces/by-key/:namespaceKey/templates/by-key/:key/export",
		http.MethodGet + " /v1/namespaces/:namespaceID/environments/:id/tunnels",
		http.MethodGet + " /v1/namespaces/by-key/:namespaceKey/environments/:id/agents/:agent/*path",
	}
	for _, route := range want {
		if !routes[route] {
			t.Fatalf("route %q is not registered", route)
		}
	}
}
