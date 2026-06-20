#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
SOURCE_DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-cluster-$(date +%Y%m%d%H%M%S)-$$"
PROJECT="bastion-cluster-e2e-$$"
NODE1_PORT="${BASTION_E2E_NODE1_PORT:-4150}"
CLUSTER_PORT="${BASTION_E2E_CLUSTER_PORT:-4151}"
POSTGRES_PORT="${BASTION_E2E_POSTGRES_PORT:-4152}"
MINIO_PORT="${BASTION_E2E_MINIO_PORT:-4153}"
MINIO_CONSOLE_PORT="${BASTION_E2E_MINIO_CONSOLE_PORT:-4154}"
NODE2_PORT="${BASTION_E2E_NODE2_PORT:-4155}"
CLUSTER_API_URL="http://localhost:$CLUSTER_PORT"
DATABASE_URL="postgres://bastion:bastion@localhost:$POSTGRES_PORT/bastion_cluster?sslmode=disable"
NODE1_DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-node-1.XXXXXX")"
NODE2_DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-node-2.XXXXXX")"
CLIENT_A_DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-client-a.XXXXXX")"
CLIENT_B_DATA_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-client-b.XXXXXX")"
LOG_DIR="$(mktemp -d "${TMPDIR:-/tmp}/bastion-cluster-logs.XXXXXX")"
NODE_PORTS=("$NODE1_PORT" "$NODE2_PORT")
NODE_API_URLS=("http://localhost:$NODE1_PORT" "http://localhost:$NODE2_PORT")
NODE_DATA_DIRS=("$NODE1_DATA_DIR" "$NODE2_DATA_DIR")
NODE_SOCKETS=("$LOG_DIR/node-1-bastiond.sock" "$LOG_DIR/node-2-bastiond.sock")
NODE_API_PIDS=("" "")
NODE_DAEMON_PIDS=("" "")
NODE_NETWORK_PREFIXES=()
CLUSTER_PID=""
REGISTERED_NODE_KEYS=()
NAMESPACE_KEYS=()
SECRET_NAMESPACE_KEYS=()
SECRET_IDS=()
TEMPLATE_NAMESPACE_KEYS=()
TEMPLATE_IDS=()
ENV_NAMESPACE_KEYS=()
ENV_IDS=()
CREATED_TEMPLATE_ID=""
CREATED_ENV_ID=""

log() {
  printf '[cluster-test] %s\n' "$*" >&2
}

fail() {
  log "FAIL: $*"
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

run_node_cli() {
  local index=$1
  shift

  "$BASTION" --api-url "${NODE_API_URLS[$index]}" "$@"
}

remove_path() {
  rm -rf "$@" 2>/dev/null || sudo -n rm -rf "$@" 2>/dev/null || true
}

stop_node() {
  local index=$1
  local api_pid="${NODE_API_PIDS[$index]}"
  local daemon_pid="${NODE_DAEMON_PIDS[$index]}"

  if [ -n "$api_pid" ]; then
    kill "$api_pid" >/dev/null 2>&1 || true
    wait "$api_pid" >/dev/null 2>&1 || true
    NODE_API_PIDS[$index]=""
  fi

  if [ -n "$daemon_pid" ]; then
    kill "$daemon_pid" >/dev/null 2>&1 || sudo -n kill "$daemon_pid" >/dev/null 2>&1 || true
    wait "$daemon_pid" >/dev/null 2>&1 || true
    NODE_DAEMON_PIDS[$index]=""
  fi
}

cleanup_cluster_resources() {
  local i
  local env_id
  local namespace_key
  local template_id
  local secret_id
  local node_key

  for i in "${!ENV_IDS[@]}"; do
    env_id="${ENV_IDS[$i]}"
    namespace_key="${ENV_NAMESPACE_KEYS[$i]}"
    if [ -n "$env_id" ] && [ -n "$namespace_key" ]; then
      run_cluster_cli --namespace "$namespace_key" env remove --id "$env_id" >/dev/null 2>&1 || log "cleanup: environment $env_id was not removed"
    fi
  done
  ENV_IDS=()
  ENV_NAMESPACE_KEYS=()

  for i in "${!TEMPLATE_IDS[@]}"; do
    template_id="${TEMPLATE_IDS[$i]}"
    namespace_key="${TEMPLATE_NAMESPACE_KEYS[$i]}"
    if [ -n "$template_id" ] && [ -n "$namespace_key" ]; then
      run_cluster_cli --namespace "$namespace_key" templates remove --id "$template_id" >/dev/null 2>&1 || log "cleanup: template $template_id was not removed"
    fi
  done
  TEMPLATE_IDS=()
  TEMPLATE_NAMESPACE_KEYS=()

  for i in "${!SECRET_IDS[@]}"; do
    secret_id="${SECRET_IDS[$i]}"
    namespace_key="${SECRET_NAMESPACE_KEYS[$i]}"
    if [ -n "$secret_id" ] && [ -n "$namespace_key" ]; then
      run_cluster_cli --namespace "$namespace_key" secrets remove --id "$secret_id" >/dev/null 2>&1 || log "cleanup: secret $secret_id was not removed"
    fi
  done
  SECRET_IDS=()
  SECRET_NAMESPACE_KEYS=()

  for namespace_key in "${NAMESPACE_KEYS[@]}"; do
    if [ -n "$namespace_key" ]; then
      run_cluster_cli cluster namespaces remove --key "$namespace_key" >/dev/null 2>&1 || log "cleanup: namespace $namespace_key was not removed"
    fi
  done
  NAMESPACE_KEYS=()

  for node_key in "${REGISTERED_NODE_KEYS[@]}"; do
    if [ -n "$node_key" ]; then
      run_cluster_cli cluster nodes remove --key "$node_key" >/dev/null 2>&1 || true
    fi
  done
  REGISTERED_NODE_KEYS=()
}

cleanup() {
  local status=$?
  set +e

  if [ "$status" -eq 0 ] || [ "${BASTION_E2E_KEEP_FAILED:-}" != "1" ]; then
    cleanup_cluster_resources
  fi

  if [ -n "$CLUSTER_PID" ]; then
    kill "$CLUSTER_PID" >/dev/null 2>&1 || true
    wait "$CLUSTER_PID" >/dev/null 2>&1 || true
  fi

  stop_node 0
  stop_node 1

  compose down >/dev/null 2>&1 || true

  if [ "$status" -ne 0 ] && [ "${BASTION_E2E_KEEP_FAILED:-}" = "1" ]; then
    log "preserving failed run data: node_data=${NODE_DATA_DIRS[*]} client_data=$CLIENT_A_DATA_DIR $CLIENT_B_DATA_DIR logs=$LOG_DIR compose_project=$PROJECT"
    exit "$status"
  fi

  remove_path "$NODE1_DATA_DIR" "$NODE2_DATA_DIR" "$CLIENT_A_DATA_DIR" "$CLIENT_B_DATA_DIR" "$LOG_DIR"
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

wait_socket() {
  local socket=$1
  local label=$2
  local waited=0
  local timeout=60

  until [ -S "$socket" ]; do
    sleep 1
    waited=$((waited + 1))
    if [ "$waited" -ge "$timeout" ]; then
      fail "timed out waiting for $label socket at $socket"
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

choose_node_network_prefixes() {
  local route=""
  local second
  local first
  local second_prefix

  if command -v ip >/dev/null 2>&1; then
    route="$(ip -4 route get 1.1.1.1 2>/dev/null || true)"
  fi

  if [[ "$route" =~ src[[:space:]]+10\.([0-9]+)\. ]]; then
    second="${BASH_REMATCH[1]}"
    first=$((10#$second + 1))
    second_prefix=$((first + 1))
    if [ "$second_prefix" -le 255 ]; then
      printf '10.%d\n10.%d\n' "$first" "$second_prefix"
      return
    fi
  fi

  printf '10.242\n10.243\n'
}

configure_node_network_prefixes() {
  if [ -n "${BASTION_E2E_NODE1_NETWORK_PREFIX:-}" ] && [ -n "${BASTION_E2E_NODE2_NETWORK_PREFIX:-}" ]; then
    NODE_NETWORK_PREFIXES=("$BASTION_E2E_NODE1_NETWORK_PREFIX" "$BASTION_E2E_NODE2_NETWORK_PREFIX")
    return
  fi

  mapfile -t NODE_NETWORK_PREFIXES < <(choose_node_network_prefixes)
  if [ "${#NODE_NETWORK_PREFIXES[@]}" -ne 2 ]; then
    fail "expected two VM network prefixes"
  fi
}

precheck() {
  require_command curl
  require_command docker
  require_command jq
  require_command sudo

  if ! docker compose version >/dev/null 2>&1; then
    fail "docker compose is required"
  fi

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! sudo -n true >/dev/null 2>&1; then
    fail "passwordless sudo is required to start bastiond"
  fi

  if ! "$BASTION" system --data-dir "$SOURCE_DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $SOURCE_DATA_DIR; run bastion system --data-dir '$SOURCE_DATA_DIR' add cloud-hypervisor --with-utilities"
  fi

  if [ ! -d "$SOURCE_DATA_DIR/cloud-hypervisor" ]; then
    fail "Cloud Hypervisor assets not found at $SOURCE_DATA_DIR/cloud-hypervisor"
  fi
}

start_infra() {
  log "starting Postgres and MinIO compose project $PROJECT"
  compose up -d postgres minio minio-init >/dev/null
  wait_postgres
}

prepare_node_data_dir() {
  local data_dir=$1

  mkdir -p "$data_dir"
  ln -s "$SOURCE_DATA_DIR/cloud-hypervisor" "$data_dir/cloud-hypervisor"
}

start_node() {
  local index=$1
  local ordinal=$((index + 1))
  local data_dir="${NODE_DATA_DIRS[$index]}"
  local socket="${NODE_SOCKETS[$index]}"
  local port="${NODE_PORTS[$index]}"
  local api_url="${NODE_API_URLS[$index]}"
  local network_prefix="${NODE_NETWORK_PREFIXES[$index]}"

  prepare_node_data_dir "$data_dir"

  log "starting node $ordinal daemon with VM network prefix $network_prefix"
  sudo -n env \
    BASTION_VM_CPUS=1 \
    BASTION_VM_MEMORY_BYTES=805306368 \
    BASTION_VM_NETWORK_PREFIX="$network_prefix" \
    "$BASTION" --data-dir "$data_dir" start daemon \
    --socket "$socket" \
    --log-format text \
    --log-level debug >"$LOG_DIR/node-$ordinal-daemon.log" 2>&1 &
  NODE_DAEMON_PIDS[$index]=$!
  wait_socket "$socket" "node $ordinal daemon"

  log "starting node $ordinal host API on $api_url"
  "$BASTION" --data-dir "$data_dir" start api \
    --addr "localhost:$port" \
    --bastiond-socket "$socket" \
    --log-format text \
    --log-level debug >"$LOG_DIR/node-$ordinal-api.log" 2>&1 &
  NODE_API_PIDS[$index]=$!
  wait_http "$api_url/v1/health" "node $ordinal API"
}

start_nodes() {
  configure_node_network_prefixes
  start_node 0
  start_node 1
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

assert_command_fails() {
  local label=$1
  shift

  if "$@" >/dev/null 2>&1; then
    fail "$label succeeded, want failure"
  fi
}

assert_node_template_count() {
  local index=$1
  local expected=$2
  local output

  output="$(run_node_cli "$index" templates list --limit 5000)"
  if ! jq -e --argjson expected "$expected" '.entries | length == $expected' <<<"$output" >/dev/null; then
    fail "node $((index + 1)) template count is $(jq -c '.entries | length' <<<"$output"), want $expected"
  fi
}

cluster_template_config() {
  local secret_key=$1

  jq -nc \
    --arg run_id "$RUN_ID" \
    --arg secret_key "$secret_key" \
    '{
      agents: {opencode: {}},
      resources: {vcpu: 1, memory: 1, volume: 8},
      actions: {
        init: [
          {run: "set -eu\nmkdir -p /opt/bastion-cluster-e2e\nprintf \"run_id=\($run_id)\\n\" > /opt/bastion-cluster-e2e/init-run\nprintf \"%s\\n\" \"${{ secret.\($secret_key) }}\" > /opt/bastion-cluster-e2e/secret-value"}
        ]
      }
    }'
}

register_nodes() {
  local node_a_json
  local node_b_json
  local nodes_json
  local node_a_key="node-a-$RUN_ID"
  local node_b_key="node-b-$RUN_ID"

  log "registering two cluster nodes"
  node_a_json="$(run_cluster_cli cluster nodes create --key "$node_a_key" --url "${NODE_API_URLS[0]}")"
  node_b_json="$(run_cluster_cli cluster nodes create --key "$node_b_key" --url "${NODE_API_URLS[1]}")"
  REGISTERED_NODE_KEYS+=("$node_a_key" "$node_b_key")

  assert_json_id_prefix "node A" "$node_a_json" "node"
  assert_json_id_prefix "node B" "$node_b_json" "node"

  run_cluster_cli cluster nodes get --key "$node_a_key" >/dev/null
  run_cluster_cli cluster nodes get --id "$(jq -r '.id' <<<"$node_b_json")" >/dev/null

  nodes_json="$(run_cluster_cli cluster nodes list --limit 10)"
  if ! jq -e --arg a "$node_a_key" --arg b "$node_b_key" '[.entries[].key] | index($a) != null and index($b) != null' <<<"$nodes_json" >/dev/null; then
    fail "cluster nodes list did not include both registered nodes: $(jq -c '.entries' <<<"$nodes_json")"
  fi
}

create_namespaces() {
  local namespace_a_json
  local namespace_b_json
  local namespace_a_key=$1
  local namespace_b_key=$2

  log "creating isolated namespaces"
  namespace_a_json="$(run_cluster_cli cluster namespaces create --key "$namespace_a_key" --vcpu 4 --memory 8589934592 --volume 10737418240)"
  namespace_b_json="$(run_cluster_cli cluster namespaces create --key "$namespace_b_key" --vcpu 4 --memory 8589934592 --volume 10737418240)"
  NAMESPACE_KEYS+=("$namespace_a_key" "$namespace_b_key")

  assert_json_id_prefix "namespace A" "$namespace_a_json" "ns"
  assert_json_id_prefix "namespace B" "$namespace_b_json" "ns"
  if ! jq -e --arg key "$namespace_a_key" '.key == $key and .limits.vcpu == 4' <<<"$namespace_a_json" >/dev/null; then
    fail "namespace A response is $(jq -c . <<<"$namespace_a_json"), want configured key and limits"
  fi
}

assert_namespace_secret_isolation() {
  local namespace_a_key=$1
  local namespace_b_key=$2
  local secret_key=$3
  local secret_a_json
  local secret_b_json
  local secret_a_id
  local secret_b_id

  if run_cluster_cli secrets list >/dev/null 2>&1; then
    fail "secrets list without --namespace succeeded, want namespace required"
  fi
  log "namespace required check passed"

  log "creating same-key secrets in separate namespaces"
  secret_a_json="$(run_cluster_cli --namespace "$namespace_a_key" secrets create --key "$secret_key" --value team-a-secret)"
  secret_b_json="$(run_cluster_cli --namespace "$namespace_b_key" secrets create --key "$secret_key" --value team-b-secret)"
  assert_json_id_prefix "namespace A secret" "$secret_a_json" "sec"
  assert_json_id_prefix "namespace B secret" "$secret_b_json" "sec"
  secret_a_id="$(jq -r '.id' <<<"$secret_a_json")"
  secret_b_id="$(jq -r '.id' <<<"$secret_b_json")"
  SECRET_NAMESPACE_KEYS+=("$namespace_a_key" "$namespace_b_key")
  SECRET_IDS+=("$secret_a_id" "$secret_b_id")

  if jq -e 'has("value")' <<<"$secret_a_json" >/dev/null || [[ "$secret_a_json" == *team-a-secret* ]]; then
    fail "secret create response leaked value: $secret_a_json"
  fi

  "$BASTION" --data-dir "$CLIENT_A_DATA_DIR" --api-url "$CLUSTER_API_URL" client set namespace "$namespace_a_key" >/dev/null
  "$BASTION" --data-dir "$CLIENT_B_DATA_DIR" --api-url "$CLUSTER_API_URL" client set namespace "$namespace_b_key" >/dev/null

  secret_a_json="$("$BASTION" --data-dir "$CLIENT_A_DATA_DIR" --api-url "$CLUSTER_API_URL" secrets get --key "$secret_key")"
  secret_b_json="$("$BASTION" --data-dir "$CLIENT_B_DATA_DIR" --api-url "$CLUSTER_API_URL" secrets get --key "$secret_key")"
  if ! jq -e --arg id "$secret_a_id" '.id == $id and .value == "team-a-secret"' <<<"$secret_a_json" >/dev/null; then
    fail "namespace A secret get response is $(jq -c . <<<"$secret_a_json"), want team A value"
  fi
  if ! jq -e --arg id "$secret_b_id" '.id == $id and .value == "team-b-secret"' <<<"$secret_b_json" >/dev/null; then
    fail "namespace B secret get response is $(jq -c . <<<"$secret_b_json"), want team B value"
  fi

  assert_command_fails "namespace B get namespace A secret by id" run_cluster_cli --namespace "$namespace_b_key" secrets get --id "$secret_a_id"
}

create_cluster_template() {
  local namespace_key=$1
  local secret_key=$2
  local template_key=$3
  local template_json
  local template_id
  local logs="$LOG_DIR/template-create.log"

  log "creating VM-backed template in namespace A"
  if ! template_json="$(run_cluster_cli --namespace "$namespace_key" templates create --key "$template_key" --config "$(cluster_template_config "$secret_key")" 2>"$logs")"; then
    fail "template create failed: $template_json $(<"$logs")"
  fi

  assert_json_id_prefix "template" "$template_json" "tpl"
  template_id="$(jq -r '.id' <<<"$template_json")"
  TEMPLATE_NAMESPACE_KEYS+=("$namespace_key")
  TEMPLATE_IDS+=("$template_id")
  CREATED_TEMPLATE_ID="$template_id"
}

create_cluster_environment() {
  local namespace_key=$1
  local template_key=$2
  local env_key=$3
  local env_json
  local env_id
  local logs="$LOG_DIR/environment-create.log"

  log "creating environment from archived template on remaining node"
  if ! env_json="$(run_cluster_cli --namespace "$namespace_key" env create --template-key "$template_key" --key "$env_key" 2>"$logs")"; then
    fail "environment create failed: $env_json $(<"$logs")"
  fi

  assert_json_id_prefix "environment" "$env_json" "env"
  env_id="$(jq -r '.id' <<<"$env_json")"
  ENV_NAMESPACE_KEYS+=("$namespace_key")
  ENV_IDS+=("$env_id")
  CREATED_ENV_ID="$env_id"

  env_json="$(run_cluster_cli --namespace "$namespace_key" env get --id "$env_id")"
  if ! jq -e '.status == "running"' <<<"$env_json" >/dev/null; then
    fail "environment $env_id status is $(jq -c '.status' <<<"$env_json"), want running"
  fi

}

assert_cluster_ssh() {
  local namespace_key=$1
  local env_id=$2
  local output

  log "checking SSH through cluster API"
  run_cluster_cli --namespace "$namespace_key" ssh --id "$env_id" -- grep -q "$RUN_ID" /opt/bastion-cluster-e2e/init-run
  run_cluster_cli --namespace "$namespace_key" ssh --id "$env_id" -- grep -q team-a-secret /opt/bastion-cluster-e2e/secret-value

  output="$(run_cluster_cli --namespace "$namespace_key" ssh --id "$env_id" -- printf cluster-ssh-ok)"
  if [ "$output" != "cluster-ssh-ok" ]; then
    fail "cluster SSH output was $output, want cluster-ssh-ok"
  fi
}

run_cluster_case() {
  local utilization_json
  local namespace_a_key="team-a-$RUN_ID"
  local namespace_b_key="team-b-$RUN_ID"
  local secret_key="shared-secret-$RUN_ID"
  local template_key="template-$RUN_ID"
  local env_key="env-$RUN_ID"
  local template_id
  local env_id
  local node_a_key="node-a-$RUN_ID"

  register_nodes

  log "checking aggregate cluster utilization"
  utilization_json="$(run_cluster_cli cluster utilization)"
  if ! jq -e '.vcpu.total > 0 and .memory.total > 0 and .volume.total > 0' <<<"$utilization_json" >/dev/null; then
    fail "cluster utilization is $(jq -c . <<<"$utilization_json"), want nonzero node capacity"
  fi

  create_namespaces "$namespace_a_key" "$namespace_b_key"
  assert_namespace_secret_isolation "$namespace_a_key" "$namespace_b_key" "$secret_key"

  assert_node_template_count 1 0
  create_cluster_template "$namespace_a_key" "$secret_key" "$template_key"
  template_id="$CREATED_TEMPLATE_ID"
  assert_command_fails "namespace B get namespace A template by key" run_cluster_cli --namespace "$namespace_b_key" templates get --key "$template_key"
  assert_command_fails "namespace B get namespace A template by id" run_cluster_cli --namespace "$namespace_b_key" templates get --id "$template_id"
  assert_node_template_count 1 0

  log "removing node A to force template import onto node B"
  run_cluster_cli cluster nodes remove --key "$node_a_key" >/dev/null
  stop_node 0
  assert_command_fails "get removed node A" run_cluster_cli cluster nodes get --key "$node_a_key"
  run_cluster_cli cluster nodes get --key "node-b-$RUN_ID" >/dev/null

  create_cluster_environment "$namespace_a_key" "$template_key" "$env_key"
  env_id="$CREATED_ENV_ID"
  assert_node_template_count 1 1
  assert_command_fails "namespace B get namespace A environment by key" run_cluster_cli --namespace "$namespace_b_key" env get --key "$env_key"
  assert_command_fails "namespace B get namespace A environment by id" run_cluster_cli --namespace "$namespace_b_key" env get --id "$env_id"

  assert_cluster_ssh "$namespace_a_key" "$env_id"
  assert_command_fails "namespace B SSH to namespace A environment" run_cluster_cli --namespace "$namespace_b_key" ssh --id "$env_id" -- true
}

main() {
  trap cleanup EXIT
  precheck

  log "starting cluster e2e run $RUN_ID"
  start_infra
  start_nodes
  start_cluster_api
  run_cluster_case
  log "cluster e2e run passed"
}

main "$@"
