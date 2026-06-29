#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"
python_version="${BASTION_INPUT_PYTHON_VERSION:-}"

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl

install_url="https://astral.sh/uv/install.sh"
if [ -n "$version" ]; then
  install_url="https://astral.sh/uv/$version/install.sh"
fi

installer="$(mktemp)"
trap 'rm -f "$installer"' EXIT
curl -LsSf "$install_url" -o "$installer"
env UV_UNMANAGED_INSTALL=/usr/local/bin sh "$installer"

uv --version
uvx --version

if [ -n "$python_version" ]; then
  uv python install "$python_version"
  uv python find "$python_version"
fi
