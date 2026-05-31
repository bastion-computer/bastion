package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/services/queue"
)

const cliTestQueueKey = "jobs"

func TestQueuesCreateCommandSendsOptionalKey(t *testing.T) {
	t.Parallel()

	gotReq := make(chan queue.CreateRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method+" "+r.URL.Path, "POST /v1/queues"; got != want {
			t.Fatalf("request = %s, want %s", got, want)
		}

		req := queue.CreateRequest{}

		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil {
			t.Fatalf("decode create request: %v", err)
		}

		gotReq <- req

		created := queue.Queue{ID: "que_keyed", Key: req.Key}

		if err := json.NewEncoder(w).Encode(created); err != nil {
			t.Fatalf("encode create response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newQueuesCreateCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{cliTestKeyFlag, cliTestQueueKey})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := <-gotReq
	if got.Key == nil || *got.Key != cliTestQueueKey {
		t.Fatalf("create request = %#v, want keyed queue", got)
	}

	var created queue.Queue
	if err := json.NewDecoder(&stdout).Decode(&created); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if created.ID != "que_keyed" || created.Key == nil || *created.Key != cliTestQueueKey {
		t.Fatalf("created output = %#v, want keyed queue", created)
	}
}

func TestQueuesPublishCommandSendsDataAndRetry(t *testing.T) {
	t.Parallel()

	gotReq := make(chan queue.PublishRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/queues/by-key/jobs/tasks" {
			t.Fatalf("request = %s %s, want POST /v1/queues/by-key/jobs/tasks", r.Method, r.URL.Path)
		}

		var req queue.PublishRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode publish request: %v", err)
		}

		gotReq <- req

		if err := json.NewEncoder(w).Encode(queue.Task{ID: "task_test", QueueID: "que_test", Status: queue.TaskStatusPending, Data: req.Data}); err != nil {
			t.Fatalf("encode publish response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer

	cmd := newQueuesPublishCommand(&rootOptions{apiURL: server.URL})
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--key", cliTestQueueKey, "--data", `{"kind":"cli"}`, "--retry", `{"max_attempts":5,"delay_ms":1000}`})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := <-gotReq
	if string(got.Data) != `{"kind":"cli"}` || got.Retry == nil || got.Retry.MaxAttempts != 5 || got.Retry.DelayMS != 1000 {
		t.Fatalf("publish request = %#v, want data and retry", got)
	}

	var task queue.Task
	if err := json.NewDecoder(&stdout).Decode(&task); err != nil {
		t.Fatalf("decode stdout: %v", err)
	}

	if task.ID != "task_test" || task.Status != queue.TaskStatusPending {
		t.Fatalf("publish output = %#v, want pending task", task)
	}
}
