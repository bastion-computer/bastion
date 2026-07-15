package system

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/core/internal/opencodeasset"
)

func TestCheckOpenCodeReportsAvailableAssets(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := newOpenCodeStore(dataDir)
	writeTestFile(t, filepath.Join(store.dir, opencodeasset.BinaryName), 0o755)

	writeTestFile(t, filepath.Join(store.dir, opencodeasset.LinuxX64ArchiveName), 0o600)

	if err := store.writeManifest(opencodeasset.Manifest{Version: opencodeasset.Version, Architecture: archX8664, OpenCode: opencodeasset.BinaryName, Archive: opencodeasset.LinuxX64ArchiveName}); err != nil {
		t.Fatalf("write opencode manifest: %v", err)
	}

	tree := checkOpenCode(openCodeProbe{dataDir: dataDir, stat: os.Stat})
	if !tree.Available() {
		t.Fatalf("tree available = false, want true: %#v", tree)
	}
}

func TestCheckIncludesOpenCodeNode(t *testing.T) {
	t.Parallel()

	tree := Check(context.Background(), t.TempDir())

	var out bytes.Buffer
	if err := tree.Render(&out); err != nil {
		t.Fatalf("render tree: %v", err)
	}

	if !strings.Contains(out.String(), "opencode") {
		t.Fatalf("check output missing opencode node:\n%s", out.String())
	}
}

func TestCheckOpenCodeDisplaysPinnedAssetVersion(t *testing.T) {
	t.Parallel()

	tree := checkOpenCode(openCodeProbe{dataDir: t.TempDir(), stat: os.Stat})

	var out bytes.Buffer
	if err := tree.Render(&out); err != nil {
		t.Fatalf("render tree: %v", err)
	}

	if want := "opencode binary (v1.18.1)"; !strings.Contains(out.String(), want) {
		t.Fatalf("check output missing %q:\n%s", want, out.String())
	}
}

func TestAddOpenCodeDownloadsPinnedAsset(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()

	result, err := AddOpenCode(context.Background(), AddOpenCodeOptions{
		DataDir:    dataDir,
		downloader: testOpenCodeDownloader{t: t},
		probe:      openCodeProbe{dataDir: dataDir, arch: archX8664, stat: os.Stat},
	})
	if err != nil {
		t.Fatalf("AddOpenCode: %v", err)
	}

	if result.Path != filepath.Join(dataDir, opencodeasset.DirName) {
		t.Fatalf("result path = %q, want opencode store", result.Path)
	}

	store := newOpenCodeStore(dataDir)
	if !store.assetsNode().Available() {
		t.Fatalf("opencode assets unavailable after add: %#v", store.assetsNode())
	}

	manifest := store.readManifest()
	if manifest.Version != opencodeasset.Version || manifest.OpenCode != opencodeasset.BinaryName || manifest.Architecture != archX8664 || manifest.Archive != opencodeasset.LinuxX64ArchiveName {
		t.Fatalf("manifest = %#v, want pinned opencode asset", manifest)
	}
}

func TestRemoveOpenCodeOnlyRemovesOpenCodeData(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := newOpenCodeStore(dataDir)
	writeTestFile(t, filepath.Join(store.dir, opencodeasset.BinaryName), 0o755)
	writeTestFile(t, filepath.Join(store.dir, opencodeasset.LinuxX64ArchiveName), 0o600)
	writeTestFile(t, filepath.Join(dataDir, "sqlite.db"), 0o600)

	result, err := RemoveOpenCode(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("RemoveOpenCode: %v", err)
	}

	if result.Path != store.dir {
		t.Fatalf("result path = %q, want %q", result.Path, store.dir)
	}

	if _, err := os.Stat(store.dir); !os.IsNotExist(err) {
		t.Fatalf("opencode dir stat error = %v, want not exist", err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "sqlite.db")); err != nil {
		t.Fatalf("sqlite db stat: %v", err)
	}
}

type testOpenCodeDownloader struct {
	t *testing.T
}

func (d testOpenCodeDownloader) download(_ context.Context, store openCodeStore, arch string) (opencodeasset.Manifest, error) {
	d.t.Helper()

	writeTestFile(d.t, filepath.Join(store.dir, opencodeasset.BinaryName), 0o755)
	writeTestFile(d.t, filepath.Join(store.dir, opencodeasset.LinuxX64ArchiveName), 0o600)

	return opencodeasset.Manifest{
		Version:      opencodeasset.Version,
		Architecture: arch,
		OpenCode:     opencodeasset.BinaryName,
		Archive:      opencodeasset.LinuxX64ArchiveName,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}
