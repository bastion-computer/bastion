// Package checkpoint manages persisted sandbox checkpoints.
package checkpoint

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Source identifies the sandbox used to create a checkpoint.
type Source struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Checkpoint describes a restorable sandbox snapshot.
type Checkpoint struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Source    Source `json:"source"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// CreateRequest contains the fields needed to create a checkpoint.
type CreateRequest struct {
	Key       string `json:"key"`
	SandboxID string `json:"sandboxId"`
}

// Service manages checkpoint persistence.
type Service struct {
	db *database.Client
}

// NewService returns a checkpoint service backed by db.
func NewService(db *database.Client) *Service {
	return &Service{db: db}
}

// Create stores a checkpoint for a paused sandbox.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Checkpoint, error) {
	if strings.TrimSpace(req.Key) == "" {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint key is required", failure.ErrInvalid)
	}

	if strings.TrimSpace(req.SandboxID) == "" {
		return Checkpoint{}, fmt.Errorf("%w: sandbox id is required", failure.ErrInvalid)
	}

	checkpointID, err := services.GenerateID("chk")
	if err != nil {
		return Checkpoint{}, err
	}

	checkpoint := Checkpoint{ID: checkpointID, Key: req.Key, Source: Source{Type: "sandbox", ID: req.SandboxID}, Status: "pending", CreatedAt: services.Now()}
	if err := s.insert(ctx, checkpoint); err != nil {
		if database.IsConstraint(err) {
			return Checkpoint{}, fmt.Errorf("%w: checkpoint already exists", failure.ErrConflict)
		}

		return Checkpoint{}, err
	}

	return checkpoint, nil
}

func (s *Service) insert(ctx context.Context, checkpoint Checkpoint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create checkpoint transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status string

	err = tx.QueryRowContext(ctx, `SELECT status FROM sandboxes WHERE id = ?`, checkpoint.Source.ID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: sandbox not found", failure.ErrNotFound)
	}

	if err != nil {
		return fmt.Errorf("get checkpoint sandbox: %w", err)
	}

	if status != "paused" {
		return fmt.Errorf("%w: sandbox must be paused before checkpointing", failure.ErrInvalid)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO checkpoints (id, key, source_sandbox_id, status, created_at) VALUES (?, ?, ?, ?, ?)`, checkpoint.ID, checkpoint.Key, checkpoint.Source.ID, checkpoint.Status, checkpoint.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert checkpoint: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create checkpoint transaction: %w", err)
	}

	return nil
}

// List returns checkpoints ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Checkpoint], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, key, source_sandbox_id, status, created_at FROM checkpoints`, limit, cursor)
	if err != nil {
		return services.Page[Checkpoint]{}, fmt.Errorf("list checkpoints: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Checkpoint, 0, limit+1)

	for rows.Next() {
		var checkpoint Checkpoint

		checkpoint.Source.Type = "sandbox"
		if err := rows.Scan(&checkpoint.ID, &checkpoint.Key, &checkpoint.Source.ID, &checkpoint.Status, &checkpoint.CreatedAt); err != nil {
			return services.Page[Checkpoint]{}, fmt.Errorf("scan checkpoint: %w", err)
		}

		entries = append(entries, checkpoint)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Checkpoint]{}, fmt.Errorf("iterate checkpoints: %w", err)
	}

	return services.FromEntries(entries, limit, func(checkpoint Checkpoint) string { return checkpoint.CreatedAt }), nil
}

// Remove deletes a checkpoint by ID or key and returns the removed record.
func (s *Service) Remove(ctx context.Context, checkpointID, key string) (Checkpoint, error) {
	checkpoint, err := s.Get(ctx, checkpointID, key)
	if err != nil {
		return Checkpoint{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM checkpoints WHERE id = ?`, checkpoint.ID); err != nil {
		return Checkpoint{}, fmt.Errorf("remove checkpoint: %w", err)
	}

	return checkpoint, nil
}

// Get returns a checkpoint by ID or key.
func (s *Service) Get(ctx context.Context, checkpointID, key string) (Checkpoint, error) {
	if err := services.RequireIDOrKey(checkpointID, key); err != nil {
		return Checkpoint{}, err
	}

	where, value := services.LookupClause(checkpointID, key, "id", "key")

	var checkpoint Checkpoint

	checkpoint.Source.Type = "sandbox"

	err := s.db.QueryRowContext(ctx, `SELECT id, key, source_sandbox_id, status, created_at FROM checkpoints WHERE `+where, value).Scan(&checkpoint.ID, &checkpoint.Key, &checkpoint.Source.ID, &checkpoint.Status, &checkpoint.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, fmt.Errorf("%w: checkpoint not found", failure.ErrNotFound)
	}

	if err != nil {
		return Checkpoint{}, fmt.Errorf("get checkpoint: %w", err)
	}

	return checkpoint, nil
}
