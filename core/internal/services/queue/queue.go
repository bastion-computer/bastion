// Package queue manages durable Bastion queues and tasks.
package queue

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
)

const (
	// TaskStatusPending means a task can be leased once available_at is reached.
	TaskStatusPending = "pending"
	// TaskStatusLeased means a worker currently owns the task lock.
	TaskStatusLeased = "leased"
	// TaskStatusComplete means a worker ACKed the task successfully.
	TaskStatusComplete = "complete"
	// TaskStatusDead means a task exhausted its retry budget.
	TaskStatusDead = "dead"

	defaultMaxAttempts       = 3
	defaultDelayMS           = 1000
	defaultMaxDelayMS        = 30000
	defaultBackoffMultiplier = 2
	defaultJitter            = true
	defaultLeaseMS           = 5 * 60 * 1000
)

// Queue describes a durable task queue.
type Queue struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
}

// CreateRequest contains the fields needed to create a queue.
type CreateRequest struct {
	Key *string `json:"key,omitempty"`
}

// RetryOptions controls task retry scheduling.
type RetryOptions struct {
	MaxAttempts       int     `json:"max_attempts"`
	DelayMS           int     `json:"delay_ms"`
	MaxDelayMS        int     `json:"max_delay_ms,omitempty"`
	BackoffMultiplier float64 `json:"backoff_multiplier,omitempty"`
	Jitter            bool    `json:"jitter,omitempty"`
}

// PublishRequest contains a task payload and optional retry configuration.
type PublishRequest struct {
	Retry *RetryOptions   `json:"retry,omitempty"`
	Data  json.RawMessage `json:"data"`
}

// Task describes a durable queue task.
type Task struct {
	ID          string          `json:"id"`
	QueueID     string          `json:"queueId"`
	Status      string          `json:"status"`
	Retry       RetryOptions    `json:"retry"`
	Data        json.RawMessage `json:"data"`
	Attempts    int             `json:"attempts"`
	AvailableAt string          `json:"availableAt"`
	LockedUntil string          `json:"lockedUntil,omitempty"`
	WorkerData  json.RawMessage `json:"workerData,omitempty"`
	LastError   string          `json:"lastError,omitempty"`
	CreatedAt   string          `json:"createdAt"`
	UpdatedAt   string          `json:"updatedAt"`
	CompletedAt string          `json:"completedAt,omitempty"`
}

// LeaseRequest asks the queue to lock one available task for a worker.
type LeaseRequest struct {
	WorkerID string `json:"worker_id"`
	LeaseMS  int    `json:"lease_ms,omitempty"`
}

// AckRequest marks a leased task as complete.
type AckRequest struct {
	WorkerID   string          `json:"worker_id"`
	WorkerData json.RawMessage `json:"worker_data,omitempty"`
}

// FailRequest records a leased task failure and schedules retry or DLQ movement.
type FailRequest struct {
	WorkerID string `json:"worker_id"`
	Error    string `json:"error"`
}

// Service manages queues and durable tasks.
type Service struct {
	db *database.Client
}

// NewService returns a queue service backed by db.
func NewService(db *database.Client) *Service {
	return &Service{db: db}
}

// Create stores a queue.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Queue, error) {
	if err := services.ValidateOptionalKey("queue", req.Key); err != nil {
		return Queue{}, err
	}

	queueID, err := services.GenerateID("que")
	if err != nil {
		return Queue{}, err
	}

	now := services.Now()
	queue := Queue{ID: queueID, Key: services.CopyStringPtr(req.Key), CreatedAt: now, UpdatedAt: now}

	_, err = s.db.ExecContext(ctx, `INSERT INTO queues (id, key, created_at, updated_at) VALUES (?, ?, ?, ?)`, queue.ID, services.OptionalStringValue(queue.Key), queue.CreatedAt, queue.UpdatedAt)
	if err != nil {
		if database.IsConstraint(err) {
			return Queue{}, fmt.Errorf("%w: queue already exists", failure.ErrConflict)
		}

		return Queue{}, fmt.Errorf("create queue: %w", err)
	}

	return queue, nil
}

// List returns queues ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Queue], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, key, created_at, updated_at FROM queues`, limit, cursor)
	if err != nil {
		return services.Page[Queue]{}, fmt.Errorf("list queues: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Queue, 0, limit+1)

	for rows.Next() {
		queue, err := scanQueue(rows)
		if err != nil {
			return services.Page[Queue]{}, fmt.Errorf("scan queue: %w", err)
		}

		entries = append(entries, queue)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Queue]{}, fmt.Errorf("iterate queues: %w", err)
	}

	return services.FromEntries(entries, limit, func(queue Queue) string { return queue.CreatedAt }), nil
}

// Get returns a queue by ID or key.
func (s *Service) Get(ctx context.Context, queueID, key string) (Queue, error) {
	if err := services.RequireIDOrKey(queueID, key); err != nil {
		return Queue{}, err
	}

	queue, err := s.get(ctx, queueID, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Queue{}, fmt.Errorf("%w: queue not found", failure.ErrNotFound)
	}

	if err != nil {
		return Queue{}, fmt.Errorf("get queue: %w", err)
	}

	return queue, nil
}

// Remove deletes a queue by ID or key and returns the removed record.
func (s *Service) Remove(ctx context.Context, queueID, key string) (Queue, error) {
	queue, err := s.Get(ctx, queueID, key)
	if err != nil {
		return Queue{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM queues WHERE id = ?`, queue.ID); err != nil {
		return Queue{}, fmt.Errorf("remove queue: %w", err)
	}

	return queue, nil
}

// Publish stores a task on a queue by ID or key.
func (s *Service) Publish(ctx context.Context, queueID, key string, req PublishRequest) (Task, error) {
	queue, err := s.Get(ctx, queueID, key)
	if err != nil {
		return Task{}, err
	}

	retry, err := normalizeRetry(req.Retry)
	if err != nil {
		return Task{}, err
	}

	if len(req.Data) == 0 || !json.Valid(req.Data) {
		return Task{}, fmt.Errorf("%w: queue task data must be valid JSON", failure.ErrInvalid)
	}

	taskID, err := services.GenerateID("task")
	if err != nil {
		return Task{}, err
	}

	now := services.Now()
	task := Task{ID: taskID, QueueID: queue.ID, Status: TaskStatusPending, Retry: retry, Data: append([]byte(nil), req.Data...), AvailableAt: now, CreatedAt: now, UpdatedAt: now}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO queue_tasks (
  id, queue_id, status, data, retry_max_attempts, retry_delay_ms, retry_max_delay_ms,
  retry_backoff_multiplier, retry_jitter, attempts, available_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, task.ID, task.QueueID, task.Status, string(task.Data), retry.MaxAttempts, retry.DelayMS, retry.MaxDelayMS, retry.BackoffMultiplier, boolInt(retry.Jitter), 0, task.AvailableAt, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return Task{}, fmt.Errorf("publish queue task: %w", err)
	}

	return task, nil
}

// GetTask returns a queue task by ID.
func (s *Service) GetTask(ctx context.Context, queueID, key, taskID string) (Task, error) {
	if taskID == "" {
		return Task{}, fmt.Errorf("%w: task id is required", failure.ErrInvalid)
	}

	queue, err := s.Get(ctx, queueID, key)
	if err != nil {
		return Task{}, err
	}

	task, err := s.getTask(ctx, queue.ID, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("%w: queue task not found", failure.ErrNotFound)
	}

	if err != nil {
		return Task{}, fmt.Errorf("get queue task: %w", err)
	}

	return task, nil
}

// Lease locks one available task for a worker. The bool is false when no task is available.
func (s *Service) Lease(ctx context.Context, queueID, key string, req LeaseRequest) (Task, bool, error) {
	if req.WorkerID == "" {
		return Task{}, false, fmt.Errorf("%w: worker_id is required", failure.ErrInvalid)
	}

	queue, err := s.Get(ctx, queueID, key)
	if err != nil {
		return Task{}, false, err
	}

	leaseMS := req.LeaseMS
	if leaseMS <= 0 {
		leaseMS = defaultLeaseMS
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, false, fmt.Errorf("begin queue lease: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	nowTime := time.Now().UTC()

	now := nowTime.Format(time.RFC3339Nano)
	if err := expireLeases(ctx, tx, queue.ID, now); err != nil {
		return Task{}, false, err
	}

	var taskID string

	err = tx.QueryRowContext(ctx, `
SELECT id
FROM queue_tasks
WHERE queue_id = ? AND status = ? AND available_at <= ? AND attempts < retry_max_attempts
ORDER BY created_at
LIMIT 1
`, queue.ID, TaskStatusPending, now).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return Task{}, false, fmt.Errorf("commit empty queue lease: %w", err)
		}

		committed = true

		return Task{}, false, nil
	}

	if err != nil {
		return Task{}, false, fmt.Errorf("select queue task to lease: %w", err)
	}

	lockedUntil := nowTime.Add(time.Duration(leaseMS) * time.Millisecond).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE queue_tasks SET status = ?, attempts = attempts + 1, locked_by = ?, locked_until = ?, updated_at = ? WHERE id = ?`, TaskStatusLeased, req.WorkerID, lockedUntil, now, taskID); err != nil {
		return Task{}, false, fmt.Errorf("lease queue task: %w", err)
	}

	task, err := getTaskTx(ctx, tx, queue.ID, taskID)
	if err != nil {
		return Task{}, false, fmt.Errorf("get leased queue task: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Task{}, false, fmt.Errorf("commit queue lease: %w", err)
	}

	committed = true

	return task, true, nil
}

// Ack marks a leased task complete.
func (s *Service) Ack(ctx context.Context, queueID, key, taskID string, req AckRequest) (Task, error) {
	if req.WorkerID == "" {
		return Task{}, fmt.Errorf("%w: worker_id is required", failure.ErrInvalid)
	}

	queue, err := s.Get(ctx, queueID, key)
	if err != nil {
		return Task{}, err
	}

	if len(req.WorkerData) > 0 && !json.Valid(req.WorkerData) {
		return Task{}, fmt.Errorf("%w: worker_data must be valid JSON", failure.ErrInvalid)
	}

	if _, err := s.requireTaskLock(ctx, queue.ID, taskID, req.WorkerID); err != nil {
		return Task{}, err
	}

	now := services.Now()
	workerData := any(nil)

	if len(req.WorkerData) > 0 {
		workerData = string(req.WorkerData)
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE queue_tasks SET status = ?, locked_by = '', locked_until = '', worker_data = ?, updated_at = ?, completed_at = ? WHERE id = ?`, TaskStatusComplete, workerData, now, now, taskID); err != nil {
		return Task{}, fmt.Errorf("ack queue task: %w", err)
	}

	return s.GetTask(ctx, queue.ID, "", taskID)
}

// Fail records a task failure and either schedules retry or marks it dead.
func (s *Service) Fail(ctx context.Context, queueID, key, taskID string, req FailRequest) (Task, error) {
	if req.WorkerID == "" {
		return Task{}, fmt.Errorf("%w: worker_id is required", failure.ErrInvalid)
	}

	queue, err := s.Get(ctx, queueID, key)
	if err != nil {
		return Task{}, err
	}

	task, err := s.requireTaskLock(ctx, queue.ID, taskID, req.WorkerID)
	if err != nil {
		return Task{}, err
	}

	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)
	message := req.Error

	if message == "" {
		message = "task failed"
	}

	if task.Attempts >= task.Retry.MaxAttempts {
		if _, err := s.db.ExecContext(ctx, `UPDATE queue_tasks SET status = ?, locked_by = '', locked_until = '', last_error = ?, updated_at = ?, completed_at = ? WHERE id = ?`, TaskStatusDead, message, now, now, task.ID); err != nil {
			return Task{}, fmt.Errorf("move queue task to dlq: %w", err)
		}

		return s.GetTask(ctx, queue.ID, "", task.ID)
	}

	availableAt := nowTime.Add(retryDelay(task)).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `UPDATE queue_tasks SET status = ?, available_at = ?, locked_by = '', locked_until = '', last_error = ?, updated_at = ? WHERE id = ?`, TaskStatusPending, availableAt, message, now, task.ID); err != nil {
		return Task{}, fmt.Errorf("schedule queue task retry: %w", err)
	}

	return s.GetTask(ctx, queue.ID, "", task.ID)
}

func (s *Service) get(ctx context.Context, queueID, key string) (Queue, error) {
	where, value := services.LookupClause(queueID, key, "id", "key")
	row := s.db.QueryRowContext(ctx, `SELECT id, key, created_at, updated_at FROM queues WHERE `+where, value)

	return scanQueue(row)
}

func scanQueue(row rowScanner) (Queue, error) {
	var (
		queue Queue
		key   sql.NullString
	)

	if err := row.Scan(&queue.ID, &key, &queue.CreatedAt, &queue.UpdatedAt); err != nil {
		return Queue{}, err
	}

	queue.Key = services.NullStringPtr(key)

	return queue, nil
}

func (s *Service) getTask(ctx context.Context, queueID, taskID string) (Task, error) {
	return getTaskRow(s.db.QueryRowContext(ctx, taskSelectQuery()+` WHERE queue_id = ? AND id = ?`, queueID, taskID))
}

func getTaskTx(ctx context.Context, tx *sql.Tx, queueID, taskID string) (Task, error) {
	return getTaskRow(tx.QueryRowContext(ctx, taskSelectQuery()+` WHERE queue_id = ? AND id = ?`, queueID, taskID))
}

func taskSelectQuery() string {
	return `
SELECT
  id, queue_id, status, data, retry_max_attempts, retry_delay_ms, retry_max_delay_ms,
  retry_backoff_multiplier, retry_jitter, attempts, available_at, locked_until,
  worker_data, last_error, created_at, updated_at, completed_at
FROM queue_tasks`
}

func getTaskRow(row rowScanner) (Task, error) {
	var (
		task        Task
		data        string
		workerData  sql.NullString
		retryJitter int
		maxDelayMS  int
		multiplier  float64
		lockedUntil sql.NullString
		completedAt sql.NullString
	)

	if err := row.Scan(
		&task.ID,
		&task.QueueID,
		&task.Status,
		&data,
		&task.Retry.MaxAttempts,
		&task.Retry.DelayMS,
		&maxDelayMS,
		&multiplier,
		&retryJitter,
		&task.Attempts,
		&task.AvailableAt,
		&lockedUntil,
		&workerData,
		&task.LastError,
		&task.CreatedAt,
		&task.UpdatedAt,
		&completedAt,
	); err != nil {
		return Task{}, err
	}

	task.Retry.MaxDelayMS = maxDelayMS
	task.Retry.BackoffMultiplier = multiplier
	task.Retry.Jitter = retryJitter != 0
	task.Data = json.RawMessage(data)

	if lockedUntil.Valid {
		task.LockedUntil = lockedUntil.String
	}

	if workerData.Valid {
		task.WorkerData = json.RawMessage(workerData.String)
	}

	if completedAt.Valid {
		task.CompletedAt = completedAt.String
	}

	return task, nil
}

func (s *Service) requireTaskLock(ctx context.Context, queueID, taskID, workerID string) (Task, error) {
	if taskID == "" {
		return Task{}, fmt.Errorf("%w: task id is required", failure.ErrInvalid)
	}

	task, err := s.getTask(ctx, queueID, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("%w: queue task not found", failure.ErrNotFound)
	}

	if err != nil {
		return Task{}, fmt.Errorf("get queue task: %w", err)
	}

	var lockedBy string
	if err := s.db.QueryRowContext(ctx, `SELECT locked_by FROM queue_tasks WHERE id = ?`, task.ID).Scan(&lockedBy); err != nil {
		return Task{}, fmt.Errorf("get queue task lock: %w", err)
	}

	if task.Status != TaskStatusLeased || lockedBy != workerID {
		return Task{}, fmt.Errorf("%w: queue task is not leased by worker", failure.ErrConflict)
	}

	return task, nil
}

func expireLeases(ctx context.Context, tx *sql.Tx, queueID, now string) error {
	if _, err := tx.ExecContext(ctx, `
UPDATE queue_tasks
SET status = ?, locked_by = '', locked_until = '', last_error = CASE WHEN last_error = '' THEN 'task lease expired after max attempts' ELSE last_error END, updated_at = ?, completed_at = ?
WHERE queue_id = ? AND status = ? AND locked_until <= ? AND attempts >= retry_max_attempts
`, TaskStatusDead, now, now, queueID, TaskStatusLeased, now); err != nil {
		return fmt.Errorf("expire exhausted queue task leases: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE queue_tasks
SET status = ?, locked_by = '', locked_until = '', available_at = ?, updated_at = ?
WHERE queue_id = ? AND status = ? AND locked_until <= ? AND attempts < retry_max_attempts
`, TaskStatusPending, now, now, queueID, TaskStatusLeased, now); err != nil {
		return fmt.Errorf("expire queue task leases: %w", err)
	}

	return nil
}

func normalizeRetry(retry *RetryOptions) (RetryOptions, error) {
	if retry == nil {
		return RetryOptions{MaxAttempts: defaultMaxAttempts, DelayMS: defaultDelayMS, MaxDelayMS: defaultMaxDelayMS, BackoffMultiplier: defaultBackoffMultiplier, Jitter: defaultJitter}, nil
	}

	resolved := *retry
	if resolved.MaxAttempts < 1 {
		return RetryOptions{}, fmt.Errorf("%w: retry max_attempts must be at least 1", failure.ErrInvalid)
	}

	if resolved.DelayMS < 1 {
		return RetryOptions{}, fmt.Errorf("%w: retry delay_ms must be at least 1", failure.ErrInvalid)
	}

	if resolved.MaxDelayMS < 0 {
		return RetryOptions{}, fmt.Errorf("%w: retry max_delay_ms cannot be negative", failure.ErrInvalid)
	}

	if resolved.BackoffMultiplier != 0 && resolved.BackoffMultiplier < 1 {
		return RetryOptions{}, fmt.Errorf("%w: retry backoff_multiplier must be at least 1", failure.ErrInvalid)
	}

	return resolved, nil
}

func retryDelay(task Task) time.Duration {
	delay := float64(task.Retry.DelayMS)
	if task.Retry.BackoffMultiplier > 0 && task.Attempts > 1 {
		delay *= math.Pow(task.Retry.BackoffMultiplier, float64(task.Attempts-1))
	}

	if task.Retry.MaxDelayMS > 0 && delay > float64(task.Retry.MaxDelayMS) {
		delay = float64(task.Retry.MaxDelayMS)
	}

	delayMS := int64(delay)
	delayMS = max(delayMS, 1)

	if task.Retry.Jitter {
		jitter, err := rand.Int(rand.Reader, big.NewInt(delayMS+1))
		if err == nil {
			delayMS = jitter.Int64()
		}

		delayMS = max(delayMS, 1)
	}

	return time.Duration(delayMS) * time.Millisecond
}

func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

type rowScanner interface {
	Scan(...any) error
}
