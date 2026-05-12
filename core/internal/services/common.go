// Package services contains shared service-layer helpers.
package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/google/uuid"
)

// GenerateID returns a prefixed UUID v4 identifier.
func GenerateID(prefix string) (string, error) {
	if strings.TrimSpace(prefix) == "" {
		return "", errors.New("id prefix is required")
	}

	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}

	return prefix + "_" + value.String(), nil
}

// RequireIDOrKey validates that exactly one resource identifier is set.
func RequireIDOrKey(id, key string) error {
	if (id == "") == (key == "") {
		return fmt.Errorf("%w: specify exactly one of id or key", failure.ErrInvalid)
	}

	return nil
}

// LookupClause returns the WHERE clause and value for an ID-or-key lookup.
func LookupClause(id, key, idColumn, keyColumn string) (string, any) {
	if id != "" {
		return idColumn + " = ?", id
	}

	return keyColumn + " = ?", key
}

// QueryPage applies common created_at cursor pagination to a base query.
func QueryPage(ctx context.Context, db *database.Client, query string, limit int, cursor string) (*sql.Rows, error) {
	if cursor == "" {
		return db.QueryContext(ctx, query+` ORDER BY created_at LIMIT ?`, limit+1)
	}

	return db.QueryContext(ctx, query+` WHERE created_at > ? ORDER BY created_at LIMIT ?`, cursor, limit+1)
}

// Now returns the canonical service timestamp string.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
