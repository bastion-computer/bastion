package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/utilization"
)

func TestUtilizationCommandCallsAPI(t *testing.T) {
	t.Parallel()

	server := newUtilizationTestServer(t)
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--" + rootFlagAPIURL, server.URL, "utilization"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	assertUtilizationOutput(t, &stdout)
}

func TestUtilizationCommandUsesPersistedAPIURL(t *testing.T) {
	t.Setenv("BASTION_API_URL", "")
	t.Setenv("BASTION_DATA_DIR", "")

	server := newUtilizationTestServer(t)
	t.Cleanup(server.Close)

	dataDir := t.TempDir()
	writeClientConfigFile(t, dataDir, testClientConfig{APIURL: server.URL})

	var stdout bytes.Buffer

	cmd := NewRootCommand()
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestDataDirFlag, dataDir, "utilization"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	assertUtilizationOutput(t, &stdout)
}

func newUtilizationTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/utilization" {
			t.Fatalf("request = %s %s, want GET /v1/utilization", r.Method, r.URL.Path)
		}

		if err := json.NewEncoder(w).Encode(testUtilization()); err != nil {
			t.Fatalf("encode utilization response: %v", err)
		}
	}))
}

func assertUtilizationOutput(t *testing.T, stdout *bytes.Buffer) {
	t.Helper()

	var got utilization.Utilization
	if err := json.NewDecoder(stdout).Decode(&got); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if got != testUtilization() {
		t.Fatalf("utilization output = %#v, want %#v", got, testUtilization())
	}
}

func testUtilization() utilization.Utilization {
	return utilization.Utilization{
		VCPU:   utilization.Resource{Total: 16, Used: 2, Available: 14},
		Memory: utilization.Resource{Total: 34359738368, Used: 2147483648, Available: 32212254720},
		Volume: utilization.Resource{Total: 1099511627776, Used: 17179869184, Available: 1082331758592},
	}
}
