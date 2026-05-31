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

	assertMigratedTable(t, db, "templates")
	assertMigratedTable(t, db, "queues")
	assertMigratedTable(t, db, "queue_tasks")
}

func TestOpenMemoryDatabaseRunsMigrations(t *testing.T) {
	t.Parallel()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	assertMigratedTable(t, db, "environments")
	assertMigratedTable(t, db, "queues")
}

func assertMigratedTable(t *testing.T, db *database.Client, name string) {
	t.Helper()

	var tableName string

	err := db.QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&tableName)
	if err != nil {
		t.Fatalf("query migrated table %s: %v", name, err)
	}

	if tableName != name {
		t.Fatalf("table name = %q, want %s", tableName, name)
	}
}
