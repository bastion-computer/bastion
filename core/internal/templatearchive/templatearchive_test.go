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

func buildArchive(t *testing.T, archiveTemplate Template) []byte {
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

	if err := tarWriter.WriteHeader(&tar.Header{Name: "payload.txt", Mode: 0o600, Size: int64(len("payload"))}); err != nil {
		t.Fatalf("write payload header: %v", err)
	}

	if _, err := io.WriteString(tarWriter, "payload"); err != nil {
		t.Fatalf("write payload: %v", err)
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
