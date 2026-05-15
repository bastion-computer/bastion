// Package template manages environment templates.
package template

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Template contains an environment template and its JSON configuration.
type Template struct {
	ID        string          `json:"id"`
	Key       string          `json:"key"`
	Config    json.RawMessage `json:"config"`
	CreatedAt string          `json:"createdAt"`
}

// Metadata describes a template without its full configuration payload.
type Metadata struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	CreatedAt string `json:"createdAt"`
}

// CreateRequest contains the fields needed to create a template.
type CreateRequest struct {
	Key    string          `json:"key"`
	Config json.RawMessage `json:"config"`
}

// Service manages environment templates.
type Service struct {
	db *database.Client
}

// NewService returns a template service backed by db.
func NewService(db *database.Client) *Service {
	return &Service{db: db}
}

// Create stores a template and returns its metadata.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Metadata, error) {
	if strings.TrimSpace(req.Key) == "" {
		return Metadata{}, fmt.Errorf("%w: template key is required", failure.ErrInvalid)
	}

	if len(req.Config) == 0 || !json.Valid(req.Config) {
		return Metadata{}, fmt.Errorf("%w: template config must be valid JSON", failure.ErrInvalid)
	}

	templateID, err := services.GenerateID("tpl")
	if err != nil {
		return Metadata{}, err
	}

	template := Template{ID: templateID, Key: req.Key, Config: append([]byte(nil), req.Config...), CreatedAt: services.Now()}

	_, err = s.db.ExecContext(ctx, `INSERT INTO templates (id, key, config, created_at) VALUES (?, ?, ?, ?)`, template.ID, template.Key, string(template.Config), template.CreatedAt)
	if err != nil {
		if database.IsConstraint(err) {
			return Metadata{}, fmt.Errorf("%w: template already exists", failure.ErrConflict)
		}

		return Metadata{}, fmt.Errorf("create template: %w", err)
	}

	return template.Metadata(), nil
}

// List returns template metadata ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Metadata], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, key, created_at FROM templates`, limit, cursor)
	if err != nil {
		return services.Page[Metadata]{}, fmt.Errorf("list templates: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Metadata, 0, limit+1)

	for rows.Next() {
		var template Metadata
		if err := rows.Scan(&template.ID, &template.Key, &template.CreatedAt); err != nil {
			return services.Page[Metadata]{}, fmt.Errorf("scan template: %w", err)
		}

		entries = append(entries, template)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Metadata]{}, fmt.Errorf("iterate templates: %w", err)
	}

	return services.FromEntries(entries, limit, func(template Metadata) string { return template.CreatedAt }), nil
}

// Get returns a template by ID or key.
func (s *Service) Get(ctx context.Context, templateID, key string) (Template, error) {
	if err := services.RequireIDOrKey(templateID, key); err != nil {
		return Template{}, err
	}

	where, value := services.LookupClause(templateID, key, "id", "key")

	var (
		template Template
		config   string
	)

	err := s.db.QueryRowContext(ctx, `SELECT id, key, config, created_at FROM templates WHERE `+where, value).Scan(&template.ID, &template.Key, &config, &template.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return Template{}, fmt.Errorf("get template: %w", err)
	}

	template.Config = json.RawMessage(config)

	return template, nil
}

// Remove deletes a template by ID or key and returns the removed record.
func (s *Service) Remove(ctx context.Context, templateID, key string) (Template, error) {
	template, err := s.Get(ctx, templateID, key)
	if err != nil {
		return Template{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, template.ID); err != nil {
		if database.IsConstraint(err) {
			return Template{}, fmt.Errorf("%w: template is in use", failure.ErrConflict)
		}

		return Template{}, fmt.Errorf("remove template: %w", err)
	}

	return template, nil
}

// Metadata returns the template's metadata view.
func (t Template) Metadata() Metadata {
	return Metadata{ID: t.ID, Key: t.Key, CreatedAt: t.CreatedAt}
}
