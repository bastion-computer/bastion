package system

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/core/internal/opencodeasset"
)

const (
	openCodeReleasesURL       = "https://api.github.com/repos/anomalyco/opencode/releases/tags/" + opencodeasset.Version
	openCodeLinuxX64AssetName = opencodeasset.LinuxX64ArchiveName
)

type openCodeHTTPDownloader struct {
	client     *http.Client
	out        io.Writer
	releaseURL string
}

func (d openCodeHTTPDownloader) download(ctx context.Context, store openCodeStore, arch string) (opencodeasset.Manifest, error) {
	d = d.withDefaults()

	if arch != archX8664 {
		return opencodeasset.Manifest{}, fmt.Errorf("opencode setup supports %s hosts only, got %s", archX8664, arch)
	}

	if err := logCloudHypervisorProgress(d.out, "fetching OpenCode %s release metadata", opencodeasset.Version); err != nil {
		return opencodeasset.Manifest{}, err
	}

	release, err := d.releaseMetadata(ctx, d.releaseURL)
	if err != nil {
		return opencodeasset.Manifest{}, err
	}

	asset, ok := findReleaseAsset(release, openCodeLinuxX64AssetName)
	if !ok {
		return opencodeasset.Manifest{}, errors.New("opencode release asset not found: " + openCodeLinuxX64AssetName)
	}

	archivePath := filepath.Join(store.dir, openCodeLinuxX64AssetName)

	if err := logCloudHypervisorProgress(d.out, "downloading OpenCode %s", release.TagName); err != nil {
		return opencodeasset.Manifest{}, err
	}

	fileDownloader := cloudHypervisorHTTPDownloader{client: d.client, out: d.out}
	if err := fileDownloader.downloadFile(ctx, asset.BrowserDownloadURL, archivePath, 0o640); err != nil {
		return opencodeasset.Manifest{}, err
	}

	checksum := strings.TrimPrefix(asset.Digest, "sha256:")
	if checksum == "" {
		return opencodeasset.Manifest{}, errors.New("opencode release asset checksum is missing: " + openCodeLinuxX64AssetName)
	}

	if err := logCloudHypervisorProgress(d.out, "verifying OpenCode checksum"); err != nil {
		return opencodeasset.Manifest{}, err
	}

	if err := verifySHA256(archivePath, checksum); err != nil {
		return opencodeasset.Manifest{}, err
	}

	if err := logCloudHypervisorProgress(d.out, "extracting OpenCode"); err != nil {
		return opencodeasset.Manifest{}, err
	}

	if err := extractOpenCodeArchive(archivePath, filepath.Join(store.dir, opencodeasset.BinaryName)); err != nil {
		return opencodeasset.Manifest{}, err
	}

	return opencodeasset.Manifest{
		Version:      release.TagName,
		Architecture: arch,
		OpenCode:     opencodeasset.BinaryName,
		Archive:      openCodeLinuxX64AssetName,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Source:       asset.BrowserDownloadURL,
		Checksum:     checksum,
	}, nil
}

func (d openCodeHTTPDownloader) withDefaults() openCodeHTTPDownloader {
	if d.client == nil {
		d.client = http.DefaultClient
	}

	if d.releaseURL == "" {
		d.releaseURL = openCodeReleasesURL
	}

	return d
}

func (d openCodeHTTPDownloader) releaseMetadata(ctx context.Context, target string) (githubRelease, error) {
	var release githubRelease

	metadataDownloader := cloudHypervisorHTTPDownloader{client: d.client, out: d.out}
	if err := metadataDownloader.getJSON(ctx, target, &release); err != nil {
		return release, err
	}

	if release.TagName == "" {
		return release, fmt.Errorf("opencode release %s did not include a tag", target)
	}

	if release.TagName != opencodeasset.Version {
		return release, fmt.Errorf("opencode release tag = %s, want %s", release.TagName, opencodeasset.Version)
	}

	return release, nil
}

func extractOpenCodeArchive(archivePath, binaryPath string) error {
	file, err := os.Open(archivePath) //nolint:gosec // Archive path is constrained to the user-selected Bastion data directory.
	if err != nil {
		return fmt.Errorf("open opencode archive: %w", err)
	}
	defer func() { _ = file.Close() }()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open opencode archive gzip: %w", err)
	}
	defer func() { _ = gzipReader.Close() }()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("read opencode archive: %w", err)
		}

		if path.Clean(header.Name) != opencodeasset.BinaryName {
			continue
		}

		if header.Typeflag != tar.TypeReg {
			return errors.New("opencode archive entry is not a regular file")
		}

		return extractOpenCodeBinary(tarReader, header.Size, binaryPath)
	}

	return errors.New("opencode archive missing opencode binary")
}

func extractOpenCodeBinary(reader io.Reader, size int64, binaryPath string) error {
	if size < 0 {
		return errors.New("opencode archive binary has invalid size")
	}

	out, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755) //nolint:gosec // Destination path is constrained to the user-selected Bastion data directory.
	if err != nil {
		return fmt.Errorf("create opencode binary: %w", err)
	}

	copied, copyErr := io.Copy(out, reader)
	closeErr := out.Close()

	if copyErr != nil {
		return fmt.Errorf("extract opencode binary: %w", copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close opencode binary: %w", closeErr)
	}

	if copied != size {
		return fmt.Errorf("extract opencode binary: copied %d bytes, want %d", copied, size)
	}

	if err := os.Chmod(binaryPath, 0o755); err != nil { //nolint:gosec // Downloaded OpenCode binary must be executable.
		return fmt.Errorf("chmod opencode binary: %w", err)
	}

	return nil
}
