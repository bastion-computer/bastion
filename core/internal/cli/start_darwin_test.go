//go:build darwin

package cli

import (
	"bytes"
	"testing"
)

func TestStartDaemonUnsupportedMatchesAPIOnDarwin(t *testing.T) {
	t.Parallel()

	outputs := make(map[string]string, 2)
	for _, process := range []string{startAPIUse, startDaemonUse} {
		var stderr bytes.Buffer

		cmd := NewRootCommand()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&stderr)
		cmd.SetArgs([]string{startUse, process})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute start %s: %v", process, err)
		}

		outputs[process] = stderr.String()
	}

	if outputs[startDaemonUse] != outputs[startAPIUse] {
		t.Fatalf("daemon output = %q, want api output %q", outputs[startDaemonUse], outputs[startAPIUse])
	}
}

func TestStartClusterCommandExistsOnDarwin(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()

	clusterCmd, remaining, err := cmd.Find([]string{startUse, startClusterUse})
	if err != nil {
		t.Fatalf("find start cluster command: %v", err)
	}

	if clusterCmd.Name() != startClusterUse {
		t.Fatalf("command = %q, want %q", clusterCmd.Name(), startClusterUse)
	}

	if len(remaining) != 0 {
		t.Fatalf("remaining args = %v, want none", remaining)
	}
}
