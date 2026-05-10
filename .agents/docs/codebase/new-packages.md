Assume the following requirements when creating a new package in this monorepo:

- New packages MUST be created as top-level directories, alongside packages such as `core`, `dev-db`, `docs`, and `spec`.
- All active packages MUST define package-level mise tasks for the operations they support.
  - `lint` runs language-specific linting, such as `eslint ./src` for TypeScript packages or `golangci-lint run ./...` for Go packages.
  - `format:check` checks formatting without writing files.
  - `format:write` rewrites formatting locally.
  - `build` compiles buildable packages, such as `go build` for Go applications or `astro build` for docs sites.
  - `typecheck` runs language-specific static checks for packages that support it, such as `tsc --noEmit` or `astro check`.
    - **Exception:** Astro-based packages (e.g. `@bastion/docs`) use `astro check` instead of `tsc --noEmit`, with `@astrojs/check` as a devDependency.
  - TypeScript packages should keep package.json scripts as thin package-local commands used by mise tasks.
- All TypeScript packages MUST have at least a `src` directory where the source code is stored.
  - **Exception:** Raw artifact packages that only export non-code assets, such as JSON files, do not need a `src` directory, `lint`, or `typecheck` task.
  - **Exception:** Go packages follow the standard Go layout with `cmd/`, `internal/`, and colocated `_test.go` files.
- When adding a new active package, add its config root to the root `mise.toml` `[monorepo].config_roots` list and include it in the appropriate root aggregate tasks.
