ALWAYS verify the code has been linted, format-checked, built, typechecked where supported, and tested from the repository root via the following commands:

- `mise run lint`
- `mise run format:check`
- `mise run build`
- `mise run typecheck`
- `mise run test`

Use `mise run format:write` to rewrite formatting locally. CI uses `mise run format:check`.

If making significant changes to `core`, run the E2E tests to completion. Do not assume a dev server is already running; start it yourself unless the user explicitly says one is running and should be reused.

Core E2E workflow:

- Build current binaries first: `cd core && GOMODCACHE=/tmp/opencode/gomodcache go build -o ./tmp/bastion ./cmd/bastion && GOMODCACHE=/tmp/opencode/gomodcache go build -o ./tmp/bastiond ./cmd/bastiond`
- Ensure Cloud Hypervisor host dependencies are installed: `./core/tmp/bastion system --data-dir ./.bastion check`
- If system check fails because assets/utilities are missing, run: `./core/tmp/bastion system --data-dir ./.bastion add cloud-hypervisor --with-utilities`
- Start `bastiond` and the API with captured logs so failures are diagnosable: `cd core && setsid -f sudo -n ./tmp/bastiond --data-dir ../.bastion --log-format text --log-level debug > /tmp/opencode/bastion-logs/bastiond.log 2>&1` and `cd core && setsid -f ./tmp/bastion start --addr localhost:3148 --data-dir ../.bastion --log-format text --log-level debug > /tmp/opencode/bastion-logs/api.log 2>&1`
- Verify the API is reachable before running E2E: `cd core && ./tmp/bastion --api-url http://localhost:3148 templates list`
- Run the standard E2E tests: `cd core && bash ./e2e/env-test.sh` and `cd core && bash ./e2e/ssh-test.sh`
- Run nested virtualization E2E when touching the VM runtime, networking, system setup, or E2E scripts: `cd core && bash ./e2e/nested-test.sh`

E2E notes:

- The scripts default to `BASTION_API_URL=http://localhost:3148`; override `BASTION_API_URL` only when intentionally targeting a different API.
- `mise run //core:test:e2e` builds the CLI/daemon and runs all E2E scripts, but it still expects a reachable local API/daemon. Prefer the explicit workflow above when debugging because it keeps API and daemon logs in `/tmp/opencode/bastion-logs/`.
- Nested E2E requires `/dev/kvm`, Cloud Hypervisor assets, working TAP/iptables setup, and enough disk/network time to download inner assets. It chooses the inner daemon network prefix from the current default route so nested child VMs do not collide with the parent VM route.
- Before starting `bastiond` for E2E, proactively choose a non-conflicting VM network prefix: run `ip -4 route get 1.1.1.1`. If the route reports a source like `src 10.N.x`, start `bastiond` with `BASTION_VM_NETWORK_PREFIX=10.$((N + 1))` when `N + 1 <= 255`; otherwise use a known-free prefix such as `10.241`. The daemon reads this value at startup, so stop any existing daemon/API and start them again with the selected prefix before running tests.
- If an E2E fails, inspect `/tmp/opencode/bastion-logs/api.log`, `/tmp/opencode/bastion-logs/bastiond.log`, and root-owned VM logs under `.bastion/environments/<env-id>/` using `sudo -n` before changing code.
- Clean up failed E2E environments/templates through the CLI where possible. Do not manually delete `.bastion/environments/*` unless the API/daemon cleanup path is unavailable.
