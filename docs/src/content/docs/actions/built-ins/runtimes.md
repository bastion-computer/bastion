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
