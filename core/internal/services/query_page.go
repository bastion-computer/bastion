//go:build !darwin

package services

import (
	"context"
	"database/sql"

	"github.com/bastion-computer/bastion/core/internal/database"
)

// QueryPage applies common created_at cursor pagination to a base query.
func QueryPage(ctx context.Context, db *database.Client, query string, limit int, cursor string) (*sql.Rows, error) {
	if cursor == "" {
		return db.QueryContext(ctx, query+` ORDER BY created_at LIMIT ?`, limit+1)
	}

	return db.QueryContext(ctx, query+` WHERE created_at > ? ORDER BY created_at LIMIT ?`, cursor, limit+1)
}
