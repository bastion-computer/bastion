// Package database opens the Linear integration SQLite database.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sqlite "github.com/mattn/go-sqlite3"

	"github.com/bastion-computer/bastion/integrations/linear/internal/migrations"
)

// Client wraps a SQLite connection.
type Client struct {
	db *sql.DB
}

// Open connects to SQLite and applies integration migrations.
func Open(path string) (*Client, error) {
	dsn, err := dsn(path)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect sqlite database: %w", err)
	}

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Client{db: db}, nil
}

// Close closes the database.
func (c *Client) Close() error { return c.db.Close() }

// ExecContext executes a statement.
func (c *Client) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, query, args...)
}

// QueryContext runs a query.
func (c *Client) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, query, args...)
}

// QueryRowContext runs a single-row query.
func (c *Client) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.db.QueryRowContext(ctx, query, args...)
}

// BeginTx starts a transaction.
func (c *Client) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.db.BeginTx(ctx, opts)
}

// IsConstraint reports SQLite constraint errors.
func IsConstraint(err error) bool {
	var sqliteErr sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite.ErrConstraint
}

func dsn(path string) (string, error) {
	if path == ":memory:" {
		return ":memory:?_foreign_keys=on&_busy_timeout=5000", nil
	}

	if strings.TrimSpace(path) == "" {
		return "", errors.New("database path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create database directory: %w", err)
	}

	return path + "?_foreign_keys=on&_busy_timeout=5000", nil
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	contents, err := migrations.FS.ReadFile("000001_init.up.sql")
	if err != nil {
		return fmt.Errorf("read migration: %w", err)
	}

	if _, err := db.ExecContext(ctx, string(contents)); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
