# Testing

## Required TDD Workflow

Use red/green TDD for behavior changes:

- Red: before changing implementation, add or run the smallest automated test, regression test, E2E script, or reproducible user-level command that fails for the current behavior.
- Green: implement the smallest correct change, then rerun the exact red check and confirm it now passes.
- Refactor: after the targeted check is green, clean up only as needed and rerun the relevant package/root verification.

Documentation-only or agent-instruction-only changes do not need artificial product tests, but they still need a concrete verification loop such as checking changed links, confirming referenced tasks exist, or building affected docs when the public docs site changes.

## Root Verification

ALWAYS verify the code has been linted, format-checked, built, typechecked where supported, and tested from the repository root via the following commands:

- `mise run lint`
- `mise run format:check`
- `mise run build`
- `mise run typecheck`
- `mise run test`

Use `mise run format:write` to rewrite formatting locally. CI uses `mise run format:check`.

If an environment limitation prevents running one of these commands, report the exact blocker and run the closest narrower check that is safe.

## End-to-End Verification

Every task must identify and run the closest user-facing verification path before finishing. Prefer the narrowest E2E path that proves the changed behavior, then run broader checks when the change crosses package/runtime boundaries.

Use these common paths:

- Core CLI/API/storage behavior: targeted Go tests, then the relevant `core/e2e/*.sh` script against a local API/daemon.
- Core client configuration behavior: `cd core && bash ./e2e/client-test.sh`.
- Core template/environment lifecycle behavior: `cd core && bash ./e2e/env-test.sh`.
- Core SSH behavior or SSH tunnel protocol changes: `cd core && bash ./e2e/ssh-test.sh`.
- Installer, systemd service, or install documentation behavior: `cd core && bash ./e2e/install-test.sh`.
- VM runtime, networking, system setup, Cloud Hypervisor, or E2E script behavior: `cd core && bash ./e2e/nested-test.sh` when the host supports nested virtualization.
- Public docs changes under `docs/`: run `mise run //docs:build` and, for navigation/content behavior, inspect the page through `mise run //docs:dev` or `mise run //docs:preview` when feasible.
- Agent docs changes under `AGENTS.md` or `.agents/docs/`: reread the changed docs, verify linked files exist, and confirm referenced commands still exist with `mise tasks --all` or package manifests.

If making significant changes to `core`, run the full core E2E set to completion. Do not assume a dev server is already running; start it yourself unless the user explicitly says one is running and should be reused.

For new user-facing core features, add or extend an E2E script when practical. Manual CLI/API verification is useful while debugging, but final regression coverage should live in `core/e2e/*.sh` unless environment limits make that infeasible; if so, report why.

Core E2E workflow:

- Build current binaries first: `cd core && mkdir -p tmp /tmp/opencode/gomodcache && GOMODCACHE=/tmp/opencode/gomodcache go build -o ./tmp/bastion ./cmd/bastion && GOMODCACHE=/tmp/opencode/gomodcache go build -o ./tmp/bastiond ./cmd/bastiond && GOMODCACHE=/tmp/opencode/gomodcache go build -o ./tmp/bastion-guest-proxy ./cmd/bastion-guest-proxy`
- Ensure Cloud Hypervisor host dependencies are installed: `./core/tmp/bastion system --data-dir ./.bastion check`
- If system check fails because assets/utilities are missing, run: `./core/tmp/bastion system --data-dir ./.bastion add cloud-hypervisor --with-utilities`
- Create the log directory before starting services: `mkdir -p /tmp/opencode/bastion-logs`
- Start `bastiond` and the API with captured logs so failures are diagnosable: `cd core && setsid -f sudo -n ./tmp/bastiond --data-dir ../.bastion --log-format text --log-level debug > /tmp/opencode/bastion-logs/bastiond.log 2>&1` and `cd core && setsid -f ./tmp/bastion start --addr localhost:3148 --data-dir ../.bastion --log-format text --log-level debug > /tmp/opencode/bastion-logs/api.log 2>&1`
- Verify the API is reachable before running E2E: `cd core && ./tmp/bastion --api-url http://localhost:3148 templates list`
- Run the standard E2E tests: `cd core && bash ./e2e/client-test.sh`, `cd core && bash ./e2e/env-test.sh`, `cd core && bash ./e2e/ssh-test.sh`, and `cd core && mise exec -- bash ./e2e/install-test.sh`.
- Run nested virtualization E2E when touching the VM runtime, networking, system setup, or E2E scripts: `cd core && bash ./e2e/nested-test.sh`.

E2E notes:

- The scripts default to `BASTION_API_URL=http://localhost:3148`; override `BASTION_API_URL` only when intentionally targeting a different API.
- The install E2E uses the docs dev server and repo-managed Bun, creates a local `dev` release archive from `core/tmp`, and points `install.sh` at that local archive so unreleased installer changes are tested before a GitHub release exists.
- `mise run //core:test:e2e` builds the CLI/daemon and runs all E2E scripts, but it still expects a reachable local API/daemon. Prefer the explicit workflow above when debugging because it keeps API and daemon logs in `/tmp/opencode/bastion-logs/`.
- Nested E2E requires `/dev/kvm`, Cloud Hypervisor assets, working TAP/iptables setup, and enough disk/network time to download inner assets. It chooses the inner daemon network prefix from the current default route so nested child VMs do not collide with the parent VM route.
- Before starting `bastiond` for E2E, proactively choose a non-conflicting VM network prefix: run `ip -4 route get 1.1.1.1`. If the route reports a source like `src 10.N.x` and `N + 1 <= 255`, start `bastiond` with `BASTION_VM_NETWORK_PREFIX=10.$((N + 1))`; otherwise leave `BASTION_VM_NETWORK_PREFIX` unset so the default value is used. The daemon reads this value at startup, so stop any existing daemon/API and start them again after choosing whether to set the prefix before running tests.
- If the OpenCode installer fails with `Failed to fetch version information` because the unauthenticated GitHub releases API is rate-limited, restart the E2E `bastiond`/API with `BASTION_OPENCODE_VERSION=<known-good-version>` and pass the same environment variable to E2E scripts that create nested or installed Bastion services.
- If an E2E fails, inspect `/tmp/opencode/bastion-logs/api.log`, `/tmp/opencode/bastion-logs/bastiond.log`, and root-owned VM logs under `.bastion/environments/<env-id>/` using `sudo -n` before changing code.
- Clean up failed E2E environments/templates through the CLI where possible. Do not manually delete `.bastion/environments/*` unless the API/daemon cleanup path is unavailable.
