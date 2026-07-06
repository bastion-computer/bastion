//nolint:wsl_v5 // Tests keep setup and assertions close to the operation under test.
package basearchive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReadAndExtractBaseArchive(t *testing.T) {
	t.Parallel()

	sourceDir := writeTestBaseFiles(t)
	contentAddress, err := ContentAddressForFiles(context.Background(), Files(sourceDir))
	if err != nil {
		t.Fatalf("content address: %v", err)
	}

	metadata := Metadata{ContentAddress: contentAddress, CreatedAt: "created", UpdatedAt: "updated"}
	var archive bytes.Buffer
	if err := Write(context.Background(), &archive, metadata, Files(sourceDir)); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	read, err := Read(context.Background(), bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}

	if read != metadata {
		t.Fatalf("read metadata = %#v, want %#v", read, metadata)
	}

	dstDir := t.TempDir()
	extracted, err := Extract(context.Background(), bytes.NewReader(archive.Bytes()), dstDir)
	if err != nil {
		t.Fatalf("extract archive: %v", err)
	}

	if extracted != metadata {
		t.Fatalf("extracted metadata = %#v, want %#v", extracted, metadata)
	}

	for _, file := range Files(dstDir) {
		if _, err := os.Stat(file.Path); err != nil {
			t.Fatalf("extracted file %s missing: %v", file.Name, err)
		}
	}
}

func TestWriteRejectsStaleContentAddress(t *testing.T) {
	t.Parallel()

	dir := writeTestBaseFiles(t)
	var archive bytes.Buffer
	err := Write(context.Background(), &archive, Metadata{ContentAddress: "sha256:stale"}, Files(dir))
	if err == nil || !strings.Contains(err.Error(), "content address mismatch") {
		t.Fatalf("write error = %v, want content address mismatch", err)
	}
}

//nolint:paralleltest // t.Chdir changes process working directory to assert Read has no side effects.
func TestReadDoesNotExtractFiles(t *testing.T) {
	sourceDir := writeTestBaseFiles(t)
	contentAddress, err := ContentAddressForFiles(context.Background(), Files(sourceDir))
	if err != nil {
		t.Fatalf("content address: %v", err)
	}

	var archive bytes.Buffer
	if err := Write(context.Background(), &archive, Metadata{ContentAddress: contentAddress}, Files(sourceDir)); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	cwd := t.TempDir()
	t.Chdir(cwd)

	if _, err := Read(context.Background(), bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatalf("read archive: %v", err)
	}

	for _, file := range Files(cwd) {
		if _, err := os.Stat(file.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read wrote %s: stat error = %v, want not exist", file.Name, err)
		}
	}
}

func writeTestBaseFiles(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	files := map[string]string{
		RootfsName: "rootfs",
		SeedName:   "seed",
		SSHKeyName: "ssh-key",
		filepath.Join(SnapshotDirName, SnapshotConfigName): `{"disks":[]}`,
		filepath.Join(SnapshotDirName, SnapshotStateName):  `{}`,
		filepath.Join(SnapshotDirName, SnapshotMemoryName): "memory",
	}

	for name, contents := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("create dir for %s: %v", name, err)
		}

		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	return dir
}
