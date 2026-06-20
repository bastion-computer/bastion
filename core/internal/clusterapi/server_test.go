//nolint:wsl_v5 // Route tests keep request setup and assertions close together.
package clusterapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/clusterapi"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/cluster"
	"github.com/bastion-computer/bastion/core/internal/services/utilization"
)

func TestClusterNodeRoutes(t *testing.T) {
	t.Parallel()

	router := newTestRouter()
	nodeKey := "node-a"

	createRes := request(t, router, http.MethodPost, "/v1/cluster/nodes", cluster.CreateNodeRequest{Key: &nodeKey, APIURL: "http://127.0.0.1:3148"})
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create node status = %d, want %d; body: %s", createRes.Code, http.StatusCreated, createRes.Body.String())
	}

	var created cluster.Node
	decode(t, createRes, &created)
	if !strings.HasPrefix(created.ID, "node_") || created.Key == nil || *created.Key != nodeKey || created.APIURL != "http://127.0.0.1:3148" {
		t.Fatalf("created node = %#v, want generated keyed node", created)
	}

	listRes := request(t, router, http.MethodGet, "/v1/cluster/nodes", nil)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list node status = %d, want %d", listRes.Code, http.StatusOK)
	}

	var page services.Page[cluster.Node]
	decode(t, listRes, &page)
	if len(page.Entries) != 1 || page.Entries[0].ID != created.ID {
		t.Fatalf("node page = %#v, want created node", page)
	}

	getRes := request(t, router, http.MethodGet, "/v1/cluster/nodes/by-key/"+nodeKey, nil)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get node status = %d, want %d", getRes.Code, http.StatusOK)
	}

	var got cluster.Node
	decode(t, getRes, &got)
	if got.ID != created.ID {
		t.Fatalf("got node = %#v, want %s", got, created.ID)
	}

	deleteRes := request(t, router, http.MethodDelete, "/v1/cluster/nodes/"+created.ID, nil)
	if deleteRes.Code != http.StatusOK {
		t.Fatalf("delete node status = %d, want %d", deleteRes.Code, http.StatusOK)
	}

	notFoundRes := request(t, router, http.MethodGet, "/v1/cluster/nodes/"+created.ID, nil)
	if notFoundRes.Code != http.StatusNotFound {
		t.Fatalf("get deleted node status = %d, want %d", notFoundRes.Code, http.StatusNotFound)
	}
}

func TestClusterNamespaceRoutesValidateAndPersistLimits(t *testing.T) {
	t.Parallel()

	router := newTestRouter()

	badKey := "ns_reserved"
	badRes := request(t, router, http.MethodPost, "/v1/cluster/namespaces", cluster.CreateNamespaceRequest{Key: &badKey})
	if badRes.Code != http.StatusBadRequest {
		t.Fatalf("reserved namespace key status = %d, want %d", badRes.Code, http.StatusBadRequest)
	}

	namespaceKey := "team-a"
	createRes := request(t, router, http.MethodPost, "/v1/cluster/namespaces", cluster.CreateNamespaceRequest{
		Key: &namespaceKey,
		Limits: cluster.NamespaceLimits{
			VCPU:        8,
			MemoryBytes: 16,
			VolumeBytes: 32,
		},
	})
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create namespace status = %d, want %d; body: %s", createRes.Code, http.StatusCreated, createRes.Body.String())
	}

	var created cluster.Namespace
	decode(t, createRes, &created)
	if !strings.HasPrefix(created.ID, "ns_") || created.Key == nil || *created.Key != namespaceKey || created.Limits.VCPU != 8 || created.Limits.MemoryBytes != 16 || created.Limits.VolumeBytes != 32 {
		t.Fatalf("created namespace = %#v, want keyed namespace with limits", created)
	}

	getRes := request(t, router, http.MethodGet, "/v1/cluster/namespaces/by-key/"+namespaceKey, nil)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get namespace status = %d, want %d", getRes.Code, http.StatusOK)
	}

	var got cluster.Namespace
	decode(t, getRes, &got)
	if got.ID != created.ID || got.Limits != created.Limits {
		t.Fatalf("got namespace = %#v, want %#v", got, created)
	}
}

func TestClusterUtilizationAggregatesNodes(t *testing.T) {
	t.Parallel()

	nodeA := utilizationNodeServer(t, utilization.Utilization{
		VCPU:   utilization.Resource{Total: 8, Used: 3, Available: 5},
		Memory: utilization.Resource{Total: 16, Used: 4, Available: 12},
		Volume: utilization.Resource{Total: 32, Used: 10, Available: 22},
	})
	nodeB := utilizationNodeServer(t, utilization.Utilization{
		VCPU:   utilization.Resource{Total: 4, Used: 1, Available: 3},
		Memory: utilization.Resource{Total: 8, Used: 2, Available: 6},
		Volume: utilization.Resource{Total: 16, Used: 6, Available: 10},
	})

	store := clusterapi.NewMemoryStore()
	if _, err := store.CreateNode(context.Background(), cluster.CreateNodeRequest{APIURL: nodeA.URL}); err != nil {
		t.Fatalf("create node A: %v", err)
	}
	if _, err := store.CreateNode(context.Background(), cluster.CreateNodeRequest{APIURL: nodeB.URL}); err != nil {
		t.Fatalf("create node B: %v", err)
	}

	res := request(t, clusterapi.NewRouter(store, nil), http.MethodGet, "/v1/cluster/utilization", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("cluster utilization status = %d, want %d; body: %s", res.Code, http.StatusOK, res.Body.String())
	}

	var got utilization.Utilization
	decode(t, res, &got)
	want := utilization.Utilization{
		VCPU:   utilization.Resource{Total: 12, Used: 4, Available: 8},
		Memory: utilization.Resource{Total: 24, Used: 6, Available: 18},
		Volume: utilization.Resource{Total: 48, Used: 16, Available: 32},
	}
	if got != want {
		t.Fatalf("cluster utilization = %#v, want %#v", got, want)
	}
}

func utilizationNodeServer(t *testing.T, out utilization.Utilization) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/utilization" {
			t.Fatalf("node utilization request = %s %s, want GET /v1/utilization", r.Method, r.URL.Path)
		}

		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestRouter() http.Handler {
	return clusterapi.NewRouter(clusterapi.NewMemoryStore(), slog.New(slog.DiscardHandler))
}

func request(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	return res
}

func decode(t *testing.T, res *httptest.ResponseRecorder, value any) {
	t.Helper()

	if err := json.NewDecoder(res.Body).Decode(value); err != nil {
		t.Fatalf("decode response %q: %v", res.Body.String(), err)
	}
}
