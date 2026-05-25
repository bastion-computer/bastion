---
title: Templates
description: Define reusable Bastion environment templates with JSON.
---

Templates describe how Bastion should create an environment. They are stored as
immutable JSON records and validated against the public template schema.
Template keys are optional human-friendly aliases. When a key is set, it must be
unique. Unkeyed templates are referenced by ID.

The current schema is available at
[`/schemas/template.json`](/schemas/template.json).

## Shape

A template has two top-level fields:

| Field       | Required | Description                                                     |
| ----------- | -------- | --------------------------------------------------------------- |
| `resources` | No       | VM CPU, memory, and volume sizing.                              |
| `actions`   | Yes      | Lifecycle actions. The current runtime supports `actions.init`. |

Minimal template:

```json
{
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

## Init Actions

`actions.init` is an ordered array of steps that run after the VM boots and SSH
is reachable. If any init action fails, environment creation fails and the
environment is marked `error`.

Each action must be one of:

| Action | Description                                                     |
| ------ | --------------------------------------------------------------- |
| `run`  | Shell command executed inside the guest.                        |
| `use`  | Local action package copied into and executed inside the guest. |

### Run Actions

Use `run` for one-off shell commands:

```json
{
  "actions": {
    "init": [
      {
        "run": "apt-get update && apt-get install -y git"
      },
      {
        "run": "mkdir -p /workspace"
      }
    ]
  }
}
```

Commands run as `root` in the guest through `sh -c`.

### Action Packages

Use `use` for reusable setup packages stored under `<data-dir>/actions`:

```json
{
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
underscores, and hyphens. Values under `with` can be strings, numbers, or
booleans. Input names must start with a letter and can contain letters,
numbers, and underscores.

See [Custom Actions](/ecosystem/custom-actions/) for custom action package
layout. Built-in actions are documented by category under
[Coding Agents](/ecosystem/built-ins/coding-agents/),
[Utility Tools](/ecosystem/built-ins/utility-tools/), and
[Runtimes](/ecosystem/built-ins/runtimes/).

## Environment Substitution

Bastion resolves host environment variables in template strings when an
environment is created:

```json
{
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
environment creation fails.

Substitution works anywhere a string appears in the template JSON, including
`run` commands and action package inputs.

## Create a Template

Create an unkeyed template from inline JSON:

```sh
bastion templates create --config '{"actions":{"init":[]}}'
```

Create a keyed template from inline JSON:

```sh
bastion templates create --key dev --config '{"actions":{"init":[]}}'
```

Create a keyed template from a file:

```sh
bastion templates create --key dev --file ./template.json
```

Exactly one of `--config` or `--file` is required.

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

Removing a template does not remove environments that were already created from
it. A template cannot be removed while records still depend on it.
