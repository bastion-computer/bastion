#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-backup-restore-$(date +%Y%m%d%H%M%S)-$$"
ARCHIVE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-backup-restore-e2e.XXXXXX")"

TEMPLATE_IDS=()
ENV_IDS=()

log() {
  printf '[backup-restore-test] %s\n' "$*" >&2
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
    log "preserving failed run resources: environments=${ENV_IDS[*]:-} templates=${TEMPLATE_IDS[*]:-} archives=$ARCHIVE_DIR"
    exit "$status"
  fi

  cleanup_environments
  cleanup_templates
  rm -rf "$ARCHIVE_DIR"

  exit "$status"
}

cleanup_environments() {
  local env_id
  for env_id in "${ENV_IDS[@]}"; do
    if [ -n "$env_id" ]; then
      run_cli env remove --id "$env_id" >/dev/null 2>&1 || log "cleanup: environment $env_id was not removed"
    fi
  done
}

cleanup_templates() {
  local template_id
  for template_id in "${TEMPLATE_IDS[@]}"; do
    if [ -n "$template_id" ]; then
      run_cli templates remove --id "$template_id" >/dev/null 2>&1 || log "cleanup: template $template_id was not removed"
    fi
  done
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

precheck() {
  require_command jq

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! run_cli templates list >/dev/null 2>&1; then
    fail "Bastion API is not reachable on $API_URL; run mise dev:up"
  fi

  if ! "$BASTION" system --data-dir "$DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' init --with-utilities"
  fi
}

json_get() {
  jq -r "$1"
}

assert_json_key() {
  local label=$1
  local json=$2
  local expected=$3

  if ! jq -e --arg expected "$expected" '.key == $expected' <<<"$json" >/dev/null; then
    fail "$label key is $(jq -c '.key // null' <<<"$json"), want $expected"
  fi
}

assert_json_no_key() {
  local label=$1
  local json=$2

  if jq -e 'has("key")' <<<"$json" >/dev/null; then
    fail "$label unexpectedly has key: $(jq -c '.key' <<<"$json")"
  fi
}

backup_restore_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-backup-restore\nprintf \"%s\\n\" \"\($run_id)\" > /opt/bastion-e2e-backup-restore/run-id"}
      ]
    }
  }'
}

create_source_template() {
  local key=$1
  local output
  local template_id

  log "creating source template $key"
  output="$(run_cli templates create --key "$key" --config "$(backup_restore_config)")"
  template_id="$(json_get '.id' <<<"$output")"

  if [ -z "$template_id" ] || [ "$template_id" = "null" ]; then
    fail "source template did not return an id"
  fi

  assert_json_key "source template" "$output" "$key"
  printf '%s\n' "$template_id"
}

import_template() {
  local archive=$1
  local key=${2:-}
  local output
  local template_id

  if [ -n "$key" ]; then
    output="$(run_cli templates import --key "$key" --file "$archive")"
    assert_json_key "imported template" "$output" "$key"
  else
    output="$(run_cli templates import --file "$archive")"
    assert_json_no_key "unkeyed imported template" "$output"
  fi

  template_id="$(json_get '.id' <<<"$output")"
  if [ -z "$template_id" ] || [ "$template_id" = "null" ]; then
    fail "imported template did not return an id"
  fi

  printf '%s\n' "$template_id"
}

assert_nonempty_archive() {
  local archive=$1

  if [ ! -s "$archive" ]; then
    fail "archive $archive was not created or is empty"
  fi
}

main() {
  local source_key="$RUN_ID-source"
  local restored_key="$RUN_ID-restored"
  local source_id
  local restored_id
  local unkeyed_id
  local env_id
  local output
  local source_archive="$ARCHIVE_DIR/source-template.tar.zst"
  local restored_archive="$ARCHIVE_DIR/restored-template.tar.zst"

  precheck
  trap cleanup EXIT

  log "starting backup/restore e2e run $RUN_ID"

  source_id="$(create_source_template "$source_key")"
  TEMPLATE_IDS+=("$source_id")

  log "exporting source template by key"
  run_cli templates export --key "$source_key" >"$source_archive"
  assert_nonempty_archive "$source_archive"

  log "removing source template $source_id"
  run_cli templates remove --id "$source_id" >/dev/null
  TEMPLATE_IDS=("${TEMPLATE_IDS[@]/$source_id}")

  log "importing source archive as $restored_key"
  restored_id="$(import_template "$source_archive" "$restored_key")"
  TEMPLATE_IDS+=("$restored_id")

  if [ "$restored_id" = "$source_id" ]; then
    fail "imported template reused source id $source_id"
  fi

  log "creating environment from restored template"
  output="$(run_cli env create --template-key "$restored_key")"
  env_id="$(json_get '.id' <<<"$output")"
  if [ -z "$env_id" ] || [ "$env_id" = "null" ]; then
    fail "environment from restored template did not return an id"
  fi

  ENV_IDS+=("$env_id")
  if ! jq -e '.status == "running"' <<<"$output" >/dev/null; then
    fail "environment $env_id status is $(jq -c '.status' <<<"$output"), want running"
  fi

  run_cli ssh --id "$env_id" -- grep -q "$RUN_ID" /opt/bastion-e2e-backup-restore/run-id

  log "exporting restored template by id"
  run_cli templates export --id "$restored_id" >"$restored_archive"
  assert_nonempty_archive "$restored_archive"

  log "importing restored archive without key"
  unkeyed_id="$(import_template "$restored_archive")"
  TEMPLATE_IDS+=("$unkeyed_id")

  if [ "$unkeyed_id" = "$source_id" ] || [ "$unkeyed_id" = "$restored_id" ]; then
    fail "unkeyed import reused an existing template id"
  fi

  log "backup/restore e2e run passed"
}

main "$@"
