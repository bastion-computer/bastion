// Package secret manages host environment secret references.
package secret

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Secret describes a host environment variable reference.
type Secret struct {
	ID         string   `json:"id"`
	Key        string   `json:"key"`
	Env        string   `json:"env"`
	AllowHosts []string `json:"allowHosts"`
	CreatedAt  string   `json:"createdAt"`
}

// CreateRequest contains the fields needed to bind a secret reference.
type CreateRequest struct {
	Key        string   `json:"key"`
	Env        string   `json:"env"`
	AllowHosts []string `json:"allowHosts"`
}

// ResolveRequest identifies a secret reference to resolve.
type ResolveRequest struct {
	ID  string `json:"id,omitempty"`
	Key string `json:"key,omitempty"`
}

// Value contains a resolved secret value.
type Value struct {
	Value string `json:"value"`
}

// Service manages secret references.
type Service struct {
	db *database.Client
}

// NewService returns a secret service backed by db.
func NewService(db *database.Client) *Service {
	return &Service{db: db}
}

// Create stores a secret reference.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Secret, error) {
	if strings.TrimSpace(req.Key) == "" {
		return Secret{}, fmt.Errorf("%w: secret key is required", failure.ErrInvalid)
	}

	if strings.TrimSpace(req.Env) == "" {
		return Secret{}, fmt.Errorf("%w: secret env is required", failure.ErrInvalid)
	}

	allowHosts := uniqueStrings(req.AllowHosts)
	if len(allowHosts) == 0 {
		return Secret{}, fmt.Errorf("%w: at least one allowed host is required", failure.ErrInvalid)
	}

	secretID, err := services.GenerateID("sec")
	if err != nil {
		return Secret{}, err
	}

	secret := Secret{ID: secretID, Key: req.Key, Env: req.Env, AllowHosts: allowHosts, CreatedAt: services.Now()}
	if err := s.insert(ctx, secret); err != nil {
		if database.IsConstraint(err) {
			return Secret{}, fmt.Errorf("%w: secret already exists", failure.ErrConflict)
		}

		return Secret{}, err
	}

	return secret, nil
}

func (s *Service) insert(ctx context.Context, secret Secret) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create secret transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `INSERT INTO secrets (id, key, env, created_at) VALUES (?, ?, ?, ?)`, secret.ID, secret.Key, secret.Env, secret.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert secret: %w", err)
	}

	for _, host := range secret.AllowHosts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO secret_allowed_hosts (secret_id, host) VALUES (?, ?)`, secret.ID, host); err != nil {
			return fmt.Errorf("insert secret allowed host: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create secret transaction: %w", err)
	}

	return nil
}

// List returns secret references ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Secret], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, key, env, created_at FROM secrets`, limit, cursor)
	if err != nil {
		return services.Page[Secret]{}, fmt.Errorf("list secrets: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Secret, 0, limit+1)

	for rows.Next() {
		var secret Secret
		if err := rows.Scan(&secret.ID, &secret.Key, &secret.Env, &secret.CreatedAt); err != nil {
			return services.Page[Secret]{}, fmt.Errorf("scan secret: %w", err)
		}

		entries = append(entries, secret)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Secret]{}, fmt.Errorf("iterate secrets: %w", err)
	}

	for i := range entries {
		hosts, err := s.allowedHosts(ctx, entries[i].ID)
		if err != nil {
			return services.Page[Secret]{}, err
		}

		entries[i].AllowHosts = hosts
	}

	return services.FromEntries(entries, limit, func(secret Secret) string { return secret.CreatedAt }), nil
}

// Get returns a secret reference by ID or key.
func (s *Service) Get(ctx context.Context, secretID, key string) (Secret, error) {
	if err := services.RequireIDOrKey(secretID, key); err != nil {
		return Secret{}, err
	}

	where, value := services.LookupClause(secretID, key, "id", "key")

	var secret Secret

	err := s.db.QueryRowContext(ctx, `SELECT id, key, env, created_at FROM secrets WHERE `+where, value).Scan(&secret.ID, &secret.Key, &secret.Env, &secret.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Secret{}, fmt.Errorf("%w: secret not found", failure.ErrNotFound)
	}

	if err != nil {
		return Secret{}, fmt.Errorf("get secret: %w", err)
	}

	secret.AllowHosts, err = s.allowedHosts(ctx, secret.ID)
	if err != nil {
		return Secret{}, err
	}

	return secret, nil
}

// Resolve returns the current host environment value for a secret reference.
func (s *Service) Resolve(ctx context.Context, secretID, key string) (Value, error) {
	secret, err := s.Get(ctx, secretID, key)
	if err != nil {
		return Value{}, err
	}

	return Value{Value: os.Getenv(secret.Env)}, nil
}

// Remove deletes a secret reference by ID or key and returns the removed record.
func (s *Service) Remove(ctx context.Context, secretID, key string) (Secret, error) {
	secret, err := s.Get(ctx, secretID, key)
	if err != nil {
		return Secret{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM secrets WHERE id = ?`, secret.ID); err != nil {
		return Secret{}, fmt.Errorf("remove secret: %w", err)
	}

	return secret, nil
}

func (s *Service) allowedHosts(ctx context.Context, secretID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT host FROM secret_allowed_hosts WHERE secret_id = ? ORDER BY host`, secretID)
	if err != nil {
		return nil, fmt.Errorf("list secret allowed hosts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	hosts := []string{}

	for rows.Next() {
		var host string
		if err := rows.Scan(&host); err != nil {
			return nil, fmt.Errorf("scan secret allowed host: %w", err)
		}

		hosts = append(hosts, host)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate secret allowed hosts: %w", err)
	}

	return hosts, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}
