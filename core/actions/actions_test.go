package actions_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bastion-computer/bastion/core/actions"
)

func TestSeedCopiesBuiltInPresetActions(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := actions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	for _, name := range []string{"manifest.json", "install_node.sh"} {
		path := filepath.Join(dataDir, actions.DirName, "setup_node", name)

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat seeded file %s: %v", name, err)
		}

		if !info.Mode().IsRegular() {
			t.Fatalf("seeded file %s is not regular", name)
		}
	}
}

func TestSeedDoesNotOverwriteExistingPresetAction(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()

	manifestPath := filepath.Join(dataDir, actions.DirName, "setup_node", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o750); err != nil {
		t.Fatalf("create existing action dir: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte("custom\n"), 0o600); err != nil {
		t.Fatalf("write existing manifest: %v", err)
	}

	if err := actions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Test path is rooted in t.TempDir().
	if err != nil {
		t.Fatalf("read existing manifest: %v", err)
	}

	if string(contents) != "custom\n" {
		t.Fatalf("manifest was overwritten: %q", contents)
	}
}
