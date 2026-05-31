#!/usr/bin/env bash
set -euo pipefail

readonly REPO="bastion-computer/bastion"
readonly DEFAULT_INSTALL_DIR="/usr/local/bin"
readonly TARGET="linux_x86_64"
readonly SOCKET_PATH="/run/bastion/bastiond.sock"
readonly SERVICE_ENV_FILE="${BASTION_SERVICE_ENV_FILE:-/etc/default/bastion}"

WITH_SERVICES=0
INSTALL_DIR="${BASTION_INSTALL_DIR:-}"
TMP_DIR=""
BASTION_BIN=""
BASTIOND_BIN=""

cat <<'EOF'

                                  |>>>
                                  |
                    |>>>      _  _|_  _         |>>>
                    |        |;| |;| |;|        |
                _  _|_  _    \\.    .  /    _  _|_  _
               |;|_|;|_|;|    \\:. ,  /    |;|_|;|_|;|
               \\..      /    ||;   . |    \\.    .  /
                \\.  ,  /     ||:  .  |     \\:  .  /
                 ||:   |_   _ ||_ . _ | _   _||:   |
                 ||:  .|||_|;|_|;|_|;|_|;|_|;||:.  |
                 ||:   ||.    .     .      . ||:  .|
                 ||: . || .     . .   .  ,   ||:   |       \,/
                 ||:   ||:  ,  _______   .   ||: , |            /`\
                 ||:   || .   /+++++++\    . ||:   |
                 ||:   ||.    |+++++++| .    ||: . |
              __ ||: . ||: ,  |+++++++|.  . _||_   |
     ____--`~    '--~~__|.    |+++++__|----~    ~`---,              ___
-~--~                   ~---__|,--~'                  ~~----_____-~'   `~----~~

EOF

usage() {
  cat <<'EOF'
Usage: install.sh [--with-services]

Installs or updates Bastion for Linux x86_64 from the latest GitHub release.

Options:
  --with-services  Install and start systemd services for bastion and bastiond.
  -h, --help       Show this help message.
EOF
}

log() {
  printf '[bastion-install] %s\n' "$*"
}

fail() {
  log "error: $*" >&2
  exit 1
}

cleanup() {
  if [ -n "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --with-services)
        WITH_SERVICES=1
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
    shift
  done
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "$1 is required"
  fi
}

run_as_root() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
    return
  fi

  if ! command -v sudo >/dev/null 2>&1; then
    fail "sudo is required to install into $INSTALL_DIR"
  fi

  sudo "$@"
}

check_platform() {
  local os
  local arch

  os="$(uname -s)"
  arch="$(uname -m)"

  if [ "$os" != "Linux" ]; then
    fail "Bastion currently supports Linux only; found $os"
  fi

  case "$arch" in
    x86_64 | amd64)
      ;;
    *)
      fail "Bastion currently supports x86_64 only; found $arch"
      ;;
  esac
}

latest_release_tag() {
  local url
  local effective_url
  local tag

  url="https://github.com/${REPO}/releases/latest"
  effective_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$url")"
  effective_url="${effective_url%/}"
  tag="${effective_url##*/}"
  tag="${tag%%\?*}"

  if [ -z "$tag" ] || [ "$tag" = "latest" ]; then
    fail "could not resolve latest Bastion release from $url"
  fi

  printf '%s\n' "$tag"
}

installed_version() {
  local bastion_path=$1

  if [ -z "$bastion_path" ] || [ ! -x "$bastion_path" ]; then
    return 1
  fi

  "$bastion_path" version 2>/dev/null | tr -d '[:space:]'
}

resolve_install_dir() {
  local existing_bastion=$1

  if [ -n "$INSTALL_DIR" ]; then
    return
  fi

  if [ -n "$existing_bastion" ] && [ "${existing_bastion#/}" != "$existing_bastion" ]; then
    INSTALL_DIR="${existing_bastion%/*}"
    return
  fi

  INSTALL_DIR="$DEFAULT_INSTALL_DIR"
}

download_and_verify() {
  local version=$1
  local archive
  local checksum
  local release_url

  archive="bastion_${version}_${TARGET}.tar.gz"
  checksum="${archive}.sha256"
  release_url="https://github.com/${REPO}/releases/download/${version}"
  TMP_DIR="$(mktemp -d)"

  log "downloading Bastion $version for $TARGET"
  curl -fsSLo "$TMP_DIR/$archive" "$release_url/$archive"
  curl -fsSLo "$TMP_DIR/$checksum" "$release_url/$checksum"

  log "verifying release checksum"
  (cd "$TMP_DIR" && sha256sum -c "$checksum")
  log "checksum verified"

  mkdir -p "$TMP_DIR/extract"
  tar -xzf "$TMP_DIR/$archive" -C "$TMP_DIR/extract"

  if [ ! -x "$TMP_DIR/extract/bastion" ] || [ ! -x "$TMP_DIR/extract/bastiond" ]; then
    fail "release archive did not contain executable bastion and bastiond binaries"
  fi
}

install_binaries() {
  local version=$1

  run_as_root install -d -m 0755 "$INSTALL_DIR"
  run_as_root install -m 0755 "$TMP_DIR/extract/bastion" "$INSTALL_DIR/bastion"
  run_as_root install -m 0755 "$TMP_DIR/extract/bastiond" "$INSTALL_DIR/bastiond"

  BASTION_BIN="$INSTALL_DIR/bastion"
  BASTIOND_BIN="$INSTALL_DIR/bastiond"

  log "installed Bastion $version to $INSTALL_DIR"
}

ensure_binaries() {
  local latest_version=$1
  local existing_bastion
  local existing_bastiond
  local current_version

  existing_bastion="$(command -v bastion 2>/dev/null || true)"
  existing_bastiond="$(command -v bastiond 2>/dev/null || true)"
  resolve_install_dir "$existing_bastion"

  current_version="$(installed_version "$existing_bastion" || true)"
  if [ -n "$current_version" ] && [ "$current_version" = "$latest_version" ] && [ -n "$existing_bastiond" ]; then
    BASTION_BIN="$existing_bastion"
    BASTIOND_BIN="$existing_bastiond"
    log "Bastion $latest_version is already installed"
    return
  fi

  if [ -n "$current_version" ]; then
    log "updating Bastion from $current_version to $latest_version"
  else
    log "installing Bastion $latest_version"
  fi

  download_and_verify "$latest_version"
  install_binaries "$latest_version"
}

service_home() {
  local user=$1
  local entry
  local home

  if command -v getent >/dev/null 2>&1; then
    entry="$(getent passwd "$user" || true)"
    if [ -n "$entry" ]; then
      IFS=: read -r _ _ _ _ _ home _ <<<"$entry"
      if [ -n "$home" ]; then
        printf '%s\n' "$home"
        return
      fi
    fi
  fi

  if [ "$user" = "$(id -un)" ] && [ -n "${HOME:-}" ]; then
    printf '%s\n' "$HOME"
    return
  fi

  fail "could not resolve home directory for service user $user"
}

expand_tilde() {
  local path=$1
  local home=$2

  case "$path" in
    '~')
      printf '%s\n' "$home"
      ;;
    '~/'*)
      printf '%s/%s\n' "$home" "${path#~/}"
      ;;
    *)
      printf '%s\n' "$path"
      ;;
  esac
}

systemd_quote() {
  local value=$1
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "$value"
}

should_setup_services() {
  local answer

  if [ "$WITH_SERVICES" -eq 1 ]; then
    return 0
  fi

  if [ ! -r /dev/tty ] || [ ! -w /dev/tty ]; then
    log "skipping service setup; pass --with-services to enable it in non-interactive installs"
    return 1
  fi

  printf 'Set up systemd services for the Bastion API and daemon with auto restart and start on boot? [y/N] ' >/dev/tty
  IFS= read -r answer </dev/tty || answer=""

  case "$answer" in
    y | Y | yes | YES)
      return 0
      ;;
    *)
      log "skipping service setup"
      return 1
      ;;
  esac
}

write_service_units() {
  local service_user=$1
  local service_group=$2
  local service_uid=$3
  local service_gid=$4
  local unit_dir
  local daemon_unit
  local api_unit

  unit_dir="$(mktemp -d)"
  daemon_unit="$unit_dir/bastiond.service"
  api_unit="$unit_dir/bastion-api.service"

  cat >"$daemon_unit" <<EOF
[Unit]
Description=Bastion Cloud Hypervisor daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=$SERVICE_ENV_FILE
ExecStart=$BASTIOND_BIN --socket-uid $service_uid --socket-gid $service_gid
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

  cat >"$api_unit" <<EOF
[Unit]
Description=Bastion Host API
After=network-online.target bastiond.service
Wants=network-online.target
Requires=bastiond.service

[Service]
Type=simple
User=$service_user
Group=$service_group
EnvironmentFile=$SERVICE_ENV_FILE
ExecStart=$BASTION_BIN start
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF

  run_as_root install -m 0644 "$daemon_unit" /etc/systemd/system/bastiond.service
  run_as_root install -m 0644 "$api_unit" /etc/systemd/system/bastion-api.service
  rm -rf "$unit_dir"
}

seed_service_environment_file() {
  local data_dir=$1
  local env_dir
  local tmp_file
  local quoted_data_dir
  local quoted_socket_path

  if run_as_root test -f "$SERVICE_ENV_FILE"; then
    log "preserving existing service environment file at $SERVICE_ENV_FILE"
    return
  fi

  env_dir="${SERVICE_ENV_FILE%/*}"
  tmp_file="$(mktemp)"
  quoted_data_dir="$(systemd_quote "$data_dir")"
  quoted_socket_path="$(systemd_quote "$SOCKET_PATH")"

  cat >"$tmp_file" <<EOF
# Bastion systemd service environment.
# This file is created by install.sh and preserved during updates.
BASTION_ADDR="localhost:3148"
BASTION_DATA_DIR=$quoted_data_dir
BASTIOND_SOCKET=$quoted_socket_path
QUEUE_PROXY_PORT="3150"
BASTION_LOG_FORMAT="json"
BASTION_LOG_LEVEL="info"
BASTIOND_LOG_FORMAT="json"
BASTIOND_LOG_LEVEL="info"
EOF

  run_as_root install -d -m 0755 "$env_dir"
  run_as_root install -m 0644 "$tmp_file" "$SERVICE_ENV_FILE"
  rm -f "$tmp_file"
  log "created service environment file at $SERVICE_ENV_FILE"
}

setup_services() {
  local service_user
  local service_group
  local service_uid
  local service_gid
  local home
  local data_dir

  if ! should_setup_services; then
    return
  fi

  require_command systemctl
  if [ ! -d /run/systemd/system ]; then
    fail "systemd is required for --with-services"
  fi

  service_user="${BASTION_SERVICE_USER:-${SUDO_USER:-$(id -un)}}"
  service_uid="$(id -u "$service_user")"
  service_gid="$(id -g "$service_user")"
  service_group="$(id -gn "$service_user")"
  home="$(service_home "$service_user")"
  data_dir="$(expand_tilde "${BASTION_DATA_DIR:-$home/.bastion}" "$home")"

  log "setting up systemd services for user $service_user"
  run_as_root install -d -m 0750 -o "$service_user" -g "$service_group" "$data_dir"
  seed_service_environment_file "$data_dir"
  write_service_units "$service_user" "$service_group" "$service_uid" "$service_gid"
  run_as_root systemctl daemon-reload
  run_as_root systemctl enable bastiond.service bastion-api.service
  run_as_root systemctl restart bastiond.service bastion-api.service
  log "services installed, enabled, and started"
}

main() {
  local latest_version

  parse_args "$@"
  check_platform
  require_command curl
  require_command tar
  require_command sha256sum
  require_command install
  require_command mktemp

  latest_version="$(latest_release_tag)"
  ensure_binaries "$latest_version"
  setup_services

  log "Bastion is ready"
}

main "$@"
