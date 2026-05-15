// Package environment manages Bastion environment records.
package environment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Environment describes a managed opencode environment.
type Environment struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	TemplateID string `json:"templateId"`
	CreatedAt  string `json:"createdAt"`
}

// CreateRequest contains the fields needed to create an environment.
type CreateRequest struct {
	TemplateID  string `json:"templateId,omitempty"`
	TemplateKey string `json:"templateKey,omitempty"`
}

// Service manages environment records.
type Service struct {
	db *database.Client
}

// NewService returns an environment service backed by db.
func NewService(db *database.Client) *Service {
	return &Service{db: db}
}

// Create stores an environment from a template.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Environment, error) {
	if err := services.RequireIDOrKey(req.TemplateID, req.TemplateKey); err != nil {
		return Environment{}, err
	}

	templateID, err := s.resolveTemplateID(ctx, req.TemplateID, req.TemplateKey)
	if err != nil {
		return Environment{}, err
	}

	environmentID, err := services.GenerateID("env")
	if err != nil {
		return Environment{}, err
	}

	environment := Environment{ID: environmentID, Status: "pending", TemplateID: templateID, CreatedAt: services.Now()}

	_, err = s.db.ExecContext(ctx, `INSERT INTO environments (id, status, template_id, created_at) VALUES (?, ?, ?, ?)`, environment.ID, environment.Status, environment.TemplateID, environment.CreatedAt)
	if err != nil {
		if database.IsConstraint(err) {
			return Environment{}, fmt.Errorf("%w: environment already exists", failure.ErrConflict)
		}

		return Environment{}, fmt.Errorf("create environment: %w", err)
	}

	return environment, nil
}

// List returns environments ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Environment], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, status, template_id, created_at FROM environments`, limit, cursor)
	if err != nil {
		return services.Page[Environment]{}, fmt.Errorf("list environments: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Environment, 0, limit+1)

	for rows.Next() {
		var environment Environment
		if err := rows.Scan(&environment.ID, &environment.Status, &environment.TemplateID, &environment.CreatedAt); err != nil {
			return services.Page[Environment]{}, fmt.Errorf("scan environment: %w", err)
		}

		entries = append(entries, environment)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Environment]{}, fmt.Errorf("iterate environments: %w", err)
	}

	return services.FromEntries(entries, limit, func(environment Environment) string { return environment.CreatedAt }), nil
}

// Get returns an environment by ID.
func (s *Service) Get(ctx context.Context, environmentID string) (Environment, error) {
	var environment Environment

	err := s.db.QueryRowContext(ctx, `SELECT id, status, template_id, created_at FROM environments WHERE id = ?`, environmentID).Scan(&environment.ID, &environment.Status, &environment.TemplateID, &environment.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Environment{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if err != nil {
		return Environment{}, fmt.Errorf("get environment: %w", err)
	}

	return environment, nil
}

// Remove deletes an environment and returns the removed record.
func (s *Service) Remove(ctx context.Context, environmentID string) (Environment, error) {
	environment, err := s.Get(ctx, environmentID)
	if err != nil {
		return Environment{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, environment.ID); err != nil {
		return Environment{}, fmt.Errorf("remove environment: %w", err)
	}

	return environment, nil
}

func (s *Service) resolveTemplateID(ctx context.Context, templateID, templateKey string) (string, error) {
	where, value := services.LookupClause(templateID, templateKey, "id", "key")

	var id string

	err := s.db.QueryRowContext(ctx, `SELECT id FROM templates WHERE `+where, value).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return "", fmt.Errorf("resolve template: %w", err)
	}

	return id, nil
}
