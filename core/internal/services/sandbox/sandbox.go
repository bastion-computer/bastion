// Package sandbox manages sandbox lifecycle records.
package sandbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/id"
	"github.com/bastion-computer/bastion/core/internal/page"
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

	if err := requireIDOrKey(req.ID, req.Key); err != nil {
		return Sandbox{}, err
	}

	sourceID, err := s.resolveSourceID(ctx, req.From, req.ID, req.Key)
	if err != nil {
		return Sandbox{}, err
	}

	sandboxID, err := id.New("sbx")
	if err != nil {
		return Sandbox{}, err
	}

	sandbox := Sandbox{ID: sandboxID, Status: "pending", Source: Source{Type: req.From, ID: sourceID}, CreatedAt: now()}

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
func (s *Service) List(ctx context.Context, limit int, cursor string) (page.Page[Sandbox], error) {
	limit = page.NormalizeLimit(limit)

	rows, err := queryPage(ctx, s.db, `SELECT id, status, source_type, source_id, created_at FROM sandboxes`, limit, cursor)
	if err != nil {
		return page.Page[Sandbox]{}, fmt.Errorf("list sandboxes: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Sandbox, 0, limit+1)

	for rows.Next() {
		var sandbox Sandbox
		if err := rows.Scan(&sandbox.ID, &sandbox.Status, &sandbox.Source.Type, &sandbox.Source.ID, &sandbox.CreatedAt); err != nil {
			return page.Page[Sandbox]{}, fmt.Errorf("scan sandbox: %w", err)
		}

		entries = append(entries, sandbox)
	}

	if err := rows.Err(); err != nil {
		return page.Page[Sandbox]{}, fmt.Errorf("iterate sandboxes: %w", err)
	}

	return page.FromEntries(entries, limit, func(sandbox Sandbox) string { return sandbox.CreatedAt }), nil
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
	where, value := lookupClause(templateID, key, "id", "key")

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
	where, value := lookupClause(checkpointID, key, "id", "key")

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

func requireIDOrKey(id, key string) error {
	if (id == "") == (key == "") {
		return fmt.Errorf("%w: specify exactly one of id or key", failure.ErrInvalid)
	}

	return nil
}

func lookupClause(id, key, idColumn, keyColumn string) (string, any) {
	if id != "" {
		return idColumn + " = ?", id
	}

	return keyColumn + " = ?", key
}

func queryPage(ctx context.Context, db *database.Client, query string, limit int, cursor string) (*sql.Rows, error) {
	if cursor == "" {
		return db.QueryContext(ctx, query+` ORDER BY created_at LIMIT ?`, limit+1)
	}

	return db.QueryContext(ctx, query+` WHERE created_at > ? ORDER BY created_at LIMIT ?`, cursor, limit+1)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
