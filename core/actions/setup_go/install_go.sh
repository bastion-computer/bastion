#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-}"
tmp_dir=""
env_tmp=""

cleanup() {
  if [ -n "$tmp_dir" ]; then
    rm -rf "$tmp_dir"
  fi

  if [ -n "$env_tmp" ]; then
    rm -f "$env_tmp"
  fi
}

trap cleanup EXIT

validate_version() {
  if ! printf '%s\n' "$1" | grep -Eq '^[0-9]+(\.[0-9]+){1,2}$'; then
    printf 'Go version must match ^[0-9]+(\.[0-9]+){1,2}$: %s\n' "$1" >&2
    exit 2
  fi
}

if [ -n "$version" ]; then
  validate_version "$version"
fi

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl jq tar

if command -v dpkg >/dev/null 2>&1; then
  detected_arch="$(dpkg --print-architecture)"
else
  detected_arch="$(uname -m)"
fi

case "$detected_arch" in
  amd64|x86_64)
    platform="linux-amd64"
    ;;
  arm64|aarch64)
    platform="linux-arm64"
    ;;
  *)
    printf 'Unsupported Go architecture %s; supported architectures are amd64 and arm64\n' "$detected_arch" >&2
    exit 2
    ;;
esac

if [ -z "$version" ]; then
  metadata_version="$(curl -fsSL 'https://go.dev/dl/?mode=json' | jq -r '[.[] | select(.stable == true)][0].version // empty')"
  version="${metadata_version#go}"

  if [ -z "$version" ] || [ "$version" = "$metadata_version" ]; then
    printf 'Unable to resolve latest stable Go version from https://go.dev/dl/?mode=json\n' >&2
    exit 1
  fi

  validate_version "$version"
fi

archive="go${version}.${platform}.tar.gz"
url="https://go.dev/dl/${archive}"
tmp_dir="$(mktemp -d)"

if ! curl -fL "$url" -o "$tmp_dir/$archive"; then
  printf 'Download failed for Go %s from %s\n' "$version" "$url" >&2
  exit 1
fi

rm -rf /usr/local/go
tar -C /usr/local -xzf "$tmp_dir/$archive"

install -d -m 0755 /usr/local/bin /root/go/bin
ln -sf /usr/local/go/bin/go /usr/local/bin/go
ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt

bashrc=/root/.bashrc
start_marker='# >>> bastion go environment >>>'
end_marker='# <<< bastion go environment <<<'
env_tmp="$(mktemp)"

touch "$bashrc"
awk -v start="$start_marker" -v end="$end_marker" '
  $0 == start { skip = 1; next }
  $0 == end { skip = 0; next }
  !skip { print }
' "$bashrc" > "$env_tmp"

{
  printf '\n%s\n' "$start_marker"
  printf 'export GOPATH=/root/go\n'
  printf 'export PATH=/usr/local/go/bin:/root/go/bin:$PATH\n'
  printf '%s\n' "$end_marker"
} >> "$env_tmp"

cp "$env_tmp" "$bashrc"
rm -f "$env_tmp"
env_tmp=""

export GOPATH=/root/go
export PATH=/usr/local/go/bin:/root/go/bin:$PATH

go version
verify_src="$tmp_dir/gofmt-check.go"
printf 'package main\nfunc main(){}\n' > "$verify_src"
gofmt -w "$verify_src"
go env GOPATH
