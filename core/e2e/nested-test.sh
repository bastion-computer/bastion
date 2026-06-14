#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-nested-$(date +%Y%m%d%H%M%S)-$$"

TEMPLATE_KEYS=()
ENV_IDS=()
CREATED_ENV_ID=""

log() {
  printf '[nested-test] %s\n' "$*"
}

fail() {
  log "FAIL: $*" >&2
  exit 1
}

run_cli() {
  "$BASTION" --api-url "$API_URL" "$@"
}

cleanup() {
	local status=$?
	set +e

	if [ "$status" -ne 0 ] && [ "${BASTION_E2E_KEEP_FAILED:-}" = "1" ]; then
		log "preserving failed run resources: environments=${ENV_IDS[*]:-} templates=${TEMPLATE_KEYS[*]:-}"
		exit "$status"
	fi

	for env_id in "${ENV_IDS[@]}"; do
    if [ -n "$env_id" ]; then
      run_cli env remove --id "$env_id" >/dev/null 2>&1 || log "cleanup: environment $env_id was not removed"
    fi
  done

  for template_key in "${TEMPLATE_KEYS[@]}"; do
    if [ -n "$template_key" ]; then
      run_cli templates remove --key "$template_key" >/dev/null 2>&1 || log "cleanup: template $template_key was not removed"
    fi
  done

  exit "$status"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

precheck() {
  require_command jq
  require_command tar

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if [ ! -x "$CORE_DIR/tmp/bastiond" ]; then
    fail "daemon build not found at $CORE_DIR/tmp/bastiond; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! run_cli templates list >/dev/null 2>&1; then
    fail "Bastion API is not reachable on $API_URL; run mise dev:up"
  fi

  if ! "$BASTION" system --data-dir "$DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' add cloud-hypervisor"
  fi
}

nested_template_config() {
	jq -nc '{
		agents: {opencode: {}},
		actions: {
			init: [
				{run: "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get update\napt-get install -y --no-install-recommends ca-certificates curl jq tar sudo bash"},
				{run: "set -eu\nrm -f /swapfile\nfallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048\nchmod 600 /swapfile\nmkswap /swapfile\nswapon /swapfile"},
				{run: "set -eu\nmodprobe kvm_intel 2>/dev/null || modprobe kvm_amd 2>/dev/null || true\ntest -e /dev/kvm\ntest -r /dev/kvm\ntest -w /dev/kvm"}
			]
		}
	}'
}

create_template() {
  local key=$1
  local output

  log "creating nested template $key"
  output="$(run_cli templates create --key "$key" --config "$(nested_template_config)")"
  if [ "$(jq -r '.id // empty' <<<"$output")" = "" ]; then
    fail "template $key did not return an id"
  fi

  TEMPLATE_KEYS+=("$key")
}

create_outer_environment() {
  local key=$1
  local output

  log "creating outer environment from $key"
  output="$(run_cli env create --template-key "$key")"
  CREATED_ENV_ID="$(jq -r '.id // empty' <<<"$output")"
  if [ -z "$CREATED_ENV_ID" ]; then
    fail "outer environment did not return an id"
  fi

  ENV_IDS+=("$CREATED_ENV_ID")
}

copy_repo_to_outer() {
  local env_id=$1

  log "copying repository into $env_id"
  tar \
    --exclude='./.git' \
    --exclude='./.bastion' \
	    --exclude='./node_modules' \
	    -C "$REPO_DIR" \
	    -cf - . | run_cli ssh --id "$env_id" -- 'rm -rf /root/bastion && mkdir -p /root/bastion && tar -xf - -C /root/bastion'
}

run_inner_bastion() {
  local env_id=$1

  log "running Bastion inside $env_id"
  run_cli ssh --id "$env_id" -- env BASTION_OPENCODE_VERSION="${BASTION_OPENCODE_VERSION:-}" bash -s <<'INNER'
set -euo pipefail

cd /root/bastion

INNER_DATA_DIR=/root/.bastion-nested
INNER_SOCKET=/run/bastion-nested/bastiond.sock
INNER_API=http://127.0.0.1:4148
CHILD_KEY=nested-child

cleanup_inner() {
  set +e
  if [ -n "${CHILD_ENV_ID:-}" ]; then
    ./core/tmp/bastion --api-url "$INNER_API" env remove --id "$CHILD_ENV_ID" >/dev/null 2>&1 || true
  fi
  ./core/tmp/bastion --api-url "$INNER_API" templates remove --key "$CHILD_KEY" >/dev/null 2>&1 || true
  if [ -n "${API_PID:-}" ]; then
    kill "$API_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "${DAEMON_PID:-}" ]; then
    kill "$DAEMON_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup_inner EXIT

choose_inner_network_prefix() {
	local route second next

	route="$(ip -4 route get 1.1.1.1 2>/dev/null || true)"
	if [[ "$route" =~ src[[:space:]]+10\.([0-9]+)\. ]]; then
		second="${BASH_REMATCH[1]}"
		next=$((10#$second + 1))
		if [ "$next" -le 255 ]; then
			printf '10.%d\n' "$next"
			return
		fi
	fi

	printf '10.242\n'
}

INNER_NETWORK_PREFIX="$(choose_inner_network_prefix)"
printf 'inner-network-prefix:%s\n' "$INNER_NETWORK_PREFIX"

rm -rf "$INNER_DATA_DIR"
mkdir -p "$INNER_DATA_DIR" /run/bastion-nested

./core/tmp/bastion system --data-dir "$INNER_DATA_DIR" add cloud-hypervisor --with-utilities
./core/tmp/bastion system --data-dir "$INNER_DATA_DIR" check
sync
echo 3 > /proc/sys/vm/drop_caches || true

BASTION_DATA_DIR="$INNER_DATA_DIR" \
BASTIOND_SOCKET="$INNER_SOCKET" \
BASTION_VM_CPUS=1 \
BASTION_VM_MEMORY_BYTES=805306368 \
BASTION_VM_NETWORK_PREFIX="$INNER_NETWORK_PREFIX" \
BASTION_OPENCODE_VERSION="${BASTION_OPENCODE_VERSION:-}" \
./core/tmp/bastiond --data-dir "$INNER_DATA_DIR" --socket "$INNER_SOCKET" >/tmp/bastion-nested-daemon.log 2>&1 &
DAEMON_PID=$!
printf 'inner-daemon-started\n'

BASTION_DATA_DIR="$INNER_DATA_DIR" \
BASTIOND_SOCKET="$INNER_SOCKET" \
BASTION_OPENCODE_VERSION="${BASTION_OPENCODE_VERSION:-}" \
./core/tmp/bastion start --addr 127.0.0.1:4148 --data-dir "$INNER_DATA_DIR" --bastiond-socket "$INNER_SOCKET" >/tmp/bastion-nested-api.log 2>&1 &
API_PID=$!
printf 'inner-api-started\n'

for _ in $(seq 1 120); do
  if ./core/tmp/bastion --api-url "$INNER_API" templates list >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

./core/tmp/bastion --api-url "$INNER_API" templates list >/dev/null
printf 'inner-api-ready\n'
./core/tmp/bastion --api-url "$INNER_API" templates create --key "$CHILD_KEY" --config '{"agents":{"opencode":{}},"actions":{"init":[{"run":"set -eu\nprintf nested-ok > /root/nested-ok"}]}}' >/dev/null
printf 'inner-child-template-created\n'

CHILD_ENV_OUTPUT="$(./core/tmp/bastion --api-url "$INNER_API" env create --template-key "$CHILD_KEY")"
CHILD_ENV_ID="$(jq -r '.id // empty' <<<"$CHILD_ENV_OUTPUT")"
if [ -z "$CHILD_ENV_ID" ]; then
	printf 'child environment output missing id: %s\n' "$CHILD_ENV_OUTPUT" >&2
	exit 1
fi
printf 'inner-child-created:%s\n' "$CHILD_ENV_ID"

./core/tmp/bastion --api-url "$INNER_API" ssh --id "$CHILD_ENV_ID" -- grep -q nested-ok /root/nested-ok
printf 'nested-bastion-ok\n'
INNER
}

main() {
  local key="$RUN_ID-template"
  local env_id

  precheck
  trap cleanup EXIT

  log "starting nested e2e run $RUN_ID"
  create_template "$key"
  create_outer_environment "$key"
  env_id="$CREATED_ENV_ID"
  copy_repo_to_outer "$env_id"
  run_inner_bastion "$env_id"
  log "nested e2e run passed"
}

main "$@"
