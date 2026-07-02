#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"
maestro_dir="/usr/local/maestro"
installer_url="https://get.maestro.mobile.dev"

if ! command -v java >/dev/null 2>&1; then
  printf 'Java 17 or newer is required for Maestro; run setup_openjdk before setup_maestro\n' >&2
  exit 2
fi

java_version="$(java -version 2>&1 | sed -n '1s/.* version "\([^"]*\)".*/\1/p')"
if [ -z "$java_version" ]; then
  printf 'Could not determine Java version; run setup_openjdk before setup_maestro\n' >&2
  exit 2
fi

java_major="${java_version%%.*}"
if [ "$java_major" = "1" ]; then
  java_minor="${java_version#1.}"
  java_major="${java_minor%%.*}"
fi

case "$java_major" in
  ''|*[!0-9]*)
    printf 'Could not determine Java major version from: %s\n' "$java_version" >&2
    exit 2
    ;;
esac

if [ "$java_major" -lt 17 ]; then
  printf 'Java 17 or newer is required for Maestro; found %s. Run setup_openjdk with version 17 or newer before setup_maestro\n' "$java_version" >&2
  exit 2
fi

if [ -z "${JAVA_HOME:-}" ]; then
  java_path="$(readlink -f "$(command -v java)")"
  JAVA_HOME="$(dirname "$(dirname "$java_path")")"
  export JAVA_HOME
fi

if [ ! -x "$JAVA_HOME/bin/java" ]; then
  printf 'JAVA_HOME does not point to a Java installation: %s\n' "$JAVA_HOME" >&2
  exit 2
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends bash ca-certificates curl unzip

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

if [ -L /usr/local/bin/maestro ]; then
  existing_target="$(readlink -f /usr/local/bin/maestro || true)"
  if [ "$existing_target" = "$maestro_dir/bin/maestro" ]; then
    rm -f /usr/local/bin/maestro
  fi
fi

curl -fsSL "$installer_url" -o "$tmp_dir/install_maestro.sh"
env \
  JAVA_HOME="$JAVA_HOME" \
  MAESTRO_DIR="$maestro_dir" \
  MAESTRO_VERSION="$version" \
  PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
  bash "$tmp_dir/install_maestro.sh"

test -x "$maestro_dir/bin/maestro"
ln -sf "$maestro_dir/bin/maestro" /usr/local/bin/maestro

tmp_environment="$tmp_dir/environment"
if [ -f /etc/environment ]; then
  grep -v '^MAESTRO_DIR=' /etc/environment > "$tmp_environment" || true
fi
printf 'MAESTRO_DIR="%s"\n' "$maestro_dir" >> "$tmp_environment"
install -m 0644 "$tmp_environment" /etc/environment

mkdir -p /etc/profile.d
{
  printf 'export MAESTRO_DIR=%s\n' "$maestro_dir"
  printf 'export PATH="$MAESTRO_DIR/bin:$PATH"\n'
} > /etc/profile.d/bastion-maestro.sh
chmod 0644 /etc/profile.d/bastion-maestro.sh

MAESTRO_DIR="$maestro_dir" JAVA_HOME="$JAVA_HOME" maestro --version
MAESTRO_DIR="$maestro_dir" JAVA_HOME="$JAVA_HOME" maestro --help >/dev/null
