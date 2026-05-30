package actions_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bastion-computer/bastion/core/actions"
)

const testManifestFileName = "manifest.json"

func TestSeedCopiesBuiltInPresetActions(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := actions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	expected := []struct {
		action string
		files  []string
	}{
		{action: "set_default_ssh_directory", files: []string{testManifestFileName, "set_default_ssh_directory.sh"}},
		{action: "setup_node", files: []string{testManifestFileName, "install_node.sh"}},
		{action: "setup_mise", files: []string{testManifestFileName, "install_mise.sh"}},
		{action: "setup_github_cli", files: []string{testManifestFileName, "install_github_cli.sh"}},
		{action: "setup_opencode", files: []string{testManifestFileName, "install_opencode.sh"}},
	}

	for _, preset := range expected {
		for _, name := range preset.files {
			path := filepath.Join(dataDir, actions.DirName, preset.action, name)

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat seeded file %s/%s: %v", preset.action, name, err)
			}

			if !info.Mode().IsRegular() {
				t.Fatalf("seeded file %s/%s is not regular", preset.action, name)
			}
		}
	}
}

func TestSeedDoesNotOverwriteExistingPresetAction(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()

	manifestPath := filepath.Join(dataDir, actions.DirName, "setup_node", testManifestFileName)
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
