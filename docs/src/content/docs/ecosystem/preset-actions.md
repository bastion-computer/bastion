---
title: Preset Actions
description: Use and author reusable Bastion template actions.
---

Preset actions are reusable packages referenced by `use` actions in templates.
They live under `<data-dir>/actions` on the host and are copied into the guest
when an environment is created.

`bastiond` seeds built-in actions into the data directory when it starts.

## Built-ins

| Action       | Inputs                     | Description                                                              |
| ------------ | -------------------------- | ------------------------------------------------------------------------ |
| `setup_node` | `version` number, required | Installs a Node.js major version from NodeSource.                        |
| `setup_mise` | `version` string, optional | Installs mise to `/usr/local/bin/mise` and activates it for root shells. |

Example template:

```json
{
  "actions": {
    "init": [
      { "use": "setup_mise" },
      { "use": "setup_node", "with": { "version": 24 } }
    ]
  }
}
```

## Package Layout

A preset action is a directory with a `manifest.json` file and any scripts or
files needed by the action.

```text
~/.bastion/actions/setup_python/
├── manifest.json
└── install.sh
```

Example manifest:

```json title="manifest.json"
{
  "inputs": {
    "version": {
      "type": "string",
      "description": "Python version to install.",
      "required": true
    }
  },
  "run": "sh ./install.sh"
}
```

Example script:

```sh title="install.sh"
#!/usr/bin/env sh
set -eu

printf 'Installing Python %s\n' "$BASTION_INPUT_VERSION"
```

## Manifest Fields

| Field    | Required | Description                                                          |
| -------- | -------- | -------------------------------------------------------------------- |
| `run`    | Yes      | Command executed from the action directory inside the guest.         |
| `inputs` | No       | Input definitions accepted from the template action's `with` object. |

Input definitions support these fields:

| Field         | Required | Description                                              |
| ------------- | -------- | -------------------------------------------------------- |
| `type`        | Yes      | One of `string`, `number`, or `boolean`.                 |
| `description` | No       | Human-readable input description.                        |
| `required`    | No       | Whether the input must be provided. Defaults to `false`. |

Unknown inputs, missing required inputs, and type mismatches fail environment
creation.

## Input Environment Variables

Values from `with` are exposed to the action command as environment variables
with the `BASTION_INPUT_` prefix.

| Input          | Environment variable         |
| -------------- | ---------------------------- |
| `version`      | `BASTION_INPUT_VERSION`      |
| `node_version` | `BASTION_INPUT_NODE_VERSION` |

Values are stringified before they are written into the guest-side action input
environment file.

## Use a Custom Action

After adding a directory under `<data-dir>/actions`, reference the directory name
from a template:

```json
{
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

Action names must start with a letter and can contain letters, numbers,
underscores, and hyphens.
