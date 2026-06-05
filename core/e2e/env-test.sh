#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-env-$(date +%Y%m%d%H%M%S)-$$"

TEMPLATE_KEYS=()
TEMPLATE_IDS=()
ENV_IDS=()
CREATED_TEMPLATE_ID=""
CREATED_ENV_ID=""
CREATED_ENV_OUTPUT=""

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
  set +e

  if [ "$status" -ne 0 ] && [ "${BASTION_E2E_KEEP_FAILED:-}" = "1" ]; then
    log "preserving failed run resources: environments=${ENV_IDS[*]:-} templates=${TEMPLATE_IDS[*]:-} template_keys=${TEMPLATE_KEYS[*]:-}"
    exit "$status"
  fi

  cleanup_resources

  exit "$status"
}

collect_template_environments() {
  local template_id
  local env_id

  for template_id in "${TEMPLATE_IDS[@]}"; do
    if [ -n "$template_id" ]; then
      while IFS= read -r env_id; do
        if [ -n "$env_id" ]; then
          ENV_IDS+=("$env_id")
        fi
      done < <(run_cli env list --limit 5000 2>/dev/null | jq -r --arg template_id "$template_id" '.entries[] | select(.templateId == $template_id) | .id' 2>/dev/null || true)
    fi
  done
}

cleanup_environments() {
  local removed_env_ids=" "
  local env_id

  for env_id in "${ENV_IDS[@]}"; do
    if [ -n "$env_id" ]; then
      if [[ "$removed_env_ids" == *" $env_id "* ]]; then
        continue
      fi

      removed_env_ids+="$env_id "
      run_cli env remove --id "$env_id" >/dev/null 2>&1 || log "cleanup: environment $env_id was not removed"
    fi
  done

  ENV_IDS=()
}

cleanup_templates() {
  local template_id

  for template_id in "${TEMPLATE_IDS[@]}"; do
    if [ -n "$template_id" ]; then
      run_cli templates remove --id "$template_id" >/dev/null 2>&1 || log "cleanup: template $template_id was not removed"
    fi
  done

  TEMPLATE_KEYS=()
  TEMPLATE_IDS=()
}

cleanup_resources() {
  collect_template_environments
  cleanup_environments
  cleanup_templates

  CREATED_TEMPLATE_ID=""
  CREATED_ENV_ID=""
  CREATED_ENV_OUTPUT=""
}

run_case() {
  "$@"
  cleanup_resources
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
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' add cloud-hypervisor"
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
  output="$(run_cli templates create --key "$key" --config "$config")"
  CREATED_TEMPLATE_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$CREATED_TEMPLATE_ID" ] || [ "$CREATED_TEMPLATE_ID" = "null" ]; then
    fail "template $key did not return an id"
  fi

  if ! jq -e --arg key "$key" '.key == $key' <<<"$output" >/dev/null; then
    fail "template $key response key is $(jq -c '.key // null' <<<"$output"), want $key"
  fi

  TEMPLATE_KEYS+=("$key")
  TEMPLATE_IDS+=("$CREATED_TEMPLATE_ID")
}

create_unkeyed_template() {
  local config=$1
  local output

  log "creating unkeyed template"
  output="$(run_cli templates create --config "$config")"
  CREATED_TEMPLATE_ID="$(json_get '.id' <<<"$output")"

  if [ -z "$CREATED_TEMPLATE_ID" ] || [ "$CREATED_TEMPLATE_ID" = "null" ]; then
    fail "unkeyed template did not return an id"
  fi

  assert_json_no_key "unkeyed template create" "$output"
  TEMPLATE_IDS+=("$CREATED_TEMPLATE_ID")
}

create_environment() {
  local key=$1
  shift
  local output

  log "creating environment from $key"
  output="$(run_cli env create --template-key "$key" "$@")"
  CREATED_ENV_ID="$(json_get '.id' <<<"$output")"
  CREATED_ENV_OUTPUT="$output"

  if [ -z "$CREATED_ENV_ID" ] || [ "$CREATED_ENV_ID" = "null" ]; then
    fail "environment from $key did not return an id"
  fi

  ENV_IDS+=("$CREATED_ENV_ID")
}

assert_json_tags() {
  local label=$1
  local json=$2
  shift 2
  local expected

  expected="$(jq -nc '$ARGS.positional' --args "$@")"
  if ! jq -e --argjson expected "$expected" '.tags == $expected' <<<"$json" >/dev/null; then
    fail "$label tags are $(jq -c '.tags' <<<"$json"), want $expected"
  fi
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

assert_environment_tags() {
  local env_id=$1
  shift
  local output

  output="$(run_cli env get --id "$env_id")"
  assert_json_tags "environment $env_id" "$output" "$@"
}

assert_environment_list_tags() {
  local env_id=$1
  shift
  local args=()
  local output

  for tag in "$@"; do
    args+=(--tag "$tag")
  done

  output="$(run_cli env list --limit 5000 "${args[@]}")"
  if ! jq -e --arg env_id "$env_id" '.entries as $entries | ($entries | length) == 1 and $entries[0].id == $env_id' <<<"$output" >/dev/null; then
    fail "tag-filtered environment list did not return only $env_id: $(jq -c '.entries | map(.id)' <<<"$output")"
  fi

  assert_json_tags "tag-filtered environment $env_id" "$(jq -c '.entries[0]' <<<"$output")" "$@"
}

assert_environment_running() {
  local env_id=$1
  local status

  status="$(run_cli env get --id "$env_id" | json_get '.status')"
  if [ "$status" != "running" ]; then
    fail "environment $env_id status is $status, want running"
  fi
}

ssh_env() {
  local env_id=$1
  shift

  run_cli ssh --id "$env_id" -- "$@"
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

preset_setup_mise_config() {
  jq -nc '{
    actions: {
      init: [
        {use: "setup_mise"}
      ]
    }
  }'
}

preset_setup_github_cli_config() {
  local token="github-cli-e2e-${RUN_ID}"

  jq -nc --arg token "$token" '{
    actions: {
      init: [
        {use: "setup_github_cli", with: {token: $token, hostname: "github.com", git_protocol: "https"}},
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-github-cli\ngh --version > /opt/bastion-e2e-github-cli/version\ngh config get git_protocol --host github.com > /opt/bastion-e2e-github-cli/git-protocol\ntest -n \"$(gh auth token --hostname github.com)\"\nprintf github-cli-ready > /opt/bastion-e2e-github-cli/auth"}
      ]
    }
  }'
}

preset_setup_opencode_config() {
  local api_key="opencode-e2e-${RUN_ID}"
  local auth
  local config
  local tui
  local verify_written
  local verify_absent

  auth="$(jq -nc --arg api_key "$api_key" '{anthropic: {type: "api", key: $api_key}}')"
  config="$(jq -nc '{model: "anthropic/claude-sonnet-4-5", small_model: "anthropic/claude-haiku-4-5", share: "disabled", permission: "allow", autoupdate: false}')"
  tui="$(jq -nc '{mouse: true}')"
  verify_written="$(printf '%s\n' \
    'set -eu' \
    'mkdir -p /opt/bastion-e2e-opencode' \
    'opencode --version > /opt/bastion-e2e-opencode/version' \
    'jq -e '\''.model == "anthropic/claude-sonnet-4-5" and .small_model == "anthropic/claude-haiku-4-5" and .share == "disabled" and .permission == "allow" and .autoupdate == false'\'' /root/.config/opencode/opencode.json > /opt/bastion-e2e-opencode/config-ok' \
    'jq -e '\''.mouse == true'\'' /root/.config/opencode/tui.json > /opt/bastion-e2e-opencode/tui-ok' \
    "jq -e --arg api_key '$api_key' '.anthropic.type == \"api\" and .anthropic.key == \$api_key' /root/.local/share/opencode/auth.json > /opt/bastion-e2e-opencode/auth-ok" \
    'stat -c %a /root/.config/opencode/opencode.json > /opt/bastion-e2e-opencode/config-mode' \
    'stat -c %a /root/.config/opencode/tui.json > /opt/bastion-e2e-opencode/tui-mode' \
    'stat -c %a /root/.local/share/opencode/auth.json > /opt/bastion-e2e-opencode/auth-mode' \
    'rm -f /root/.config/opencode/opencode.json /root/.config/opencode/tui.json /root/.local/share/opencode/auth.json')"
  verify_absent="$(printf '%s\n' \
    'set -eu' \
    'opencode --version > /opt/bastion-e2e-opencode/version-no-inputs' \
    'test ! -e /root/.config/opencode/opencode.json' \
    'test ! -e /root/.local/share/opencode/auth.json' \
    'jq -e '\''.mouse == false and .keybinds.input_paste == "none"'\'' /root/.config/opencode/tui.json > /opt/bastion-e2e-opencode/default-tui-ok' \
    'printf true > /opt/bastion-e2e-opencode/absent-ok')"

  jq -nc --arg auth "$auth" --arg config "$config" --arg tui "$tui" --arg verify_written "$verify_written" --arg verify_absent "$verify_absent" '{
    actions: {
      init: [
        {use: "setup_opencode", with: {auth: $auth, config: $config, tui: $tui}},
        {run: $verify_written},
        {use: "setup_opencode"},
        {run: $verify_absent}
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

working_directory_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    actions: {
      init: [
        {run: "set -eu\nprintf \"run_id=\($run_id)\\n\" > run-id\npwd > pwd\nmkdir -p nested\nprintf working-directory-ready > nested/status", working_directory: "/opt/bastion-e2e-working/new-dir"},
        {run: "set -eu\ntest -s run-id\ntest \"$(pwd)\" = \"/opt/bastion-e2e-working/new-dir\"\nprintf verified > verified", working_directory: "/opt/bastion-e2e-working/new-dir"}
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
  local output

  log "verifying schema rejects actions.start"
  if output="$(run_cli templates create --key "$key" --config "$config" 2>&1)"; then
    CREATED_TEMPLATE_ID="$(json_get '.id // empty' <<<"$output")"
    TEMPLATE_KEYS+=("$key")
    if [ -n "$CREATED_TEMPLATE_ID" ]; then
      TEMPLATE_IDS+=("$CREATED_TEMPLATE_ID")
    fi
    fail "template containing actions.start was accepted"
  else
    printf '%s\n' "$output" >/tmp/bastion-env-test-rejected.out
  fi
}

run_optional_template_key_case() {
  local template_id
  local output

  create_unkeyed_template "$(basic_setup_config)"
  template_id="$CREATED_TEMPLATE_ID"

  output="$(run_cli templates get --id "$template_id")"
  assert_json_no_key "unkeyed template get" "$output"

  log "optional template key case passed for $template_id"
}

run_basic_setup_case() {
  local key="$RUN_ID-basic"
  local first_env
  local second_env
  local first_env_key="$RUN_ID-basic-env"
  local output
  local run_id_mode
  local first_env_tag="$RUN_ID-basic-first"
  local shared_tag="$RUN_ID-basic"

  create_template "$key" "$(basic_setup_config)"

  create_environment "$key" --key "$first_env_key" -t "$shared_tag" --tag "$first_env_tag"
  first_env="$CREATED_ENV_ID"
  assert_json_key "env create $first_env" "$CREATED_ENV_OUTPUT" "$first_env_key"
  assert_json_tags "env create $first_env" "$CREATED_ENV_OUTPUT" "$shared_tag" "$first_env_tag"
  assert_environment_running "$first_env"
  assert_environment_tags "$first_env" "$shared_tag" "$first_env_tag"
  assert_environment_list_tags "$first_env" "$shared_tag" "$first_env_tag"
  output="$(run_cli env get --key "$first_env_key")"
  if [ "$(json_get '.id' <<<"$output")" != "$first_env" ]; then
    fail "environment key lookup returned $(json_get '.id' <<<"$output"), want $first_env"
  fi
  assert_json_key "env get --key $first_env_key" "$output" "$first_env_key"

  cleanup_environments

  create_environment "$key"
  second_env="$CREATED_ENV_ID"
  assert_json_no_key "env create $second_env" "$CREATED_ENV_OUTPUT"
  assert_environment_running "$second_env"
  ssh_env "$second_env" grep -q "$RUN_ID" /opt/bastion-e2e/run-id
  ssh_env "$second_env" grep -q basic-complete /opt/bastion-e2e/status
  ssh_env "$second_env" id bastione2e >/dev/null
  ssh_env "$second_env" test -s /opt/bastion-e2e/hostname
  run_id_mode="$(ssh_env "$second_env" stat -c %a /opt/bastion-e2e/run-id)"
  if [ "$run_id_mode" != "600" ]; then
    fail "run-id file mode in $second_env is $run_id_mode, want 600"
  fi

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

run_preset_setup_mise_case() {
  local key="$RUN_ID-preset-mise"
  local env_id
  local version

  create_template "$key" "$(preset_setup_mise_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  version="$(ssh_env "$env_id" mise --version)"
  if [[ ! "$version" =~ ^(mise[[:space:]])?[0-9] ]]; then
    fail "mise --version returned unexpected output: $version"
  fi

  log "preset setup_mise case passed for $env_id"
}

run_preset_setup_github_cli_case() {
  local key="$RUN_ID-preset-github-cli"
  local env_id

  create_template "$key" "$(preset_setup_github_cli_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" "grep -q '^gh version' /opt/bastion-e2e-github-cli/version"
  ssh_env "$env_id" grep -q '^https$' /opt/bastion-e2e-github-cli/git-protocol
  ssh_env "$env_id" grep -q github-cli-ready /opt/bastion-e2e-github-cli/auth

  log "preset setup_github_cli case passed for $env_id"
}

run_preset_setup_opencode_case() {
  local key="$RUN_ID-preset-opencode"
  local env_id

  create_template "$key" "$(preset_setup_opencode_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" "set -eu
test -s /opt/bastion-e2e-opencode/version
test -s /opt/bastion-e2e-opencode/version-no-inputs
grep -q true /opt/bastion-e2e-opencode/config-ok
grep -q true /opt/bastion-e2e-opencode/tui-ok
grep -q true /opt/bastion-e2e-opencode/auth-ok
grep -q true /opt/bastion-e2e-opencode/absent-ok
grep -q true /opt/bastion-e2e-opencode/default-tui-ok
config_mode=\$(cat /opt/bastion-e2e-opencode/config-mode)
if [ \"\$config_mode\" != \"600\" ]; then
  printf 'config mode is %s, want 600\n' \"\$config_mode\" >&2
  exit 1
fi
tui_mode=\$(cat /opt/bastion-e2e-opencode/tui-mode)
if [ \"\$tui_mode\" != \"600\" ]; then
  printf 'tui mode is %s, want 600\n' \"\$tui_mode\" >&2
  exit 1
fi
auth_mode=\$(cat /opt/bastion-e2e-opencode/auth-mode)
if [ \"\$auth_mode\" != \"600\" ]; then
  printf 'auth mode is %s, want 600\n' \"\$auth_mode\" >&2
  exit 1
fi"

  log "preset setup_opencode case passed for $env_id"
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

run_working_directory_case() {
  local key="$RUN_ID-working-directory"
  local env_id

  create_template "$key" "$(working_directory_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" grep -q "$RUN_ID" /opt/bastion-e2e-working/new-dir/run-id
  ssh_env "$env_id" grep -q '^/opt/bastion-e2e-working/new-dir$' /opt/bastion-e2e-working/new-dir/pwd
  ssh_env "$env_id" grep -q working-directory-ready /opt/bastion-e2e-working/new-dir/nested/status
  ssh_env "$env_id" grep -q verified /opt/bastion-e2e-working/new-dir/verified

  log "working_directory case passed for $env_id"
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
  if output="$(run_cli env create --template-key "$key" 2>&1)"; then
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
  last_error="$(run_cli env get --id "$failed_env_id" | json_get '.lastError // ""')"
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
  run_case run_optional_template_key_case
  run_case run_basic_setup_case
  run_case run_env_substitution_case
  run_case run_working_directory_case
  run_case run_preset_setup_node_case
  run_case run_preset_setup_mise_case
  run_case run_preset_setup_github_cli_case
  run_case run_preset_setup_opencode_case
  run_case run_node_docker_case
  run_case run_failure_case
  log "environment e2e run passed"
}

main "$@"
