#!/usr/bin/env bash
set -euo pipefail

SERVICE_USER="bastion-e2e"

INSTALL_URL=""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CORE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$CORE_DIR/.." && pwd)"

BASTION="$CORE_DIR/tmp/bastion"
API_URL="${BASTION_API_URL:-http://localhost:3148}"
DATA_DIR="$REPO_DIR/.bastion"
RUN_ID="e2e-install-$(date +%Y%m%d%H%M%S)-$$"
DOCS_PORT="${BASTION_INSTALL_E2E_DOCS_PORT:-44321}"
DOCS_HOST=""
DOCS_SOURCE_CIDR="${BASTION_INSTALL_E2E_SOURCE_CIDR:-${BASTION_VM_NETWORK_PREFIX:-10.241}.0.0/16}"
DOCS_LOG="$CORE_DIR/tmp/install-docs-server.log"
DOCS_PID=""
DOCS_FIREWALL_RULE_ADDED=0
LATEST_VERSION=""
LOCAL_RELEASE_VERSION="${BASTION_INSTALL_E2E_VERSION:-dev}"
LOCAL_RELEASE_DIR="$REPO_DIR/docs/public/e2e-releases/$LOCAL_RELEASE_VERSION"

TEMPLATE_KEYS=()
ENV_IDS=()
CREATED_ENV_ID=""

log() {
  printf '[install-test] %s\n' "$*"
}

fail() {
  log "FAIL: $*" >&2
  exit 1
}

run_cli() {
  "$BASTION" --api-url "$API_URL" "$@"
}

stop_docs_server() {
  if [ -n "$DOCS_PID" ]; then
    kill "$DOCS_PID" >/dev/null 2>&1 || true
    wait "$DOCS_PID" >/dev/null 2>&1 || true
    DOCS_PID=""
  fi
}

remove_docs_firewall_rule() {
  if [ "$DOCS_FIREWALL_RULE_ADDED" -eq 1 ]; then
    sudo -n iptables -D INPUT -p tcp -s "$DOCS_SOURCE_CIDR" --dport "$DOCS_PORT" -j ACCEPT >/dev/null 2>&1 || true
    DOCS_FIREWALL_RULE_ADDED=0
  fi
}

cleanup() {
  local status=$?
  local env_id
  local template_key

  set +e
  stop_docs_server
  remove_docs_firewall_rule
  rm -rf "$LOCAL_RELEASE_DIR"
  rmdir "${LOCAL_RELEASE_DIR%/*}" >/dev/null 2>&1 || true

  if [ "$status" -ne 0 ] && [ "${BASTION_E2E_KEEP_FAILED:-}" = "1" ]; then
    log "preserving failed run resources: environments=${ENV_IDS[*]:-} templates=${TEMPLATE_KEYS[*]:-}"
    exit "$status"
  fi

  for env_id in "${ENV_IDS[@]}"; do
    if [ -n "$env_id" ]; then
      run_cli env remove --id "$env_id" >/dev/null 2>&1 || log "cleanup: environment $env_id was not removed"
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

latest_release_tag() {
  local url="https://github.com/bastion-computer/bastion/releases/latest"
  local effective_url
  local tag

  effective_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$url")"
  effective_url="${effective_url%/}"
  tag="${effective_url##*/}"
  tag="${tag%%\?*}"

  if [ -z "$tag" ] || [ "$tag" = "latest" ]; then
    fail "could not resolve latest Bastion release from $url"
  fi

  printf '%s\n' "$tag"
}

host_network_ip() {
  local output

  output="$(ip -4 route get 1.1.1.1 2>/dev/null || true)"
  set -- $output
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "src" ] && [ "$#" -ge 2 ]; then
      printf '%s\n' "$2"
      return
    fi
    shift
  done

  fail "could not determine host network IP for docs dev server"
}

precheck() {
  require_command bun
  require_command curl
  require_command ip
  require_command iptables
  require_command jq
  require_command sha256sum
  require_command sudo
  require_command tar

  if ! sudo -n true >/dev/null 2>&1; then
    fail "passwordless sudo is required to expose the local docs dev server to the VM"
  fi

  if [ ! -x "$BASTION" ]; then
    fail "CLI build not found at $BASTION; run mise run //core:build or mise run //core:test:e2e"
  fi

  if ! run_cli templates list >/dev/null 2>&1; then
    fail "Bastion API is not reachable on $API_URL; run mise dev:up"
  fi

  if ! "$BASTION" system --data-dir "$DATA_DIR" check >/dev/null 2>&1; then
    fail "Bastion system check is not ok for $DATA_DIR; run bastion system --data-dir '$DATA_DIR' init --with-utilities"
  fi

  LATEST_VERSION="$LOCAL_RELEASE_VERSION"
  DOCS_HOST="${BASTION_INSTALL_E2E_DOCS_HOST:-$(host_network_ip)}"
}

prepare_local_release() {
  local archive
  local staging

  archive="bastion_${LATEST_VERSION}_linux_x86_64.tar.gz"
  staging="$(mktemp -d)"

  rm -rf "$LOCAL_RELEASE_DIR"
  mkdir -p "$LOCAL_RELEASE_DIR"

  install -m 0755 "$CORE_DIR/tmp/bastion" "$staging/bastion"
  install -m 0755 "$CORE_DIR/tmp/bastion-guest-proxy" "$staging/bastion-guest-proxy"

  tar -C "$staging" -czf "$LOCAL_RELEASE_DIR/$archive" bastion bastion-guest-proxy
  (cd "$LOCAL_RELEASE_DIR" && sha256sum "$archive" >"$archive.sha256")
  rm -rf "$staging"
}

start_docs_server() {
  local attempt

  mkdir -p "$CORE_DIR/tmp"
  rm -f "$DOCS_LOG"

  log "starting docs dev server on 0.0.0.0:$DOCS_PORT"
  (cd "$REPO_DIR/docs" && exec bun run dev --host 0.0.0.0 --port "$DOCS_PORT") >"$DOCS_LOG" 2>&1 &
  DOCS_PID=$!

  for ((attempt = 1; attempt <= 120; attempt++)); do
    if curl -fsSL "http://127.0.0.1:${DOCS_PORT}/install.sh" >/dev/null 2>&1; then
      log "docs dev server is ready"
      return
    fi

    if ! kill -0 "$DOCS_PID" >/dev/null 2>&1; then
      fail "docs dev server exited early; see $DOCS_LOG"
    fi

    sleep 1
  done

  fail "docs dev server did not become ready; see $DOCS_LOG"
}

allow_docs_from_guests() {
  if sudo -n iptables -C INPUT -p tcp -s "$DOCS_SOURCE_CIDR" --dport "$DOCS_PORT" -j ACCEPT >/dev/null 2>&1; then
    return
  fi

  log "allowing VM access to docs dev server from $DOCS_SOURCE_CIDR"
  sudo -n iptables -I INPUT -p tcp -s "$DOCS_SOURCE_CIDR" --dport "$DOCS_PORT" -j ACCEPT
  DOCS_FIREWALL_RULE_ADDED=1
}

install_template_config() {
  jq -nc '{
    resources: {memory: 4},
    agents: {opencode: {}},
    actions: {
      init: [
        {
          run: "set -eu\nexport DEBIAN_FRONTEND=noninteractive\napt-get update\napt-get install -y --no-install-recommends ca-certificates curl tar bash coreutils iproute2 jq kmod systemd systemd-sysv\nsystemctl --version >/dev/null"
        },
        {
          run: "set -eu\nrm -f /swapfile\nfallocate -l 2G /swapfile || dd if=/dev/zero of=/swapfile bs=1M count=2048\nchmod 600 /swapfile\nmkswap /swapfile\nswapon /swapfile"
        }
      ]
    }
  }'
}

create_template() {
  local key=$1
  local output

  log "creating install template $key"
  output="$(run_cli templates create --key "$key" --config "$(install_template_config)")"
  if [ "$(jq -r '.id // empty' <<<"$output")" = "" ]; then
    fail "template $key did not return an id"
  fi

  TEMPLATE_KEYS+=("$key")
}

create_environment() {
  local key=$1
  local output

  log "creating fresh install environment from $key"
  output="$(run_cli env create --template-key "$key")"
  CREATED_ENV_ID="$(jq -r '.id // empty' <<<"$output")"
  if [ -z "$CREATED_ENV_ID" ]; then
    fail "install environment did not return an id"
  fi

  ENV_IDS+=("$CREATED_ENV_ID")
}

run_remote_install() {
  local env_id=$1

  log "running installer inside $env_id from local docs server"
  run_cli ssh --id "$env_id" -- env DOCS_HOST="$DOCS_HOST" DOCS_PORT="$DOCS_PORT" LATEST_VERSION="$LATEST_VERSION" SERVICE_USER="$SERVICE_USER" bash -s <<'INNER'
set -euo pipefail

fail() {
  printf '[remote-install] FAIL: %s\n' "$*" >&2
  exit 1
}

wait_active() {
  local service=$1
  local attempt

  for ((attempt = 1; attempt <= 60; attempt++)); do
    if systemctl is-active --quiet "$service"; then
      return
    fi
    sleep 1
  done

  systemctl status "$service" --no-pager >&2 || true
  fail "$service did not become active"
}

wait_bastion_api() {
  local attempt

  for ((attempt = 1; attempt <= 60; attempt++)); do
    if bastion templates list >/dev/null 2>&1; then
      return
    fi
    sleep 1
  done

  systemctl status bastion-api.service --no-pager >&2 || true
  fail "bastion CLI could not reach the installed API service"
}

choose_inner_network_prefix() {
  local route second next

  route="$(ip -4 route get 1.1.1.1 2>/dev/null || true)"
  if [[ "$route" =~ src[[:space:]]+10\.([0-9]+)\. ]]; then
    second="${BASH_REMATCH[1]}"
    next=$((10#$second + 1))
    if [ "$next" -le 255 ]; then
      printf '10.%d\n' "$next"
      return
    fi
  fi

  printf '10.242\n'
}

service_data_dir() {
  # shellcheck disable=SC1091
  . /etc/default/bastion
  printf '%s\n' "$BASTION_DATA_DIR"
}

assert_base_ssh_access() {
  local data_dir
  local expected_owner
  local actual_owner
  local actual_mode

  data_dir="$(service_data_dir)"
  expected_owner="$(id -u "$SERVICE_USER"):$(id -g "$SERVICE_USER")"
  actual_owner="$(stat -c '%u:%g' "$data_dir/base")"
  actual_mode="$(stat -c '%a' "$data_dir/base")"
  if [ "$actual_owner" != "$expected_owner" ] || [ "$actual_mode" != "750" ]; then
    fail "base directory access is $actual_owner $actual_mode, want $expected_owner 750"
  fi

  actual_owner="$(stat -c '%u:%g' "$data_dir/base/ssh_key")"
  actual_mode="$(stat -c '%a' "$data_dir/base/ssh_key")"
  if [ "$actual_owner" != "$expected_owner" ] || [ "$actual_mode" != "600" ]; then
    fail "base SSH key access is $actual_owner $actual_mode, want $expected_owner 600"
  fi
}

restrict_base_ssh_access_to_root() {
  local data_dir

  data_dir="$(service_data_dir)"
  chown root:root "$data_dir/base" "$data_dir/base/ssh_key"
  chmod 0750 "$data_dir/base"
  chmod 0600 "$data_dir/base/ssh_key"
}

assert_bastion_opencode_attach() {
  local env_id=$1
  local label=$2
  local api_url="${BASTION_API_URL:-http://localhost:3148}"
  local fake_bin="/tmp/bastion-install-opencode-$env_id-$$"
  local proxy_file="$fake_bin/proxy-url"
  local expected_url="${api_url%/}/v1/environments/$env_id/agents/opencode"
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

curl -fsS --connect-timeout 5 --max-time 20 "$2/global/health" | jq -e '.healthy == true' >/dev/null
printf '%s\n' "$2" >"${BASTION_E2E_OPENCODE_PROXY_FILE:?}"
SH
  chmod +x "$fake_bin/opencode"

  if ! (export PATH="$fake_bin:$PATH" BASTION_E2E_OPENCODE_PROXY_FILE="$proxy_file"; bastion --api-url "$api_url" opencode --id "$env_id"); then
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

CHILD_ENV_ID=""
CHILD_TEMPLATE_KEY=""

cleanup_child_environment() {
  set +e
  if [ -n "$CHILD_ENV_ID" ]; then
    bastion env remove --id "$CHILD_ENV_ID" >/dev/null 2>&1 || true
  fi
  if [ -n "$CHILD_TEMPLATE_KEY" ]; then
    bastion templates remove --key "$CHILD_TEMPLATE_KEY" >/dev/null 2>&1 || true
  fi
}
trap cleanup_child_environment EXIT

verify_bastiond_restart_preserves_environment() {
  local data_dir
  local output
  local status

  data_dir="$(service_data_dir)"

  bastion system --data-dir "$data_dir" init --with-utilities
  bastion system --data-dir "$data_dir" check
  sync
  echo 3 > /proc/sys/vm/drop_caches || true

  CHILD_TEMPLATE_KEY="restart-child-$(date +%s)-$$"
  bastion templates create --key "$CHILD_TEMPLATE_KEY" --config '{"agents":{"opencode":{}},"resources":{"vcpu":1,"memory":1,"volume":5},"actions":{"init":[{"run":"set -eu\nprintf restart-ok >/root/restart-ok"}]}}' >/dev/null

  output="$(bastion env create --template-key "$CHILD_TEMPLATE_KEY")"
  CHILD_ENV_ID="$(jq -r '.id // empty' <<<"$output")"
  if [ -z "$CHILD_ENV_ID" ]; then
    fail "restart child environment did not return an id: $output"
  fi

  bastion ssh --id "$CHILD_ENV_ID" -- grep -q restart-ok /root/restart-ok
  assert_bastion_opencode_attach "$CHILD_ENV_ID" "before restart"
  systemctl restart bastiond.service bastion-api.service
  wait_active bastiond.service
  wait_active bastion-api.service
  wait_bastion_api

  status="$(bastion env get --id "$CHILD_ENV_ID" | jq -r '.status')"
  if [ "$status" != "running" ]; then
    fail "environment $CHILD_ENV_ID status is $status after bastiond restart, want running"
  fi

  assert_bastion_opencode_attach "$CHILD_ENV_ID" "after restart"
  bastion ssh --id "$CHILD_ENV_ID" -- grep -q restart-ok /root/restart-ok
  printf '[remote-install] bastiond restart preserved environment %s\n' "$CHILD_ENV_ID"
}

download_installer() {
  local gateway=$1
  local candidate
  local url

  for candidate in "$DOCS_HOST" "$gateway"; do
    if [ -z "$candidate" ]; then
      continue
    fi

    url="http://${candidate}:${DOCS_PORT}/install.sh"
    printf '[remote-install] fetching installer from %s\n' "$url"
    if curl --connect-timeout 10 --max-time 60 -fsSL "$url" -o /tmp/bastion-install.sh; then
      INSTALL_URL="$url"
      return
    fi
  done

  fail "could not fetch installer from local docs dev server"
}

set -- $(ip -4 route show default)
if [ "${1:-}" != "default" ] || [ "${2:-}" != "via" ] || [ -z "${3:-}" ]; then
  fail "could not determine Bastion host gateway from default route"
fi

gateway=$3
download_installer "$gateway"
release_base_url="${INSTALL_URL%/install.sh}/e2e-releases/$LATEST_VERSION"

if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --create-home "$SERVICE_USER"
fi

if ! grep -q 'bastion-computer/bastion' /tmp/bastion-install.sh; then
  fail "downloaded installer does not look like the local Bastion installer"
fi

curl --connect-timeout 10 --max-time 60 -fsSL "$INSTALL_URL" | BASTION_INSTALL_VERSION="$LATEST_VERSION" BASTION_RELEASE_BASE_URL="$release_base_url" BASTION_SERVICE_USER="$SERVICE_USER" bash 2>&1 | tee /tmp/bastion-install.log

grep -q 'checksum verified' /tmp/bastion-install.log || fail "installer did not verify the release checksum"
grep -q 'services installed, enabled, and started' /tmp/bastion-install.log || fail "installer did not set up services by default"
command -v bastion >/dev/null || fail "bastion was not installed on PATH"
test -f /etc/default/bastion || fail "/etc/default/bastion was not created"
grep -q '^BASTION_DATA_DIR=' /etc/default/bastion || fail "service env file is missing BASTION_DATA_DIR"
grep -q '^BASTIOND_SOCKET=' /etc/default/bastion || fail "service env file is missing BASTIOND_SOCKET"
grep -q '^EnvironmentFile=/etc/default/bastion$' /etc/systemd/system/bastiond.service || fail "bastiond.service does not read /etc/default/bastion"
grep -q '^EnvironmentFile=/etc/default/bastion$' /etc/systemd/system/bastion-api.service || fail "bastion-api.service does not read /etc/default/bastion"
grep -q '^ExecStart=.*/bastion start daemon --socket-uid ' /etc/systemd/system/bastiond.service || fail "bastiond.service does not run bastion start daemon"
grep -q '^ExecStart=.*/bastion start api$' /etc/systemd/system/bastion-api.service || fail "bastion-api.service does not run bastion start api"
grep -q "^User=$SERVICE_USER$" /etc/systemd/system/bastion-api.service || fail "bastion-api.service does not run as $SERVICE_USER"

inner_network_prefix="$(choose_inner_network_prefix)"
printf '\nBASTION_E2E_SENTINEL=preserve\nBASTION_VM_CPUS=1\nBASTION_VM_NETWORK_PREFIX=%s\n' "$inner_network_prefix" >>/etc/default/bastion

systemctl restart bastiond.service
wait_active bastiond.service
wait_active bastion-api.service
wait_bastion_api

data_dir="$(service_data_dir)"
bastion system --data-dir "$data_dir" init --with-utilities
bastion system --data-dir "$data_dir" check
printf '[remote-install] building base before restart-preservation checks\n'
bastion base build --force >/dev/null
assert_base_ssh_access
restrict_base_ssh_access_to_root

printf '\n# BASTION_E2E_UNIT_SENTINEL=reset\n' >>/etc/systemd/system/bastiond.service
printf '\n# BASTION_E2E_UNIT_SENTINEL=reset\n' >>/etc/systemd/system/bastion-api.service

bastion_path="$(command -v bastion)"
real_bastion="${bastion_path}.e2e-real"
mv "$bastion_path" "$real_bastion"
cat >"$bastion_path" <<EOF
#!/usr/bin/env sh
if [ "\${1:-}" = "version" ]; then
  printf 'v0.0.0\n'
  exit 0
fi
exec "$real_bastion" "\$@"
EOF
chmod +x "$bastion_path"

curl --connect-timeout 10 --max-time 60 -fsSL "$INSTALL_URL" | BASTION_INSTALL_VERSION="$LATEST_VERSION" BASTION_RELEASE_BASE_URL="$release_base_url" BASTION_SERVICE_USER="$SERVICE_USER" bash 2>&1 | tee /tmp/bastion-reinstall.log
rm -f "$real_bastion"

grep -q 'updating Bastion from v0.0.0' /tmp/bastion-reinstall.log || fail "installer did not enter the update path"
grep -q 'services installed, enabled, and started' /tmp/bastion-reinstall.log || fail "installer did not reset services during update"
grep -q '^BASTION_E2E_SENTINEL=preserve$' /etc/default/bastion || fail "installer overwrote the service environment file"
grep -q 'preserving existing service environment file' /tmp/bastion-reinstall.log || fail "installer did not report preserving the service environment file"
if grep -q 'BASTION_E2E_UNIT_SENTINEL' /etc/systemd/system/bastiond.service /etc/systemd/system/bastion-api.service; then
  fail "installer did not reset systemd service units"
fi

installed_version="$(bastion version)"
if [ "$installed_version" != "$LATEST_VERSION" ]; then
  fail "installed version is $installed_version, want $LATEST_VERSION"
fi

systemctl is-enabled --quiet bastiond.service || fail "bastiond.service is not enabled"
systemctl is-enabled --quiet bastion-api.service || fail "bastion-api.service is not enabled"
wait_active bastiond.service
wait_active bastion-api.service
wait_bastion_api
assert_base_ssh_access
verify_bastiond_restart_preserves_environment
grep -q '^KillMode=process$' /etc/systemd/system/bastiond.service || fail "bastiond.service does not keep VM child processes outside daemon restarts"

printf '[remote-install] Bastion API is ready\n'
INNER
}

main() {
  local key="$RUN_ID-template"
  local env_id

  precheck
  trap cleanup EXIT

  log "starting install e2e run $RUN_ID against $LATEST_VERSION"
  prepare_local_release
  start_docs_server
  allow_docs_from_guests
  create_template "$key"
  create_environment "$key"
  env_id="$CREATED_ENV_ID"
  run_remote_install "$env_id"
  log "install e2e run passed"
}

main "$@"
