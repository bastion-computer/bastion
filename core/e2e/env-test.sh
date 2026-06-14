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
PROXY_PIDS=()
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

  cleanup_proxies

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

cleanup_proxies() {
  local proxy_pid

  for proxy_pid in "${PROXY_PIDS[@]}"; do
    if [ -n "$proxy_pid" ]; then
      kill "$proxy_pid" >/dev/null 2>&1 || true
      wait "$proxy_pid" >/dev/null 2>&1 || true
    fi
  done

  PROXY_PIDS=()
}

cleanup_resources() {
  cleanup_proxies
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
  require_command curl
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

assert_opencode_proxy_health() {
  local label=$1
  local url=$2
  local output

  if ! output="$(curl -fsS --connect-timeout 5 --max-time 10 "$url" 2>&1)"; then
    fail "$label proxy health request failed: $output"
  fi

  if ! jq -e '.healthy == true' <<<"$output" >/dev/null; then
    fail "$label proxy health response is $(jq -c . <<<"$output"), want healthy true"
  fi
}

wait_for_proxy_url() {
  local logs=$1
  local line
  local i=0

  while [ "$i" -lt 50 ]; do
    if [ -s "$logs" ]; then
      while IFS= read -r line; do
        case "$line" in
          "proxy listening on "*)
            printf '%s\n' "${line#proxy listening on }"
            return 0
            ;;
        esac
      done <"$logs"
    fi

    i=$((i + 1))
    sleep 0.1
  done

  fail "proxy did not report a local URL: $(<"$logs")"
}

assert_bastion_opencode_attach() {
  local label=$1
  local ref_flag=$2
  local ref_value=$3
  local expected_url=$4
  local fake_bin="$CORE_DIR/tmp/opencode-e2e-$RUN_ID-$label"
  local proxy_file="$fake_bin/proxy-url"
  local got_url

  rm -rf "$fake_bin"
  mkdir -p "$fake_bin"
  cat >"$fake_bin/opencode" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 2 ] || [ "$1" != "attach" ]; then
  printf 'unexpected opencode invocation: %s\n' "$*" >&2
  exit 64
fi

curl -fsS --connect-timeout 5 --max-time 10 "$2/global/health" | jq -e '.healthy == true' >/dev/null
printf '%s\n' "$2" >"${BASTION_E2E_OPENCODE_PROXY_FILE:?}"
SH
  chmod +x "$fake_bin/opencode"

  if ! (export PATH="$fake_bin:$PATH" BASTION_E2E_OPENCODE_PROXY_FILE="$proxy_file"; run_cli opencode "$ref_flag" "$ref_value"); then
    fail "$label bastion opencode attach failed"
  fi

  if [ ! -s "$proxy_file" ]; then
    fail "$label fake opencode did not record a proxy URL"
  fi

  got_url="$(<"$proxy_file")"
  if [ "$got_url" != "$expected_url" ]; then
    fail "$label proxy URL was $got_url, want $expected_url"
  fi

  rm -rf "$fake_bin"
}

ssh_env() {
  local env_id=$1
  shift

  run_cli ssh --id "$env_id" -- "$@"
}

basic_setup_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    agents: {opencode: {}},
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
    agents: {opencode: {}},
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
    agents: {opencode: {}},
    actions: {
      init: [
        {use: "setup_node", with: {version: 24}},
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-preset\nnode --version > /opt/bastion-e2e-preset/node-version\nnpm --version > /opt/bastion-e2e-preset/npm-version"}
      ]
    }
  }'
}

preset_setup_bun_config() {
  jq -nc '{
    agents: {opencode: {}},
    actions: {
      init: [
        {use: "setup_bun"},
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-bun\nbun --version > /opt/bastion-e2e-bun/version\nbun --revision > /opt/bastion-e2e-bun/revision\nbun -e '\''console.log(\"bun-ok\")'\'' > /opt/bastion-e2e-bun/runtime"}
      ]
    }
  }'
}

preset_setup_mise_config() {
  jq -nc '{
    agents: {opencode: {}},
    actions: {
      init: [
        {use: "setup_mise"}
      ]
    }
  }'
}

preset_setup_rust_config() {
  local verify

  verify="$(printf '%s\n' \
    'set -eu' \
    'mkdir -p /opt/bastion-e2e-rust' \
    'rustc --version > /opt/bastion-e2e-rust/rustc-version' \
    'cargo --version > /opt/bastion-e2e-rust/cargo-version' \
    'rustup show active-toolchain > /opt/bastion-e2e-rust/active-toolchain' \
    "cat > /opt/bastion-e2e-rust/main.rs <<'EOF'" \
    'fn main() { println!("rust-e2e-ok"); }' \
    'EOF' \
    'rustc /opt/bastion-e2e-rust/main.rs -o /opt/bastion-e2e-rust/main' \
    '/opt/bastion-e2e-rust/main > /opt/bastion-e2e-rust/program-output')"

  jq -nc --arg verify "$verify" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {use: "setup_rust", with: {toolchain: "stable", profile: "minimal"}},
        {run: $verify}
      ]
    }
  }'
}

preset_setup_github_cli_config() {
  local token="github-cli-e2e-${RUN_ID}"
  local verify

  verify="$(printf '%s\n' \
    'set -eu' \
    'mkdir -p /opt/bastion-e2e-github-cli' \
    'gh --version > /opt/bastion-e2e-github-cli/version' \
    'gh config get git_protocol --host github.com > /opt/bastion-e2e-github-cli/git-protocol' \
    'git config --global user.name > /opt/bastion-e2e-github-cli/git-name' \
    'git config --global user.email > /opt/bastion-e2e-github-cli/git-email' \
    'git config --global --get credential.https://github.com.helper > /opt/bastion-e2e-github-cli/git-helper' \
    'test -n "$(gh auth token --hostname github.com)"' \
    "printf 'protocol=https\nhost=github.com\n\n' | git credential fill | grep -q 'password=$token'" \
    'printf github-cli-ready > /opt/bastion-e2e-github-cli/auth')"

  jq -nc --arg token "$token" --arg verify "$verify" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {use: "setup_github_cli", with: {token: $token, hostname: "github.com", git_protocol: "https"}},
        {run: $verify}
      ]
    }
  }'
}

preset_setup_docker_config() {
  local verify

  verify="$(printf '%s\n' \
    'set -eu' \
    'mkdir -p /opt/bastion-e2e-docker-preset' \
    'docker --version > /opt/bastion-e2e-docker-preset/docker-version' \
    'docker buildx version > /opt/bastion-e2e-docker-preset/buildx-version' \
    'docker compose version > /opt/bastion-e2e-docker-preset/compose-version' \
    'docker info --format "{{.ServerVersion}}" > /opt/bastion-e2e-docker-preset/server-version' \
    'systemctl is-enabled --quiet docker' \
    'systemctl is-active --quiet docker')"

  jq -nc --arg verify "$verify" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {use: "setup_docker"},
        {run: $verify}
      ]
    }
  }'
}

opencode_agent_config() {
  local api_key="opencode-e2e-${RUN_ID}"
  local verify_written
  local verify_started

  verify_written="$(printf '%s\n' \
    'set -eu' \
    'mkdir -p /opt/bastion-e2e-opencode' \
    'opencode --version > /opt/bastion-e2e-opencode/version' \
    'jq -e '\''.model == "anthropic/claude-sonnet-4-5" and .small_model == "anthropic/claude-haiku-4-5" and .share == "disabled" and .permission == "allow" and .autoupdate == false and .server.port == 4097'\'' /root/.config/opencode/opencode.json > /opt/bastion-e2e-opencode/config-ok' \
    "jq -e --arg api_key '$api_key' '.anthropic.type == \"api\" and .anthropic.key == \$api_key' /root/.local/share/opencode/auth.json > /opt/bastion-e2e-opencode/auth-ok" \
    'stat -c %a /root/.config/opencode/opencode.json > /opt/bastion-e2e-opencode/config-mode' \
    'stat -c %a /root/.local/share/opencode/auth.json > /opt/bastion-e2e-opencode/auth-mode' \
    'systemctl is-enabled --quiet bastion-opencode.service' \
    'grep -q "WorkingDirectory=/opt/bastion-e2e-opencode/workspace" /etc/systemd/system/bastion-opencode.service')"
  verify_started="$(printf '%s\n' \
    'set -eu' \
    'systemctl is-active --quiet bastion-opencode.service' \
    'curl -sS --connect-timeout 1 --max-time 2 http://127.0.0.1:4097/ > /opt/bastion-e2e-opencode/health' \
    'printf true > /opt/bastion-e2e-opencode/started-ok')"

  jq -nc --arg api_key "$api_key" --arg verify_written "$verify_written" --arg verify_started "$verify_started" '{
    agents: {
      opencode: {
        working_directory: "/opt/bastion-e2e-opencode/workspace",
        auth: {anthropic: {type: "api", key: $api_key}},
        config: {
          model: "anthropic/claude-sonnet-4-5",
          small_model: "anthropic/claude-haiku-4-5",
          share: "disabled",
          permission: "allow",
          autoupdate: false,
          server: {port: 4097}
        }
      }
    },
    actions: {
      init: [
        {run: $verify_written}
      ],
      start: [
        {run: $verify_started}
      ]
    }
  }'
}

env_substitution_config() {
  jq -nc '{
    agents: {opencode: {}},
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-env\nprintf \"%s\\n\" \"${{ env.HOME }}\" > /opt/bastion-e2e-env/home"}
      ]
    }
  }'
}

write_env_file_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {
          use: "write_env_file",
          with: {path: "/opt/bastion-e2e-write-env"},
          context: {
            BASTION_E2E_STATIC: "hello world",
            BASTION_E2E_HOME: "${{ env.HOME }}",
            BASTION_E2E_NUMBER: 42,
            BASTION_E2E_BOOL: true,
            BASTION_E2E_JSON: {run_id: $run_id}
          }
        }
      ]
    }
  }'
}

working_directory_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {run: "set -eu\nprintf \"run_id=\($run_id)\\n\" > run-id\npwd > pwd\nmkdir -p nested\nprintf working-directory-ready > nested/status", working_directory: "/opt/bastion-e2e-working/new-dir"},
        {run: "set -eu\ntest -s run-id\ntest \"$(pwd)\" = \"/opt/bastion-e2e-working/new-dir\"\nprintf verified > verified", working_directory: "/opt/bastion-e2e-working/new-dir"}
      ]
    }
  }'
}

start_action_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    agents: {opencode: {}},
    actions: {
      init: [
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-start\nprintf init-complete > /opt/bastion-e2e-start/init-status"}
      ],
      start: [
        {run: "set -eu\nmkdir -p /opt/bastion-e2e-start\nprintf \"run_id=\($run_id)\\n\" > /opt/bastion-e2e-start/run-id\nprintf \"start-stream-\($run_id)\\n\""},
        {run: "set -eu\nprintf \"$(pwd)\\n\" > pwd\nprintf working-directory-start > status", working_directory: "/opt/bastion-e2e-start/workdir"}
      ]
    }
  }'
}

tunnel_config() {
  jq -nc --arg run_id "$RUN_ID" '{
    agents: {opencode: {}},
    resources: {vcpu: 1, memory: 1, volume: 5},
    tunnel: {frontend: 3000},
    actions: {
      init: [
        {run: "set -eu\nexport DEBIAN_FRONTEND=noninteractive\nmkdir -p /opt/bastion-e2e-tunnel/absolute\nprintf \"tunnel-ok \($run_id)\\n\" > /opt/bastion-e2e-tunnel/index.html\nprintf \"proxy-path-ok \($run_id)\\n\" > /opt/bastion-e2e-tunnel/absolute/path\nif ! command -v python3 >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1; then apt-get update; apt-get install -y --no-install-recommends python3 curl ca-certificates; fi"}
      ],
      start: [
        {run: "set -eu\ncd /opt/bastion-e2e-tunnel\nnohup python3 -m http.server 3000 --bind 127.0.0.1 > /opt/bastion-e2e-tunnel/server.log 2>&1 &\nprintf \"%s\\n\" \"$!\" > /opt/bastion-e2e-tunnel/server.pid\ni=0\nwhile [ \"$i\" -lt 50 ]; do\n  if curl -fsS --connect-timeout 1 --max-time 2 http://127.0.0.1:3000/ > /opt/bastion-e2e-tunnel/health 2>/dev/null; then\n    exit 0\n  fi\n  i=$((i + 1))\n  sleep 1\ndone\ncat /opt/bastion-e2e-tunnel/server.log >&2 || true\nprintf \"local tunnel server did not become ready\\n\" >&2\nexit 1"}
      ]
    }
  }'
}

failing_action_config() {
  jq -nc '{
    agents: {opencode: {}},
    actions: {
      init: [
        {run: "set -eu\nprintf before > /tmp/bastion-e2e-before-failure"},
        {run: "set -eu\nprintf '\''intentional e2e failure\\n'\'' >&2\nexit 42"},
        {run: "set -eu\nprintf after > /tmp/bastion-e2e-after-failure"}
      ]
    }
  }'
}

failing_start_action_config() {
  jq -nc '{
    agents: {opencode: {}},
    actions: {
      init: [],
      start: [
        {run: "set -eu\nprintf before > /tmp/bastion-e2e-start-before-failure"},
        {run: "set -eu\nprintf '\''intentional start failure\\n'\'' >&2\nexit 42"},
        {run: "set -eu\nprintf after > /tmp/bastion-e2e-start-after-failure"}
      ]
    }
  }'
}

run_start_action_case() {
  local key="$RUN_ID-start"
  local env_id
  local output
  local logs
  local log_contents

  create_template "$key" "$(start_action_config)"

  logs="/tmp/bastion-env-test-start-$RUN_ID.log"
  log "creating environment from $key with start actions"
  if ! output="$(run_cli env create --template-key "$key" 2>"$logs")"; then
    fail "environment from $key failed: $output $(<"$logs")"
  fi

  CREATED_ENV_ID="$(json_get '.id' <<<"$output")"
  CREATED_ENV_OUTPUT="$output"
  if [ -z "$CREATED_ENV_ID" ] || [ "$CREATED_ENV_ID" = "null" ]; then
    fail "environment from $key did not return an id"
  fi

  ENV_IDS+=("$CREATED_ENV_ID")
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  log_contents="$(<"$logs")"
  if [[ "$log_contents" != *"start-stream-$RUN_ID"* ]]; then
    fail "env create did not stream start action output: $log_contents"
  fi

  ssh_env "$env_id" grep -q "$RUN_ID" /opt/bastion-e2e-start/run-id
  ssh_env "$env_id" grep -q init-complete /opt/bastion-e2e-start/init-status
  ssh_env "$env_id" grep -q '^/opt/bastion-e2e-start/workdir$' /opt/bastion-e2e-start/workdir/pwd
  ssh_env "$env_id" grep -q working-directory-start /opt/bastion-e2e-start/workdir/status

  log "start action case passed for $env_id"
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

  first_ip="$(ssh_env "$first_env" "ip -4 -o addr show dev eth0 | tr -s ' ' | cut -d' ' -f4")"
  second_ip="$(ssh_env "$second_env" "ip -4 -o addr show dev eth0 | tr -s ' ' | cut -d' ' -f4")"
  if [ -z "$first_ip" ] || [ -z "$second_ip" ] || [ "$first_ip" = "$second_ip" ]; then
    fail "concurrent environments have non-unique DHCP addresses: first=$first_ip second=$second_ip"
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

run_preset_setup_bun_case() {
  local key="$RUN_ID-preset-bun"
  local env_id

  create_template "$key" "$(preset_setup_bun_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" "grep -Eq '^[0-9]+\.' /opt/bastion-e2e-bun/version"
  ssh_env "$env_id" test -s /opt/bastion-e2e-bun/revision
  ssh_env "$env_id" grep -q '^bun-ok$' /opt/bastion-e2e-bun/runtime

  log "preset setup_bun case passed for $env_id"
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

run_preset_setup_rust_case() {
  local key="$RUN_ID-preset-rust"
  local env_id

  create_template "$key" "$(preset_setup_rust_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" "grep -q '^rustc ' /opt/bastion-e2e-rust/rustc-version"
  ssh_env "$env_id" "grep -q '^cargo ' /opt/bastion-e2e-rust/cargo-version"
  ssh_env "$env_id" "grep -q '^stable' /opt/bastion-e2e-rust/active-toolchain"
  ssh_env "$env_id" grep -q '^rust-e2e-ok$' /opt/bastion-e2e-rust/program-output

  log "preset setup_rust case passed for $env_id"
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
  ssh_env "$env_id" grep -q '^bastion-agent$' /opt/bastion-e2e-github-cli/git-name
  ssh_env "$env_id" grep -q '^agent@bastion.computer$' /opt/bastion-e2e-github-cli/git-email
  ssh_env "$env_id" "grep -q '/usr/local/bin/gh auth git-credential' /opt/bastion-e2e-github-cli/git-helper"
  ssh_env "$env_id" grep -q github-cli-ready /opt/bastion-e2e-github-cli/auth

  log "preset setup_github_cli case passed for $env_id"
}

run_preset_setup_docker_case() {
  local key="$RUN_ID-preset-docker"
  local env_id

  create_template "$key" "$(preset_setup_docker_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" "grep -q '^Docker version' /opt/bastion-e2e-docker-preset/docker-version"
  ssh_env "$env_id" grep -q 'buildx' /opt/bastion-e2e-docker-preset/buildx-version
  ssh_env "$env_id" "grep -q 'Docker Compose' /opt/bastion-e2e-docker-preset/compose-version"
  ssh_env "$env_id" test -s /opt/bastion-e2e-docker-preset/server-version

  log "preset setup_docker case passed for $env_id"
}

run_opencode_agent_case() {
  local key="$RUN_ID-opencode-agent"
  local env_key="$RUN_ID-opencode-agent-env"
  local env_id

  create_template "$key" "$(opencode_agent_config)"
  create_environment "$key" --key "$env_key"
  env_id="$CREATED_ENV_ID"
  assert_json_key "env create $env_id" "$CREATED_ENV_OUTPUT" "$env_key"
  assert_environment_running "$env_id"

  ssh_env "$env_id" "set -eu
test -s /opt/bastion-e2e-opencode/version
test -s /opt/bastion-e2e-opencode/health
grep -q true /opt/bastion-e2e-opencode/config-ok
grep -q true /opt/bastion-e2e-opencode/auth-ok
grep -q true /opt/bastion-e2e-opencode/started-ok
config_mode=\$(cat /opt/bastion-e2e-opencode/config-mode)
if [ \"\$config_mode\" != \"600\" ]; then
  printf 'config mode is %s, want 600\n' \"\$config_mode\" >&2
  exit 1
fi
auth_mode=\$(cat /opt/bastion-e2e-opencode/auth-mode)
if [ \"\$auth_mode\" != \"600\" ]; then
  printf 'auth mode is %s, want 600\n' \"\$auth_mode\" >&2
  exit 1
fi"

  assert_opencode_proxy_health "OpenCode id route" "${API_URL%/}/v1/environments/$env_id/agents/opencode/global/health"
  assert_opencode_proxy_health "OpenCode key route" "${API_URL%/}/v1/environments/by-key/$env_key/agents/opencode/global/health"
  assert_bastion_opencode_attach "OpenCode CLI id route" --id "$env_id" "${API_URL%/}/v1/environments/$env_id/agents/opencode"
  assert_bastion_opencode_attach "OpenCode CLI key route" --key "$env_key" "${API_URL%/}/v1/environments/by-key/$env_key/agents/opencode"

  log "OpenCode agent case passed for $env_id"
}

run_tunnel_case() {
  local key="$RUN_ID-tunnel"
  local env_key="$RUN_ID-tunnel-env"
  local env_id
  local expected_id_url
  local expected_key_url
  local output
  local proxy_logs
  local proxy_url

  create_template "$key" "$(tunnel_config)"
  create_environment "$key" --key "$env_key"
  env_id="$CREATED_ENV_ID"
  assert_json_key "env create $env_id" "$CREATED_ENV_OUTPUT" "$env_key"
  assert_environment_running "$env_id"

  expected_id_url="${API_URL%/}/v1/environments/$env_id/tunnel/frontend"
  expected_key_url="${API_URL%/}/v1/environments/by-key/$env_key/tunnel/frontend"

  output="$(run_cli env tunnels --id "$env_id")"
  if ! jq -e --arg url "$expected_id_url" '.entries | length == 1 and .[0].name == "frontend" and .[0].port == 3000 and .[0].url == $url' <<<"$output" >/dev/null; then
    fail "env tunnels --id response is $(jq -c . <<<"$output"), want frontend tunnel URL $expected_id_url"
  fi

  output="$(run_cli env tunnels --key "$env_key")"
  if ! jq -e --arg url "$expected_key_url" '.entries | length == 1 and .[0].name == "frontend" and .[0].port == 3000 and .[0].url == $url' <<<"$output" >/dev/null; then
    fail "env tunnels --key response is $(jq -c . <<<"$output"), want frontend tunnel URL $expected_key_url"
  fi

  output="$(curl -fsS --connect-timeout 5 --max-time 20 "$expected_id_url")"
  if [ "$output" != "tunnel-ok $RUN_ID" ]; then
    fail "tunnel id URL returned $output, want tunnel-ok $RUN_ID"
  fi

  output="$(curl -fsS --connect-timeout 5 --max-time 20 "$expected_key_url")"
  if [ "$output" != "tunnel-ok $RUN_ID" ]; then
    fail "tunnel key URL returned $output, want tunnel-ok $RUN_ID"
  fi

  proxy_logs="/tmp/bastion-env-test-proxy-$RUN_ID.log"
  : >"$proxy_logs"
  run_cli proxy --env-key "$env_key" --name frontend 2>"$proxy_logs" &
  PROXY_PIDS+=("$!")
  proxy_url="$(wait_for_proxy_url "$proxy_logs")"

  output="$(curl -fsS --connect-timeout 5 --max-time 20 "$proxy_url/absolute/path")"
  if [ "$output" != "proxy-path-ok $RUN_ID" ]; then
    fail "local proxy returned $output, want proxy-path-ok $RUN_ID"
  fi

  if [[ "$(<"$proxy_logs")" != *"GET /absolute/path -> 200"* ]]; then
    fail "local proxy did not log the proxied request: $(<"$proxy_logs")"
  fi

  log "tunnel case passed for $env_id"
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

run_write_env_file_case() {
  local key="$RUN_ID-write-env-file"
  local env_id

  create_template "$key" "$(write_env_file_config)"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  assert_environment_running "$env_id"

  ssh_env "$env_id" "set -eu
env_file=/opt/bastion-e2e-write-env/.env
test -s \"\$env_file\"
mode=\$(stat -c %a \"\$env_file\")
if [ \"\$mode\" != 600 ]; then
  printf 'env file mode is %s, want 600\n' \"\$mode\" >&2
  exit 1
fi
. \"\$env_file\"
test \"\$BASTION_E2E_STATIC\" = 'hello world'
case \"\$BASTION_E2E_HOME\" in
  /*) ;;
  *) printf 'BASTION_E2E_HOME was %s, want absolute path\n' \"\$BASTION_E2E_HOME\" >&2; exit 1 ;;
esac
test \"\$BASTION_E2E_NUMBER\" = 42
test \"\$BASTION_E2E_BOOL\" = true
printf '%s' \"\$BASTION_E2E_JSON\" | jq -e --arg run_id '$RUN_ID' '.run_id == \$run_id' >/dev/null
test ! -e /opt/bastion/actions/init-1-write_env_file/.bastion-context.json"

  log "write_env_file case passed for $env_id"
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
  local output

  log "verifying failed init action rejects template creation"
  if output="$(run_cli templates create --key "$key" --config "$(failing_action_config)" 2>&1)"; then
    CREATED_TEMPLATE_ID="$(json_get '.id // empty' <<<"$output")"
    if [ -n "$CREATED_TEMPLATE_ID" ]; then
      TEMPLATE_IDS+=("$CREATED_TEMPLATE_ID")
    fi
    fail "template $key unexpectedly succeeded despite failing init action"
  fi

  if [[ "$output" != *"424 Failed Dependency"* ]]; then
    fail "failed init action returned unexpected template create error: $output"
  fi

  if [[ "$output" != *"init action 2 failed"* ]] || [[ "$output" != *"intentional e2e failure"* ]]; then
    fail "failed template create error was unexpected: $output"
  fi

  if [[ "$output" == *"ssh -i"* ]] || [[ "$output" == *"root@"* ]]; then
    fail "failed template create error leaked ssh wrapper details: $output"
  fi

  if run_cli templates get --key "$key" >/dev/null 2>&1; then
    fail "failed template $key was registered"
  fi

  log "failure case passed for template $key"
}

run_start_failure_case() {
  local key="$RUN_ID-start-fails"
  local output

  create_template "$key" "$(failing_start_action_config)"

  log "verifying failed start action rejects environment creation"
  if output="$(run_cli env create --template-key "$key" 2>&1)"; then
    CREATED_ENV_ID="$(json_get '.id // empty' <<<"$output")"
    if [ -n "$CREATED_ENV_ID" ]; then
      ENV_IDS+=("$CREATED_ENV_ID")
    fi
    fail "environment from $key unexpectedly succeeded despite failing start action"
  fi

  if [[ "$output" != *"424 Failed Dependency"* ]]; then
    fail "failed start action returned unexpected env create error: $output"
  fi

  if [[ "$output" != *"start action 2 failed"* ]] || [[ "$output" != *"intentional start failure"* ]]; then
    fail "failed env create error was unexpected: $output"
  fi

  if [[ "$output" == *"ssh -i"* ]] || [[ "$output" == *"root@"* ]]; then
    fail "failed env create error leaked ssh wrapper details: $output"
  fi

  log "start failure case passed for template $key"
}

main() {
  precheck
  trap cleanup EXIT

  log "starting environment e2e run $RUN_ID"
  run_case run_optional_template_key_case
  run_case run_basic_setup_case
  run_case run_env_substitution_case
  run_case run_write_env_file_case
  run_case run_working_directory_case
  run_case run_start_action_case
  run_case run_preset_setup_node_case
  run_case run_preset_setup_bun_case
  run_case run_preset_setup_mise_case
  run_case run_preset_setup_rust_case
  run_case run_preset_setup_github_cli_case
  run_case run_preset_setup_docker_case
  run_case run_opencode_agent_case
  run_case run_tunnel_case
  run_case run_node_docker_case
  run_case run_failure_case
  run_case run_start_failure_case
  log "environment e2e run passed"
}

main "$@"
