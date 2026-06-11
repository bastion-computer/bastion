This repository is a mixed Go and TypeScript monorepo. Use `mise` as the primary entrypoint for tool versions and task execution; do not assume a package is a generic Bun application.

## Toolchain

The root `mise.toml` is the source of truth for local tools:

| Tool | Current use |
| ---- | ----------- |
| `bun` | JavaScript package management and package scripts. |
| `go` | The `core/` host API service, CLI, daemon, tests, and builds. |
| `node` | JavaScript ecosystem compatibility for Astro/SST tooling. |
| `air` | Live reload for the Go API and daemon in development. |
| `golangci-lint` | Go linting. |
| `tmux` | Root development session orchestration. |
| `zig` | C compiler via `CC = "zig cc"` for CGO-enabled Go builds. |

Run `mise install` from the repository root to install pinned tools and JavaScript dependencies. Root tasks should usually be run through `mise run ...` rather than invoking language-specific commands directly.

## Packages

Active package roots are configured in root `mise.toml` under `[monorepo].config_roots`:

- `core/`: Go module `github.com/bastion-computer/bastion/core`, included in the root `go.work` workspace.
- `docs/`: Astro Starlight documentation site using Bun package scripts.
- `.dev/drizzle/`: development-only Drizzle Studio package for inspecting the local SQLite database.

Package-specific details live in:

- `.agents/docs/packages/core.md`
- `.agents/docs/packages/docs.md`
- `.agents/docs/dev/drizzle.md`

## Root Tasks

Prefer these repository-root commands for normal verification and development:

| Task | Purpose |
| ---- | ------- |
| `mise run lint` | Run Go and docs linters. |
| `mise run format:check` | Check formatting for active packages. |
| `mise run format:write` | Rewrite formatting for active packages. |
| `mise run build` | Build `core` binaries and the docs site. |
| `mise run typecheck` | Run docs typechecking. |
| `mise run test` | Run Go tests. |
| `mise run dev:up` | Start the tmux development session. |
| `mise run dev:down` | Stop the tmux development session. |
| `mise run dev:reset` | Remove the local `.bastion` development data directory. |
| `mise run dev:bastion ...` | Run the Bastion CLI against the local API. |

Use package-qualified tasks such as `mise run //core:test`, `mise run //docs:dev`, or `mise run //.dev/drizzle:dev` when working on one package.

## Go Runtime

The `core/` package owns the product runtime. It builds two binaries:

- `bastion`: CLI and host API service entrypoint.
- `bastiond`: privileged Cloud Hypervisor daemon entrypoint.

Core uses standard Go tooling and libraries, including:

- Gin for HTTP routing in both the host API and daemon.
- Cobra for CLI commands.
- SQLite through `github.com/mattn/go-sqlite3` with CGO enabled.
- Embedded SQL migrations under `core/internal/migrations`.
- Cloud Hypervisor orchestration under `core/internal/cloudhypervisor`.

Do not apply Bun server/API conventions to `core`. For example, do not replace Gin with `Bun.serve()`, Go SQLite with `bun:sqlite`, or Go build/test flows with Bun commands.

## TypeScript Runtime

JavaScript and TypeScript code in this repo uses Bun for package installation and script execution, but the current packages are framework/tooling packages rather than standalone Bun servers.

- The root package manages workspace dependencies and SST configuration.
- `docs/` runs Astro scripts through Bun, such as `bun run dev`, `bun run build`, and `bun run typecheck`.
- `docs/` is an Astro Starlight site deployed through SST's Cloudflare Astro construct.
- `.dev/drizzle/` runs Drizzle Studio through Bun for development database inspection.

Use package scripts through mise tasks where available. If a package does not define a mise task for an operation, use `bun run <script>` inside that package rather than npm, yarn, pnpm, or npx equivalents.

## API Guidance

Use the APIs and storage layers already established by each package:

- `core/` HTTP APIs use Gin handlers and services in Go.
- `core/` persistent state is SQLite at `<data-dir>/sqlite.db`, managed by Go migrations.
- `.dev/drizzle/` may depend on `better-sqlite3` because Drizzle Kit uses it for local inspection; it does not own the production schema.
- `docs/` APIs/pages use Astro conventions.

Avoid blanket Bun API substitutions in this repo. Bun built-ins such as `Bun.serve`, `Bun.file`, `Bun.sql`, `Bun.redis`, and `bun:sqlite` are not the default choice unless a specific package has been designed around them.
