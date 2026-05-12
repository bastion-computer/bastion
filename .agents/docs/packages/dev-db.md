This package contains development-only database debugging tools.

## Overview

The dev-db package lives in `dev-db/` and is included in the root `mise run dev:up` tmux workflow. It currently runs Drizzle Studio as a browser-based explorer for the local Bastion SQLite database.

## Tasks

| Task | Command | Purpose |
| ---- | ------- | ------- |
| `dev` | `drizzle-kit studio --port 3149` | Start Drizzle Studio for `.bastion/sqlite.db`. |
| `format:check` | `prettier --check '**'` | Check formatting without writing files. |
| `format:write` | `prettier --write '**'` | Rewrite formatting locally. |

`dev-db` does not own the database schema. Core migrations in `core/internal/migrations` are the source of truth. The dev task waits for `.bastion/sqlite.db` to exist before launching Drizzle Studio.
