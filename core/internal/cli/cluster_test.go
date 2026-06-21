package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services"
	clusterservice "github.com/bastion-computer/bastion/core/internal/services/cluster"
)

const (
	cliTestClusterNodeID       = "node_123"
	cliTestClusterNodeKey      = "node-key"
	cliTestClusterNodeURL      = "http://node.test"
	cliTestClusterNamespaceID  = "ns_123"
	cliTestClusterNamespaceKey = "namespace-key"
)

func TestClusterCommandsUseResourcePaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.String())
		writeClusterCommandResponse(t, w, r)
	}))
	t.Cleanup(server.Close)

	runs := []struct {
		cmd  string
		args []string
	}{
		{cmd: "nodes-create", args: []string{clusterUse, clusterNodesUse, "create", cliTestKeyFlag, cliTestClusterNodeKey, "--url", cliTestClusterNodeURL}},
		{cmd: "nodes-list", args: []string{clusterUse, clusterNodesUse, listUse, cliTestLimitFlag, "5", cliTestCursorFlag, cliTestNextCursor}},
		{cmd: "nodes-get", args: []string{clusterUse, clusterNodesUse, "get", cliTestKeyFlag, cliTestClusterNodeKey}},
		{cmd: "nodes-remove", args: []string{clusterUse, clusterNodesUse, removeUse, cliTestIDFlag, cliTestClusterNodeID}},
		{cmd: "namespaces-create", args: []string{clusterUse, clusterNamespacesUse, "create", cliTestKeyFlag, cliTestClusterNamespaceKey}},
		{cmd: "namespaces-list", args: []string{clusterUse, clusterNamespacesUse, listUse, cliTestLimitFlag, "5", cliTestCursorFlag, cliTestNextCursor}},
		{cmd: "namespaces-get", args: []string{clusterUse, clusterNamespacesUse, "get", cliTestKeyFlag, cliTestClusterNamespaceKey}},
		{cmd: "namespaces-remove", args: []string{clusterUse, clusterNamespacesUse, removeUse, cliTestIDFlag, cliTestClusterNamespaceID}},
	}

	for _, run := range runs {
		var stdout bytes.Buffer

		cmd := NewRootCommand()
		cmd.SetOut(&stdout)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(append([]string{"--" + rootFlagAPIURL, server.URL}, run.args...))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute %s: %v", run.cmd, err)
		}
	}

	want := []string{
		"POST /v1/cluster/nodes",
		"GET /v1/cluster/nodes?cursor=next&limit=5",
		"GET /v1/cluster/nodes/by-key/node-key",
		"DELETE /v1/cluster/nodes/node_123",
		"POST /v1/cluster/namespaces",
		"GET /v1/cluster/namespaces?cursor=next&limit=5",
		"GET /v1/cluster/namespaces/by-key/namespace-key",
		"DELETE /v1/cluster/namespaces/ns_123",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func writeClusterCommandResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	switch r.URL.Path {
	case "/v1/cluster/nodes":
		writeClusterNodesResponse(t, w, r)
	case "/v1/cluster/nodes/by-key/" + cliTestClusterNodeKey, "/v1/cluster/nodes/" + cliTestClusterNodeID:
		if err := json.NewEncoder(w).Encode(testClusterNode()); err != nil {
			t.Fatalf("encode node response: %v", err)
		}
	case "/v1/cluster/namespaces":
		writeClusterNamespacesResponse(t, w, r)
	case "/v1/cluster/namespaces/by-key/" + cliTestClusterNamespaceKey, "/v1/cluster/namespaces/" + cliTestClusterNamespaceID:
		if err := json.NewEncoder(w).Encode(testClusterNamespace()); err != nil {
			t.Fatalf("encode namespace response: %v", err)
		}
	default:
		t.Fatalf("unexpected cluster path %s", r.URL.Path)
	}
}

func writeClusterNodesResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	switch r.Method {
	case http.MethodPost:
		var req clusterservice.CreateNodeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode node create request: %v", err)
		}

		if req.Key == nil || *req.Key != cliTestClusterNodeKey || req.URL != cliTestClusterNodeURL {
			t.Fatalf("node create request = %#v, want keyed node URL", req)
		}

		w.WriteHeader(http.StatusCreated)

		if err := json.NewEncoder(w).Encode(testClusterNode()); err != nil {
			t.Fatalf("encode node create response: %v", err)
		}
	case http.MethodGet:
		page := services.Page[clusterservice.Node]{Entries: []clusterservice.Node{testClusterNode()}}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Fatalf("encode node list response: %v", err)
		}
	default:
		t.Fatalf("unexpected node method %s", r.Method)
	}
}

func writeClusterNamespacesResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	switch r.Method {
	case http.MethodPost:
		var req clusterservice.CreateNamespaceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode namespace create request: %v", err)
		}

		if req.Key == nil || *req.Key != cliTestClusterNamespaceKey {
			t.Fatalf("namespace create request = %#v, want keyed namespace", req)
		}

		w.WriteHeader(http.StatusCreated)

		if err := json.NewEncoder(w).Encode(testClusterNamespace()); err != nil {
			t.Fatalf("encode namespace create response: %v", err)
		}
	case http.MethodGet:
		page := services.Page[clusterservice.Namespace]{Entries: []clusterservice.Namespace{testClusterNamespace()}}
		if err := json.NewEncoder(w).Encode(page); err != nil {
			t.Fatalf("encode namespace list response: %v", err)
		}
	default:
		t.Fatalf("unexpected namespace method %s", r.Method)
	}
}

func testClusterNode() clusterservice.Node {
	key := cliTestClusterNodeKey
	return clusterservice.Node{ID: cliTestClusterNodeID, Key: &key, URL: cliTestClusterNodeURL, CreatedAt: "now"}
}

func testClusterNamespace() clusterservice.Namespace {
	key := cliTestClusterNamespaceKey
	return clusterservice.Namespace{ID: cliTestClusterNamespaceID, Key: &key, CreatedAt: "now"}
}
