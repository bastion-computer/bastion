package firecracker

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	firecrackerReleasesURL = "https://api.github.com/repos/firecracker-microvm/firecracker/releases/latest"
	firecrackerS3ListURL   = "http://spec.ccfc.min.s3.amazonaws.com/?prefix=firecracker-ci/%s/%s/&list-type=2"
	firecrackerS3ObjectURL = "https://s3.amazonaws.com/spec.ccfc.min/%s"
	downloadTimeout        = 30 * time.Second
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type s3ListBucket struct {
	Contents []s3Object `xml:"Contents"`
}

type s3Object struct {
	Key string `xml:"Key"`
}

// Downloader downloads Firecracker release and guest assets.
type Downloader struct {
	Client *http.Client
}

// Download downloads Firecracker release and guest assets into store.
func (d Downloader) Download(ctx context.Context, store Store, arch string) (Manifest, error) {
	d = d.withDefaults()

	release, err := d.latestRelease(ctx)
	if err != nil {
		return Manifest{}, err
	}

	manifest := Manifest{
		Version:      release.TagName,
		Architecture: arch,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := d.downloadReleaseAssets(ctx, store, arch, release, &manifest); err != nil {
		return Manifest{}, err
	}

	if err := d.downloadGuestAssets(ctx, store, arch, release.TagName, &manifest); err != nil {
		return Manifest{}, err
	}

	return manifest, nil
}

func (d Downloader) withDefaults() Downloader {
	if d.Client == nil {
		d.Client = &http.Client{Timeout: downloadTimeout}
	}

	return d
}

func (d Downloader) latestRelease(ctx context.Context) (githubRelease, error) {
	var release githubRelease
	if err := d.getJSON(ctx, firecrackerReleasesURL, &release); err != nil {
		return release, err
	}

	if release.TagName == "" {
		return release, errors.New("firecracker latest release did not include a tag")
	}

	return release, nil
}

func (d Downloader) downloadReleaseAssets(ctx context.Context, store Store, arch string, release githubRelease, manifest *Manifest) error {
	archiveName := fmt.Sprintf("%s-%s-%s.tgz", dependencyName, release.TagName, arch)

	archive, ok := findReleaseAsset(release, archiveName)
	if !ok {
		return fmt.Errorf("firecracker release asset not found: %s", archiveName)
	}

	archivePath := filepath.Join(store.Dir, archiveName)
	if err := d.downloadFile(ctx, archive.BrowserDownloadURL, archivePath); err != nil {
		return err
	}

	checksum, err := d.releaseChecksum(ctx, release, archiveName, archive)
	if err != nil {
		return err
	}

	if checksum != "" {
		if err := verifySHA256(archivePath, checksum); err != nil {
			return err
		}
	}

	if err := extractArchive(archivePath, store.Dir, release.TagName, arch); err != nil {
		return err
	}

	manifest.Firecracker = dependencyName
	manifest.Jailer = jailerName
	manifest.ReleaseAsset = archiveName
	manifest.ReleaseChecksum = checksum

	return nil
}

func (d Downloader) downloadGuestAssets(ctx context.Context, store Store, arch, tag string, manifest *Manifest) error {
	version := ciVersion(tag)

	keys, err := d.ciKeys(ctx, version, arch)
	if err != nil {
		return err
	}

	kernelKey, err := newestKey(keys, kernelPattern(version, arch))
	if err != nil {
		return err
	}

	rootfsKey, err := newestKey(keys, rootfsPattern(version, arch))
	if err != nil {
		return err
	}

	manifest.Kernel = filepath.Base(kernelKey)
	manifest.RootFSSquashfs = filepath.Base(rootfsKey)
	manifest.KernelSource = kernelKey
	manifest.RootFSSource = rootfsKey

	if err := d.downloadFile(ctx, fmt.Sprintf(firecrackerS3ObjectURL, kernelKey), filepath.Join(store.Dir, manifest.Kernel)); err != nil {
		return err
	}

	return d.downloadFile(ctx, fmt.Sprintf(firecrackerS3ObjectURL, rootfsKey), filepath.Join(store.Dir, manifest.RootFSSquashfs))
}

func findReleaseAsset(release githubRelease, name string) (githubAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return asset, true
		}
	}

	return githubAsset{}, false
}

func (d Downloader) releaseChecksum(ctx context.Context, release githubRelease, archiveName string, archive githubAsset) (string, error) {
	checksumAsset, ok := findReleaseAsset(release, archiveName+".sha256.txt")
	if ok {
		contents, err := d.getString(ctx, checksumAsset.BrowserDownloadURL)
		if err != nil {
			return "", err
		}

		fields := strings.Fields(contents)
		if len(fields) > 0 {
			return fields[0], nil
		}
	}

	return strings.TrimPrefix(archive.Digest, "sha256:"), nil
}

func (d Downloader) ciKeys(ctx context.Context, version, arch string) ([]string, error) {
	contents, err := d.getString(ctx, fmt.Sprintf(firecrackerS3ListURL, version, arch))
	if err != nil {
		return nil, err
	}

	var list s3ListBucket
	if err := xml.Unmarshal([]byte(contents), &list); err != nil {
		return nil, fmt.Errorf("parse firecracker CI asset list: %w", err)
	}

	keys := make([]string, 0, len(list.Contents))
	for _, object := range list.Contents {
		keys = append(keys, object.Key)
	}

	return keys, nil
}

func ciVersion(tag string) string {
	index := strings.LastIndex(tag, ".")
	if index == -1 {
		return tag
	}

	return tag[:index]
}

func kernelPattern(version, arch string) *regexp.Regexp {
	prefix := regexp.QuoteMeta(fmt.Sprintf("firecracker-ci/%s/%s/vmlinux-", version, arch))
	return regexp.MustCompile("^" + prefix + `(\d+)\.(\d+)\.(\d+)$`)
}

func rootfsPattern(version, arch string) *regexp.Regexp {
	prefix := regexp.QuoteMeta(fmt.Sprintf("firecracker-ci/%s/%s/ubuntu-", version, arch))
	return regexp.MustCompile("^" + prefix + `(\d+)\.(\d+)\.squashfs$`)
}

func newestKey(keys []string, pattern *regexp.Regexp) (string, error) {
	var (
		bestKey     string
		bestVersion []int
	)

	for _, key := range keys {
		matches := pattern.FindStringSubmatch(key)
		if len(matches) == 0 {
			continue
		}

		version := parseVersion(matches[1:])
		if bestKey == "" || compareVersion(version, bestVersion) > 0 {
			bestKey = key
			bestVersion = version
		}
	}

	if bestKey == "" {
		return "", errors.New("matching firecracker CI asset not found")
	}

	return bestKey, nil
}

func parseVersion(parts []string) []int {
	version := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			value = 0
		}

		version = append(version, value)
	}

	return version
}

func compareVersion(left, right []int) int {
	length := max(len(left), len(right))

	for i := range length {
		leftValue := versionPart(left, i)
		rightValue := versionPart(right, i)

		if leftValue > rightValue {
			return 1
		}

		if leftValue < rightValue {
			return -1
		}
	}

	return 0
}

func versionPart(version []int, index int) int {
	if index >= len(version) {
		return 0
	}

	return version[index]
}

func (d Downloader) getJSON(ctx context.Context, target string, out any) error {
	contents, err := d.getString(ctx, target)
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(contents), out)
}

func (d Downloader) getString(ctx context.Context, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	res, err := d.Client.Do(req)
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

// downloadFile downloads an asset into the Firecracker store.
//
//nolint:gosec // The destination path is constrained to the user-selected Bastion data directory.
func (d Downloader) downloadFile(ctx context.Context, source, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	res, err := d.Client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", source, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("download %s returned %s", source, res.Status)
	}

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, res.Body); err != nil {
		return fmt.Errorf("write %s: %w", destination, err)
	}

	return nil
}

// verifySHA256 verifies a downloaded Firecracker asset checksum.
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

// extractArchive extracts firecracker and jailer into the store.
//
//nolint:gosec // Archive extraction is constrained to known file basenames in the data directory.
func extractArchive(archivePath, dir, tag, arch string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", archivePath, err)
	}
	defer func() { _ = file.Close() }()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer func() { _ = reader.Close() }()

	found := make(map[string]bool)
	archive := tar.NewReader(reader)

	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return fmt.Errorf("read firecracker archive: %w", err)
		}

		name := binaryName(filepath.Base(header.Name), tag, arch)
		if name == "" || header.Typeflag != tar.TypeReg {
			continue
		}

		if err := writeArchiveFile(filepath.Join(dir, name), archive); err != nil {
			return err
		}

		found[name] = true
	}

	if !found[dependencyName] || !found[jailerName] {
		return errors.New("firecracker archive missing firecracker or jailer binary")
	}

	return nil
}

func binaryName(base, tag, arch string) string {
	switch base {
	case fmt.Sprintf("%s-%s-%s", dependencyName, tag, arch):
		return dependencyName
	case fmt.Sprintf("%s-%s-%s", jailerName, tag, arch):
		return jailerName
	default:
		return ""
	}
}

// writeArchiveFile writes an extracted Firecracker executable.
//
//nolint:gosec // The destination path is constrained to the user-selected Bastion data directory.
func writeArchiveFile(path string, reader io.Reader) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(file, reader); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
