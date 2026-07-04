package system

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/opencodeasset"
)

func TestOpenCodeHTTPDownloaderUsesPinnedReleaseAsset(t *testing.T) {
	t.Parallel()

	if strings.Contains(openCodeReleasesURL, "/latest") || strings.Contains(openCodeReleasesURL, "/current/") {
		t.Fatalf("OpenCode release URL = %q, want pinned URL", openCodeReleasesURL)
	}

	if openCodeReleasesURL != "https://api.github.com/repos/anomalyco/opencode/releases/tags/v1.17.13" {
		t.Fatalf("OpenCode metadata URL = %q, want v1.17.13 tag URL", openCodeReleasesURL)
	}

	if openCodeLinuxX64AssetName != "opencode-linux-x64.tar.gz" {
		t.Fatalf("OpenCode Linux x64 asset = %q, want opencode-linux-x64.tar.gz", openCodeLinuxX64AssetName)
	}
}

func TestOpenCodeHTTPDownloaderDownloadsAndExtractsPinnedAsset(t *testing.T) {
	t.Parallel()

	archive := buildOpenCodeArchive(t, []byte("#!/bin/sh\n"))
	checksumBytes := sha256.Sum256(archive)
	checksum := hex.EncodeToString(checksumBytes[:])

	var assetURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/release":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: opencodeasset.Version,
				Assets: []githubAsset{{
					Name:               openCodeLinuxX64AssetName,
					BrowserDownloadURL: assetURL,
					Digest:             "sha256:" + checksum,
				}},
			})
		case "/asset":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	assetURL = server.URL + "/asset"

	store := newOpenCodeStore(t.TempDir())
	requireNoError(t, store.ensure(), "ensure store")

	downloader := openCodeHTTPDownloader{client: server.Client(), releaseURL: server.URL + "/release"}

	manifest, err := downloader.download(context.Background(), store, archX8664)
	requireNoError(t, err, "download opencode")

	if manifest.Version != opencodeasset.Version || manifest.Architecture != archX8664 || manifest.OpenCode != opencodeasset.BinaryName || manifest.Archive != opencodeasset.LinuxX64ArchiveName || manifest.Checksum != checksum {
		t.Fatalf("manifest = %#v, want pinned opencode asset", manifest)
	}

	assetPath := filepath.Join(store.dir, opencodeasset.BinaryName)

	contents, err := os.ReadFile(assetPath) //nolint:gosec // Test path is generated under t.TempDir().
	requireNoError(t, err, "read extracted opencode")

	if string(contents) != "#!/bin/sh\n" {
		t.Fatalf("extracted opencode = %q, want archive contents", contents)
	}

	info, err := os.Stat(assetPath)
	requireNoError(t, err, "stat extracted opencode")

	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("extracted opencode mode = %s, want executable", info.Mode().Perm())
	}
}

func buildOpenCodeArchive(t *testing.T, contents []byte) []byte {
	t.Helper()

	var out bytes.Buffer

	gzipWriter := gzip.NewWriter(&out)
	tarWriter := tar.NewWriter(gzipWriter)

	if err := tarWriter.WriteHeader(&tar.Header{Name: opencodeasset.BinaryName, Mode: 0o755, Size: int64(len(contents))}); err != nil {
		t.Fatalf("write opencode archive header: %v", err)
	}

	if _, err := tarWriter.Write(contents); err != nil {
		t.Fatalf("write opencode archive contents: %v", err)
	}

	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	return out.Bytes()
}

func requireNoError(t *testing.T, err error, message string) {
	t.Helper()

	if err != nil {
		t.Fatalf("%s: %v", message, err)
	}
}
