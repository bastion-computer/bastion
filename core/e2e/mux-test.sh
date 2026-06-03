#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-mux-$(date +%Y%m%d%H%M%S)-$$"
MUX_SESSION="$RUN_ID-session"

CREATED_TEMPLATE_KEY=""
CREATED_ENV_ID=""

log() {
  printf '[mux-test] %s\n' "$*"
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

  tmux kill-session -t "$MUX_SESSION" >/dev/null 2>&1 || true

  if [ -n "$CREATED_ENV_ID" ]; then
    run_cli env remove --id "$CREATED_ENV_ID" >/dev/null 2>&1 || log "cleanup: environment $CREATED_ENV_ID was not removed"
  fi

  if [ -n "$CREATED_TEMPLATE_KEY" ]; then
    run_cli templates remove --key "$CREATED_TEMPLATE_KEY" >/dev/null 2>&1 || log "cleanup: template $CREATED_TEMPLATE_KEY was not removed"
  fi

  exit "$status"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

precheck() {
  require_command jq
  require_command script
  require_command tmux

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! run_cli templates list >/dev/null 2>&1; then
    fail "Bastion API is not reachable on $API_URL; run mise dev:up"
  fi

  if ! "$BASTION" system --data-dir "$DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' add cloud-hypervisor"
  fi
}

json_get() {
  jq -r "$1"
}

mux_template_config() {
  jq -nc '{actions:{init:[]}}'
}

create_template() {
  CREATED_TEMPLATE_KEY="$RUN_ID-template"

  log "creating template $CREATED_TEMPLATE_KEY"
  run_cli templates create --key "$CREATED_TEMPLATE_KEY" --config "$(mux_template_config)" >/dev/null
}

create_environment() {
  local output

  log "creating environment from $CREATED_TEMPLATE_KEY"
  output="$(run_cli env create --template-key "$CREATED_TEMPLATE_KEY")"
  CREATED_ENV_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$CREATED_ENV_ID" ] || [ "$CREATED_ENV_ID" = "null" ]; then
    fail "environment did not return an id"
  fi
}

drive_mux_create_session() {
  local output

  log "opening mux and creating an SSH session"
  output="$(
    {
      sleep 1
      printf 'n'
      sleep 3
      for _ in {1..200}; do
        printf '\033[B'
      done
      printf '\r'
      sleep 6
      printf 'd'
    } | script -qfec "$BASTION --api-url $API_URL mux --session $MUX_SESSION" /dev/null
  )"

  if [[ "$output" != *"New SSH Session"* ]]; then
    fail "mux did not show the new session picker: $output"
  fi
}

reattach_and_detach_mux() {
  log "reattaching mux and detaching again"
  printf 'd' | script -qfec "$BASTION --api-url $API_URL mux --session $MUX_SESSION" /dev/null >/dev/null
}

mux_window_target() {
  local target
  local env_id

  while IFS=$'\t' read -r target env_id; do
    if [ "$env_id" = "$CREATED_ENV_ID" ]; then
      printf '%s\n' "$target"
      return 0
    fi
  done < <(tmux list-windows -t "$MUX_SESSION" -F '#{window_id}	#{@bastion-env-id}')

  return 1
}

assert_mux_session_exists() {
  local target

  target="$(mux_window_target)" || fail "mux tmux session does not contain $CREATED_ENV_ID"
  if [ -z "$target" ]; then
    fail "mux tmux target is empty"
  fi

  printf '%s\n' "$target"
}

assert_mux_ssh_stays_alive() {
  local target=$1
  local marker="/tmp/$RUN_ID"

  tmux send-keys -t "$target" "printf mux-ok > $marker" Enter
  sleep 2

  run_cli ssh --id "$CREATED_ENV_ID" -- grep -q mux-ok "$marker"
  log "mux SSH session stayed alive after detach"
}

main() {
  local target

  precheck
  trap cleanup EXIT

  log "starting mux e2e run $RUN_ID"
  create_template
  create_environment
  drive_mux_create_session
  target="$(assert_mux_session_exists)"
  assert_mux_ssh_stays_alive "$target"
  reattach_and_detach_mux
  assert_mux_session_exists >/dev/null
  log "mux e2e run passed"
}

main "$@"
