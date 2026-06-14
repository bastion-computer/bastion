---
title: Runtimes
description: Built-in Bastion actions for runtime setup.
---

Runtime actions install language runtimes or runtime managers in guest VMs.

## `setup_node`

`setup_node` installs a Node.js major version from NodeSource.

| Input     | Required | Default | Description                       |
| --------- | -------- | ------- | --------------------------------- |
| `version` | Yes      | None    | Node.js major version to install. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_node",
        "with": {
          "version": 24
        }
      }
    ]
  }
}
```

The action installs the selected Node.js release with apt packages from
NodeSource.

## `setup_bun`

`setup_bun` installs Bun with the official installer.

| Input     | Required | Default | Description                                           |
| --------- | -------- | ------- | ----------------------------------------------------- |
| `version` | No       | Latest  | Bun release tag to install, for example `bun-v1.3.3`. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_bun",
        "with": {
          "version": "bun-v1.3.3"
        }
      }
    ]
  }
}
```

Omit `version` to install the latest Bun release:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_bun" }]
  }
}
```

The action installs Bun to `/usr/local/bin/bun` and verifies the installation
with `bun --version` and `bun --revision`.

## `setup_mise`

`setup_mise` installs mise to `/usr/local/bin/mise` and activates it for root
shells by updating `/root/.bashrc`.

| Input     | Required | Default | Description                                        |
| --------- | -------- | ------- | -------------------------------------------------- |
| `version` | No       | Latest  | mise version to install, for example `v2025.12.0`. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_mise",
        "with": {
          "version": "v2025.12.0"
        }
      }
    ]
  }
}
```

Omit `version` to install the latest mise release:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_mise" }]
  }
}
```

## `setup_rust`

`setup_rust` installs Rust with rustup under `/usr/local/rustup` and
`/usr/local/cargo`.

| Input       | Required | Default   | Description                                                              |
| ----------- | -------- | --------- | ------------------------------------------------------------------------ |
| `toolchain` | No       | `stable`  | Rust toolchain to install, for example `stable`, `nightly`, or `1.85.0`. |
| `profile`   | No       | `default` | rustup profile: `minimal`, `default`, or `complete`.                     |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_rust",
        "with": {
          "toolchain": "stable",
          "profile": "minimal"
        }
      }
    ]
  }
}
```

Omit inputs to install the stable toolchain with rustup's default profile:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_rust" }]
  }
}
```

The action installs apt build prerequisites, adds `/usr/local/cargo/bin` to root
shells, symlinks Rust binaries into `/usr/local/bin`, and verifies Rust by
compiling and running a small program.
