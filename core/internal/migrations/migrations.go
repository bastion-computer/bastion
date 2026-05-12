// Package migrations embeds the SQLite migration files.
package migrations

import "embed"

// FS contains the SQL migrations applied by the core database package.
//
//go:embed *.sql
var FS embed.FS
