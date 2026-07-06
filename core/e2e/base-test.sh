#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-base-e2e.XXXXXX")"

log() {
  printf '[base-test] %s\n' "$*"
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
  rm -rf "$WORK_DIR"
  exit "$status"
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

assert_command_fails() {
  if "$@" >/dev/null 2>&1; then
    fail "command unexpectedly succeeded: $*"
  fi
}

assert_output_contains() {
  local label=$1
  local output=$2
  local needle=$3

  if [[ "$output" != *"$needle"* ]]; then
    fail "$label did not include $needle: $output"
  fi
}

main() {
  precheck
  trap cleanup EXIT

  local build_output build_logs get_output export_path import_output import_logs content_address imported_address
  build_logs="$WORK_DIR/base-build.log"
  export_path="$WORK_DIR/base.tar.zst"
  import_logs="$WORK_DIR/base-import.log"

  log "building base with force"
  build_output="$(run_cli base build --force 2>"$build_logs")"
  content_address="$(jq -r '.contentAddress' <<<"$build_output")"
  if [[ "$content_address" != sha256:* ]]; then
    fail "base build content address is $content_address, want sha256 prefix"
  fi
  assert_output_contains "base build logs" "$(<"$build_logs")" "base"

  log "getting base metadata"
  get_output="$(run_cli base get)"
  if ! jq -e --arg content_address "$content_address" '.contentAddress == $content_address' <<<"$get_output" >/dev/null; then
    fail "base get returned $(jq -c . <<<"$get_output"), want $content_address"
  fi

  log "exporting base archive"
  run_cli base export >"$export_path"
  if [ ! -s "$export_path" ]; then
    fail "base export did not write an archive"
  fi

  log "verifying import without force is rejected"
  assert_command_fails run_cli base import --file "$export_path"

  log "force importing exported base archive"
  import_output="$(run_cli base import --force --file "$export_path" 2>"$import_logs")"
  imported_address="$(jq -r '.contentAddress' <<<"$import_output")"
  if [ "$imported_address" != "$content_address" ]; then
    fail "imported content address is $imported_address, want $content_address"
  fi
  assert_output_contains "base import logs" "$(<"$import_logs")" "importing base"

  log "base e2e run passed"
}

main "$@"
