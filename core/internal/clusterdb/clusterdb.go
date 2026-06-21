// Package clusterdb opens Postgres and applies cluster control plane migrations.
package clusterdb

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastion-computer/bastion/core/internal/clustermigrations"
)

const uniqueViolationCode = "23505"

// Client wraps a Postgres connection pool.
type Client struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres and applies pending cluster migrations.
func Open(ctx context.Context, databaseURL string) (*Client, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("cluster database URL is required")
	}

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse cluster database URL: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open cluster database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect cluster database: %w", err)
	}

	if err := runMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	return &Client{pool: pool}, nil
}

// Close closes the underlying connection pool.
func (c *Client) Close() {
	c.pool.Close()
}

// Exec executes a query without returning rows.
func (c *Client) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return c.pool.Exec(ctx, sql, args...)
}

// Query executes a query that returns rows.
func (c *Client) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return c.pool.Query(ctx, sql, args...)
}

// QueryRow executes a query expected to return one row.
func (c *Client) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return c.pool.QueryRow(ctx, sql, args...)
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS cluster_schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("initialize cluster migrations table: %w", err)
	}

	migrations, err := migrationFiles()
	if err != nil {
		return err
	}

	for _, name := range migrations {
		applied, err := migrationApplied(ctx, pool, name)
		if err != nil {
			return err
		}

		if applied {
			continue
		}

		if err := applyMigration(ctx, pool, name); err != nil {
			return err
		}
	}

	return nil
}

func migrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(clustermigrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read cluster migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}

		names = append(names, entry.Name())
	}

	sort.Strings(names)

	return names, nil
}

func migrationApplied(ctx context.Context, pool *pgxpool.Pool, name string) (bool, error) {
	var version string

	err := pool.QueryRow(ctx, `SELECT version FROM cluster_schema_migrations WHERE version = $1`, name).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("query cluster migration %s: %w", name, err)
	}

	return true, nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, name string) error {
	contents, err := fs.ReadFile(clustermigrations.FS, name)
	if err != nil {
		return fmt.Errorf("read cluster migration %s: %w", name, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cluster migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, string(contents)); err != nil {
		return fmt.Errorf("run cluster migration %s: %w", name, err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO cluster_schema_migrations (version) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("record cluster migration %s: %w", name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cluster migration %s: %w", name, err)
	}

	return nil
}

// IsConstraint reports whether err is a Postgres constraint violation.
func IsConstraint(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}
