This package contains the Go implementation of Bastion's host API service and CLI.

## Overview

The core package lives in `core/`. On Linux it builds the `bastion` and `bastion-guest-proxy` binaries; on Darwin it builds the client-only `bastion` binary. The `bastion` binary is responsible for process entrypoints and client commands:

- `bastion start api` runs the local host API service on `localhost:3148` by default.
- `sudo bastion start daemon` runs privileged Cloud Hypervisor runtime operations behind a Unix socket.
- `bastion start cluster` runs the cluster control plane on `localhost:3150` by default.
- CLI commands operate locally when managing host/client configuration, or call the host API service and print JSON responses for product resources.

## Layout

| Path | Purpose |
| ---- | ------- |
| `actions` | Embedded built-in preset actions seeded into `<data-dir>/actions`. |
| `cmd/bastion` | Minimal CLI/API/daemon binary entrypoint. |
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
| `internal/services/secret` | Secret request/response types and persistence service. |
| `internal/services/utilization` | Host capacity and live environment resource usage service. |
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
| `mise run //core:dev:daemon` | `air -c .air.daemon.toml` | Start the daemon with live reload. |
| `mise run //core:dev:cluster` | `air -c .air.cluster.toml` | Start the cluster API with live reload. |
| `mise run //core:lint` | `golangci-lint run ./...` | Run Go linters. |
| `mise run //core:format:check` | `gofmt -l .` check | Check Go formatting without writing files. |
| `mise run //core:format:write` | `gofmt -w .` | Rewrite Go formatting. |
| `mise run //core:build` | `go build -o ./tmp/bastion ./cmd/bastion` and, on Linux, `go build -o ./tmp/bastion-guest-proxy ./cmd/bastion-guest-proxy` | Build the CLI and supported runtime binaries. |
| `mise run //core:test` | `go test ./...` | Run Go tests. |
| `mise run //core:test:e2e` | Build binaries and run `core/e2e/*.sh` scripts | Run core E2E tests against a reachable local API/daemon. |

Root aggregate tasks include this package, so `mise run dev:up`, `mise run lint`, `mise run format:check`, `mise run build`, and `mise run test` can all be run from the repository root. The root `dev:up` task starts Docker Compose Postgres/MinIO infrastructure and opens a tmux session with panes for the API, daemon, cluster API, Drizzle, docs, and a shell.

Local builds report `dev` from `internal/config.Version`. Release builds can inject a version by setting `BASTION_VERSION` before running `mise run //core:build`.

## API Service

`bastion start api` accepts:

- `--addr`: listen address. Defaults to `localhost:3148` and can be set with `BASTION_ADDR`.
- `--data-dir`: persistent data directory. Defaults to `~/.bastion` and can be set with `BASTION_DATA_DIR`.
- `--bastiond-socket`: Unix socket path for the privileged daemon. Defaults to `/run/bastion/bastiond.sock` and can be set with `BASTIOND_SOCKET`.
- `--log-format`: log handler format. Defaults to `json` and can be set with `BASTION_LOG_FORMAT`; supported values are `json` and `text`.
- `--log-level`: minimum log level. Defaults to `info` and can be set with `BASTION_LOG_LEVEL`; supported values are `debug`, `info`, `warn`, and `error`.

The service uses Gin and wraps it in `http.Server` so timeouts and graceful shutdown remain explicit. `internal/api/server.go` owns route registration. Domain-specific handler packages under `internal/handlers` expose `NewHandler(service)` constructors and handler methods used by those routes.

`bastion start daemon` accepts `--socket`, `--socket-uid`, `--socket-gid`, `--vm-uid`, `--vm-gid`, `--log-format`, and `--log-level`, plus the root `--data-dir` flag. It also uses Gin, but listens on a Unix socket instead of TCP. The `--socket-uid` and `--socket-gid` owner is also used for per-VM proxy sockets such as `vsock.socket`, so keep it aligned with the API service user. Root-only Cloud Hypervisor operations, TAP device setup, VMM launch, guest proxy installation, and VM cleanup belong in `internal/cloudhypervisor`; do not add runtime orchestration to `internal/system`, which is limited to `bastion system ...` host setup commands.

Host-initiated guest proxy traffic must use `internal/tunnel.DialGuestProxy`; Cloud Hypervisor requires sending `CONNECT <port>\n` and consuming the `OK <host-port>\n` acknowledgement before speaking HTTP.

## Cluster API

`bastion start cluster` accepts `--addr`, `--database-url`, `--archive-bucket`, `--archive-endpoint`, `--archive-region`, `--archive-access-key-id`, `--archive-secret-access-key`, `--archive-force-path-style`, `--log-format`, and `--log-level`. It stores cluster state in Postgres and source template archives in an S3-compatible bucket. Development defaults in `.air.cluster.toml` target the root `compose.yml` services on `localhost:3152` for Postgres and `localhost:3153` for MinIO.

Cluster routes live in `internal/clusterapi`. Cluster management APIs use `/v1/cluster/...`; host-compatible resource APIs require a namespace path such as `/v1/namespaces/:namespace/templates`. The CLI global `--namespace` flag and `BASTION_NAMESPACE` environment variable select this namespace when calling a cluster API.

## Database

Core stores persistent data in SQLite at `<data-dir>/sqlite.db`.

- The default data directory is `~/.bastion`.
- The development data directory is `.bastion` via the Air configuration.
- `bastion start api` owns creating the top-level data directory; `bastion start daemon` waits for it at startup and must not create it first.
- Tests use `:memory:` and run the same migrations as local development.

SQL migrations live in `core/internal/migrations` and are embedded into the Go binary. `internal/database.Open()` runs pending migrations automatically before the API starts serving. If migrations fail, startup fails rather than serving against a partially migrated schema.

The core migrations are the schema source of truth. Development tools such as Drizzle Studio may inspect `.bastion/sqlite.db`, but they do not own or generate core migrations.

`internal/database` intentionally stays small: it opens SQLite, runs migrations, exposes context-aware query and transaction methods, and detects SQLite constraint errors. Service packages under `internal/services` own their own SQL and CRUD behavior.

Cluster control-plane state uses Postgres through `internal/clusterapi.PostgresStore`; its migrations live under `core/internal/clusterapi/migrations`. Source template archives use `internal/clusterapi.ArchiveStore`, with S3-compatible storage in production/dev cluster startup and memory storage in tests.

## CLI

Most client commands call the host API configured by `--api-url`, `BASTION_API_URL`, or a persisted override in `<data-dir>/client.json`. The default is `http://localhost:3148`. Server, system, version, and local client-configuration commands operate locally.

Supported top-level commands are intentionally limited to the current product scope:

- `bastion start api`, `bastion start daemon`, and `bastion start cluster`
- `bastion system check`, `bastion system add cloud-hypervisor`, and `bastion system remove cloud-hypervisor`
- `bastion utilization`
- `bastion secrets ...`
- `bastion templates ...`
- `bastion env ...`
- `bastion client set api-url URL`, `bastion client remove api-url`, and `bastion client config`
- `bastion client set namespace NAMESPACE` and `bastion client remove namespace`
- `bastion cluster nodes ...`, `bastion cluster namespaces ...`, and `bastion cluster utilization`
- `bastion mux`
- `bastion opencode (--id ID | --key KEY)`
- `bastion proxy (--env-id ID | --env-key KEY) --name NAME [--port PORT]`
- `bastion ssh (--id ID | --key KEY) [-- COMMAND...]`
- `bastion version`

Logs and diagnostics go to stderr. Host API logs are structured and include fields such as `request_id`, `method`, `route`, `status`, `duration`, `client_ip`, and `body_size`. The default format is JSON for machine parsing; the Air dev entrypoint uses `--log-format text` for readable local logs. API responses echo or generate `X-Request-ID` so request logs can be correlated with callers. JSON command output goes to stdout.
