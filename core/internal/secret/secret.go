// Package secret manages host environment secret references.
package secret

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/id"
	"github.com/bastion-computer/bastion/core/internal/page"
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

// New returns a secret service backed by db.
func New(db *database.Client) *Service {
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

	for attempt := range id.Retries {
		secretID, err := id.New("sec")
		if err != nil {
			return Secret{}, err
		}

		secret := Secret{ID: secretID, Key: req.Key, Env: req.Env, AllowHosts: allowHosts, CreatedAt: now()}
		if err := s.insert(ctx, secret); err != nil {
			if database.IsConstraint(err) {
				if attempt == id.Retries-1 {
					return Secret{}, fmt.Errorf("%w: secret already exists", failure.ErrConflict)
				}

				continue
			}

			return Secret{}, err
		}

		return secret, nil
	}

	return Secret{}, fmt.Errorf("%w: unable to generate unique secret id", failure.ErrConflict)
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
func (s *Service) List(ctx context.Context, limit int, cursor string) (page.Page[Secret], error) {
	limit = page.NormalizeLimit(limit)

	rows, err := queryPage(ctx, s.db, `SELECT id, key, env, created_at FROM secrets`, limit, cursor)
	if err != nil {
		return page.Page[Secret]{}, fmt.Errorf("list secrets: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Secret, 0, limit+1)

	for rows.Next() {
		var secret Secret
		if err := rows.Scan(&secret.ID, &secret.Key, &secret.Env, &secret.CreatedAt); err != nil {
			return page.Page[Secret]{}, fmt.Errorf("scan secret: %w", err)
		}

		entries = append(entries, secret)
	}

	if err := rows.Err(); err != nil {
		return page.Page[Secret]{}, fmt.Errorf("iterate secrets: %w", err)
	}

	for i := range entries {
		hosts, err := s.allowedHosts(ctx, entries[i].ID)
		if err != nil {
			return page.Page[Secret]{}, err
		}

		entries[i].AllowHosts = hosts
	}

	return page.FromEntries(entries, limit, func(secret Secret) string { return secret.CreatedAt }), nil
}

// Get returns a secret reference by ID or key.
func (s *Service) Get(ctx context.Context, secretID, key string) (Secret, error) {
	if err := requireIDOrKey(secretID, key); err != nil {
		return Secret{}, err
	}

	where, value := lookupClause(secretID, key, "id", "key")

	secret, err := s.getWhere(ctx, where, value)
	if err != nil {
		return Secret{}, err
	}

	secret.AllowHosts, err = s.allowedHosts(ctx, secret.ID)
	if err != nil {
		return Secret{}, err
	}

	return secret, nil
}

func (s *Service) getWhere(ctx context.Context, where string, value any) (Secret, error) {
	var secret Secret

	err := s.db.QueryRowContext(ctx, `SELECT id, key, env, created_at FROM secrets WHERE `+where, value).Scan(&secret.ID, &secret.Key, &secret.Env, &secret.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Secret{}, fmt.Errorf("%w: secret not found", failure.ErrNotFound)
	}

	if err != nil {
		return Secret{}, fmt.Errorf("get secret: %w", err)
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
