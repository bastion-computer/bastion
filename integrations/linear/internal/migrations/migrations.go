// Package migrations embeds Linear integration SQLite migrations.
package migrations

import "embed"

// FS contains migration files.
//
//go:embed *.sql
var FS embed.FS
