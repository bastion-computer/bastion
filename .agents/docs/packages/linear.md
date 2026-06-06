This package contains the Go implementation of Bastion's Linear integration sidecar.

## Overview

The package lives in `integrations/linear/` and builds the `bastion-linear` binary. It receives Linear Agent Session webhooks, stores durable work in SQLite, assigns matching running Bastion environments to sessions, launches OpenCode inside environments over the Bastion SSH API, and emits Linear Agent Activities through Linear's GraphQL API.

The service is intentionally isolated from future integrations. Other integrations should live in sibling modules such as `integrations/slack` or `integrations/github`.

## Layout

| Path | Purpose |
| ---- | ------- |
| `cmd/bastion-linear` | Linear sidecar entrypoint. |
| `cmd/mock-linear` | Local mock Linear API used by E2E tests. |
| `internal/api` | HTTP server for health checks and Linear webhooks. |
| `internal/bastion` | Small Bastion API and SSH tunnel client. |
| `internal/config` | Environment and flag configuration. |
| `internal/database` | SQLite open/migration helpers. |
| `internal/linear` | Linear webhook and GraphQL helpers. |
| `internal/mocklinear` | Mock Linear GraphQL API for tests. |
| `internal/opencode` | OpenCode server adapter over Bastion SSH commands. |
| `internal/service` | Durable jobs, environment assignment, and worker orchestration. |
| `e2e` | Linear end-to-end test script and helpers. |

## Tasks

Run package tasks from the repo root with mise:

| Task | Command | Purpose |
| ---- | ------- | ------- |
| `mise run //integrations/linear:lint` | `golangci-lint run ./...` | Run Go linters. |
| `mise run //integrations/linear:format:check` | `gofmt -l .` check | Check formatting. |
| `mise run //integrations/linear:format:write` | `gofmt -w .` | Rewrite formatting. |
| `mise run //integrations/linear:build` | `go build -o ./tmp/bastion-linear ./cmd/bastion-linear` | Build the sidecar. |
| `mise run //integrations/linear:test` | `go test ./...` | Run Go tests. |

## E2E

The E2E script lives with the package:

```sh
cd integrations/linear && bash ./e2e/linear-test.sh
```

It expects a built core CLI at `core/tmp/bastion` and a reachable local Bastion API/daemon. It creates a temporary environment with a fake OpenCode server, starts the mock Linear API, starts `bastion-linear`, sends a signed Agent Session webhook, and waits for a Linear response activity.

## Release

GitHub releases upload the Linear integration in a separate archive from core:

```text
bastion-linear_<tag>_linux_x86_64.tar.gz
bastion-linear_<tag>_linux_x86_64.tar.gz.sha256
```

The shared installer installs it when invoked with `--integration linear`.
