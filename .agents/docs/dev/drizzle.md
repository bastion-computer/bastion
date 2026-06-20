This development tooling package contains database debugging tools.

## Overview

The `@bastion/dev/drizzle` package lives in `.dev/drizzle/` and is included in the root `mise run dev:up` tmux workflow. It runs Drizzle Studio as a browser-based explorer for the local Bastion SQLite node database and the cluster Postgres database.

## Tasks

| Task | Command | Purpose |
| ---- | ------- | ------- |
| `dev` | `bun run dev:node` | Start Drizzle Studio for `.bastion/sqlite.db`. |
| `dev:node` | `drizzle-kit studio --config drizzle.node.config.ts --port 3149` | Start Drizzle Studio for `.bastion/sqlite.db`. |
| `dev:cluster` | `drizzle-kit studio --config drizzle.cluster.config.ts --port 3151` | Start Drizzle Studio for cluster Postgres. |
| `format:check` | `prettier --check '**'` | Check formatting without writing files. |
| `format:write` | `prettier --write '**'` | Rewrite formatting locally. |

`@bastion/dev/drizzle` does not own database schemas. Core node migrations in `core/internal/migrations` and cluster migrations in `core/internal/clusterapi/migrations` are the sources of truth. The node dev task waits for `.bastion/sqlite.db`; the cluster dev task waits for Postgres on `localhost:3152`.
