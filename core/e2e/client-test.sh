#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-client-e2e.XXXXXX")"

log() {
  printf '[client-test] %s\n' "$*"
}

fail() {
  log "FAIL: $*" >&2
  exit 1
}

cleanup() {
  local status=$?
  rm -rf "$DATA_DIR"
  exit "$status"
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

run_configured_cli() {
  env -u BASTION_API_URL "$BASTION" --data-dir "$DATA_DIR" "$@"
}

precheck() {
  require_command jq

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! "$BASTION" --api-url "$API_URL" templates list >/dev/null 2>&1; then
    fail "Bastion API is not reachable on $API_URL; run mise dev:up"
  fi
}

assert_config_api_url() {
  local output=$1
  local value=$2
  local source=$3

  if ! jq -e --arg value "$value" --arg source "$source" '.apiUrl.value == $value and .apiUrl.source == $source' <<<"$output" >/dev/null; then
    fail "client config is $(jq -c '.apiUrl' <<<"$output"), want value=$value source=$source"
  fi
}

assert_config_namespace_id() {
  local output=$1
  local value=$2
  local source=$3

  if ! jq -e --arg value "$value" --arg source "$source" '.namespaceId.value == $value and .namespaceId.source == $source' <<<"$output" >/dev/null; then
    fail "client config namespaceId is $(jq -c '.namespaceId' <<<"$output"), want value=$value source=$source"
  fi
}

assert_config_namespace_key() {
  local output=$1
  local value=$2
  local source=$3

  if ! jq -e --arg value "$value" --arg source "$source" '.namespaceKey.value == $value and .namespaceKey.source == $source' <<<"$output" >/dev/null; then
    fail "client config namespaceKey is $(jq -c '.namespaceKey' <<<"$output"), want value=$value source=$source"
  fi
}

run_client_config_case() {
  local output
  local bad_api_url="http://127.0.0.1:1"

  log "setting persisted client API URL to $API_URL"
  run_configured_cli client set api-url "$API_URL"

  output="$(run_configured_cli client config)"
  assert_config_api_url "$output" "$API_URL" "config"

  run_configured_cli templates list >/dev/null
  log "persisted client API URL reached the host API"

  run_configured_cli client set api-url "$bad_api_url"
  env -u BASTION_API_URL "$BASTION" --data-dir "$DATA_DIR" --api-url "$API_URL" templates list >/dev/null
  log "explicit --api-url overrides persisted client API URL"

  env BASTION_API_URL="$API_URL" "$BASTION" --data-dir "$DATA_DIR" client config | jq -e --arg api_url "$API_URL" '.apiUrl.value == $api_url and .apiUrl.source == "environment"' >/dev/null
  log "BASTION_API_URL overrides persisted client API URL"

  run_configured_cli client remove api-url
  output="$(run_configured_cli client config)"
  assert_config_api_url "$output" "http://localhost:3148" "default"
  log "removed persisted client API URL"

  run_configured_cli client set namespace-id ns_client_e2e
  output="$(run_configured_cli client config)"
  assert_config_namespace_id "$output" "ns_client_e2e" "config"
  assert_config_namespace_key "$output" "" "default"
  log "persisted client namespace ID"

  run_configured_cli client set namespace-key team-client-e2e
  output="$(run_configured_cli client config)"
  assert_config_namespace_id "$output" "" "default"
  assert_config_namespace_key "$output" "team-client-e2e" "config"
  log "persisted client namespace key"

  env BASTION_NAMESPACE_ID=ns_env_e2e BASTION_NAMESPACE_KEY= "$BASTION" --data-dir "$DATA_DIR" client config | jq -e '.namespaceId.value == "ns_env_e2e" and .namespaceId.source == "environment"' >/dev/null
  log "BASTION_NAMESPACE_ID overrides persisted namespace"

  env BASTION_NAMESPACE_ID=ns_env_e2e BASTION_NAMESPACE_KEY= "$BASTION" --data-dir "$DATA_DIR" --namespace-key team-flag-e2e client config | jq -e '.namespaceKey.value == "team-flag-e2e" and .namespaceKey.source == "flag" and .namespaceId.value == ""' >/dev/null
  log "explicit --namespace-key overrides namespace environment"

  run_configured_cli client remove namespace-key
  output="$(run_configured_cli client config)"
  assert_config_namespace_id "$output" "" "default"
  assert_config_namespace_key "$output" "" "default"
  log "removed persisted client namespace"
}

main() {
  precheck
  trap cleanup EXIT

  log "starting client config e2e run"
  run_client_config_case
  log "client config e2e run passed"
}

main "$@"
