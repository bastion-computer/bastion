#!/usr/bin/env sh
set -eu

PATH="/usr/local/bin:/usr/bin:/bin:${PATH:-}"
export PATH

version="${BASTION_INPUT_VERSION:-}"
install_uv="${BASTION_INPUT_INSTALL_UV:-true}"

fail_validation() {
  printf '%s\n' "$1" >&2
  exit 2
}

validate_version() {
  if [ -z "$version" ]; then
    return 0
  fi

  case "$version" in
    *[!0-9.]*|.*|*.|*..*|*.*.*)
      fail_validation "Python version must be a major or major.minor value containing only digits and dots, for example 3 or 3.12"
      ;;
  esac

  major="${version%%.*}"
  if [ -z "$major" ]; then
    fail_validation "Python version must include a major version"
  fi

  case "$version" in
    *.*)
      minor="${version#*.}"
      if [ -z "$minor" ]; then
        fail_validation "Python version must include a minor version after the dot"
      fi
      ;;
  esac
}

validate_install_uv() {
  case "$install_uv" in
    true|false)
      ;;
    *)
      fail_validation "install_uv must be the exact boolean value true or false"
      ;;
  esac
}

ensure_package_available() {
  package="$1"

  if ! apt-cache show "$package" >/dev/null 2>&1; then
    printf 'Requested Python version %s is unavailable from configured apt repositories; missing package %s\n' "$version" "$package" >&2
    exit 1
  fi
}

ensure_safe_link() {
  target="$1"
  source="$2"

  if [ -e "$target" ] && [ ! -L "$target" ]; then
    printf 'Cannot update %s because it already exists and is not a symlink; requested Python binary is %s\n' "$target" "$source" >&2
    exit 1
  fi

  ln -sfn "$source" "$target"
}

validate_version
validate_install_uv

if [ "$(id -u)" -ne 0 ]; then
  printf 'setup_python must run as root\n' >&2
  exit 1
fi

export DEBIAN_FRONTEND=noninteractive

apt-get update

base_packages="python3 python3-pip python3-venv python3-dev ca-certificates curl"
version_packages=""
requested_binary=""

if [ -n "$version" ]; then
  version_packages="python$version python$version-venv python$version-dev"
  for package in $version_packages; do
    ensure_package_available "$package"
  done

  requested_binary="/usr/bin/python$version"
fi

apt-get install -y --no-install-recommends $base_packages $version_packages

python_link_created=false
if [ -n "$requested_binary" ]; then
  if [ ! -x "$requested_binary" ]; then
    printf 'Requested Python binary %s was not installed\n' "$requested_binary" >&2
    exit 1
  fi

  mkdir -p /usr/local/bin
  ensure_safe_link /usr/local/bin/python "$requested_binary"
  ensure_safe_link /usr/local/bin/python3 "$requested_binary"
  python_link_created=true
fi

if [ "$install_uv" = "true" ]; then
  mkdir -p /usr/local/bin
  export UV_INSTALL_DIR=/usr/local/bin
  curl -fsSL https://astral.sh/uv/install.sh | sh
fi

python3 --version
if [ "$python_link_created" = "true" ]; then
  python --version
fi
pip3 --version
if [ "$install_uv" = "true" ]; then
  uv --version
fi
