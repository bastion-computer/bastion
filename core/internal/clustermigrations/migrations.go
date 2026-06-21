// Package clustermigrations embeds the Postgres migrations for the cluster control plane.
package clustermigrations

import "embed"

// FS contains the SQL migrations applied by the cluster database package.
//
//go:embed *.sql
var FS embed.FS
