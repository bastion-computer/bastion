This package contains the Go implementation of Bastion's host API service and CLI.

## Overview

The core package lives in `core/`. On Linux it builds the `bastion`, `bastiond`, and `bastion-guest-proxy` binaries; on Darwin it builds the client-only `bastion` binary. The `bastion` binary is responsible for two kinds of entrypoints:

- `bastion start` runs the local host API service on `localhost:3148` by default.
- CLI commands operate locally when managing host/client configuration, or call the host API service and print JSON responses for product resources.

The `bastiond` binary runs privileged Cloud Hypervisor runtime operations behind a Unix socket and should be started with `sudo bastiond`.

## Layout

| Path | Purpose |
| ---- | ------- |
| `actions` | Embedded built-in preset actions seeded into `<data-dir>/actions`. |
| `cmd/bastion` | Minimal CLI/API binary entrypoint. |
| `cmd/bastiond` | Privileged Cloud Hypervisor daemon entrypoint. |
| `cmd/bastion-guest-proxy` | Minimal Linux guest-side vsock HTTP proxy installed into templates. |
| `pkg/sshtunnel` | Public SSH tunnel framing protocol shared by CLI/API SSH streams. |
| `internal/cli` | Cobra command tree and CLI output handling. |
| `internal/api` | Gin router assembly and HTTP server setup. Route definitions live here. |
| `internal/client` | HTTP client used by CLI commands to call the host API. |
| `internal/config` | Environment defaults and local path handling. |
| `internal/database` | SQLite client setup, query helpers, transactions, and migration handling. |
| `internal/failure` | Shared domain error sentinels mapped by the API layer. |
| `internal/cloudhypervisor` | Cloud Hypervisor runtime orchestration, bastiond Gin routes, Unix socket client, and per-environment VM state. |
| `internal/handlers` | HTTP route handlers grouped by domain, plus shared handler helpers. Handlers adapt Gin requests to services. |
| `internal/logging` | Shared `slog` logger configuration for API and daemon processes. |
| `internal/services` | Shared service-layer helpers and response types. |
| `internal/services/template` | Template request/response types and persistence service. |
| `internal/services/environment` | Environment request/response types and persistence service. |
| `internal/tunnel` | Shared guest-proxy tunnel constants and Cloud Hypervisor vsock dial helpers. |
| `internal/schema` | Embedded JSON Schema documents and validation helpers. |
| `internal/system` | Host setup/check commands for Cloud Hypervisor assets and utilities. |
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
| `mise run //core:build` | `go build -o ./tmp/bastion ./cmd/bastion` and, on Linux, `go build -o ./tmp/bastiond ./cmd/bastiond` plus `go build -o ./tmp/bastion-guest-proxy ./cmd/bastion-guest-proxy` | Build the CLI and supported runtime binaries. |
| `mise run //core:test` | `go test ./...` | Run Go tests. |
| `mise run //core:test:e2e` | Build binaries and run `core/e2e/*.sh` scripts | Run core E2E tests against a reachable local API/daemon. |

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

`bastiond` accepts `--data-dir`, `--socket`, `--socket-uid`, `--socket-gid`, `--vm-uid`, `--vm-gid`, `--log-format`, and `--log-level`. It also uses Gin, but listens on a Unix socket instead of TCP. Root-only Cloud Hypervisor operations, TAP device setup, VMM launch, guest proxy installation, and VM cleanup belong in `internal/cloudhypervisor`; do not add runtime orchestration to `internal/system`, which is limited to `bastion system ...` host setup commands.

Host-initiated guest proxy traffic must use `internal/tunnel.DialGuestProxy`; Cloud Hypervisor requires sending `CONNECT <port>\n` and consuming the `OK <host-port>\n` acknowledgement before speaking HTTP.

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

Most client commands call the host API configured by `--api-url`, `BASTION_API_URL`, or a persisted override in `<data-dir>/client.json`. The default is `http://localhost:3148`. Server, system, version, and local client-configuration commands operate locally.

Supported top-level commands are intentionally limited to the current product scope:

- `bastion start`
- `bastion system check`, `bastion system add cloud-hypervisor`, and `bastion system remove cloud-hypervisor`
- `bastion templates ...`
- `bastion env ...`
- `bastion client set api-url URL`, `bastion client remove api-url`, and `bastion client config`
- `bastion mux`
- `bastion opencode (--id ID | --key KEY)`
- `bastion ssh (--id ID | --key KEY) [-- COMMAND...]`
- `bastion version`

Logs and diagnostics go to stderr. Host API logs are structured and include fields such as `request_id`, `method`, `route`, `status`, `duration`, `client_ip`, and `body_size`. The default format is JSON for machine parsing; the Air dev entrypoint uses `--log-format text` for readable local logs. API responses echo or generate `X-Request-ID` so request logs can be correlated with callers. JSON command output goes to stdout.
