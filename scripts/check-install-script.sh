#!/usr/bin/env bash
set -euo pipefail

INSTALL_SCRIPT="${1:-docs/public/install.sh}"
TMP_DIR=""

fail() {
  printf 'install script check failed: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [ -n "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

write_fake_uname() {
  local path=$1

  cat >"$path" <<'SH'
#!/usr/bin/env sh
case "${1:-}" in
  -s)
    printf 'Darwin\n'
    ;;
  -m)
    printf 'arm64\n'
    ;;
  *)
    exec /usr/bin/uname "$@"
    ;;
esac
SH
  chmod 0755 "$path"
}

write_fake_curl() {
  local path=$1

  cat >"$path" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

out=""
write_out=""
url=""

while [ "$#" -gt 0 ]; do
  case "$1" in
    -o)
      out=$2
      shift 2
      ;;
    -w)
      write_out=$2
      shift 2
      ;;
    -*o)
      out=$2
      shift 2
      ;;
    -* )
      shift
      ;;
    *)
      url=$1
      shift
      ;;
  esac
done

case "$url" in
  https://github.com/bastion-computer/bastion/releases/latest)
    if [ -n "$write_out" ]; then
      printf '%s' 'https://github.com/bastion-computer/bastion/releases/tag/v1.0.0'
    fi
    ;;
  'https://api.github.com/repos/bastion-computer/bastion/releases?per_page=100')
    trap 'printf '\''curl: (23) Failure writing output to destination\n'\'' >&2; exit 23' PIPE
    cat <<'JSON'
[
  {
    "tag_name": "v1.1.0",
    "prerelease": false
  },
  {
    "tag_name": "v1.1.0-rc.2",
    "prerelease": true
  },
JSON
    for ((i = 1; i <= 20000; i++)); do
      printf '  {"tag_name":"v0.0.0-%s","prerelease": false},\n' "$i"
    done
    cat <<'JSON'
  {
    "tag_name": "v1.1.0-rc.1",
    "prerelease": true
  }
]
JSON
    ;;
  https://github.com/bastion-computer/bastion/releases/download/*)
    if [ -z "$out" ]; then
      printf 'missing curl output path for %s\n' "$url" >&2
      exit 2
    fi
    cp "$CHECK_INSTALL_RELEASES_DIR/${url##*/}" "$out"
    ;;
  *)
    printf 'unexpected curl URL: %s\n' "$url" >&2
    exit 2
    ;;
esac
SH
  chmod 0755 "$path"
}

prepare_release() {
  local version=$1
  local staging="$TMP_DIR/staging-$version"
  local archive="bastion_${version}_darwin_arm64.tar.gz"

  mkdir -p "$staging" "$TMP_DIR/releases"
  {
    printf '#!/usr/bin/env sh\n'
    printf 'if [ "${1:-}" = "version" ]; then\n'
    printf "  printf '%%s\\n' '%s'\n" "$version"
    printf '  exit 0\n'
    printf 'fi\n'
    printf 'exit 0\n'
  } >"$staging/bastion"
  chmod 0755 "$staging/bastion"

  tar -C "$staging" -czf "$TMP_DIR/releases/$archive" bastion
  (cd "$TMP_DIR/releases" && sha256sum "$archive" >"$archive.sha256")
}

assert_installed_version() {
  local label=$1
  local want=$2
  local install_dir="$TMP_DIR/$label/bin"
  local log="$TMP_DIR/$label.log"
  local got
  shift 2

  mkdir -p "$install_dir"
  PATH="$TMP_DIR/fake-bin:$PATH" \
    CHECK_INSTALL_RELEASES_DIR="$TMP_DIR/releases" \
    BASTION_INSTALL_DIR="$install_dir" \
    bash "$INSTALL_SCRIPT" "$@" >"$log" 2>&1 || {
      cat "$log" >&2
      fail "$label install failed"
    }

  if grep -q 'Failure writing output to destination' "$log"; then
    cat "$log" >&2
    fail "$label install wrote a curl pipe warning"
  fi

  if [ ! -x "$install_dir/bastion" ]; then
    cat "$log" >&2
    fail "$label install did not install bastion"
  fi

  got="$("$install_dir/bastion" version)"
  if [ "$got" != "$want" ]; then
    cat "$log" >&2
    fail "$label installed $got, want $want"
  fi
}

if [ ! -f "$INSTALL_SCRIPT" ]; then
  fail "installer not found: $INSTALL_SCRIPT"
fi

TMP_DIR="$(mktemp -d)"
mkdir -p "$TMP_DIR/fake-bin"
write_fake_uname "$TMP_DIR/fake-bin/uname"
write_fake_curl "$TMP_DIR/fake-bin/curl"
prepare_release v1.0.0
prepare_release v1.1.0-rc.2

assert_installed_version stable v1.0.0

if ! bash "$INSTALL_SCRIPT" --help | grep -q -- '--experimental'; then
  fail "installer help does not document --experimental"
fi

assert_installed_version experimental v1.1.0-rc.2 --experimental

printf 'install script latest and experimental release selection is valid\n'
