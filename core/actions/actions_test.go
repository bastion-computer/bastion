package actions_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestSeedOverwritesExistingBuiltInPresetAction(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	manifestPath := writeCustomManifest(t, dataDir, "setup_node")

	extraPath := filepath.Join(dataDir, actions.DirName, "setup_node", "custom.txt")
	if err := os.WriteFile(extraPath, []byte("custom\n"), 0o600); err != nil {
		t.Fatalf("write custom extra file: %v", err)
	}

	if err := actions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Test path is rooted in t.TempDir().
	if err != nil {
		t.Fatalf("read overwritten manifest: %v", err)
	}

	if string(contents) == "custom\n" || !strings.Contains(string(contents), `"run": "sh ./install_node.sh"`) {
		t.Fatalf("manifest was not overwritten with built-in setup_node manifest: %s", contents)
	}

	if _, err := os.Stat(extraPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("custom extra file stat error = %v, want not exist", err)
	}
}

func TestSeedPreservesUniquelyNamedCustomAction(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	manifestPath := writeCustomManifest(t, dataDir, "custom_setup")

	if err := actions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	assertCustomManifest(t, manifestPath, "custom action manifest")
}

func writeCustomManifest(t *testing.T, dataDir, action string) string {
	t.Helper()

	manifestPath := filepath.Join(dataDir, actions.DirName, action, testManifestFileName)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o750); err != nil {
		t.Fatalf("create existing action dir: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte("custom\n"), 0o600); err != nil {
		t.Fatalf("write existing manifest: %v", err)
	}

	return manifestPath
}

func assertCustomManifest(t *testing.T, manifestPath, label string) {
	t.Helper()

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Test path is rooted in t.TempDir().
	if err != nil {
		t.Fatalf("read existing manifest: %v", err)
	}

	if string(contents) != "custom\n" {
		t.Fatalf("%s was overwritten: %q", label, contents)
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
