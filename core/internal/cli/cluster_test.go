package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/cluster"
	"github.com/bastion-computer/bastion/core/internal/services/utilization"
)

const (
	cliTestNodeID       = "node_123"
	cliTestNamespaceID  = "ns_123"
	cliTestNodeKey      = "node-a"
	cliTestNamespaceKey = "team-a"
)

func TestRootCommandIncludesCluster(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()
	for _, subcommand := range cmd.Commands() {
		if subcommand.Name() == clusterUse && !subcommand.Hidden {
			return
		}
	}

	t.Fatal("root command is missing visible cluster subcommand")
}

func TestClusterNodeCommandsUseClusterPaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.String())

		switch r.Method {
		case http.MethodPost:
			if r.URL.Path != "/v1/cluster/nodes" {
				t.Fatalf("create node path = %s, want /v1/cluster/nodes", r.URL.Path)
			}

			var req cluster.CreateNodeRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create node request: %v", err)
			}

			if req.Key == nil || *req.Key != cliTestNodeKey || req.APIURL != "http://node-a.example" {
				t.Fatalf("create node request = %#v, want keyed node URL", req)
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cluster.Node{ID: cliTestNodeID, Key: req.Key, APIURL: req.APIURL})
		case http.MethodGet:
			if r.URL.Path == "/v1/cluster/nodes" {
				_ = json.NewEncoder(w).Encode(services.Page[cluster.Node]{Entries: []cluster.Node{{ID: cliTestNodeID}}})

				return
			}

			_ = json.NewEncoder(w).Encode(cluster.Node{ID: cliTestNodeID})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(cluster.Node{ID: cliTestNodeID})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	commands := [][]string{
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNodesUse, "create", cliTestKeyFlag, cliTestNodeKey, "--url", "http://node-a.example"},
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNodesUse, listUse, cliTestLimitFlag, "5", cliTestCursorFlag, cliTestNextCursor},
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNodesUse, "get", cliTestKeyFlag, cliTestNodeKey},
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNodesUse, removeUse, "--id", cliTestNodeID},
	}

	for _, args := range commands {
		runClusterCommand(t, args)
	}

	want := []string{
		"POST /v1/cluster/nodes",
		"GET /v1/cluster/nodes?cursor=next&limit=5",
		"GET /v1/cluster/nodes/by-key/node-a",
		"DELETE /v1/cluster/nodes/node_123",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestClusterNamespaceCommandsUseClusterPaths(t *testing.T) {
	t.Parallel()

	paths := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.String())

		switch r.Method {
		case http.MethodPost:
			var req cluster.CreateNamespaceRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode create namespace request: %v", err)
			}

			if req.Key == nil || *req.Key != cliTestNamespaceKey || req.Limits.VCPU != 8 || req.Limits.MemoryBytes != 16 || req.Limits.VolumeBytes != 32 {
				t.Fatalf("create namespace request = %#v, want keyed resource limits", req)
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cluster.Namespace{ID: cliTestNamespaceID, Key: req.Key, Limits: req.Limits})
		case http.MethodGet:
			if r.URL.Path == "/v1/cluster/namespaces" {
				_ = json.NewEncoder(w).Encode(services.Page[cluster.Namespace]{Entries: []cluster.Namespace{{ID: cliTestNamespaceID}}})

				return
			}

			_ = json.NewEncoder(w).Encode(cluster.Namespace{ID: cliTestNamespaceID})
		case http.MethodDelete:
			_ = json.NewEncoder(w).Encode(cluster.Namespace{ID: cliTestNamespaceID})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	commands := [][]string{
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNamespacesUse, "create", cliTestKeyFlag, cliTestNamespaceKey, "--vcpu", "8", "--memory", "16", "--volume", "32"},
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNamespacesUse, listUse, cliTestLimitFlag, "5", cliTestCursorFlag, cliTestNextCursor},
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNamespacesUse, "get", cliTestKeyFlag, cliTestNamespaceKey},
		{"--" + rootFlagAPIURL, server.URL, clusterUse, clusterNamespacesUse, removeUse, "--id", cliTestNamespaceID},
	}

	for _, args := range commands {
		runClusterCommand(t, args)
	}

	want := []string{
		"POST /v1/cluster/namespaces",
		"GET /v1/cluster/namespaces?cursor=next&limit=5",
		"GET /v1/cluster/namespaces/by-key/team-a",
		"DELETE /v1/cluster/namespaces/ns_123",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func TestClusterUtilizationCommandUsesClusterPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/cluster/utilization" {
			t.Fatalf("request = %s %s, want GET /v1/cluster/utilization", r.Method, r.URL.Path)
		}

		_ = json.NewEncoder(w).Encode(utilization.Utilization{})
	}))
	t.Cleanup(server.Close)

	runClusterCommand(t, []string{"--" + rootFlagAPIURL, server.URL, clusterUse, utilizationUse})
}

func runClusterCommand(t *testing.T, args []string) {
	t.Helper()

	var stdout bytes.Buffer

	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
}
