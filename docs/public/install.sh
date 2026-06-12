#!/usr/bin/env bash
set -euo pipefail

readonly REPO="bastion-computer/bastion"
readonly DEFAULT_INSTALL_DIR="/usr/local/bin"
readonly SOCKET_PATH="/run/bastion/bastiond.sock"
readonly SERVICE_ENV_FILE="${BASTION_SERVICE_ENV_FILE:-/etc/default/bastion}"
readonly INSTALL_VERSION="${BASTION_INSTALL_VERSION:-}"
readonly RELEASE_BASE_URL="${BASTION_RELEASE_BASE_URL:-}"

TARGET=""
INSTALL_SERVICES=0
INSTALL_DIR="${BASTION_INSTALL_DIR:-}"
TMP_DIR=""
BASTION_BIN=""
BASTIOND_BIN=""
BASTION_GUEST_PROXY_BIN=""

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
Usage: install.sh

Installs or updates Bastion from the latest GitHub release.

Supported targets:
  - Linux x86_64 host install with bastion, bastiond, and systemd services.
  - macOS Apple silicon CLI install with bastion only.

Options:
  -h, --help                 Show this help message.
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

  case "$os:$arch" in
    Linux:x86_64 | Linux:amd64)
      TARGET="linux_x86_64"
      INSTALL_SERVICES=1
      ;;
    Darwin:arm64 | Darwin:aarch64)
      TARGET="darwin_arm64"
      INSTALL_SERVICES=0
      ;;
    Linux:*)
      fail "Bastion Linux host installs currently support x86_64 only; found $arch"
      ;;
    Darwin:*)
      fail "Bastion macOS CLI installs currently support Apple silicon only; found $arch"
      ;;
    *)
      fail "Bastion currently supports Linux x86_64 hosts and macOS Apple silicon CLI installs; found $os $arch"
      ;;
  esac
}

require_checksum_command() {
  if command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1; then
    return
  fi

  fail "sha256sum or shasum is required"
}

verify_checksum() {
  local archive=$1
  local checksum=$2
  local expected
  local actual

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMP_DIR" && sha256sum -c "$checksum")
    return
  fi

  if ! IFS=' ' read -r expected _ <"$TMP_DIR/$checksum" || [ -z "$expected" ]; then
    fail "release checksum file is empty"
  fi

  actual="$(shasum -a 256 "$TMP_DIR/$archive")"
  actual="${actual%% *}"
  if [ "$actual" != "$expected" ]; then
    fail "checksum mismatch for $archive"
  fi
}

latest_release_tag() {
  local url
  local effective_url
  local tag

  if [ -n "$INSTALL_VERSION" ]; then
    printf '%s\n' "$INSTALL_VERSION"
    return
  fi

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
  if [ -n "$RELEASE_BASE_URL" ]; then
    release_url="${RELEASE_BASE_URL%/}"
  else
    release_url="https://github.com/${REPO}/releases/download/${version}"
  fi
  TMP_DIR="$(mktemp -d)"

  log "downloading Bastion $version for $TARGET"
  curl -fsSLo "$TMP_DIR/$archive" "$release_url/$archive"
  curl -fsSLo "$TMP_DIR/$checksum" "$release_url/$checksum"

  log "verifying release checksum"
  verify_checksum "$archive" "$checksum"
  log "checksum verified"

  mkdir -p "$TMP_DIR/extract"
  tar -xzf "$TMP_DIR/$archive" -C "$TMP_DIR/extract"

  if [ ! -x "$TMP_DIR/extract/bastion" ]; then
    fail "release archive did not contain an executable bastion binary"
  fi

  if [ "$INSTALL_SERVICES" -eq 1 ] && [ ! -x "$TMP_DIR/extract/bastiond" ]; then
    fail "release archive did not contain an executable bastiond binary"
  fi

  if [ "$INSTALL_SERVICES" -eq 1 ] && [ ! -x "$TMP_DIR/extract/bastion-guest-proxy" ]; then
    fail "release archive did not contain an executable bastion-guest-proxy binary"
  fi
}

install_binaries() {
  local version=$1

  run_as_root install -d -m 0755 "$INSTALL_DIR"
  run_as_root install -m 0755 "$TMP_DIR/extract/bastion" "$INSTALL_DIR/bastion"

  BASTION_BIN="$INSTALL_DIR/bastion"
  if [ "$INSTALL_SERVICES" -eq 1 ]; then
    run_as_root install -m 0755 "$TMP_DIR/extract/bastiond" "$INSTALL_DIR/bastiond"
    run_as_root install -m 0755 "$TMP_DIR/extract/bastion-guest-proxy" "$INSTALL_DIR/bastion-guest-proxy"
    BASTIOND_BIN="$INSTALL_DIR/bastiond"
    BASTION_GUEST_PROXY_BIN="$INSTALL_DIR/bastion-guest-proxy"
  fi

  log "installed Bastion $version to $INSTALL_DIR"
}

ensure_binaries() {
  local latest_version=$1
  local existing_bastion
  local existing_bastiond
  local existing_guest_proxy
  local current_version

  existing_bastion="$(command -v bastion 2>/dev/null || true)"
  existing_bastiond="$(command -v bastiond 2>/dev/null || true)"
  existing_guest_proxy="$(command -v bastion-guest-proxy 2>/dev/null || true)"
  resolve_install_dir "$existing_bastion"

  current_version="$(installed_version "$existing_bastion" || true)"
  if [ -n "$current_version" ] && [ "$current_version" = "$latest_version" ] && { [ "$INSTALL_SERVICES" -eq 0 ] || { [ -n "$existing_bastiond" ] && [ -n "$existing_guest_proxy" ]; }; }; then
    BASTION_BIN="$existing_bastion"
    if [ "$INSTALL_SERVICES" -eq 1 ]; then
      BASTIOND_BIN="$existing_bastiond"
      BASTION_GUEST_PROXY_BIN="$existing_guest_proxy"
    fi
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
KillMode=process
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

  require_command systemctl
  if [ ! -d /run/systemd/system ]; then
    fail "systemd is required to install Bastion services"
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
  require_checksum_command
  require_command install
  require_command mktemp

  latest_version="$(latest_release_tag)"
  ensure_binaries "$latest_version"
  if [ "$INSTALL_SERVICES" -eq 1 ]; then
    setup_services
  else
    log "macOS CLI install complete; use bastion --api-url to connect to a remote Bastion host API"
  fi

  log "Bastion is ready"
}

main "$@"
