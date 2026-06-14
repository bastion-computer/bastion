package actions_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		{action: "setup_bun", files: []string{testManifestFileName, "install_bun.sh"}},
		{action: "setup_mise", files: []string{testManifestFileName, "install_mise.sh"}},
		{action: "setup_rust", files: []string{testManifestFileName, "install_rust.sh"}},
		{action: "setup_github_cli", files: []string{testManifestFileName, "install_github_cli.sh"}},
		{action: "setup_docker", files: []string{testManifestFileName, "install_docker.sh"}},
		{action: "write_env_file", files: []string{testManifestFileName, "write_env_file.sh"}},
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

func TestSeedOverwritesExistingBuiltInPresetActionAndPreservesCustomActions(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()

	manifestPath := filepath.Join(dataDir, actions.DirName, "setup_node", testManifestFileName)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o750); err != nil {
		t.Fatalf("create existing action dir: %v", err)
	}

	if err := os.WriteFile(manifestPath, []byte("custom\n"), 0o600); err != nil {
		t.Fatalf("write existing manifest: %v", err)
	}

	stalePath := filepath.Join(dataDir, actions.DirName, "setup_node", "stale.txt")
	if err := os.WriteFile(stalePath, []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("write stale built-in file: %v", err)
	}

	customManifestPath := filepath.Join(dataDir, actions.DirName, "setup_python", testManifestFileName)
	if err := os.MkdirAll(filepath.Dir(customManifestPath), 0o750); err != nil {
		t.Fatalf("create custom action dir: %v", err)
	}

	if err := os.WriteFile(customManifestPath, []byte("custom\n"), 0o600); err != nil {
		t.Fatalf("write custom manifest: %v", err)
	}

	if err := actions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Test path is rooted in t.TempDir().
	if err != nil {
		t.Fatalf("read existing manifest: %v", err)
	}

	if string(contents) == "custom\n" {
		t.Fatalf("built-in manifest was not overwritten: %q", contents)
	}

	if _, err := os.Stat(stalePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale built-in file error = %v, want not exist", err)
	}

	contents, err = os.ReadFile(customManifestPath) //nolint:gosec // Test path is rooted in t.TempDir().
	if err != nil {
		t.Fatalf("read custom manifest: %v", err)
	}

	if string(contents) != "custom\n" {
		t.Fatalf("custom manifest was overwritten: %q", contents)
	}
}

func TestSetupRustScriptRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		env  []string
		want string
	}{
		{
			name: "profile",
			env:  []string{"BASTION_INPUT_PROFILE=fast", "BASTION_INPUT_TOOLCHAIN=stable"},
			want: "Rust profile must be one of minimal, default, or complete",
		},
		{
			name: "toolchain characters",
			env:  []string{"BASTION_INPUT_PROFILE=minimal", "BASTION_INPUT_TOOLCHAIN=stable;rm"},
			want: "Rust toolchain must contain only letters, numbers, dots, dashes, and underscores",
		},
		{
			name: "empty toolchain",
			env:  []string{"BASTION_INPUT_PROFILE=minimal", "BASTION_INPUT_TOOLCHAIN="},
			want: "Rust toolchain must contain only letters, numbers, dots, dashes, and underscores",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)

			cmd := exec.CommandContext(ctx, "sh", "setup_rust/install_rust.sh")

			cmd.Env = append(os.Environ(), tc.env...)

			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("setup_rust validation error = nil, want failure; output: %s", output)
			}

			if !strings.Contains(string(output), tc.want) {
				t.Fatalf("setup_rust validation output = %q, want to contain %q", output, tc.want)
			}
		})
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
