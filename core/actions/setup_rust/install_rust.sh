#!/usr/bin/env sh
set -eu

toolchain="${BASTION_INPUT_TOOLCHAIN-stable}"
profile="${BASTION_INPUT_PROFILE-default}"

case "$profile" in
  minimal|default|complete)
    ;;
  *)
    printf 'Rust profile must be one of minimal, default, or complete: %s\n' "$profile" >&2
    exit 2
    ;;
esac

case "$toolchain" in
  ''|*[!A-Za-z0-9._-]*)
    printf 'Rust toolchain must contain only letters, numbers, dots, dashes, and underscores: %s\n' "$toolchain" >&2
    exit 2
    ;;
esac

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl build-essential pkg-config

export RUSTUP_HOME=/usr/local/rustup
export CARGO_HOME=/usr/local/cargo
export PATH="$CARGO_HOME/bin:$PATH"
export RUSTUP_INIT_SKIP_PATH_CHECK=yes

mkdir -p "$RUSTUP_HOME" "$CARGO_HOME" /usr/local/bin

link_root_home_dir() {
  target="$1"
  link="$2"

  if [ -L "$link" ]; then
    ln -sfn "$target" "$link"
    return
  fi

  if [ -e "$link" ]; then
    printf 'Root Rust path already exists and is not a symlink: %s\n' "$link" >&2
    exit 2
  fi

  ln -s "$target" "$link"
}

link_root_home_dir "$RUSTUP_HOME" /root/.rustup
link_root_home_dir "$CARGO_HOME" /root/.cargo

curl --proto '=https' --tlsv1.2 -fsSL https://sh.rustup.rs \
  | sh -s -- -y --no-modify-path --profile "$profile" --default-toolchain "$toolchain"

rustup toolchain install "$toolchain" --profile "$profile"
rustup default "$toolchain"

for binary in rustc cargo rustup; do
  ln -sf "$CARGO_HOME/bin/$binary" "/usr/local/bin/$binary"
done

for binary in rustfmt clippy-driver; do
  if [ -x "$CARGO_HOME/bin/$binary" ]; then
    ln -sf "$CARGO_HOME/bin/$binary" "/usr/local/bin/$binary"
  fi
done

add_bashrc_line() {
  line="$1"
  touch /root/.bashrc
  if ! grep -Fxq "$line" /root/.bashrc; then
    printf '\n%s\n' "$line" >> /root/.bashrc
  fi
}

add_bashrc_line 'export RUSTUP_HOME=/usr/local/rustup'
add_bashrc_line 'export CARGO_HOME=/usr/local/cargo'
add_bashrc_line 'export PATH="/usr/local/cargo/bin:$PATH"'

rustc --version
cargo --version
rustup show active-toolchain

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

printf 'fn main() { println!("rust-ok"); }\n' > "$tmp_dir/main.rs"
rustc "$tmp_dir/main.rs" -o "$tmp_dir/main"

output="$("$tmp_dir/main")"
if [ "$output" != "rust-ok" ]; then
  printf 'Rust smoke test returned %s, want rust-ok\n' "$output" >&2
  exit 1
fi
