---
title: Templates
description: Define reusable Bastion environment templates with JSON.
---

Templates describe how Bastion prepares reusable environment snapshots. During
template creation, Bastion boots a temporary Cloud Hypervisor VM, runs the init
actions, snapshots the paused VM, and stores an immutable prepared root disk.
Environments created from the template restore that snapshot, run any start
actions, and then become ready. Template JSON records are immutable and
validated against the public template schema.
Template keys are optional human-friendly aliases. When a key is set, it must be
unique. Unkeyed templates are referenced by ID.

The current schema is available at
[`/schemas/template.json`](/schemas/template.json).

## Shape

A template has three top-level fields:

| Field       | Required | Description                                     |
| ----------- | -------- | ----------------------------------------------- |
| `resources` | No       | VM CPU, memory, and volume sizing.              |
| `agents`    | Yes      | Agent servers Bastion installs and manages.     |
| `actions`   | Yes      | Lifecycle actions: `init` and optional `start`. |

Minimal template:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": []
  }
}
```

## Resources

Use `resources` to override the default VM allocation.

```json
{
  "resources": {
    "vcpu": 4,
    "memory": 8,
    "volume": 40
  },
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": []
  }
}
```

| Field    | Unit       | Default | Description             |
| -------- | ---------- | ------- | ----------------------- |
| `vcpu`   | vCPU count | `2`     | Number of virtual CPUs. |
| `memory` | GiB        | `2`     | Guest memory size.      |
| `volume` | GiB        | `20`    | Guest root volume size. |

All resource values must be integers greater than or equal to `1`.

## Agents

Every template must declare an `agents` object with `opencode`. Bastion installs
OpenCode during template preparation before `actions.init`, snapshots the result,
and restarts the OpenCode service during environment creation before
`actions.start`.

Minimal agent config:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": []
  }
}
```

OpenCode supports these optional fields:

| Field               | Description                                      |
| ------------------- | ------------------------------------------------ |
| `working_directory` | Guest directory where the OpenCode service runs. |
| `auth`              | JSON object written to OpenCode `auth.json`.     |
| `config`            | JSON object written to OpenCode `opencode.json`. |

Example with provider credentials and a custom model:

```json
{
  "agents": {
    "opencode": {
      "working_directory": "/workspace/project",
      "auth": {
        "anthropic": {
          "type": "api",
          "key": "${{ env.ANTHROPIC_API_KEY }}"
        }
      },
      "config": {
        "model": "anthropic/claude-sonnet-4-20250514",
        "permission": "ask"
      }
    }
  },
  "actions": {
    "init": []
  }
}
```

OpenCode is exposed through the host API at
`/v1/environments/:id/agents/opencode` or
`/v1/environments/by-key/:key/agents/opencode` after an environment is running.
Use `bastion opencode --id ENV_ID` or `bastion opencode --key ENV_KEY` to start
the local OpenCode TUI against that proxied server.

## Lifecycle Actions

`actions.init` is an ordered array of steps that run while the template VM is
being prepared, after it boots and SSH is reachable. If any init action fails,
template creation fails and no reusable template is registered.

`actions.start` is an optional ordered array of steps that run during
`bastion env create`, after the environment is restored from the template
snapshot and SSH is reachable. If any start action fails, environment creation
fails and the environment is recorded in an error state.

Start actions are useful for per-environment setup that should not be baked into
the template snapshot. Use them for work that should happen each time an
environment is created, such as running `git pull` in a cloned repository to get
the latest code changes.

Each init or start action must be one of:

| Action | Description                                                     |
| ------ | --------------------------------------------------------------- |
| `run`  | Shell command executed inside the guest.                        |
| `use`  | Local action package copied into and executed inside the guest. |

### Run Actions

Use `run` for one-off shell commands:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "run": "apt-get update && apt-get install -y git"
      },
      {
        "run": "printf 'ready\\n' > status.txt",
        "working_directory": "/workspace"
      }
    ],
    "start": [
      {
        "run": "printf 'environment ready\\n' > /workspace/start.txt"
      }
    ]
  }
}
```

Commands run as `root` in the guest through `sh -c`.

Run actions support these fields:

| Field               | Required | Description                                                   |
| ------------------- | -------- | ------------------------------------------------------------- |
| `run`               | Yes      | Shell command executed inside the guest.                      |
| `working_directory` | No       | Guest directory to create if needed and run the command from. |

### Action Packages

Use `use` for reusable setup packages stored under `<data-dir>/actions`:

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

The `use` value must start with a letter and can contain letters, numbers,
underscores, and hyphens.

Use actions support these fields:

| Field     | Required | Description                                                     |
| --------- | -------- | --------------------------------------------------------------- |
| `use`     | Yes      | Action package name.                                            |
| `with`    | No       | Manifest-defined scalar inputs for the action package.          |
| `context` | No       | Arbitrary JSON exposed to the action as `BASTION_CONTEXT_FILE`. |

Values under `with` can be strings, numbers, or booleans. Input names must start
with a letter and can contain letters, numbers, and underscores. `context` is not
validated against the action manifest and is useful for structured data such as
environment file contents.

Preset actions can run in either `actions.init` or `actions.start`.

See [Custom Actions](/actions/custom-actions/) for custom action package
layout. Built-in actions are documented by category under
[Utility Tools](/actions/built-ins/utility-tools/) and
[Runtimes](/actions/built-ins/runtimes/).

## Environment Substitution

Bastion resolves host environment variables in template strings when the
template is created:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "run": "printf '%s\\n' '${{ env.PROJECT_NAME }}' > /workspace/project.txt"
      }
    ]
  }
}
```

The expression `${{ env.PROJECT_NAME }}` is replaced with the `PROJECT_NAME`
value from the `bastion start` process environment. If the variable is not set,
template creation fails.

Substitution works anywhere a string appears in the template JSON, including
`agents.opencode`, `actions.init`, `actions.start`, `run` commands,
`working_directory`, action package inputs, and action package context.

## Create a Template

Create an unkeyed template from inline JSON:

```sh
bastion templates create --config '{"agents":{"opencode":{}},"actions":{"init":[]}}'
```

Create a keyed template from inline JSON:

```sh
bastion templates create --key dev --config '{"agents":{"opencode":{}},"actions":{"init":[]}}'
```

Create a keyed template from a file:

```sh
bastion templates create --key dev --file ./template.json
```

Exactly one of `--config` or `--file` is required.

Creation may take several minutes for templates with package installs or other
expensive init work. Bastion streams init logs to stderr and writes the final
template metadata to stdout. Start action logs stream later during
`bastion env create`.

Example response:

```json
{
  "id": "tpl_xxxxxx",
  "key": "dev",
  "createdAt": "<iso_timestamp>"
}
```

Unkeyed template responses omit `key`.

## List Templates

```sh
bastion templates list --limit 20
```

Example response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "tpl_xxxxxx",
      "key": "dev",
      "createdAt": "<iso_timestamp>"
    }
  ]
}
```

The list response contains metadata only. Use `get` to inspect the full
configuration. Unkeyed entries omit `key`.

## Get a Template

Get by key:

```sh
bastion templates get --key dev
```

Key lookup only works for templates created with a key.

Get by ID:

```sh
bastion templates get --id tpl_xxxxxx
```

## Remove a Template

Remove by key:

```sh
bastion templates remove --key dev
```

Key removal only works for templates created with a key.

Remove by ID:

```sh
bastion templates remove --id tpl_xxxxxx
```

Removing a template also removes its prepared snapshot and immutable root disk.
A template cannot be removed while environment records still depend on it.
