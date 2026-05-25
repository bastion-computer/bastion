#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-ssh-$(date +%Y%m%d%H%M%S)-$$"

TEMPLATE_KEYS=()
ENV_IDS=()
CREATED_TEMPLATE_ID=""
CREATED_ENV_ID=""
CREATE_OUTPUT=""

log() {
  printf '[ssh-test] %s\n' "$*"
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

  for env_id in "${ENV_IDS[@]}"; do
    if [ -n "$env_id" ]; then
      run_cli env remove "$env_id" >/dev/null 2>&1 || log "cleanup: environment $env_id was not removed"
    fi
  done

  for template_key in "${TEMPLATE_KEYS[@]}"; do
    if [ -n "$template_key" ]; then
      run_cli templates remove --key "$template_key" >/dev/null 2>&1 || log "cleanup: template $template_key was not removed"
    fi
  done

  exit "$status"
}

remove_environment() {
  local env_id=$1
  local kept_env_ids=()
  local existing_env_id

  if [ -n "$env_id" ]; then
    run_cli env remove "$env_id" >/dev/null 2>&1 || log "cleanup: environment $env_id was not removed"
  fi

  for existing_env_id in "${ENV_IDS[@]}"; do
    if [ "$existing_env_id" != "$env_id" ]; then
      kept_env_ids+=("$existing_env_id")
    fi
  done

  ENV_IDS=("${kept_env_ids[@]}")
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

precheck() {
  require_command jq
  require_command script

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

ssh_template_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-ssh-e2e\nprintf \"%s\\n\" \"\($run_id)\" > /opt/bastion-ssh-e2e/init-run"}
      ]
    }
  }'
}

assert_no_ssh_fields() {
  local label=$1
  local json=$2

  if jq -e 'has("sshHost") or has("sshPort") or has("sshUser") or has("sshKeyPath")' <<<"$json" >/dev/null; then
    fail "$label exposed SSH fields: $json"
  fi
}

create_template() {
  local key=$1
  local output

  log "creating template $key"
  output="$(run_cli templates create --key "$key" --config "$(ssh_template_config)")"
  CREATED_TEMPLATE_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$CREATED_TEMPLATE_ID" ] || [ "$CREATED_TEMPLATE_ID" = "null" ]; then
    fail "template $key did not return an id"
  fi

  TEMPLATE_KEYS+=("$key")
}

create_environment() {
  local key=$1
  local env_key=$2
  local args=(--template-key "$key")

  if [ -n "$env_key" ]; then
    args+=(--key "$env_key")
  fi

  log "creating environment from $key"
  CREATE_OUTPUT="$(run_cli env create "${args[@]}")"
  CREATED_ENV_ID="$(json_get '.id' <<<"$CREATE_OUTPUT")"

  if [ -z "$CREATED_ENV_ID" ] || [ "$CREATED_ENV_ID" = "null" ]; then
    fail "environment from $key did not return an id"
  fi

  ENV_IDS+=("$CREATED_ENV_ID")
  assert_no_ssh_fields "env create" "$CREATE_OUTPUT"
}

assert_environment_running_without_ssh_fields() {
  local env_id=$1
  local output
  local status

  output="$(run_cli env get "$env_id")"
  assert_no_ssh_fields "env get" "$output"

  status="$(json_get '.status' <<<"$output")"
  if [ "$status" != "running" ]; then
    fail "environment $env_id status is $status, want running"
  fi
}

ssh_env() {
  local env_id=$1
  shift

  run_cli ssh --id "$env_id" -- "$@"
}

ssh_env_by_key() {
  local env_key=$1
  shift

  run_cli ssh --key "$env_key" -- "$@"
}

run_command_case() {
  local env_id=$1
  local output

  output="$(ssh_env "$env_id" printf api-ssh-ok)"
  if [ "$output" != "api-ssh-ok" ]; then
    fail "ssh command output was $output, want api-ssh-ok"
  fi

  ssh_env "$env_id" "printf %s '$RUN_ID' > /opt/bastion-ssh-e2e/api-ssh"
  ssh_env "$env_id" grep -q "$RUN_ID" /opt/bastion-ssh-e2e/api-ssh
  ssh_env "$env_id" grep -q "$RUN_ID" /opt/bastion-ssh-e2e/init-run

  log "command SSH id case passed for $env_id"
}

run_key_reference_case() {
  local env_key=$1
  local output

  output="$(ssh_env_by_key "$env_key" printf api-ssh-key-ok)"
  if [ "$output" != "api-ssh-key-ok" ]; then
    fail "ssh key command output was $output, want api-ssh-key-ok"
  fi

  log "command SSH key case passed for $env_key"
}

run_exit_status_case() {
  local env_id=$1
  local output

  if output="$(ssh_env "$env_id" false 2>&1)"; then
    fail "ssh false unexpectedly succeeded"
  fi

  if [[ "$output" != *"remote command exited with status"* ]]; then
    fail "ssh false returned unexpected error: $output"
  fi

  log "exit status SSH case passed for $env_id"
}

run_interactive_case() {
  local env_id=$1
  local output

  output="$(printf 'test -t 0 && echo bastion-pty-ok\nstty size\nexit\n' | script -qfec "$BASTION --api-url $API_URL ssh --id $env_id" /dev/null)"
  if [[ "$output" != *"bastion-pty-ok"* ]]; then
    fail "interactive SSH session did not report a TTY: $output"
  fi

  log "interactive SSH case passed for $env_id"
}

main() {
  local key="$RUN_ID-template"
  local env_key="$RUN_ID-env"
  local key_env_id
  local id_env_id

  precheck
  trap cleanup EXIT

  log "starting SSH e2e run $RUN_ID"
  create_template "$key"

  create_environment "$key" "$env_key"
  key_env_id="$CREATED_ENV_ID"
  assert_environment_running_without_ssh_fields "$key_env_id"
  run_key_reference_case "$env_key"
  remove_environment "$key_env_id"

  create_environment "$key" ""
  id_env_id="$CREATED_ENV_ID"
  assert_environment_running_without_ssh_fields "$id_env_id"
  run_command_case "$id_env_id"
  run_exit_status_case "$id_env_id"
  run_interactive_case "$id_env_id"
  log "SSH e2e run passed"
}

main "$@"
