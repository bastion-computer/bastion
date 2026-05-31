package queueproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/services/queue"
)

func TestRouterServesWorkerLeaseAndAckRoutes(t *testing.T) {
	t.Parallel()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	service := queue.NewService(db)
	ctx := context.Background()

	created, err := service.Create(ctx, queue.CreateRequest{Key: new("jobs")})
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	published, err := service.Publish(ctx, created.ID, "", queue.PublishRequest{Data: json.RawMessage(`{"kind":"proxy"}`)})
	if err != nil {
		t.Fatalf("publish task: %v", err)
	}

	router := NewRouter(service)

	res := proxyRequest(t, router, http.MethodPost, "/v1/queues/by-key/jobs/lease", queue.LeaseRequest{WorkerID: "worker-proxy", LeaseMS: 1000})
	if res.Code != http.StatusOK {
		t.Fatalf("lease status = %d, want %d", res.Code, http.StatusOK)
	}

	var leased queue.Task
	decodeProxyResponse(t, res, &leased)

	if leased.ID != published.ID || leased.Status != queue.TaskStatusLeased {
		t.Fatalf("leased task = %#v, want published leased task", leased)
	}

	res = proxyRequest(t, router, http.MethodPost, "/v1/queues/by-key/jobs/tasks/"+published.ID+"/ack", queue.AckRequest{WorkerID: "worker-proxy"})
	if res.Code != http.StatusOK {
		t.Fatalf("ack status = %d, want %d", res.Code, http.StatusOK)
	}

	var acked queue.Task
	decodeProxyResponse(t, res, &acked)

	if acked.Status != queue.TaskStatusComplete {
		t.Fatalf("acked task = %#v, want complete", acked)
	}
}

func proxyRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}

	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	return res
}

func decodeProxyResponse(t *testing.T, res *httptest.ResponseRecorder, value any) {
	t.Helper()

	if err := json.NewDecoder(res.Body).Decode(value); err != nil {
		t.Fatalf("decode response %q: %v", res.Body.String(), err)
	}
}
