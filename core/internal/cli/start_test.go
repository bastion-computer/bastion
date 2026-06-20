//go:build !darwin

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartCommandRequiresProcessSubcommand(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand()

	startCmd, remaining, err := cmd.Find([]string{startUse})
	if err != nil {
		t.Fatalf("find start command: %v", err)
	}

	if startCmd.Name() != startUse {
		t.Fatalf("command = %q, want %q", startCmd.Name(), startUse)
	}

	if len(remaining) != 0 {
		t.Fatalf("remaining args = %v, want none", remaining)
	}

	if startCmd.Runnable() {
		t.Fatal("bastion start is runnable, want process subcommand required")
	}
}

func TestStartCommandIncludesProcessSubcommands(t *testing.T) {
	t.Parallel()

	for _, process := range []string{startAPIUse, startDaemonUse, startClusterUse} {
		t.Run(process, func(t *testing.T) {
			t.Parallel()

			cmd := NewRootCommand()

			processCmd, remaining, err := cmd.Find([]string{startUse, process})
			if err != nil {
				t.Fatalf("find start %s command: %v", process, err)
			}

			if processCmd.Name() != process {
				t.Fatalf("command = %q, want %q", processCmd.Name(), process)
			}

			if len(remaining) != 0 {
				t.Fatalf("remaining args = %v, want none", remaining)
			}
		})
	}
}

func TestStartClusterCommandIncludesArchiveStorageFlags(t *testing.T) {
	t.Setenv("BASTION_CLUSTER_ARCHIVE_BUCKET", "archives")
	t.Setenv("BASTION_CLUSTER_ARCHIVE_ENDPOINT", "http://localhost:9000")
	t.Setenv("BASTION_CLUSTER_ARCHIVE_REGION", "us-east-1")
	t.Setenv("BASTION_CLUSTER_ARCHIVE_ACCESS_KEY_ID", "minio")
	t.Setenv("BASTION_CLUSTER_ARCHIVE_SECRET_ACCESS_KEY", "secret")
	t.Setenv("BASTION_CLUSTER_ARCHIVE_FORCE_PATH_STYLE", "true")

	cmd := NewRootCommand()

	clusterCmd, remaining, err := cmd.Find([]string{startUse, startClusterUse})
	if err != nil {
		t.Fatalf("find start cluster command: %v", err)
	}

	if len(remaining) != 0 {
		t.Fatalf("remaining args = %v, want none", remaining)
	}

	want := map[string]string{
		"archive-bucket":            "archives",
		"archive-endpoint":          "http://localhost:9000",
		"archive-region":            "us-east-1",
		"archive-access-key-id":     "minio",
		"archive-secret-access-key": "secret",
		"archive-force-path-style":  "true",
	}
	for name, value := range want {
		flag := clusterCmd.Flag(name)
		if flag == nil {
			t.Fatalf("start cluster flag %q is missing", name)
		}

		if flag.DefValue != value {
			t.Fatalf("start cluster flag %q default = %q, want %q", name, flag.DefValue, value)
		}
	}
}

func TestWaitForDataDirTimesOutWithoutCreatingDir(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "missing")

	err := waitForDataDir(context.Background(), dataDir, 10*time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting") {
		t.Fatalf("wait error = %v, want timeout", err)
	}

	if _, statErr := os.Stat(dataDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("data dir stat error = %v, want not exist", statErr)
	}
}

func TestWaitForDataDirSucceedsWhenDirAppears(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	created := make(chan error, 1)

	go func() {
		time.Sleep(5 * time.Millisecond)

		created <- os.Mkdir(dataDir, 0o750)
	}()

	if err := waitForDataDir(context.Background(), dataDir, time.Second, time.Millisecond); err != nil {
		t.Fatalf("wait for data dir: %v", err)
	}

	if err := <-created; err != nil {
		t.Fatalf("create data dir: %v", err)
	}
}

func TestWaitForDataDirRejectsNonDirectory(t *testing.T) {
	t.Parallel()

	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(dataDir, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write data dir file: %v", err)
	}

	err := waitForDataDir(context.Background(), dataDir, time.Second, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("wait error = %v, want not a directory", err)
	}
}
