// Package templatearchive reads and rewrites Bastion template archive manifests.
package templatearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	archiveFormat       = "bastion-template-v1"
	archiveManifestName = "manifest.json"
	archiveRootfsName   = "rootfs.img"
	archiveMemoryName   = "snapshot/memory-ranges"
	archiveManifestMax  = 1 << 20
)

// ErrInvalid marks malformed or unsupported template archives.
var ErrInvalid = errors.New("invalid template archive")

// Template describes the template metadata embedded in an archive manifest.
type Template struct {
	ID                 string          `json:"id"`
	Key                *string         `json:"key,omitempty"`
	Config             json.RawMessage `json:"config"`
	BaseContentAddress string          `json:"baseContentAddress"`
}

type manifest struct {
	Format   string   `json:"format"`
	Template Template `json:"template"`
}

// ReadTemplate returns the template metadata from an archive manifest.
//
//nolint:gocyclo // Archive validation branches by tar entry type and required manifest state.
func ReadTemplate(ctx context.Context, archive io.Reader) (Template, error) {
	if archive == nil {
		return Template{}, errors.New("template archive reader is required")
	}

	zstdReader, err := zstd.NewReader(archive)
	if err != nil {
		return Template{}, fmt.Errorf("%w: open template archive: %w", ErrInvalid, err)
	}
	defer zstdReader.Close()

	tarReader := tar.NewReader(zstdReader)
	manifestSeen := false
	rootfsSeen := false

	var archiveManifest manifest

	for {
		if err := ctx.Err(); err != nil {
			return Template{}, err
		}

		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return Template{}, fmt.Errorf("%w: read template archive: %w", ErrInvalid, err)
		}

		if err := validateArchiveEntry(header); err != nil {
			return Template{}, err
		}

		switch header.Name {
		case archiveManifestName:
			if manifestSeen {
				return Template{}, fmt.Errorf("%w: template archive contains duplicate manifest", ErrInvalid)
			}

			read, err := readManifest(tarReader, header.Size)
			if err != nil {
				return Template{}, err
			}

			archiveManifest = read
			manifestSeen = true
		case archiveRootfsName:
			rootfsSeen = true

			if _, err := io.CopyN(io.Discard, tarReader, header.Size); err != nil {
				return Template{}, fmt.Errorf("%w: read template archive overlay: %w", ErrInvalid, err)
			}
		default:
			return Template{}, fmt.Errorf("%w: template archive contains unexpected entry %s", ErrInvalid, header.Name)
		}
	}

	if !manifestSeen {
		return Template{}, fmt.Errorf("%w: template archive missing manifest", ErrInvalid)
	}

	if !rootfsSeen {
		return Template{}, fmt.Errorf("%w: template archive missing %s", ErrInvalid, archiveRootfsName)
	}

	return archiveManifest.Template, nil
}

func validateArchiveEntry(header *tar.Header) error {
	if header.Typeflag != tar.TypeReg {
		return fmt.Errorf("%w: template archive entry %s is not a regular file", ErrInvalid, header.Name)
	}

	if header.Size < 0 {
		return fmt.Errorf("%w: template archive entry %s has invalid size", ErrInvalid, header.Name)
	}

	if header.Name == archiveMemoryName {
		return fmt.Errorf("%w: template archive contains unsupported entry %s", ErrInvalid, archiveMemoryName)
	}

	switch header.Name {
	case archiveManifestName, archiveRootfsName:
		return nil
	default:
		return fmt.Errorf("%w: template archive contains unexpected entry %s", ErrInvalid, header.Name)
	}
}

// RewriteTemplate copies archive to writer with its manifest template replaced.
func RewriteTemplate(ctx context.Context, archive io.Reader, writer io.Writer, archiveTemplate Template) error {
	if err := validateRewriteTemplateRequest(archive, writer, archiveTemplate); err != nil {
		return err
	}

	zstdReader, err := zstd.NewReader(archive)
	if err != nil {
		return fmt.Errorf("%w: open template archive: %w", ErrInvalid, err)
	}
	defer zstdReader.Close()

	zstdWriter, err := zstd.NewWriter(writer, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("create template archive compressor: %w", err)
	}

	tarReader := tar.NewReader(zstdReader)
	tarWriter := tar.NewWriter(zstdWriter)
	closed := false

	defer func() {
		if !closed {
			_ = closeArchiveWriters(tarWriter, zstdWriter)
		}
	}()

	if err := rewriteArchiveEntries(ctx, tarReader, tarWriter, archiveTemplate); err != nil {
		return err
	}

	closed = true

	if err := closeArchiveWriters(tarWriter, zstdWriter); err != nil {
		return err
	}

	return nil
}

func validateRewriteTemplateRequest(archive io.Reader, writer io.Writer, archiveTemplate Template) error {
	if archive == nil {
		return errors.New("template archive reader is required")
	}

	if writer == nil {
		return errors.New("template archive writer is required")
	}

	if strings.TrimSpace(archiveTemplate.ID) == "" {
		return errors.New("template id is required")
	}

	if len(archiveTemplate.Config) == 0 || !json.Valid(archiveTemplate.Config) {
		return errors.New("template config must be valid JSON")
	}

	if strings.TrimSpace(archiveTemplate.BaseContentAddress) == "" {
		return errors.New("template base content address is required")
	}

	return nil
}

func rewriteArchiveEntries(ctx context.Context, tarReader *tar.Reader, tarWriter *tar.Writer, archiveTemplate Template) error {
	manifestSeen := false
	rootfsSeen := false

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("%w: read template archive: %w", ErrInvalid, err)
		}

		if err := validateArchiveEntry(header); err != nil {
			return err
		}

		if header.Name == archiveManifestName {
			if manifestSeen {
				return fmt.Errorf("%w: template archive contains duplicate manifest", ErrInvalid)
			}

			if err := writeReplacementManifest(tarReader, tarWriter, header, archiveTemplate); err != nil {
				return err
			}

			manifestSeen = true

			continue
		}

		if header.Name == archiveRootfsName {
			rootfsSeen = true
		}

		if err := copyArchiveEntry(tarReader, tarWriter, header); err != nil {
			return err
		}
	}

	if !manifestSeen {
		return fmt.Errorf("%w: template archive missing manifest", ErrInvalid)
	}

	if !rootfsSeen {
		return fmt.Errorf("%w: template archive missing %s", ErrInvalid, archiveRootfsName)
	}

	return nil
}

func writeReplacementManifest(tarReader *tar.Reader, tarWriter *tar.Writer, header *tar.Header, archiveTemplate Template) error {
	if _, err := readManifest(tarReader, header.Size); err != nil {
		return err
	}

	contents, err := json.Marshal(manifest{Format: archiveFormat, Template: archiveTemplate})
	if err != nil {
		return fmt.Errorf("encode template archive manifest: %w", err)
	}

	contents = append(contents, '\n')
	copiedHeader := *header
	copiedHeader.Size = int64(len(contents))

	if err := tarWriter.WriteHeader(&copiedHeader); err != nil {
		return fmt.Errorf("write template archive manifest header: %w", err)
	}

	if _, err := tarWriter.Write(contents); err != nil {
		return fmt.Errorf("write template archive manifest: %w", err)
	}

	return nil
}

func copyArchiveEntry(tarReader *tar.Reader, tarWriter *tar.Writer, header *tar.Header) error {
	copiedHeader := *header
	if err := tarWriter.WriteHeader(&copiedHeader); err != nil {
		return fmt.Errorf("write template archive header %s: %w", header.Name, err)
	}

	if header.Size == 0 {
		return nil
	}

	if _, err := io.CopyN(tarWriter, tarReader, header.Size); err != nil {
		return fmt.Errorf("write template archive entry %s: %w", header.Name, err)
	}

	return nil
}

func closeArchiveWriters(tarWriter *tar.Writer, zstdWriter *zstd.Encoder) error {
	if err := tarWriter.Close(); err != nil {
		_ = zstdWriter.Close()

		return fmt.Errorf("close template archive: %w", err)
	}

	if err := zstdWriter.Close(); err != nil {
		return fmt.Errorf("close template archive compressor: %w", err)
	}

	return nil
}

func readManifest(reader io.Reader, size int64) (manifest, error) {
	if size < 0 || size > archiveManifestMax {
		return manifest{}, fmt.Errorf("%w: template archive manifest is too large", ErrInvalid)
	}

	var buffer bytes.Buffer
	if _, err := io.CopyN(&buffer, reader, size); err != nil {
		return manifest{}, fmt.Errorf("%w: read template archive manifest: %w", ErrInvalid, err)
	}

	var archiveManifest manifest
	if err := json.Unmarshal(buffer.Bytes(), &archiveManifest); err != nil {
		return manifest{}, fmt.Errorf("%w: parse template archive manifest: %w", ErrInvalid, err)
	}

	if archiveManifest.Format != archiveFormat {
		return manifest{}, fmt.Errorf("%w: unsupported template archive format %q", ErrInvalid, archiveManifest.Format)
	}

	if len(archiveManifest.Template.Config) == 0 || !json.Valid(archiveManifest.Template.Config) {
		return manifest{}, fmt.Errorf("%w: template archive manifest config must be valid JSON", ErrInvalid)
	}

	if strings.TrimSpace(archiveManifest.Template.BaseContentAddress) == "" {
		return manifest{}, fmt.Errorf("%w: template archive manifest missing base content address", ErrInvalid)
	}

	return archiveManifest, nil
}
