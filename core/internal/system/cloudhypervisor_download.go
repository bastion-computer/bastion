package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cloudHypervisorVersion     = "v52.0"
	cloudHypervisorReleasesURL = "https://api.github.com/repos/cloud-hypervisor/cloud-hypervisor/releases/tags/" + cloudHypervisorVersion
	ubuntuNobleVersion         = "Ubuntu 24.04"
	ubuntuNobleBuild           = "20260615"
	ubuntuNobleAssetVersion    = ubuntuNobleVersion + " " + ubuntuNobleBuild
	ubuntuNobleBaseURL         = "https://cloud-images.ubuntu.com/noble/" + ubuntuNobleBuild
	ubuntuNobleImageURL        = ubuntuNobleBaseURL + "/noble-server-cloudimg-amd64.img"
	ubuntuNobleKernelURL       = ubuntuNobleBaseURL + "/unpacked/noble-server-cloudimg-amd64-vmlinuz-generic"
	ubuntuNobleInitramfsURL    = ubuntuNobleBaseURL + "/unpacked/noble-server-cloudimg-amd64-initrd-generic"
	ubuntuNobleImageName       = "ubuntu-24.04.img"
	ubuntuNobleKernelName      = "ubuntu-24.04-vmlinuz-generic"
	ubuntuNobleInitramfsName   = "ubuntu-24.04-initrd-generic"
	metadataTimeout            = 30 * time.Second
)

type cloudHypervisorHTTPDownloader struct {
	client *http.Client
	out    io.Writer
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func (d cloudHypervisorHTTPDownloader) download(ctx context.Context, store cloudHypervisorStore, arch string) (cloudHypervisorManifest, error) {
	d = d.withDefaults()

	if arch != archX8664 {
		return cloudHypervisorManifest{}, fmt.Errorf("cloud-hypervisor setup supports %s hosts only, got %s", archX8664, arch)
	}

	if err := logCloudHypervisorProgress(d.out, "fetching Cloud Hypervisor %s release metadata", cloudHypervisorVersion); err != nil {
		return cloudHypervisorManifest{}, err
	}

	release, err := d.releaseMetadata(ctx, cloudHypervisorReleasesURL)
	if err != nil {
		return cloudHypervisorManifest{}, err
	}

	manifest := cloudHypervisorManifest{
		Version:         release.TagName,
		UbuntuVersion:   ubuntuNobleVersion,
		UbuntuBuild:     ubuntuNobleBuild,
		Architecture:    arch,
		RootFSImageType: "Qcow2",
		CreatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := d.downloadCloudHypervisor(ctx, store, release, &manifest); err != nil {
		return cloudHypervisorManifest{}, err
	}

	if err := d.downloadRootFS(ctx, store, &manifest); err != nil {
		return cloudHypervisorManifest{}, err
	}

	if err := d.downloadKernel(ctx, store, &manifest); err != nil {
		return cloudHypervisorManifest{}, err
	}

	if err := d.downloadInitramfs(ctx, store, &manifest); err != nil {
		return cloudHypervisorManifest{}, err
	}

	return manifest, nil
}

func (d cloudHypervisorHTTPDownloader) withDefaults() cloudHypervisorHTTPDownloader {
	if d.client == nil {
		d.client = http.DefaultClient
	}

	return d
}

func (d cloudHypervisorHTTPDownloader) releaseMetadata(ctx context.Context, target string) (githubRelease, error) {
	var release githubRelease
	if err := d.getJSON(ctx, target, &release); err != nil {
		return release, err
	}

	if release.TagName == "" {
		return release, fmt.Errorf("cloud-hypervisor release %s did not include a tag", target)
	}

	if release.TagName != cloudHypervisorVersion {
		return release, fmt.Errorf("cloud-hypervisor release tag = %s, want %s", release.TagName, cloudHypervisorVersion)
	}

	return release, nil
}

func (d cloudHypervisorHTTPDownloader) downloadCloudHypervisor(
	ctx context.Context,
	store cloudHypervisorStore,
	release githubRelease,
	manifest *cloudHypervisorManifest,
) error {
	asset, ok := findReleaseAsset(release, "cloud-hypervisor-static")
	if !ok {
		return errors.New("cloud-hypervisor release asset not found: cloud-hypervisor-static")
	}

	path := filepath.Join(store.dir, cloudHypervisorName)

	if err := logCloudHypervisorProgress(d.out, "downloading Cloud Hypervisor %s", release.TagName); err != nil {
		return err
	}

	if err := d.downloadFile(ctx, asset.BrowserDownloadURL, path, 0o755); err != nil {
		return err
	}

	checksum := strings.TrimPrefix(asset.Digest, "sha256:")
	if checksum != "" {
		if err := logCloudHypervisorProgress(d.out, "verifying Cloud Hypervisor checksum"); err != nil {
			return err
		}

		if err := verifySHA256(path, checksum); err != nil {
			return err
		}
	}

	manifest.CloudHypervisor = cloudHypervisorName
	manifest.CloudHypervisorSource = asset.BrowserDownloadURL
	manifest.ReleaseChecksum = checksum

	return nil
}

func (d cloudHypervisorHTTPDownloader) downloadRootFS(ctx context.Context, store cloudHypervisorStore, manifest *cloudHypervisorManifest) error {
	if err := logCloudHypervisorProgress(d.out, "downloading Ubuntu 24.04 cloud image"); err != nil {
		return err
	}

	if err := d.downloadFile(ctx, ubuntuNobleImageURL, filepath.Join(store.dir, ubuntuNobleImageName), 0o640); err != nil {
		return err
	}

	manifest.RootFSImage = ubuntuNobleImageName
	manifest.RootFSSource = ubuntuNobleImageURL

	return nil
}

func (d cloudHypervisorHTTPDownloader) downloadKernel(ctx context.Context, store cloudHypervisorStore, manifest *cloudHypervisorManifest) error {
	if err := logCloudHypervisorProgress(d.out, "downloading Ubuntu 24.04 kernel"); err != nil {
		return err
	}

	if err := d.downloadFile(ctx, ubuntuNobleKernelURL, filepath.Join(store.dir, ubuntuNobleKernelName), 0o640); err != nil {
		return err
	}

	manifest.Kernel = ubuntuNobleKernelName
	manifest.KernelSource = ubuntuNobleKernelURL

	return nil
}

func (d cloudHypervisorHTTPDownloader) downloadInitramfs(ctx context.Context, store cloudHypervisorStore, manifest *cloudHypervisorManifest) error {
	if err := logCloudHypervisorProgress(d.out, "downloading Ubuntu 24.04 initramfs"); err != nil {
		return err
	}

	if err := d.downloadFile(ctx, ubuntuNobleInitramfsURL, filepath.Join(store.dir, ubuntuNobleInitramfsName), 0o640); err != nil {
		return err
	}

	manifest.Initramfs = ubuntuNobleInitramfsName
	manifest.InitramfsSource = ubuntuNobleInitramfsURL

	return nil
}

func (d cloudHypervisorHTTPDownloader) getJSON(ctx context.Context, target string, out any) error {
	contents, err := d.getString(ctx, target)
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(contents), out)
}

func (d cloudHypervisorHTTPDownloader) getString(ctx context.Context, target string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	res, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", target, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("download %s returned %s", target, res.Status)
	}

	contents, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", target, err)
	}

	return string(contents), nil
}

// downloadFile downloads an asset into the Cloud Hypervisor store.
//
//nolint:gosec // The destination path is constrained to the user-selected Bastion data directory.
func (d cloudHypervisorHTTPDownloader) downloadFile(ctx context.Context, source, destination string, mode os.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	res, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", source, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("download %s returned %s", source, res.Status)
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	defer func() { _ = file.Close() }()

	progress := newCloudHypervisorDownloadProgress(d.out, filepath.Base(destination), res.ContentLength)
	reader := io.TeeReader(res.Body, progress)

	_, err = io.Copy(file, reader)
	if finishErr := progress.finish(err == nil); finishErr != nil && err == nil {
		return finishErr
	}

	if err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}

	return nil
}

func findReleaseAsset(release githubRelease, name string) (githubAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, true
		}
	}

	return githubAsset{}, false
}

// verifySHA256 verifies a downloaded Cloud Hypervisor asset checksum.
//
//nolint:gosec // The source path is constrained to the user-selected Bastion data directory.
func verifySHA256(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}

	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(path))
	}

	return nil
}
