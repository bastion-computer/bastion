package templatearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestReadAndRewriteTemplateArchiveManifest(t *testing.T) {
	t.Parallel()

	source := buildArchive(t, Template{ID: "tpl_derivative", Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"echo ${{ secret.sec_derivative }}"}]}}`)})

	read, err := ReadTemplate(context.Background(), bytes.NewReader(source))
	if err != nil {
		t.Fatalf("read template archive: %v", err)
	}

	if read.ID != "tpl_derivative" {
		t.Fatalf("read id = %q, want derivative", read.ID)
	}

	key := "source"

	var rewritten bytes.Buffer

	if err := RewriteTemplate(context.Background(), bytes.NewReader(source), &rewritten, Template{ID: "tpl_source", Key: &key, Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[{"run":"echo ${{ secret.source-key }}"}]}}`)}); err != nil {
		t.Fatalf("rewrite template archive: %v", err)
	}

	read, err = ReadTemplate(context.Background(), bytes.NewReader(rewritten.Bytes()))
	if err != nil {
		t.Fatalf("read rewritten template archive: %v", err)
	}

	if read.ID != "tpl_source" || read.Key == nil || *read.Key != key || !bytes.Contains(read.Config, []byte("secret.source-key")) {
		t.Fatalf("rewritten template = %#v config %s, want source manifest", read, read.Config)
	}

	if payload := readArchiveEntry(t, rewritten.Bytes(), "payload.txt"); payload != "payload" {
		t.Fatalf("payload entry = %q, want preserved payload", payload)
	}
}

func TestReadAndRewriteRejectSnapshotMemoryEntry(t *testing.T) {
	t.Parallel()

	source := buildArchiveEntries(t,
		Template{ID: "tpl_memory", Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)},
		map[string]string{archiveMemoryName: "memory"},
	)

	if _, err := ReadTemplate(context.Background(), bytes.NewReader(source)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("read archive error = %v, want invalid", err)
	}

	var rewritten bytes.Buffer
	if err := RewriteTemplate(context.Background(), bytes.NewReader(source), &rewritten, Template{ID: "tpl_rewritten", Config: json.RawMessage(`{"agents":{"opencode":{}},"actions":{"init":[]}}`)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("rewrite archive error = %v, want invalid", err)
	}
}

func buildArchive(t *testing.T, archiveTemplate Template) []byte {
	t.Helper()

	return buildArchiveEntries(t, archiveTemplate, map[string]string{"payload.txt": "payload"})
}

func buildArchiveEntries(t *testing.T, archiveTemplate Template, entries map[string]string) []byte {
	t.Helper()

	var out bytes.Buffer

	zstdWriter, err := zstd.NewWriter(&out)
	if err != nil {
		t.Fatalf("create compressor: %v", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)

	manifestContents, err := json.Marshal(manifest{Format: archiveFormat, Template: archiveTemplate})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	manifestContents = append(manifestContents, '\n')

	if err := tarWriter.WriteHeader(&tar.Header{Name: archiveManifestName, Mode: 0o600, Size: int64(len(manifestContents))}); err != nil {
		t.Fatalf("write manifest header: %v", err)
	}

	if _, err := tarWriter.Write(manifestContents); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	for name, contents := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents))}); err != nil {
			t.Fatalf("write %s header: %v", name, err)
		}

		if _, err := io.WriteString(tarWriter, contents); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	if err := zstdWriter.Close(); err != nil {
		t.Fatalf("close compressor: %v", err)
	}

	return out.Bytes()
}

func readArchiveEntry(t *testing.T, archive []byte, name string) string {
	t.Helper()

	zstdReader, err := zstd.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			t.Fatalf("archive entry %s not found", name)
		}

		if err != nil {
			t.Fatalf("read archive: %v", err)
		}

		if header.Name != name {
			continue
		}

		contents, err := io.ReadAll(tarReader)
		if err != nil {
			t.Fatalf("read entry: %v", err)
		}

		return string(contents)
	}
}
