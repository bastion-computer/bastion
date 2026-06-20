#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
RUN_ID="e2e-cluster-$(date +%Y%m%d%H%M%S)-$$"
PROJECT="bastion-cluster-e2e-$$"
NODE_PORT="${BASTION_E2E_NODE_PORT:-4150}"
CLUSTER_PORT="${BASTION_E2E_CLUSTER_PORT:-4151}"
POSTGRES_PORT="${BASTION_E2E_POSTGRES_PORT:-4152}"
MINIO_PORT="${BASTION_E2E_MINIO_PORT:-4153}"
MINIO_CONSOLE_PORT="${BASTION_E2E_MINIO_CONSOLE_PORT:-4154}"
NODE_API_URL="http://localhost:$NODE_PORT"
CLUSTER_API_URL="http://localhost:$CLUSTER_PORT"
DATABASE_URL="postgres://bastion:bastion@localhost:$POSTGRES_PORT/bastion_cluster?sslmode=disable"
NODE_DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-node.XXXXXX")"
CLIENT_DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-client.XXXXXX")"
LOG_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-logs.XXXXXX")"
NODE_PID=""
CLUSTER_PID=""

log() {
  printf '[cluster-test] %s\n' "$*"
}

fail() {
  log "FAIL: $*" >&2
  exit 1
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

compose() {
  BASTION_DEV_POSTGRES_PORT="$POSTGRES_PORT" \
    BASTION_DEV_MINIO_PORT="$MINIO_PORT" \
    BASTION_DEV_MINIO_CONSOLE_PORT="$MINIO_CONSOLE_PORT" \
    docker compose -p "$PROJECT" -f "$REPO_DIR/compose.yml" "$@"
}

run_cluster_cli() {
  "$BASTION" --api-url "$CLUSTER_API_URL" "$@"
}

cleanup() {
  local status=$?
  set +e

  if [ -n "$CLUSTER_PID" ]; then
    kill "$CLUSTER_PID" >/dev/null 2>&1 || true
    wait "$CLUSTER_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "$NODE_PID" ]; then
    kill "$NODE_PID" >/dev/null 2>&1 || true
    wait "$NODE_PID" >/dev/null 2>&1 || true
  fi

  compose down >/dev/null 2>&1 || true

  if [ "$status" -ne 0 ] && [ "${BASTION_E2E_KEEP_FAILED:-}" = "1" ]; then
    log "preserving failed run data: node_data=$NODE_DATA_DIR client_data=$CLIENT_DATA_DIR logs=$LOG_DIR compose_project=$PROJECT"
    exit "$status"
  fi

  rm -rf "$NODE_DATA_DIR" "$CLIENT_DATA_DIR" "$LOG_DIR"
  exit "$status"
}

wait_http() {
  local url=$1
  local label=$2
  local waited=0
  local timeout=60

  until curl -fsS "$url" >/dev/null 2>&1; do
    sleep 1
    waited=$((waited + 1))
    if [ "$waited" -ge "$timeout" ]; then
      fail "timed out waiting for $label at $url"
    fi
  done
}

wait_postgres() {
  local waited=0
  local timeout=60

  until compose exec -T postgres pg_isready -U bastion -d bastion_cluster >/dev/null 2>&1; do
    sleep 1
    waited=$((waited + 1))
    if [ "$waited" -ge "$timeout" ]; then
      fail "timed out waiting for Postgres"
    fi
  done
}

precheck() {
  require_command curl
  require_command docker
  require_command jq

  if ! docker compose version >/dev/null 2>&1; then
    fail "docker compose is required"
  fi

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi
}

start_infra() {
  log "starting Postgres and MinIO compose project $PROJECT"
  compose up -d postgres minio minio-init >/dev/null
  wait_postgres
}

start_node_api() {
  log "starting node host API on $NODE_API_URL"
  "$BASTION" --data-dir "$NODE_DATA_DIR" start api --addr "localhost:$NODE_PORT" --log-format text --log-level debug >"$LOG_DIR/node-api.log" 2>&1 &
  NODE_PID=$!
  wait_http "$NODE_API_URL/v1/health" "node API"
}

start_cluster_api() {
  log "starting cluster API on $CLUSTER_API_URL"
  "$BASTION" start cluster \
    --addr "localhost:$CLUSTER_PORT" \
    --database-url "$DATABASE_URL" \
    --archive-bucket bastion-templates \
    --archive-endpoint "http://localhost:$MINIO_PORT" \
    --archive-region us-east-1 \
    --archive-access-key-id bastion \
    --archive-secret-access-key bastion-secret \
    --archive-force-path-style \
    --log-format text \
    --log-level debug >"$LOG_DIR/cluster-api.log" 2>&1 &
  CLUSTER_PID=$!
  wait_http "$CLUSTER_API_URL/v1/cluster/health" "cluster API"
}

assert_json_id_prefix() {
  local label=$1
  local json=$2
  local prefix=$3
  local id

  id="$(jq -r '.id' <<<"$json")"
  if [[ "$id" != "$prefix"_* ]]; then
    fail "$label id is $id, want $prefix prefix"
  fi
}

run_cluster_case() {
  local node_json
  local namespace_json
  local secret_json
  local secret_id
  local utilization_json
  local namespace_key="team-$RUN_ID"
  local secret_key="TOKEN_$RUN_ID"

  log "registering node"
  node_json="$(run_cluster_cli cluster nodes create --key "node-$RUN_ID" --url "$NODE_API_URL")"
  assert_json_id_prefix "node" "$node_json" "node"

  log "checking cluster utilization"
  utilization_json="$(run_cluster_cli cluster utilization)"
  if ! jq -e '.vcpu.total > 0 and .memory.total > 0 and .volume.total > 0' <<<"$utilization_json" >/dev/null; then
    fail "cluster utilization is $(jq -c . <<<"$utilization_json"), want nonzero node capacity"
  fi

  log "creating namespace"
  namespace_json="$(run_cluster_cli cluster namespaces create --key "$namespace_key" --vcpu 4 --memory 8589934592 --volume 10737418240)"
  assert_json_id_prefix "namespace" "$namespace_json" "ns"
  if ! jq -e --arg key "$namespace_key" '.key == $key and .limits.vcpu == 4' <<<"$namespace_json" >/dev/null; then
    fail "namespace response is $(jq -c . <<<"$namespace_json"), want configured key and limits"
  fi

  if run_cluster_cli secrets list >/dev/null 2>&1; then
    fail "secrets list without --namespace succeeded, want namespace required"
  fi
  log "namespace required check passed"

  log "creating namespaced secret"
  secret_json="$(run_cluster_cli --namespace "$namespace_key" secrets create --key "$secret_key" --value cluster-secret)"
  assert_json_id_prefix "secret" "$secret_json" "sec"
  secret_id="$(jq -r '.id' <<<"$secret_json")"
  if jq -e 'has("value")' <<<"$secret_json" >/dev/null || [[ "$secret_json" == *cluster-secret* ]]; then
    fail "secret create response leaked value: $secret_json"
  fi

  log "reading namespaced secret through persisted namespace"
  "$BASTION" --data-dir "$CLIENT_DATA_DIR" --api-url "$CLUSTER_API_URL" client set namespace "$namespace_key" >/dev/null
  secret_json="$("$BASTION" --data-dir "$CLIENT_DATA_DIR" --api-url "$CLUSTER_API_URL" secrets get --key "$secret_key")"
  if ! jq -e --arg id "$secret_id" '.id == $id and .value == "cluster-secret"' <<<"$secret_json" >/dev/null; then
    fail "secret get response is $(jq -c . <<<"$secret_json"), want persisted namespace secret"
  fi

  run_cluster_cli --namespace "$namespace_key" secrets remove --id "$secret_id" >/dev/null
  run_cluster_cli cluster namespaces remove --key "$namespace_key" >/dev/null
  run_cluster_cli cluster nodes remove --key "node-$RUN_ID" >/dev/null
}

main() {
  precheck
  trap cleanup EXIT

  log "starting cluster e2e run $RUN_ID"
  start_infra
  start_node_api
  start_cluster_api
  run_cluster_case
  log "cluster e2e run passed"
}

main "$@"
