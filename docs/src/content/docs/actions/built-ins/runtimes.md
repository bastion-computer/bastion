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

## `setup_python`

`setup_python` installs Python 3 tooling from configured Ubuntu apt
repositories and can also install `uv` globally.

| Input        | Required | Default             | Description                                                       |
| ------------ | -------- | ------------------- | ----------------------------------------------------------------- |
| `version`    | No       | OS default Python 3 | Python version to install, as a string such as `"3"` or `"3.12"`. |
| `install_uv` | No       | `true`              | Whether to install `uv` to `/usr/local/bin/uv`.                   |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_python",
        "with": {
          "version": "3.12"
        }
      }
    ]
  }
}
```

Use a JSON string for `version`, not a number, so values such as `"3.12"` are
preserved exactly. Omit `version` to install the distro default Python 3
packages:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_python" }]
  }
}
```

The action always installs `python3`, `python3-pip`, `python3-venv`,
`python3-dev`, `ca-certificates`, and `curl`. When `version` is set, it also
installs matching apt packages such as `python3.12`, `python3.12-venv`, and
`python3.12-dev` when available. It updates `/usr/local/bin/python` and
`/usr/local/bin/python3` only when those paths can safely point to the requested
Python binary.

Omit `install_uv` or set it to `true` to install `uv`. Set it to `false` to skip
`uv` installation.

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
