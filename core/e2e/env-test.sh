#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="http://localhost:3148"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-env-$(date +%Y%m%d%H%M%S)-$$"

TEMPLATE_KEYS=()
TEMPLATE_IDS=()
ENV_IDS=()
CREATED_TEMPLATE_ID=""
CREATED_ENV_ID=""

log() {
  printf '[env-test] %s\n' "$*"
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
  local removed_env_ids=" "
  local env_id
  set +e

  for template_id in "${TEMPLATE_IDS[@]}"; do
    if [ -n "$template_id" ]; then
      while IFS= read -r env_id; do
        if [ -n "$env_id" ]; then
          ENV_IDS+=("$env_id")
        fi
      done < <(run_cli env list --limit 5000 2>/dev/null | jq -r --arg template_id "$template_id" '.entries[] | select(.templateId == $template_id) | .id' 2>/dev/null || true)
    fi
  done

  for env_id in "${ENV_IDS[@]}"; do
    if [ -n "$env_id" ]; then
      if [[ "$removed_env_ids" == *" $env_id "* ]]; then
        continue
      fi

      removed_env_ids+="$env_id "
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
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' add firecracker"
  fi
}

json_get() {
  jq -r "$1"
}

create_template() {
  local key=$1
  local config=$2
  local output

  log "creating template $key"
  output="$(run_cli templates create "$key" --config "$config")"
  CREATED_TEMPLATE_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$CREATED_TEMPLATE_ID" ] || [ "$CREATED_TEMPLATE_ID" = "null" ]; then
    fail "template $key did not return an id"
  fi

  TEMPLATE_KEYS+=("$key")
  TEMPLATE_IDS+=("$CREATED_TEMPLATE_ID")
}

create_environment() {
  local key=$1
  local output

  log "creating environment from $key"
  output="$(run_cli env create --template "$key")"
  CREATED_ENV_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$CREATED_ENV_ID" ] || [ "$CREATED_ENV_ID" = "null" ]; then
    fail "environment from $key did not return an id"
  fi

  ENV_IDS+=("$CREATED_ENV_ID")
}

assert_environment_running() {
  local env_id=$1
  local status

  status="$(run_cli env get "$env_id" | json_get '.status')"
  if [ "$status" != "running" ]; then
    fail "environment $env_id status is $status, want running"
  fi
}

ssh_env() {
  local env_id=$1
  shift

  run_cli ssh "$env_id" -- "$@"
}

basic_setup_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-e2e /var/log/bastion-e2e"},
        {run: "set -eu\nprintf \"run_id=\($run_id)\\n\" > /opt/bastion-e2e/run-id\nprintf \"hostname=%s\\n\" \"$(hostname)\" > /opt/bastion-e2e/hostname"},
        {run: "set -eu\nuseradd -m bastione2e\nid bastione2e > /opt/bastion-e2e/user"},
        {run: "set -eu\nchmod 600 /opt/bastion-e2e/run-id\nprintf basic-complete > /opt/bastion-e2e/status"}
      ]
    }
  }'
}

node_docker_config() {
  jq -nc '{
    actions: {
      init: [
        {run: "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get update"},
        {run: "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get install -y --no-install-recommends nodejs docker.io ca-certificates"},
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-node\nnode --version > /opt/bastion-e2e-node/node-version\nnode -e '\''require(\"fs\").writeFileSync(\"/opt/bastion-e2e-node/app.txt\", \"node-ok\\n\")'\''"},
        {run: "set -eu\nmkdir -p /etc/docker /opt/bastion-e2e-docker\ndocker --version > /opt/bastion-e2e-docker/docker-version\nprintf '\''%s\\n'\'' '\''{\"log-driver\":\"json-file\",\"storage-driver\":\"overlay2\"}'\'' > /etc/docker/daemon.json"},
        {run: "set -eu\ntest -s /opt/bastion-e2e-node/node-version\ntest -s /opt/bastion-e2e-node/app.txt\ntest -s /opt/bastion-e2e-docker/docker-version\ntest -s /etc/docker/daemon.json"}
      ]
    }
  }'
}

preset_setup_node_config() {
  jq -nc '{
    actions: {
      init: [
        {use: "setup_node", with: {version: 24}},
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-preset\nnode --version > /opt/bastion-e2e-preset/node-version\nnpm --version > /opt/bastion-e2e-preset/npm-version"}
      ]
    }
  }'
}

env_substitution_config() {
  jq -nc '{
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-env\nprintf \"%s\\n\" \"${{ env.HOME }}\" > /opt/bastion-e2e-env/home"}
      ]
    }
  }'
}

failing_action_config() {
  jq -nc '{
    actions: {
      init: [
        {run: "set -eu\nprintf before > /tmp/bastion-e2e-before-failure"},
        {run: "set -eu\nprintf '\''intentional e2e failure\\n'\'' >&2\nexit 42"},
        {run: "set -eu\nprintf after > /tmp/bastion-e2e-after-failure"}
      ]
    }
  }'
}

assert_template_rejected() {
  local key="$RUN_ID-rejected"
  local config='{"actions":{"init":[],"start":[{"run":"echo should-be-rejected"}]}}'

  log "verifying schema rejects actions.start"
  if run_cli templates create "$key" --config "$config" >/tmp/bastion-env-test-rejected.out 2>&1; then
    TEMPLATE_KEYS+=("$key")
    fail "template containing actions.start was accepted"
  fi
}

run_basic_setup_case() {
  local key="$RUN_ID-basic"
  local first_env
  local second_env
  local run_id_mode

  create_template "$key" "$(basic_setup_config)"

  create_environment "$key"
  first_env="$CREATED_ENV_ID"
  assert_environment_running "$first_env"
  ssh_env "$first_env" grep -q "$RUN_ID" /opt/bastion-e2e/run-id
  ssh_env "$first_env" grep -q basic-complete /opt/bastion-e2e/status
  ssh_env "$first_env" id bastione2e >/dev/null
  run_id_mode="$(ssh_env "$first_env" stat -c %a /opt/bastion-e2e/run-id)"
  if [ "$run_id_mode" != "600" ]; then
    fail "run-id file mode in $first_env is $run_id_mode, want 600"
  fi

  create_environment "$key"
  second_env="$CREATED_ENV_ID"
  assert_environment_running "$second_env"
  ssh_env "$second_env" grep -q "$RUN_ID" /opt/bastion-e2e/run-id
  ssh_env "$second_env" test -s /opt/bastion-e2e/hostname

  log "basic setup case passed for $first_env and $second_env"
}

run_node_docker_case() {
  local key="$RUN_ID-node-docker"
  local env_id

  create_template "$key" "$(node_docker_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" grep -q '^v' /opt/bastion-e2e-node/node-version
  ssh_env "$env_id" grep -q node-ok /opt/bastion-e2e-node/app.txt
  ssh_env "$env_id" grep -q Docker /opt/bastion-e2e-docker/docker-version
  ssh_env "$env_id" grep -q overlay2 /etc/docker/daemon.json

  log "node/docker setup case passed for $env_id"
}

run_preset_setup_node_case() {
  local key="$RUN_ID-preset-node"
  local env_id

  create_template "$key" "$(preset_setup_node_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" grep -q '^v24\.' /opt/bastion-e2e-preset/node-version
  ssh_env "$env_id" test -s /opt/bastion-e2e-preset/npm-version

  log "preset setup_node case passed for $env_id"
}

run_env_substitution_case() {
  local key="$RUN_ID-env-substitution"
  local env_id

  create_template "$key" "$(env_substitution_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" test -s /opt/bastion-e2e-env/home
  ssh_env "$env_id" grep -q '^/' /opt/bastion-e2e-env/home
  if ssh_env "$env_id" "grep -F -q '\${{ env.HOME }}' /opt/bastion-e2e-env/home" 2>/dev/null; then
    fail "environment variable placeholder was not substituted in $env_id"
  fi

  log "environment substitution case passed for $env_id"
}

run_failure_case() {
  local key="$RUN_ID-fails"
  local template_id
  local output
  local failed_env_id
  local last_error

  create_template "$key" "$(failing_action_config)"
  template_id="$CREATED_TEMPLATE_ID"

  log "verifying failed init action marks environment error"
  if output="$(run_cli env create --template "$key" 2>&1)"; then
    CREATED_ENV_ID="$(json_get '.id' <<<"$output")"
    ENV_IDS+=("$CREATED_ENV_ID")
    fail "environment $CREATED_ENV_ID unexpectedly succeeded for failing template"
  fi

  if [[ "$output" != *"424 Failed Dependency"* ]]; then
    fail "failed init action returned unexpected create error: $output"
  fi

  failed_env_id="$(run_cli env list --limit 5000 | jq -r --arg template_id "$template_id" 'first(.entries[] | select(.templateId == $template_id and .status == "error") | .id) // ""')"
  if [ -z "$failed_env_id" ]; then
    fail "failed environment for template $template_id was not registered as error"
  fi

  ENV_IDS+=("$failed_env_id")
  last_error="$(run_cli env get "$failed_env_id" | json_get '.lastError // ""')"
  if [[ "$last_error" != *"init action 2 failed"* ]] || [[ "$last_error" != *"intentional e2e failure"* ]]; then
    fail "failed environment lastError was unexpected: $last_error"
  fi

  if [[ "$last_error" == *"ssh -i"* ]] || [[ "$last_error" == *"root@"* ]]; then
    fail "failed environment lastError leaked ssh wrapper details: $last_error"
  fi

  log "failure case passed for $failed_env_id"
}

main() {
  precheck
  trap cleanup EXIT

  log "starting environment e2e run $RUN_ID"
  assert_template_rejected
  run_basic_setup_case
  run_env_substitution_case
  run_preset_setup_node_case
  run_node_docker_case
  run_failure_case
  log "environment e2e run passed"
}

main "$@"
