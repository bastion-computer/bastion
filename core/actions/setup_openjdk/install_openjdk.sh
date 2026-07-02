#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"

export DEBIAN_FRONTEND=noninteractive
apt-get update

if [ -n "$version" ]; then
  major="${version%%.*}"
  case "$major" in
    ''|*[!0-9]*)
      printf 'OpenJDK version must start with a numeric major version: %s\n' "$version" >&2
      exit 2
      ;;
  esac
else
  major="$(
    apt-cache pkgnames openjdk- \
      | sed -n 's/^openjdk-\([0-9][0-9]*\)-jdk-headless$/\1/p' \
      | sort -n \
      | tail -n 1
  )"
  if [ -z "$major" ]; then
    printf 'No OpenJDK JDK packages are available from apt\n' >&2
    exit 2
  fi
fi

apt-get install -y --no-install-recommends "openjdk-$major-jdk-headless"

javac_path="$(readlink -f "$(command -v javac)")"
java_home="$(dirname "$(dirname "$javac_path")")"

tmp_environment="$(mktemp)"
trap 'rm -f "$tmp_environment"' EXIT
if [ -f /etc/environment ]; then
  grep -v '^JAVA_HOME=' /etc/environment > "$tmp_environment" || true
fi
printf 'JAVA_HOME="%s"\n' "$java_home" >> "$tmp_environment"
install -m 0644 "$tmp_environment" /etc/environment

mkdir -p /etc/profile.d
{
  printf 'export JAVA_HOME=%s\n' "$java_home"
  printf 'export PATH="$JAVA_HOME/bin:$PATH"\n'
} > /etc/profile.d/bastion-openjdk.sh
chmod 0644 /etc/profile.d/bastion-openjdk.sh

java -version
javac -version
