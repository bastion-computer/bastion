#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends bash ca-certificates curl unzip

if [ -n "$version" ]; then
  export BUN_INSTALL_VERSION="$version"
fi

export BUN_INSTALL=/root/.bun
curl -fsSL https://bun.sh/install | bash
ln -sf /root/.bun/bin/bun /usr/local/bin/bun
bun --version
