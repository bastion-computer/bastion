// Package sandbox manages sandbox lifecycle records.
package sandbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Source identifies the template or checkpoint used to create a sandbox.
type Source struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Sandbox describes a provisioned sandbox instance.
type Sandbox struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Source    Source `json:"source"`
	CreatedAt string `json:"createdAt"`
}

// CreateRequest contains the fields needed to create a sandbox.
type CreateRequest struct {
	From string `json:"from"`
	ID   string `json:"id,omitempty"`
	Key  string `json:"key,omitempty"`
}

// ExecRequest contains a command to run in a sandbox.
type ExecRequest struct {
	Command []string `json:"command"`
}

// ExecResponse describes the result of a sandbox command request.
type ExecResponse struct {
	ID      string   `json:"id"`
	Command []string `json:"command"`
	Status  string   `json:"status"`
}

// Service manages sandbox records.
type Service struct {
	db *database.Client
}

// NewService returns a sandbox service backed by db.
func NewService(db *database.Client) *Service {
	return &Service{db: db}
}

// Create stores a sandbox from a template or checkpoint source.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Sandbox, error) {
	if req.From != "template" && req.From != "checkpoint" {
		return Sandbox{}, fmt.Errorf("%w: source must be template or checkpoint", failure.ErrInvalid)
	}

	if err := services.RequireIDOrKey(req.ID, req.Key); err != nil {
		return Sandbox{}, err
	}

	sourceID, err := s.resolveSourceID(ctx, req.From, req.ID, req.Key)
	if err != nil {
		return Sandbox{}, err
	}

	sandboxID, err := services.GenerateID("sbx")
	if err != nil {
		return Sandbox{}, err
	}

	sandbox := Sandbox{ID: sandboxID, Status: "pending", Source: Source{Type: req.From, ID: sourceID}, CreatedAt: services.Now()}

	_, err = s.db.ExecContext(ctx, `INSERT INTO sandboxes (id, status, source_type, source_id, created_at) VALUES (?, ?, ?, ?, ?)`, sandbox.ID, sandbox.Status, sandbox.Source.Type, sandbox.Source.ID, sandbox.CreatedAt)
	if err != nil {
		if database.IsConstraint(err) {
			return Sandbox{}, fmt.Errorf("%w: sandbox already exists", failure.ErrConflict)
		}

		return Sandbox{}, fmt.Errorf("create sandbox: %w", err)
	}

	return sandbox, nil
}

// List returns sandboxes ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Sandbox], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, status, source_type, source_id, created_at FROM sandboxes`, limit, cursor)
	if err != nil {
		return services.Page[Sandbox]{}, fmt.Errorf("list sandboxes: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Sandbox, 0, limit+1)

	for rows.Next() {
		var sandbox Sandbox
		if err := rows.Scan(&sandbox.ID, &sandbox.Status, &sandbox.Source.Type, &sandbox.Source.ID, &sandbox.CreatedAt); err != nil {
			return services.Page[Sandbox]{}, fmt.Errorf("scan sandbox: %w", err)
		}

		entries = append(entries, sandbox)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Sandbox]{}, fmt.Errorf("iterate sandboxes: %w", err)
	}

	return services.FromEntries(entries, limit, func(sandbox Sandbox) string { return sandbox.CreatedAt }), nil
}

// Pause marks a sandbox as paused.
func (s *Service) Pause(ctx context.Context, sandboxID string) (Sandbox, error) {
	sandbox, err := s.Get(ctx, sandboxID)
	if err != nil {
		return Sandbox{}, err
	}

	sandbox.Status = "paused"
	if _, err := s.db.ExecContext(ctx, `UPDATE sandboxes SET status = ? WHERE id = ?`, sandbox.Status, sandbox.ID); err != nil {
		return Sandbox{}, fmt.Errorf("pause sandbox: %w", err)
	}

	return sandbox, nil
}

// Remove deletes a sandbox and returns the removed record.
func (s *Service) Remove(ctx context.Context, sandboxID string) (Sandbox, error) {
	sandbox, err := s.Get(ctx, sandboxID)
	if err != nil {
		return Sandbox{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM sandboxes WHERE id = ?`, sandbox.ID); err != nil {
		return Sandbox{}, fmt.Errorf("remove sandbox: %w", err)
	}

	return sandbox, nil
}

// Exec records a command execution request for a sandbox.
func (s *Service) Exec(ctx context.Context, sandboxID string, command []string) (ExecResponse, error) {
	if len(command) == 0 {
		return ExecResponse{}, fmt.Errorf("%w: command is required", failure.ErrInvalid)
	}

	if _, err := s.Get(ctx, sandboxID); err != nil {
		return ExecResponse{}, err
	}

	return ExecResponse{ID: sandboxID, Command: append([]string(nil), command...), Status: "not_implemented"}, nil
}

// Get returns a sandbox by ID.
func (s *Service) Get(ctx context.Context, sandboxID string) (Sandbox, error) {
	var sandbox Sandbox

	err := s.db.QueryRowContext(ctx, `SELECT id, status, source_type, source_id, created_at FROM sandboxes WHERE id = ?`, sandboxID).Scan(&sandbox.ID, &sandbox.Status, &sandbox.Source.Type, &sandbox.Source.ID, &sandbox.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Sandbox{}, fmt.Errorf("%w: sandbox not found", failure.ErrNotFound)
	}

	if err != nil {
		return Sandbox{}, fmt.Errorf("get sandbox: %w", err)
	}

	return sandbox, nil
}

func (s *Service) resolveSourceID(ctx context.Context, sourceType, sourceID, key string) (string, error) {
	switch sourceType {
	case "template":
		return s.resolveTemplateID(ctx, sourceID, key)
	case "checkpoint":
		return s.resolveCheckpointID(ctx, sourceID, key)
	default:
		return "", fmt.Errorf("%w: unsupported source type", failure.ErrInvalid)
	}
}

func (s *Service) resolveTemplateID(ctx context.Context, templateID, key string) (string, error) {
	where, value := services.LookupClause(templateID, key, "id", "key")

	var id string

	err := s.db.QueryRowContext(ctx, `SELECT id FROM templates WHERE `+where, value).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return "", fmt.Errorf("resolve template source: %w", err)
	}

	return id, nil
}

func (s *Service) resolveCheckpointID(ctx context.Context, checkpointID, key string) (string, error) {
	where, value := services.LookupClause(checkpointID, key, "id", "key")

	var id string

	err := s.db.QueryRowContext(ctx, `SELECT id FROM checkpoints WHERE `+where, value).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: checkpoint not found", failure.ErrNotFound)
	}

	if err != nil {
		return "", fmt.Errorf("resolve checkpoint source: %w", err)
	}

	return id, nil
}
