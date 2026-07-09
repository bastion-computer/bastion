package cluster

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/bastion-computer/bastion/core/internal/failure"
	templatesvc "github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/internal/services/utilization"
	"github.com/bastion-computer/bastion/core/internal/templatearchive"
)

const testBaseContentAddress = "sha256:base"

func TestValidateTemplateArchiveBaseAcceptsMatchingBaseAndRewindsArchive(t *testing.T) {
	t.Parallel()

	archive := writeClusterTestTemplateArchive(t, testBaseContentAddress)
	defer func() { _ = archive.Close() }()
	defer func() { _ = os.Remove(archive.Name()) }()

	if err := validateTemplateArchiveBase(context.Background(), archive, testBaseContentAddress); err != nil {
		t.Fatalf("validate template archive base: %v", err)
	}

	position, err := archive.Seek(0, io.SeekCurrent)
	if err != nil {
		t.Fatalf("read archive position: %v", err)
	}

	if position != 0 {
		t.Fatalf("archive position = %d, want 0", position)
	}
}

func TestValidateTemplateArchiveBaseRejectsMismatchedBase(t *testing.T) {
	t.Parallel()

	archive := writeClusterTestTemplateArchive(t, "sha256:other")
	defer func() { _ = archive.Close() }()
	defer func() { _ = os.Remove(archive.Name()) }()

	if err := validateTemplateArchiveBase(context.Background(), archive, testBaseContentAddress); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("validate template archive base error = %v, want invalid", err)
	}
}

func TestTemplateMetadataIncludesBaseContentAddress(t *testing.T) {
	t.Parallel()

	metadata := templateMetadata(templatesvc.Template{ID: "tpl_test", BaseContentAddress: testBaseContentAddress, CreatedAt: "2026-01-01T00:00:00Z"})

	if metadata.BaseContentAddress != testBaseContentAddress {
		t.Fatalf("metadata base content address = %q, want %s", metadata.BaseContentAddress, testBaseContentAddress)
	}
}

func TestDerivativeArchiveTemplateIncludesSourceBaseContentAddress(t *testing.T) {
	t.Parallel()

	derivativeConfig := json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)
	archiveTemplate := derivativeArchiveTemplate(templatesvc.Template{ID: "tpl_test", BaseContentAddress: testBaseContentAddress}, derivativeConfig)

	if archiveTemplate.BaseContentAddress != testBaseContentAddress {
		t.Fatalf("derivative archive base content address = %q, want %s", archiveTemplate.BaseContentAddress, testBaseContentAddress)
	}
}

func TestResourceHasCapacityUsesClusterSourceUsageWhenNodeUndercounts(t *testing.T) {
	t.Parallel()

	resource := utilization.Resource{Total: 2, Used: 1, Available: 1}

	if resourceHasCapacity(resource, 2, 1) {
		t.Fatal("resource has capacity with cluster source usage 2 and required 1, want exhausted")
	}
}

func writeClusterTestTemplateArchive(t *testing.T, baseContentAddress string) *os.File {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "template-*.tar.zst")
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	zstdWriter, err := zstd.NewWriter(file)
	if err != nil {
		t.Fatalf("create compressor: %v", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)
	archiveTemplate := templatearchive.Template{ID: "tpl_test", Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`), BaseContentAddress: baseContentAddress}

	manifestContents, err := json.Marshal(struct {
		Format   string                   `json:"format"`
		Template templatearchive.Template `json:"template"`
	}{Format: "bastion-template-v1", Template: archiveTemplate})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	manifestContents = append(manifestContents, '\n')
	if err := tarWriter.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifestContents))}); err != nil {
		t.Fatalf("write manifest header: %v", err)
	}

	if _, err := tarWriter.Write(manifestContents); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	rootfs := []byte("rootfs")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "rootfs.img", Mode: 0o600, Size: int64(len(rootfs))}); err != nil {
		t.Fatalf("write rootfs header: %v", err)
	}

	if _, err := tarWriter.Write(rootfs); err != nil {
		t.Fatalf("write rootfs: %v", err)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	if err := zstdWriter.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind archive: %v", err)
	}

	return file
}
