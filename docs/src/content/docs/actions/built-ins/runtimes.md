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

## `setup_go`

`setup_go` installs the Go toolchain from official Go tarballs.

| Input     | Required | Default                                 | Description                                  |
| --------- | -------- | --------------------------------------- | -------------------------------------------- |
| `version` | No       | Latest stable from Go download metadata | Go version to install, for example `1.25.4`. |

Example:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "use": "setup_go",
        "with": {
          "version": "1.25.4"
        }
      }
    ]
  }
}
```

Omit `version` to install the latest stable Go release reported by
`https://go.dev/dl/?mode=json`:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [{ "use": "setup_go" }]
  }
}
```

The action installs Go to `/usr/local/go`, adds `/usr/local/bin/go` and
`/usr/local/bin/gofmt` symlinks, and configures root shells with
`GOPATH=/root/go` and `/usr/local/go/bin:/root/go/bin` on `PATH`.

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
