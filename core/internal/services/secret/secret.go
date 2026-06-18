//go:build !darwin

// Package secret manages Bastion secrets.
package secret

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

// Secret contains a secret and its value.
type Secret struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	Value     string  `json:"value"`
	CreatedAt string  `json:"createdAt"`
}

// Metadata describes a secret without its value.
type Metadata struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	CreatedAt string  `json:"createdAt"`
}

// CreateRequest contains the fields needed to create a secret.
type CreateRequest struct {
	Key   *string `json:"key,omitempty"`
	Value string  `json:"value"`
}

// Service manages secrets.
type Service struct {
	db *database.Client
}

// NewService returns a secret service backed by db.
func NewService(db *database.Client) *Service {
	return &Service{db: db}
}

// Create stores a secret and returns its metadata.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Metadata, error) {
	if err := services.ValidateOptionalKey("secret", req.Key); err != nil {
		return Metadata{}, err
	}

	if req.Value == "" {
		return Metadata{}, fmt.Errorf("%w: secret value is required", failure.ErrInvalid)
	}

	secretID, err := services.GenerateID("sec")
	if err != nil {
		return Metadata{}, err
	}

	secret := Secret{ID: secretID, Key: services.CopyStringPtr(req.Key), Value: req.Value, CreatedAt: services.Now()}

	_, err = s.db.ExecContext(ctx, `INSERT INTO secrets (id, key, value, created_at) VALUES (?, ?, ?, ?)`, secret.ID, services.OptionalStringValue(secret.Key), secret.Value, secret.CreatedAt)
	if err != nil {
		if database.IsConstraint(err) {
			return Metadata{}, fmt.Errorf("%w: secret already exists", failure.ErrConflict)
		}

		return Metadata{}, fmt.Errorf("create secret: %w", err)
	}

	return secret.Metadata(), nil
}

// List returns secret metadata ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Metadata], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, key, created_at FROM secrets`, limit, cursor)
	if err != nil {
		return services.Page[Metadata]{}, fmt.Errorf("list secrets: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Metadata, 0, limit+1)

	for rows.Next() {
		var (
			secret Metadata
			key    sql.NullString
		)
		if err := rows.Scan(&secret.ID, &key, &secret.CreatedAt); err != nil {
			return services.Page[Metadata]{}, fmt.Errorf("scan secret: %w", err)
		}

		secret.Key = services.NullStringPtr(key)
		entries = append(entries, secret)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Metadata]{}, fmt.Errorf("iterate secrets: %w", err)
	}

	return services.FromEntries(entries, limit, func(secret Metadata) string { return secret.CreatedAt }), nil
}

// Get returns a secret by ID or key.
func (s *Service) Get(ctx context.Context, secretID, key string) (Secret, error) {
	if err := services.RequireIDOrKey(secretID, key); err != nil {
		return Secret{}, err
	}

	where, value := services.LookupClause(secretID, key, "id", "key")

	var (
		secret    Secret
		secretKey sql.NullString
	)

	err := s.db.QueryRowContext(ctx, `SELECT id, key, value, created_at FROM secrets WHERE `+where, value).Scan(&secret.ID, &secretKey, &secret.Value, &secret.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Secret{}, fmt.Errorf("%w: secret not found", failure.ErrNotFound)
	}

	if err != nil {
		return Secret{}, fmt.Errorf("get secret: %w", err)
	}

	secret.Key = services.NullStringPtr(secretKey)

	return secret, nil
}

// GetReference returns a secret by template reference, treating sec_ values as IDs.
func (s *Service) GetReference(ctx context.Context, reference string) (Secret, error) {
	if strings.HasPrefix(reference, "sec_") {
		return s.Get(ctx, reference, "")
	}

	return s.Get(ctx, "", reference)
}

// Remove deletes a secret by ID or key and returns its metadata.
func (s *Service) Remove(ctx context.Context, secretID, key string) (Metadata, error) {
	secret, err := s.Get(ctx, secretID, key)
	if err != nil {
		return Metadata{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE id = ?`, secret.ID); err != nil {
		return Metadata{}, fmt.Errorf("remove secret: %w", err)
	}

	return secret.Metadata(), nil
}

// Metadata returns the secret's metadata view.
func (s Secret) Metadata() Metadata {
	return Metadata{ID: s.ID, Key: services.CopyStringPtr(s.Key), CreatedAt: s.CreatedAt}
}
