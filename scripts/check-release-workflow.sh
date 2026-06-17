#!/usr/bin/env bash
set -euo pipefail

WORKFLOW="${1:-.github/workflows/release.yml}"
TMP_DIR=""

fail() {
  printf 'release workflow check failed: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [ -n "$TMP_DIR" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT

matrix_binaries() {
  local target=$1
  local current=""
  local line

  while IFS= read -r line; do
    line="${line#"${line%%[![:space:]]*}"}"
    case "$line" in
      '- name: '*)
        current="${line#- name: }"
        ;;
      'binaries: '*)
        if [ "$current" = "$target" ]; then
          printf '%s\n' "${line#binaries: }"
          return 0
        fi
        ;;
    esac
  done <"$WORKFLOW"

  return 1
}

make_fake_binary() {
  local path=$1

  if [ -e "$path" ]; then
    return
  fi

  printf '#!/usr/bin/env sh\nexit 0\n' >"$path"
  chmod 0755 "$path"
}

check_target() {
  local target=$1
  local expected=$2
  local actual
  local target_dir
  local archive
  local binary

  actual="$(matrix_binaries "$target")" || fail "missing $target matrix binaries"
  if [ "$actual" != "$expected" ]; then
    fail "$target matrix binaries are '$actual', want '$expected'"
  fi

  target_dir="$TMP_DIR/$target"
  archive="$target_dir/dist/bastion_test_${target}.tar.gz"

  mkdir -p "$target_dir/tmp" "$target_dir/dist" "$target_dir/extract"
  for binary in $expected $actual; do
    make_fake_binary "$target_dir/tmp/$binary"
  done

  tar -C "$target_dir/tmp" -czf "$archive" $actual
  tar -xzf "$archive" -C "$target_dir/extract"

  for binary in $expected; do
    if [ ! -x "$target_dir/extract/$binary" ]; then
      fail "$target archive is missing executable $binary (matrix binaries: $actual)"
    fi
  done
}

if [ ! -f "$WORKFLOW" ]; then
  fail "workflow not found: $WORKFLOW"
fi

TMP_DIR="$(mktemp -d)"
check_target linux_x86_64 'bastion bastion-guest-proxy'
check_target darwin_arm64 'bastion'

printf 'release workflow package contents are valid\n'
