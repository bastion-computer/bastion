// Package basearchive reads and writes Bastion base image archives.
//
//nolint:wsl_v5 // Archive validation is easier to review with closely grouped checks.
package basearchive

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

const (
	// ContentType is the media type used for base import/export streams.
	ContentType = "application/vnd.bastion.base+tar+zstd"

	archiveFormat       = "bastion-base-v2"
	archiveManifestName = "manifest.json"
	archiveManifestMax  = 1 << 20

	// RootfsName is the root filesystem archive entry.
	RootfsName = "rootfs.img"
	// SeedName is the cloud-init seed archive entry.
	SeedName = "cidata.img"
	// SSHKeyName is the private SSH key archive entry.
	SSHKeyName = "ssh_key"
)

// ErrInvalid marks malformed or unsupported base archives.
var ErrInvalid = errors.New("invalid base archive")

// Metadata describes a built base image.
type Metadata struct {
	ContentAddress string `json:"contentAddress"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

// File describes a file included in a base archive.
type File struct {
	Name string
	Path string
	Mode os.FileMode
}

type manifest struct {
	Format string   `json:"format"`
	Base   Metadata `json:"base"`
}

type fileSummary struct {
	name   string
	size   int64
	digest string
}

// Files returns the canonical base artifact files rooted at dir.
func Files(dir string) []File {
	return []File{
		{Name: RootfsName, Path: filepath.Join(dir, RootfsName), Mode: 0o400},
		{Name: SeedName, Path: filepath.Join(dir, SeedName), Mode: 0o600},
		{Name: SSHKeyName, Path: filepath.Join(dir, SSHKeyName), Mode: 0o600},
	}
}

// ContentAddressForFiles returns the canonical content address for base artifact files.
func ContentAddressForFiles(ctx context.Context, files []File) (string, error) {
	summaries := make(map[string]fileSummary, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		summary, err := summarizeFile(file)
		if err != nil {
			return "", err
		}

		summaries[file.Name] = summary
	}

	return contentAddress(summaries), nil
}

// Write streams a compressed base archive.
func Write(ctx context.Context, writer io.Writer, metadata Metadata, files []File) error {
	if writer == nil {
		return errors.New("base archive writer is required")
	}

	contentAddress, err := ContentAddressForFiles(ctx, files)
	if err != nil {
		return err
	}

	if metadata.ContentAddress != "" && metadata.ContentAddress != contentAddress {
		return errors.New("base content address mismatch")
	}

	metadata.ContentAddress = contentAddress

	zstdWriter, err := zstd.NewWriter(writer, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("create base archive compressor: %w", err)
	}

	tarWriter := tar.NewWriter(zstdWriter)

	if err := writeJSON(ctx, tarWriter, archiveManifestName, manifest{Format: archiveFormat, Base: metadata}); err != nil {
		_ = tarWriter.Close()
		_ = zstdWriter.Close()

		return err
	}

	for _, file := range files {
		if err := writeFile(ctx, tarWriter, file); err != nil {
			_ = tarWriter.Close()
			_ = zstdWriter.Close()

			return err
		}
	}

	if err := tarWriter.Close(); err != nil {
		_ = zstdWriter.Close()

		return fmt.Errorf("close base archive: %w", err)
	}

	if err := zstdWriter.Close(); err != nil {
		return fmt.Errorf("close base archive compressor: %w", err)
	}

	return nil
}

// Read validates a base archive and returns its metadata.
func Read(ctx context.Context, reader io.Reader) (Metadata, error) {
	return read(ctx, reader, "")
}

// Extract validates and extracts a base archive into dstDir.
func Extract(ctx context.Context, reader io.Reader, dstDir string) (Metadata, error) {
	if strings.TrimSpace(dstDir) == "" {
		return Metadata{}, errors.New("base archive destination is required")
	}

	return read(ctx, reader, dstDir)
}

func writeJSON(ctx context.Context, writer *tar.Writer, name string, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	contents, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode base archive manifest: %w", err)
	}

	contents = append(contents, '\n')
	header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(contents)), ModTime: time.Unix(0, 0)}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write base archive manifest header: %w", err)
	}

	if _, err := writer.Write(contents); err != nil {
		return fmt.Errorf("write base archive manifest: %w", err)
	}

	return nil
}

func writeFile(ctx context.Context, writer *tar.Writer, file File) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	info, err := os.Stat(file.Path)
	if err != nil {
		return fmt.Errorf("stat base archive file %s: %w", file.Name, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("base archive file %s is not regular", file.Name)
	}

	header := &tar.Header{Name: file.Name, Typeflag: tar.TypeReg, Mode: int64(file.Mode.Perm()), Size: info.Size(), ModTime: time.Unix(0, 0)}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write base archive %s header: %w", file.Name, err)
	}

	in, err := os.Open(file.Path)
	if err != nil {
		return fmt.Errorf("open base archive file %s: %w", file.Name, err)
	}
	defer func() { _ = in.Close() }()

	if _, err := io.Copy(writer, in); err != nil {
		return fmt.Errorf("write base archive file %s: %w", file.Name, err)
	}

	return nil
}

//nolint:gocyclo // Coordinates decompression, validation, optional extraction, and content-address verification.
func read(ctx context.Context, reader io.Reader, dstDir string) (Metadata, error) {
	if reader == nil {
		return Metadata{}, errors.New("base archive reader is required")
	}

	zstdReader, err := zstd.NewReader(reader)
	if err != nil {
		return Metadata{}, fmt.Errorf("%w: open base archive compressor: %w", ErrInvalid, err)
	}
	defer zstdReader.Close()

	allowed := allowedFiles(dstDir)
	seen := map[string]bool{}
	summaries := map[string]fileSummary{}
	var parsed manifest
	manifestSeen := false
	tarReader := tar.NewReader(zstdReader)

	for {
		if err := ctx.Err(); err != nil {
			return Metadata{}, err
		}

		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return Metadata{}, fmt.Errorf("%w: read base archive: %w", ErrInvalid, err)
		}

		name, err := cleanName(header.Name)
		if err != nil {
			return Metadata{}, err
		}

		if seen[name] {
			return Metadata{}, fmt.Errorf("%w: duplicate archive entry %s", ErrInvalid, name)
		}
		seen[name] = true

		if name == archiveManifestName {
			if manifestSeen {
				return Metadata{}, fmt.Errorf("%w: duplicate archive manifest", ErrInvalid)
			}

			if err := readManifest(tarReader, header.Size, &parsed); err != nil {
				return Metadata{}, err
			}

			manifestSeen = true

			continue
		}

		file, ok := allowed[name]
		if !ok {
			return Metadata{}, fmt.Errorf("%w: unexpected archive entry %s", ErrInvalid, name)
		}

		if header.Typeflag != tar.TypeReg {
			return Metadata{}, fmt.Errorf("%w: archive entry %s is not a regular file", ErrInvalid, name)
		}

		summary, err := readArchiveFile(tarReader, header.Size, file)
		if err != nil {
			return Metadata{}, err
		}

		summaries[name] = summary
	}

	if !manifestSeen {
		return Metadata{}, fmt.Errorf("%w: archive manifest is missing", ErrInvalid)
	}

	if parsed.Format != archiveFormat {
		return Metadata{}, fmt.Errorf("%w: unsupported archive format %q", ErrInvalid, parsed.Format)
	}

	for name := range allowed {
		if !seen[name] {
			return Metadata{}, fmt.Errorf("%w: required archive entry %s is missing", ErrInvalid, name)
		}
	}

	metadata := parsed.Base
	computed := contentAddress(summaries)
	if metadata.ContentAddress != "" && metadata.ContentAddress != computed {
		return Metadata{}, fmt.Errorf("%w: base content address mismatch", ErrInvalid)
	}

	metadata.ContentAddress = computed

	return metadata, nil
}

func allowedFiles(dstDir string) map[string]File {
	out := map[string]File{}
	for _, file := range Files(dstDir) {
		if dstDir == "" {
			file.Path = ""
		}

		out[file.Name] = file
	}

	return out
}

func cleanName(name string) (string, error) {
	clean := path.Clean(name)
	if clean == "." || clean != name || path.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("%w: unsafe archive entry %s", ErrInvalid, name)
	}

	return clean, nil
}

func readManifest(reader io.Reader, size int64, out *manifest) error {
	if size < 0 || size > archiveManifestMax {
		return fmt.Errorf("%w: archive manifest is too large", ErrInvalid)
	}

	contents, err := io.ReadAll(io.LimitReader(reader, archiveManifestMax+1))
	if err != nil {
		return fmt.Errorf("%w: read archive manifest: %w", ErrInvalid, err)
	}

	if int64(len(contents)) > archiveManifestMax {
		return fmt.Errorf("%w: archive manifest is too large", ErrInvalid)
	}

	if err := json.Unmarshal(contents, out); err != nil {
		return fmt.Errorf("%w: parse archive manifest: %w", ErrInvalid, err)
	}

	return nil
}

func readArchiveFile(reader io.Reader, size int64, file File) (fileSummary, error) {
	if size < 0 {
		return fileSummary{}, fmt.Errorf("%w: archive entry %s has invalid size", ErrInvalid, file.Name)
	}

	writer := io.Discard
	var out *os.File
	if file.Path != "" {
		if err := os.MkdirAll(filepath.Dir(file.Path), 0o750); err != nil {
			return fileSummary{}, fmt.Errorf("create base archive directory: %w", err)
		}

		created, err := os.OpenFile(file.Path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode)
		if err != nil {
			return fileSummary{}, fmt.Errorf("create base archive file %s: %w", file.Name, err)
		}

		out = created
		writer = created
	}

	hash := sha256.New()
	read, err := io.Copy(io.MultiWriter(writer, hash), reader)
	if out != nil {
		if closeErr := out.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}

	if err != nil {
		return fileSummary{}, fmt.Errorf("read base archive file %s: %w", file.Name, err)
	}

	if read != size {
		return fileSummary{}, fmt.Errorf("%w: archive entry %s size mismatch", ErrInvalid, file.Name)
	}

	if file.Path != "" {
		if err := os.Chmod(file.Path, file.Mode); err != nil {
			return fileSummary{}, fmt.Errorf("chmod base archive file %s: %w", file.Name, err)
		}
	}

	return fileSummary{name: file.Name, size: read, digest: hex.EncodeToString(hash.Sum(nil))}, nil
}

func summarizeFile(file File) (fileSummary, error) {
	info, err := os.Stat(file.Path)
	if err != nil {
		return fileSummary{}, fmt.Errorf("stat base file %s: %w", file.Name, err)
	}

	if !info.Mode().IsRegular() {
		return fileSummary{}, fmt.Errorf("base file %s is not regular", file.Name)
	}

	in, err := os.Open(file.Path)
	if err != nil {
		return fileSummary{}, fmt.Errorf("open base file %s: %w", file.Name, err)
	}
	defer func() { _ = in.Close() }()

	hash := sha256.New()
	read, err := io.Copy(hash, in)
	if err != nil {
		return fileSummary{}, fmt.Errorf("hash base file %s: %w", file.Name, err)
	}

	return fileSummary{name: file.Name, size: read, digest: hex.EncodeToString(hash.Sum(nil))}, nil
}

func contentAddress(summaries map[string]fileSummary) string {
	names := make([]string, 0, len(summaries))
	for name := range summaries {
		names = append(names, name)
	}
	sort.Strings(names)

	hash := sha256.New()
	for _, name := range names {
		summary := summaries[name]
		_, _ = hash.Write([]byte(summary.name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.FormatInt(summary.size, 10)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(summary.digest))
		_, _ = hash.Write([]byte{0})
	}

	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}
