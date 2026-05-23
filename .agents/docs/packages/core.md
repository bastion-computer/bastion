This package contains the Go implementation of Bastion's host API service and CLI.

## Overview

The core package lives in `core/` and builds the `bastion` and `bastiond` binaries. The `bastion` binary is responsible for two entrypoints:

- `bastion start` runs the local host API service on `localhost:3148` by default.
- The remaining `bastion` CLI commands call the host API service and print JSON responses.

The `bastiond` binary runs privileged Firecracker runtime operations behind a Unix socket and should be started with `sudo bastiond`.

## Layout

| Path | Purpose |
| ---- | ------- |
| `cmd/bastion` | Minimal CLI/API binary entrypoint. |
| `cmd/bastiond` | Privileged Firecracker daemon entrypoint. |
| `internal/cli` | Cobra command tree and CLI output handling. |
| `internal/api` | Gin router assembly and HTTP server setup. Route definitions live here. |
| `internal/client` | HTTP client used by CLI commands to call the host API. |
| `internal/config` | Environment defaults and local path handling. |
| `internal/database` | SQLite client setup, query helpers, transactions, and migration handling. |
| `internal/failure` | Shared domain error sentinels mapped by the API layer. |
| `internal/firecracker` | Firecracker runtime orchestration, bastiond Gin routes, Unix socket client, and per-environment VM state. |
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
| `mise run //core:dev:api` | `air -c .air.api.toml` | Start the host API with live reload. |
| `mise run //core:dev:daemon` | `air -c .air.daemon.toml` | Start bastiond with live reload. |
| `mise run //core:lint` | `golangci-lint run ./...` | Run Go linters. |
| `mise run //core:format:check` | `gofmt -l .` check | Check Go formatting without writing files. |
| `mise run //core:format:write` | `gofmt -w .` | Rewrite Go formatting. |
| `mise run //core:build` | `go build -o ./tmp/bastion ./cmd/bastion` and `go build -o ./tmp/bastiond ./cmd/bastiond` with optional version ldflags | Build the CLI and daemon binaries. |
| `mise run //core:test` | `go test ./...` | Run Go tests. |

Root aggregate tasks include this package, so `mise run dev:up`, `mise run lint`, `mise run format:check`, `mise run build`, and `mise run test` can all be run from the repository root. The root `dev:up` task opens a tmux session with dedicated panes for the API and bastiond Air processes.

Local builds report `dev` from `internal/config.Version`. Release builds can inject a version by setting `BASTION_VERSION` before running `mise run //core:build`.

## API Service

`bastion start` accepts:

- `--addr`: listen address. Defaults to `localhost:3148` and can be set with `BASTION_ADDR`.
- `--data-dir`: persistent data directory. Defaults to `~/.bastion` and can be set with `BASTION_DATA_DIR`.
- `--bastiond-socket`: Unix socket path for the privileged daemon. Defaults to `/run/bastion/bastiond.sock` and can be set with `BASTIOND_SOCKET`.
- `--log-format`: log handler format. Defaults to `json` and can be set with `BASTION_LOG_FORMAT`; supported values are `json` and `text`.
- `--log-level`: minimum log level. Defaults to `info` and can be set with `BASTION_LOG_LEVEL`; supported values are `debug`, `info`, `warn`, and `error`.

The service uses Gin and wraps it in `http.Server` so timeouts and graceful shutdown remain explicit. `internal/api/server.go` owns route registration. Domain-specific handler packages under `internal/handlers` expose `NewHandler(service)` constructors and handler methods used by those routes.

`bastiond` accepts `--data-dir`, `--socket`, `--socket-uid`, `--socket-gid`, `--vm-uid`, `--vm-gid`, `--log-format`, and `--log-level`. It also uses Gin, but listens on a Unix socket instead of TCP. Root-only Firecracker operations, TAP device setup, jailer launch, and VM cleanup belong in `internal/firecracker`; do not add runtime orchestration to `internal/system`, which is limited to `bastion system ...` host setup commands.

## Database

Core stores persistent data in SQLite at `<data-dir>/sqlite.db`.

- The default data directory is `~/.bastion`.
- The development data directory is `.bastion` via the Air configuration.
- `bastion start` owns creating the top-level data directory; `bastiond` waits for it at startup and must not create it first.
- Tests use `:memory:` and run the same migrations as local development.

SQL migrations live in `core/internal/migrations` and are embedded into the Go binary. `internal/database.Open()` runs pending migrations automatically before the API starts serving. If migrations fail, startup fails rather than serving against a partially migrated schema.

The core migrations are the schema source of truth. Development tools such as Drizzle Studio may inspect `.bastion/sqlite.db`, but they do not own or generate core migrations.

`internal/database` intentionally stays small: it opens SQLite, runs migrations, exposes context-aware query and transaction methods, and detects SQLite constraint errors. Service packages under `internal/services` own their own SQL and CRUD behavior.

## CLI

CLI commands call the host API configured by `--api-url` or `BASTION_API_URL`. The default is `http://localhost:3148`.

Supported command groups are intentionally limited to the current product scope:

- `bastion templates ...`
- `bastion env ...`
- `bastion ssh ENVIRONMENT_ID [-- COMMAND...]`

Logs and diagnostics go to stderr. Host API logs are structured and include fields such as `request_id`, `method`, `route`, `status`, `duration`, `client_ip`, and `body_size`. The default format is JSON for machine parsing; the Air dev entrypoint uses `--log-format text` for readable local logs. API responses echo or generate `X-Request-ID` so request logs can be correlated with callers. JSON command output goes to stdout.
