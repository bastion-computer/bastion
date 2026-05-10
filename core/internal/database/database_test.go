package database_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bastion-computer/bastion/core/internal/database"
)

func TestOpenCreatesAndMigratesSQLiteDatabase(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()

	db, err := database.Open(dataDir)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if _, err := os.Stat(filepath.Join(dataDir, "sqlite.db")); err != nil {
		t.Fatalf("stat sqlite db: %v", err)
	}

	var tableName string

	err = db.QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'templates'`).Scan(&tableName)
	if err != nil {
		t.Fatalf("query migrated table: %v", err)
	}

	if tableName != "templates" {
		t.Fatalf("table name = %q, want templates", tableName)
	}
}

func TestOpenMemoryDatabaseRunsMigrations(t *testing.T) {
	t.Parallel()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	var tableName string

	err = db.QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'secrets'`).Scan(&tableName)
	if err != nil {
		t.Fatalf("query migrated table: %v", err)
	}

	if tableName != "secrets" {
		t.Fatalf("table name = %q, want secrets", tableName)
	}
}
