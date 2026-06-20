//nolint:wsl_v5 // Fake S3 handler fixtures are easier to read without whitespace churn.
package clusterapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/clusterapi"
)

func TestS3ArchiveStorePutGetDelete(t *testing.T) {
	t.Parallel()

	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/archives/templates/tpl_source.tar.zst" {
			t.Fatalf("S3 path = %s, want bucket-prefixed object path", r.URL.Path)
		}

		switch r.Method {
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read put body: %v", err)
			}

			objects[r.URL.Path] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			body, ok := objects[r.URL.Path]
			if !ok {
				http.NotFound(w, r)

				return
			}

			_, _ = w.Write(body)
		case http.MethodDelete:
			delete(objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("S3 method = %s, want PUT/GET/DELETE", r.Method)
		}
	}))
	t.Cleanup(server.Close)

	store, err := clusterapi.NewS3ArchiveStore(context.Background(), clusterapi.S3ArchiveStoreConfig{
		Bucket:          "archives",
		Endpoint:        server.URL,
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		ForcePathStyle:  true,
	})
	if err != nil {
		t.Fatalf("new S3 archive store: %v", err)
	}

	if err := store.Put(context.Background(), "templates/tpl_source.tar.zst", []byte("template-archive")); err != nil {
		t.Fatalf("put archive: %v", err)
	}

	got, err := store.Get(context.Background(), "templates/tpl_source.tar.zst")
	if err != nil {
		t.Fatalf("get archive: %v", err)
	}
	if string(got) != "template-archive" {
		t.Fatalf("archive = %q, want template-archive", got)
	}

	if err := store.Delete(context.Background(), "templates/tpl_source.tar.zst"); err != nil {
		t.Fatalf("delete archive: %v", err)
	}
	if _, ok := objects["/archives/templates/tpl_source.tar.zst"]; ok {
		t.Fatal("archive still exists after delete")
	}
}
