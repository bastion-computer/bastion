package system

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCloudHypervisorHTTPDownloaderDoesNotApplyGlobalClientTimeout(t *testing.T) {
	t.Parallel()

	downloader := cloudHypervisorHTTPDownloader{}.withDefaults()
	if downloader.client.Timeout != 0 {
		t.Fatalf("client timeout = %s, want no global timeout for large asset downloads", downloader.client.Timeout)
	}
}

func TestDownloadFileShowsProgressBar(t *testing.T) {
	t.Parallel()

	payload := []byte(strings.Repeat("a", 2048))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	var out bytes.Buffer

	downloader := cloudHypervisorHTTPDownloader{client: server.Client(), out: &out}
	destination := filepath.Join(t.TempDir(), "asset.bin")

	if err := downloader.downloadFile(context.Background(), server.URL, destination, 0o640); err != nil {
		t.Fatalf("download file: %v", err)
	}

	contents, err := os.ReadFile(destination) //nolint:gosec // Test path is generated under t.TempDir().
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}

	if !bytes.Equal(contents, payload) {
		t.Fatalf("downloaded payload = %q, want %q", contents, payload)
	}

	progress := out.String()
	for _, want := range []string{"bastion: asset.bin [", "100%", "2.0 KiB/2.0 KiB"} {
		if !strings.Contains(progress, want) {
			t.Fatalf("progress output missing %q:\n%s", want, progress)
		}
	}
}
