This package contains the Go implementation of Bastion's host API service and CLI.

## Overview

The core package lives in `core/` and builds the `bastion` binary. That binary is responsible for two entrypoints:

- `bastion start` runs the local host API service on `localhost:3148` by default.
- The remaining `bastion` CLI commands call the host API service and print JSON responses.

Core is intentionally narrow while the runtime is under development: it stores reusable templates and manages environment records. Connection, attach, terminal, exec, secret proxy, checkpoint, and Firecracker runtime behavior are not implemented yet.

## Layout

| Path | Purpose |
| ---- | ------- |
| `cmd/bastion` | Minimal binary entrypoint. |
| `internal/cli` | Cobra command tree and CLI output handling. |
| `internal/api` | Gin router assembly and HTTP server setup. Route definitions live here. |
| `internal/client` | HTTP client used by CLI commands to call the host API. |
| `internal/config` | Environment defaults and local path handling. |
| `internal/database` | SQLite client setup, query helpers, transactions, and migration handling. |
| `internal/failure` | Shared domain error sentinels mapped by the API layer. |
| `internal/handlers` | HTTP route handlers grouped by domain, plus shared handler helpers. Handlers adapt Gin requests to services. |
| `internal/services` | Shared service-layer helpers and response types. |
| `internal/services/template` | Template request/response types and persistence service. |
| `internal/services/environment` | Environment request/response types and persistence service. |
| `internal/migrations` | Embedded SQL migrations applied by core. |

Implementation packages should stay under `internal/` unless another Go module has a concrete need to import them.

## Tasks

Run package tasks from the repo root with mise:

| Task | Command | Purpose |
| ---- | ------- | ------- |
| `mise run //core:dev` | `air -c .air.toml` | Start the host API with live reload. |
| `mise run //core:lint` | `golangci-lint run ./...` | Run Go linters. |
| `mise run //core:format:check` | `gofmt -l .` check | Check Go formatting without writing files. |
| `mise run //core:format:write` | `gofmt -w .` | Rewrite Go formatting. |
| `mise run //core:build` | `go build -o ./tmp/bastion ./cmd/bastion` with optional version ldflags | Build the CLI binary. |
| `mise run //core:test` | `go test ./...` | Run Go tests. |

Root aggregate tasks include this package, so `mise run dev:up`, `mise run lint`, `mise run format:check`, `mise run build`, and `mise run test` can all be run from the repository root. The root `dev:up` task opens a tmux session with a dedicated pane for this package's Air process.

Local builds report `dev` from `internal/config.Version`. Release builds can inject a version by setting `BASTION_VERSION` before running `mise run //core:build`.

## API Service

`bastion start` accepts:

- `--addr`: listen address. Defaults to `localhost:3148` and can be set with `BASTION_ADDR`.
- `--data-dir`: persistent data directory. Defaults to `~/.bastion` and can be set with `BASTION_DATA_DIR`.

The service uses Gin and wraps it in `http.Server` so timeouts and graceful shutdown remain explicit. `internal/api/server.go` owns route registration. Domain-specific handler packages under `internal/handlers` expose `NewHandler(service)` constructors and handler methods used by those routes.

## Database

Core stores persistent data in SQLite at `<data-dir>/sqlite.db`.

- The default data directory is `~/.bastion`.
- The development data directory is `.bastion` via the Air configuration.
- Tests use `:memory:` and run the same migrations as local development.

SQL migrations live in `core/internal/migrations` and are embedded into the Go binary. `internal/database.Open()` runs pending migrations automatically before the API starts serving. If migrations fail, startup fails rather than serving against a partially migrated schema.

The core migrations are the schema source of truth. Development tools such as Drizzle Studio may inspect `.bastion/sqlite.db`, but they do not own or generate core migrations.

`internal/database` intentionally stays small: it opens SQLite, runs migrations, exposes context-aware query and transaction methods, and detects SQLite constraint errors. Service packages under `internal/services` own their own SQL and CRUD behavior.

## CLI

CLI commands call the host API configured by `--api-url` or `BASTION_API_URL`. The default is `http://localhost:3148`.

Supported command groups are intentionally limited to the current product scope:

- `bastion templates ...`
- `bastion env ...`

Logs and diagnostics go to stderr. JSON command output goes to stdout.
