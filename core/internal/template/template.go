// Package template manages sandbox templates.
package template

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/id"
	"github.com/bastion-computer/bastion/core/internal/page"
)

// Template contains a sandbox template and its JSON configuration.
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

// Service manages sandbox templates.
type Service struct {
	db *database.Client
}

// New returns a template service backed by db.
func New(db *database.Client) *Service {
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

	for attempt := range id.Retries {
		templateID, err := id.New("tpl")
		if err != nil {
			return Metadata{}, err
		}

		template := Template{ID: templateID, Key: req.Key, Config: append([]byte(nil), req.Config...), CreatedAt: now()}

		_, err = s.db.ExecContext(ctx, `INSERT INTO templates (id, key, config, created_at) VALUES (?, ?, ?, ?)`, template.ID, template.Key, string(template.Config), template.CreatedAt)
		if err != nil {
			if database.IsConstraint(err) {
				if attempt == id.Retries-1 {
					return Metadata{}, fmt.Errorf("%w: template already exists", failure.ErrConflict)
				}

				continue
			}

			return Metadata{}, fmt.Errorf("create template: %w", err)
		}

		return template.Metadata(), nil
	}

	return Metadata{}, fmt.Errorf("%w: unable to generate unique template id", failure.ErrConflict)
}

// List returns template metadata ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (page.Page[Metadata], error) {
	limit = page.NormalizeLimit(limit)

	rows, err := queryPage(ctx, s.db, `SELECT id, key, created_at FROM templates`, limit, cursor)
	if err != nil {
		return page.Page[Metadata]{}, fmt.Errorf("list templates: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Metadata, 0, limit+1)

	for rows.Next() {
		var template Metadata
		if err := rows.Scan(&template.ID, &template.Key, &template.CreatedAt); err != nil {
			return page.Page[Metadata]{}, fmt.Errorf("scan template: %w", err)
		}

		entries = append(entries, template)
	}

	if err := rows.Err(); err != nil {
		return page.Page[Metadata]{}, fmt.Errorf("iterate templates: %w", err)
	}

	return page.FromEntries(entries, limit, func(template Metadata) string { return template.CreatedAt }), nil
}

// Get returns a template by ID or key.
func (s *Service) Get(ctx context.Context, templateID, key string) (Template, error) {
	if err := requireIDOrKey(templateID, key); err != nil {
		return Template{}, err
	}

	where, value := lookupClause(templateID, key, "id", "key")

	return s.getWhere(ctx, where, value)
}

// Remove deletes a template by ID or key and returns the removed record.
func (s *Service) Remove(ctx context.Context, templateID, key string) (Template, error) {
	template, err := s.Get(ctx, templateID, key)
	if err != nil {
		return Template{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, template.ID); err != nil {
		return Template{}, fmt.Errorf("remove template: %w", err)
	}

	return template, nil
}

func (s *Service) getWhere(ctx context.Context, where string, value any) (Template, error) {
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

// Metadata returns the template's metadata view.
func (t Template) Metadata() Metadata {
	return Metadata{ID: t.ID, Key: t.Key, CreatedAt: t.CreatedAt}
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
