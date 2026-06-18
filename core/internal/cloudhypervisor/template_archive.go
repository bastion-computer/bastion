package cloudhypervisor

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	templateArchiveFormat       = "bastion-template-v1"
	templateArchiveManifestName = "manifest.json"
	templateArchiveManifestMax  = 1 << 20
)

// ErrInvalidTemplateArchive marks malformed or unsupported template archives.
var ErrInvalidTemplateArchive = errors.New("invalid template archive")

type templateArchiveManifest struct {
	Format   string   `json:"format"`
	Template Template `json:"template"`
}

// ExportTemplate streams a compressed archive containing template metadata and prepared artifacts.
func (m Manager) ExportTemplate(ctx context.Context, req ExportTemplateRequest) error {
	m = m.withDefaults()

	if strings.TrimSpace(req.Template.ID) == "" {
		return errors.New("template id is required")
	}

	if len(req.Template.Config) == 0 || !json.Valid(req.Template.Config) {
		return errors.New("template config must be valid JSON")
	}

	if req.Writer == nil {
		return errors.New("template archive writer is required")
	}

	prepared, err := loadPreparedTemplate(m.DataDir, req.Template.ID)
	if err != nil {
		return err
	}

	gzipWriter, err := gzip.NewWriterLevel(req.Writer, gzip.BestSpeed)
	if err != nil {
		return fmt.Errorf("create template archive compressor: %w", err)
	}

	tarWriter := tar.NewWriter(gzipWriter)

	manifest := templateArchiveManifest{
		Format: templateArchiveFormat,
		Template: Template{
			ID:     req.Template.ID,
			Key:    req.Template.Key,
			Config: append(json.RawMessage(nil), req.Template.Config...),
		},
	}

	if err := writeTemplateArchiveJSON(ctx, tarWriter, templateArchiveManifestName, manifest); err != nil {
		_ = tarWriter.Close()
		_ = gzipWriter.Close()

		return err
	}

	for _, file := range preparedTemplateArchiveFiles(prepared.TemplateDir) {
		if err := writeTemplateArchiveFile(ctx, tarWriter, file.archiveName, file.path); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()

			return err
		}
	}

	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()

		return fmt.Errorf("close template archive: %w", err)
	}

	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close template archive compressor: %w", err)
	}

	return nil
}

// ImportTemplate restores prepared artifacts from a compressed template archive.
func (m Manager) ImportTemplate(ctx context.Context, req ImportTemplateRequest) (ImportedTemplate, error) {
	m = m.withDefaults()

	templateID := strings.TrimSpace(req.TemplateID)
	if templateID == "" {
		return ImportedTemplate{}, errors.New("template id is required")
	}

	if req.Reader == nil {
		return ImportedTemplate{}, errors.New("template archive reader is required")
	}

	templatesPath := filepath.Join(m.DataDir, templatesDir)
	if err := os.MkdirAll(templatesPath, 0o750); err != nil {
		return ImportedTemplate{}, fmt.Errorf("create templates directory: %w", err)
	}

	finalDir := templateDir(m.DataDir, templateID)
	if _, err := os.Stat(finalDir); err == nil {
		return ImportedTemplate{}, fmt.Errorf("template directory %s already exists", templateID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ImportedTemplate{}, fmt.Errorf("stat template directory: %w", err)
	}

	tmpDir, err := os.MkdirTemp(templatesPath, "."+templateID+".import-*")
	if err != nil {
		return ImportedTemplate{}, fmt.Errorf("create import template directory: %w", err)
	}

	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	manifest, err := extractTemplateArchive(ctx, req.Reader, tmpDir)
	if err != nil {
		return ImportedTemplate{}, err
	}

	if err := chownIfConfigured(filepath.Join(tmpDir, envRootfsFileName), m.UID, m.GID); err != nil {
		return ImportedTemplate{}, fmt.Errorf("chown imported template rootfs: %w", err)
	}

	if err := os.Rename(tmpDir, finalDir); err != nil {
		return ImportedTemplate{}, fmt.Errorf("install imported template artifacts: %w", err)
	}

	removeTemp = false

	return ImportedTemplate{
		Template: Template{
			ID:     templateID,
			Config: append(json.RawMessage(nil), manifest.Template.Config...),
		},
		UpdatedAt: now(),
	}, nil
}

type templateArchiveFile struct {
	archiveName string
	path        string
	mode        os.FileMode
}

func preparedTemplateArchiveFiles(templateDir string) []templateArchiveFile {
	return []templateArchiveFile{
		{archiveName: envRootfsFileName, path: filepath.Join(templateDir, envRootfsFileName), mode: 0o400},
		{archiveName: envSeedFileName, path: filepath.Join(templateDir, envSeedFileName), mode: 0o600},
		{archiveName: path.Join(snapshotDirName, snapshotConfigFileName), path: filepath.Join(templateDir, snapshotDirName, snapshotConfigFileName), mode: 0o600},
		{archiveName: path.Join(snapshotDirName, snapshotStateFileName), path: filepath.Join(templateDir, snapshotDirName, snapshotStateFileName), mode: 0o600},
		{archiveName: path.Join(snapshotDirName, snapshotMemoryFileName), path: filepath.Join(templateDir, snapshotDirName, snapshotMemoryFileName), mode: 0o600},
	}
}

func writeTemplateArchiveJSON(ctx context.Context, writer *tar.Writer, name string, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	contents, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode template archive manifest: %w", err)
	}

	contents = append(contents, '\n')

	header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(contents))}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write template archive manifest header: %w", err)
	}

	if _, err := writer.Write(contents); err != nil {
		return fmt.Errorf("write template archive manifest: %w", err)
	}

	return nil
}

func writeTemplateArchiveFile(ctx context.Context, writer *tar.Writer, archiveName, sourcePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat template archive file %s: %w", filepath.Base(sourcePath), err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("template archive file %s is not regular", filepath.Base(sourcePath))
	}

	file, err := os.Open(sourcePath) //nolint:gosec // Source path is rooted in a prepared template directory.
	if err != nil {
		return fmt.Errorf("open template archive file %s: %w", filepath.Base(sourcePath), err)
	}
	defer func() { _ = file.Close() }()

	header := &tar.Header{Name: archiveName, Mode: int64(info.Mode().Perm()), Size: info.Size()}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write template archive file header %s: %w", archiveName, err)
	}

	copied, err := io.Copy(writer, file)
	if err != nil {
		return fmt.Errorf("write template archive file %s: %w", archiveName, err)
	}

	if copied != info.Size() {
		return fmt.Errorf("write template archive file %s: copied %d bytes, want %d", archiveName, copied, info.Size())
	}

	return nil
}

func extractTemplateArchive(ctx context.Context, reader io.Reader, templateDir string) (templateArchiveManifest, error) {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return templateArchiveManifest{}, fmt.Errorf("%w: open template archive: %w", ErrInvalidTemplateArchive, err)
	}
	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)
	state := newTemplateArchiveReadState(templateDir)

	for {
		done, err := state.readNext(ctx, tarReader)
		if done {
			break
		}

		if err != nil {
			return templateArchiveManifest{}, err
		}
	}

	return state.validate()
}

type templateArchiveReadState struct {
	expectedFiles map[string]templateArchiveFile
	seen          map[string]bool
	manifest      templateArchiveManifest
}

func newTemplateArchiveReadState(templateDir string) templateArchiveReadState {
	expectedFiles := make(map[string]templateArchiveFile)
	for _, file := range preparedTemplateArchiveFiles(templateDir) {
		expectedFiles[file.archiveName] = file
	}

	return templateArchiveReadState{
		expectedFiles: expectedFiles,
		seen:          make(map[string]bool, len(expectedFiles)+1),
	}
}

func (s *templateArchiveReadState) readNext(ctx context.Context, reader *tar.Reader) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	header, err := reader.Next()
	if errors.Is(err, io.EOF) {
		return true, nil
	}

	if err != nil {
		return false, fmt.Errorf("%w: read template archive: %w", ErrInvalidTemplateArchive, err)
	}

	return false, s.readEntry(reader, header)
}

func (s *templateArchiveReadState) readEntry(reader io.Reader, header *tar.Header) error {
	name, err := canonicalTemplateArchiveName(header.Name)
	if err != nil {
		return err
	}

	if header.Typeflag != tar.TypeReg {
		return fmt.Errorf("%w: template archive entry %s is not a regular file", ErrInvalidTemplateArchive, name)
	}

	if s.seen[name] {
		return fmt.Errorf("%w: template archive contains duplicate entry %s", ErrInvalidTemplateArchive, name)
	}

	if name == templateArchiveManifestName {
		manifest, err := readTemplateArchiveManifest(reader, header.Size)
		if err != nil {
			return err
		}

		s.manifest = manifest
		s.seen[name] = true

		return nil
	}

	file, ok := s.expectedFiles[name]
	if !ok {
		return fmt.Errorf("%w: template archive contains unexpected entry %s", ErrInvalidTemplateArchive, name)
	}

	if err := extractTemplateArchiveFile(reader, header.Size, file); err != nil {
		return err
	}

	s.seen[name] = true

	return nil
}

func (s templateArchiveReadState) validate() (templateArchiveManifest, error) {
	if !s.seen[templateArchiveManifestName] {
		return templateArchiveManifest{}, fmt.Errorf("%w: template archive missing manifest", ErrInvalidTemplateArchive)
	}

	for name := range s.expectedFiles {
		if !s.seen[name] {
			return templateArchiveManifest{}, fmt.Errorf("%w: template archive missing %s", ErrInvalidTemplateArchive, name)
		}
	}

	if s.manifest.Format != templateArchiveFormat {
		return templateArchiveManifest{}, fmt.Errorf("%w: unsupported template archive format %q", ErrInvalidTemplateArchive, s.manifest.Format)
	}

	if len(s.manifest.Template.Config) == 0 || !json.Valid(s.manifest.Template.Config) {
		return templateArchiveManifest{}, fmt.Errorf("%w: template archive manifest config must be valid JSON", ErrInvalidTemplateArchive)
	}

	return s.manifest, nil
}

func canonicalTemplateArchiveName(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("%w: template archive contains unsafe entry %q", ErrInvalidTemplateArchive, name)
	}

	clean := path.Clean(name)
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", fmt.Errorf("%w: template archive contains unsafe entry %q", ErrInvalidTemplateArchive, name)
	}

	return clean, nil
}

func readTemplateArchiveManifest(reader io.Reader, size int64) (templateArchiveManifest, error) {
	if size < 0 || size > templateArchiveManifestMax {
		return templateArchiveManifest{}, fmt.Errorf("%w: template archive manifest is too large", ErrInvalidTemplateArchive)
	}

	contents, err := io.ReadAll(io.LimitReader(reader, templateArchiveManifestMax+1))
	if err != nil {
		return templateArchiveManifest{}, fmt.Errorf("%w: read template archive manifest: %w", ErrInvalidTemplateArchive, err)
	}

	if len(contents) > templateArchiveManifestMax {
		return templateArchiveManifest{}, fmt.Errorf("%w: template archive manifest is too large", ErrInvalidTemplateArchive)
	}

	var manifest templateArchiveManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return templateArchiveManifest{}, fmt.Errorf("%w: parse template archive manifest: %w", ErrInvalidTemplateArchive, err)
	}

	return manifest, nil
}

func extractTemplateArchiveFile(reader io.Reader, size int64, file templateArchiveFile) error {
	if size < 0 {
		return fmt.Errorf("%w: template archive entry %s has invalid size", ErrInvalidTemplateArchive, file.archiveName)
	}

	if err := os.MkdirAll(filepath.Dir(file.path), 0o750); err != nil {
		return fmt.Errorf("create template archive entry directory %s: %w", file.archiveName, err)
	}

	out, err := os.OpenFile(file.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create template archive entry %s: %w", file.archiveName, err)
	}

	copied, copyErr := io.Copy(out, reader)
	closeErr := out.Close()

	if copyErr != nil {
		return fmt.Errorf("%w: extract template archive entry %s: %w", ErrInvalidTemplateArchive, file.archiveName, copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close template archive entry %s: %w", file.archiveName, closeErr)
	}

	if copied != size {
		return fmt.Errorf("%w: extract template archive entry %s: copied %d bytes, want %d", ErrInvalidTemplateArchive, file.archiveName, copied, size)
	}

	if err := os.Chmod(file.path, file.mode); err != nil {
		return fmt.Errorf("chmod template archive entry %s: %w", file.archiveName, err)
	}

	return nil
}
