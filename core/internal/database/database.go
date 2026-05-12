// Package database opens SQLite and applies core migrations.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	sqlite "github.com/mattn/go-sqlite3"

	"github.com/bastion-computer/bastion/core/internal/migrations"
)

// Client wraps a SQLite database connection.
type Client struct {
	db *sql.DB
}

// Open connects to SQLite in dataDir and applies migrations.
func Open(dataDir string) (*Client, error) {
	dsn, err := dsn(dataDir)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
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

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Client{db: db}, nil
}

// Close closes the underlying database connection.
func (c *Client) Close() error {
	return c.db.Close()
}

// ExecContext executes a query without returning rows.
func (c *Client) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, query, args...)
}

// QueryContext executes a query that returns rows.
func (c *Client) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, query, args...)
}

// QueryRowContext executes a query expected to return one row.
func (c *Client) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.db.QueryRowContext(ctx, query, args...)
}

// BeginTx starts a database transaction.
func (c *Client) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return c.db.BeginTx(ctx, opts)
}

// IsConstraint reports whether err is a SQLite constraint violation.
func IsConstraint(err error) bool {
	var sqliteErr sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code == sqlite.ErrConstraint
}

func dsn(dataDir string) (string, error) {
	if dataDir == ":memory:" {
		return ":memory:?_foreign_keys=on&_busy_timeout=5000", nil
	}

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}

	return filepath.Join(dataDir, "sqlite.db") + "?_foreign_keys=on&_busy_timeout=5000", nil
}

func runMigrations(db *sql.DB) error {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{})
	if err != nil {
		return fmt.Errorf("initialize migration database driver: %w", err)
	}

	runner, err := migrate.NewWithInstance("iofs", source, "sqlite3", driver)
	if err != nil {
		return fmt.Errorf("initialize migrations: %w", err)
	}

	if err := runner.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
