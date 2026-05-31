package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services/queue"
)

const queueTestWorkerID = "worker-1"

//nolint:gocyclo // This lifecycle test intentionally exercises the full CRUD path.
func TestServiceCreatesListsGetsAndRemovesQueue(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := queue.NewService(db)
	ctx := context.Background()

	created, err := service.Create(ctx, queue.CreateRequest{Key: new("jobs")})
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	if created.ID == "" || created.Key == nil || *created.Key != "jobs" {
		t.Fatalf("created queue = %#v, want keyed queue", created)
	}

	page, err := service.List(ctx, 20, "")
	if err != nil {
		t.Fatalf("list queues: %v", err)
	}

	if len(page.Entries) != 1 || page.Entries[0].ID != created.ID || page.Cursor != nil {
		t.Fatalf("queue page = %#v, want one entry", page)
	}

	got, err := service.Get(ctx, "", "jobs")
	if err != nil {
		t.Fatalf("get queue: %v", err)
	}

	if got.ID != created.ID {
		t.Fatalf("got queue id = %q, want %q", got.ID, created.ID)
	}

	removed, err := service.Remove(ctx, created.ID, "")
	if err != nil {
		t.Fatalf("remove queue: %v", err)
	}

	if removed.ID != created.ID {
		t.Fatalf("removed queue id = %q, want %q", removed.ID, created.ID)
	}

	if _, err := service.Get(ctx, created.ID, ""); !errors.Is(err, failure.ErrNotFound) {
		t.Fatalf("get removed queue error = %v, want not found", err)
	}
}

func TestServiceRejectsDuplicateAndBlankQueueKeys(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := queue.NewService(db)
	ctx := context.Background()

	if _, err := service.Create(ctx, queue.CreateRequest{Key: new("jobs")}); err != nil {
		t.Fatalf("create queue: %v", err)
	}

	if _, err := service.Create(ctx, queue.CreateRequest{Key: new("jobs")}); !errors.Is(err, failure.ErrConflict) {
		t.Fatalf("duplicate queue error = %v, want conflict", err)
	}

	if _, err := service.Create(ctx, queue.CreateRequest{Key: new("")}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("blank queue key error = %v, want invalid", err)
	}
}

//nolint:gocyclo // This lifecycle test intentionally exercises lease and ACK transitions.
func TestServicePublishesLeasesAndAcksTask(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := queue.NewService(db)
	ctx := context.Background()
	created := createQueue(t, service)

	published, err := service.Publish(ctx, created.ID, "", queue.PublishRequest{Data: json.RawMessage(`{"kind":"sync"}`)})
	if err != nil {
		t.Fatalf("publish task: %v", err)
	}

	if published.Status != queue.TaskStatusPending || published.Retry.MaxAttempts != 3 || published.Attempts != 0 {
		t.Fatalf("published task = %#v, want pending with default retry", published)
	}

	leased, ok, err := service.Lease(ctx, "", "jobs", queue.LeaseRequest{WorkerID: queueTestWorkerID, LeaseMS: 1000})
	if err != nil {
		t.Fatalf("lease task: %v", err)
	}

	if !ok || leased.ID != published.ID || leased.Status != queue.TaskStatusLeased || leased.Attempts != 1 {
		t.Fatalf("leased task = %#v ok=%v, want first lease", leased, ok)
	}

	if _, ok, err := service.Lease(ctx, created.ID, "", queue.LeaseRequest{WorkerID: "worker-2", LeaseMS: 1000}); err != nil || ok {
		t.Fatalf("second lease ok=%v err=%v, want no task", ok, err)
	}

	acked, err := service.Ack(ctx, created.ID, "", published.ID, queue.AckRequest{WorkerID: queueTestWorkerID, WorkerData: json.RawMessage(`{"ok":true}`)})
	if err != nil {
		t.Fatalf("ack task: %v", err)
	}

	if acked.Status != queue.TaskStatusComplete || string(acked.WorkerData) != `{"ok":true}` || acked.CompletedAt == "" {
		t.Fatalf("acked task = %#v, want complete with worker data", acked)
	}
}

//nolint:gocyclo // This lifecycle test intentionally exercises retry and DLQ transitions.
func TestServiceRetriesAndMovesTaskToDeadLetterQueue(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := queue.NewService(db)
	ctx := context.Background()
	created := createQueue(t, service)

	retry := queue.RetryOptions{MaxAttempts: 2, DelayMS: 1, BackoffMultiplier: 2}

	published, err := service.Publish(ctx, created.ID, "", queue.PublishRequest{Retry: &retry, Data: json.RawMessage(`{"kind":"retry"}`)})
	if err != nil {
		t.Fatalf("publish task: %v", err)
	}

	leased, ok, err := service.Lease(ctx, created.ID, "", queue.LeaseRequest{WorkerID: queueTestWorkerID, LeaseMS: 1000})
	if err != nil || !ok {
		t.Fatalf("lease first attempt ok=%v err=%v", ok, err)
	}

	failed, err := service.Fail(ctx, created.ID, "", leased.ID, queue.FailRequest{WorkerID: queueTestWorkerID, Error: "boom"})
	if err != nil {
		t.Fatalf("fail first attempt: %v", err)
	}

	if failed.Status != queue.TaskStatusPending || failed.LastError != "boom" {
		t.Fatalf("failed first attempt = %#v, want pending retry", failed)
	}

	time.Sleep(3 * time.Millisecond)

	leased, ok, err = service.Lease(ctx, created.ID, "", queue.LeaseRequest{WorkerID: queueTestWorkerID, LeaseMS: 1000})
	if err != nil || !ok || leased.Attempts != 2 {
		t.Fatalf("lease second attempt = %#v ok=%v err=%v", leased, ok, err)
	}

	dead, err := service.Fail(ctx, created.ID, "", published.ID, queue.FailRequest{WorkerID: queueTestWorkerID, Error: "still boom"})
	if err != nil {
		t.Fatalf("fail final attempt: %v", err)
	}

	if dead.Status != queue.TaskStatusDead || dead.LastError != "still boom" || dead.CompletedAt == "" {
		t.Fatalf("dead task = %#v, want DLQ task", dead)
	}
}

func TestServiceRejectsInvalidPublishRetry(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	service := queue.NewService(db)
	ctx := context.Background()
	created := createQueue(t, service)

	retry := queue.RetryOptions{MaxAttempts: 0, DelayMS: 1}
	if _, err := service.Publish(ctx, created.ID, "", queue.PublishRequest{Retry: &retry, Data: json.RawMessage(`{}`)}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("invalid retry error = %v, want invalid", err)
	}

	if _, err := service.Publish(ctx, created.ID, "", queue.PublishRequest{Data: json.RawMessage(`{`)}); !errors.Is(err, failure.ErrInvalid) {
		t.Fatalf("invalid data error = %v, want invalid", err)
	}
}

func createQueue(t *testing.T, service *queue.Service) queue.Queue {
	t.Helper()

	created, err := service.Create(context.Background(), queue.CreateRequest{Key: new("jobs")})
	if err != nil {
		t.Fatalf("create queue: %v", err)
	}

	return created
}

func openDB(t *testing.T) *database.Client {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}
