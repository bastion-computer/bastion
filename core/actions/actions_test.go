package actions_test

import (
	"errors"
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

func TestSeedRequiresExistingDataDir(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "missing")

	err := actions.Seed(dataDir)
	if err == nil {
		t.Fatal("seed actions error = nil, want missing data dir error")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("seed actions error = %v, want not exist", err)
	}

	if _, statErr := os.Stat(dataDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("data dir stat error = %v, want not exist", statErr)
	}
}

func TestSeedRejectsDataDirFile(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(dataDir, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write data dir file: %v", err)
	}

	err := actions.Seed(dataDir)
	if err == nil {
		t.Fatal("seed actions error = nil, want non-directory data dir error")
	}
}
