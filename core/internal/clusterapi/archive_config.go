package clusterapi

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/klauspost/compress/zstd"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

const templateArchiveManifestName = "manifest.json"

func rewriteTemplateArchiveConfig(ctx context.Context, archive []byte, config json.RawMessage) ([]byte, error) {
	if len(config) == 0 || !json.Valid(config) {
		return nil, fmt.Errorf("%w: template config must be valid JSON", failure.ErrInvalid)
	}

	zstdReader, err := zstd.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read template archive: %w", err)
	}
	defer zstdReader.Close()

	var out bytes.Buffer

	zstdWriter, err := zstd.NewWriter(&out, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("create template archive compressor: %w", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)

	foundManifest, err := copyTemplateArchiveEntries(ctx, tar.NewReader(zstdReader), tarWriter, config)
	if err != nil {
		_ = tarWriter.Close()
		_ = zstdWriter.Close()

		return nil, err
	}

	if !foundManifest {
		_ = tarWriter.Close()
		_ = zstdWriter.Close()

		return nil, fmt.Errorf("%w: template archive missing manifest", failure.ErrInvalid)
	}

	if err := tarWriter.Close(); err != nil {
		_ = zstdWriter.Close()

		return nil, fmt.Errorf("close template archive: %w", err)
	}

	if err := zstdWriter.Close(); err != nil {
		return nil, fmt.Errorf("close template archive compressor: %w", err)
	}

	return out.Bytes(), nil
}

func copyTemplateArchiveEntries(ctx context.Context, reader *tar.Reader, writer *tar.Writer, config json.RawMessage) (bool, error) {
	foundManifest := false

	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}

		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return foundManifest, nil
		}

		if err != nil {
			return false, fmt.Errorf("read template archive entry: %w", err)
		}

		if path.Clean(header.Name) == templateArchiveManifestName {
			if err := copyRewrittenTemplateArchiveManifest(reader, writer, header, config); err != nil {
				return false, err
			}

			foundManifest = true

			continue
		}

		if err := copyTemplateArchiveEntry(reader, writer, header); err != nil {
			return false, err
		}
	}
}

func copyRewrittenTemplateArchiveManifest(reader io.Reader, writer *tar.Writer, header *tar.Header, config json.RawMessage) error {
	rewritten, err := rewriteTemplateArchiveManifestConfig(reader, config)
	if err != nil {
		return err
	}

	rewrittenHeader := *header
	rewrittenHeader.Size = int64(len(rewritten))

	if err := writer.WriteHeader(&rewrittenHeader); err != nil {
		return fmt.Errorf("write template archive manifest header: %w", err)
	}

	if _, err := writer.Write(rewritten); err != nil {
		return fmt.Errorf("write template archive manifest: %w", err)
	}

	return nil
}

func copyTemplateArchiveEntry(reader *tar.Reader, writer *tar.Writer, header *tar.Header) error {
	copiedHeader := *header
	if err := writer.WriteHeader(&copiedHeader); err != nil {
		return fmt.Errorf("write template archive entry header: %w", err)
	}

	if header.Size == 0 {
		return nil
	}

	if _, err := io.CopyN(writer, reader, header.Size); err != nil {
		return fmt.Errorf("write template archive entry: %w", err)
	}

	return nil
}

func rewriteTemplateArchiveManifestConfig(reader io.Reader, config json.RawMessage) ([]byte, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read template archive manifest: %w", err)
	}

	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return nil, fmt.Errorf("parse template archive manifest: %w", err)
	}

	templateRaw, ok := manifest["template"]
	if !ok {
		return nil, fmt.Errorf("%w: template archive manifest missing template", failure.ErrInvalid)
	}

	var archivedTemplate map[string]json.RawMessage
	if err := json.Unmarshal(templateRaw, &archivedTemplate); err != nil {
		return nil, fmt.Errorf("parse template archive manifest template: %w", err)
	}

	archivedTemplate["config"] = append(json.RawMessage(nil), config...)

	rewrittenTemplate, err := json.Marshal(archivedTemplate)
	if err != nil {
		return nil, fmt.Errorf("encode template archive manifest template: %w", err)
	}

	manifest["template"] = rewrittenTemplate

	rewritten, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("encode template archive manifest: %w", err)
	}

	return append(rewritten, '\n'), nil
}
