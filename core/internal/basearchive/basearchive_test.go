//nolint:wsl_v5 // Tests keep setup and assertions close to the operation under test.
package basearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
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

func TestFilesExcludeSnapshotArtifacts(t *testing.T) {
	t.Parallel()

	for _, file := range Files(t.TempDir()) {
		if strings.HasPrefix(file.Name, "snapshot/") {
			t.Fatalf("base archive file set includes snapshot artifact %s", file.Name)
		}
	}
}

func TestReadRejectsLegacySnapshotBaseArchive(t *testing.T) {
	t.Parallel()

	archive := writeLegacySnapshotBaseArchive(t)
	if _, err := Read(context.Background(), bytes.NewReader(archive)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("read legacy snapshot archive error = %v, want invalid", err)
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

func writeLegacySnapshotBaseArchive(t *testing.T) []byte {
	t.Helper()

	var out bytes.Buffer
	zstdWriter, err := zstd.NewWriter(&out)
	if err != nil {
		t.Fatalf("create compressor: %v", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)
	manifestContents, err := json.Marshal(manifest{Format: "bastion-base-v1", Base: Metadata{}})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	manifestContents = append(manifestContents, '\n')
	writeTarEntry(t, tarWriter, archiveManifestName, string(manifestContents))

	entries := map[string]string{
		RootfsName:                             "rootfs",
		SeedName:                               "seed",
		SSHKeyName:                             "ssh-key",
		path.Join("snapshot", "config.json"):   `{"disks":[]}`,
		path.Join("snapshot", "state.json"):    `{}`,
		path.Join("snapshot", "memory-ranges"): "memory",
	}

	for name, contents := range entries {
		writeTarEntry(t, tarWriter, name, contents)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := zstdWriter.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}

	return out.Bytes()
}

func writeTarEntry(t *testing.T, writer *tar.Writer, name, contents string) {
	t.Helper()

	header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(contents))}
	if err := writer.WriteHeader(header); err != nil {
		t.Fatalf("write %s header: %v", name, err)
	}

	if _, err := io.WriteString(writer, contents); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
