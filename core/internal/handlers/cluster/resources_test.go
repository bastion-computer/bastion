package cluster

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNamespaceSelectorUsesPathParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		params  gin.Params
		wantID  string
		wantKey string
	}{
		{
			name:   "id",
			params: gin.Params{{Key: "namespaceID", Value: "ns_123"}},
			wantID: "ns_123",
		},
		{
			name:    "key",
			params:  gin.Params{{Key: "namespaceKey", Value: "team-a"}},
			wantKey: "team-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := &gin.Context{Params: tt.params, Request: httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)}
			got := namespaceSelector(ctx)

			if got.ID != tt.wantID || got.Key != tt.wantKey {
				t.Fatalf("namespace selector = %#v, want id %q key %q", got, tt.wantID, tt.wantKey)
			}
		})
	}
}
