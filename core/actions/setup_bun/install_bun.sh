#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends bash ca-certificates curl unzip

export BUN_INSTALL=/usr/local
if [ -n "$version" ]; then
  curl -fsSL https://bun.com/install | bash -s "$version"
else
  curl -fsSL https://bun.com/install | bash
fi

/usr/local/bin/bun --version
/usr/local/bin/bun --revision
