---
title: Templates
description: Define reusable Bastion environment templates with JSON.
---

Templates describe how Bastion prepares replicable dev environments. During template
creation, Bastion creates a qcow2 overlay backed by the shared [base](/guides/base/),
boots a temporary Cloud Hypervisor VM, runs the init actions, and stores the resulting
overlay as immutable. Every Bastion environment VM is then backed by a prepared
template.

The current template schema is available at [`/schemas/template.json`](/schemas/template.json).

## Shape

A template has four top-level fields:

| Field       | Required | Description                                     |
| ----------- | -------- | ----------------------------------------------- |
| `resources` | No       | VM CPU, memory, and volume sizing.              |
| `tunnels`   | No       | Named localhost ports exposed through the API.  |
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

## Tunnels

Use `tunnels` to register named HTTP ports that should be reachable through the
Bastion host API after an environment starts:

```json
{
  "agents": {
    "opencode": {}
  },
  "tunnels": {
    "frontend": 3000,
    "backend": 3001
  },
  "actions": {
    "init": []
  }
}
```

The tunnel name is used in URLs such as
`/v1/environments/:id/tunnels/frontend`. The port is the guest-side localhost
port. Services do not need to bind `0.0.0.0`; Bastion reaches them through a
guest proxy over Cloud Hypervisor vsock and connects to `localhost:<port>` from
inside the VM.

Tunnel names must start with a letter and may contain letters, numbers,
underscores, and hyphens. Ports must be integers from `1` to `65535`.

## Agents

Every template must declare an `agents` object with `opencode`. Base construction
installs the pinned OpenCode binary. Template preparation writes the configured
auth, config, working directory, and service definition before `actions.init`. Environment
creation refreshes that configuration and restarts the service before `actions.start`.

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

`auth` and `config` uses the same [schema](https://opencode.ai/docs/config/) defined
by OpenCode.

Example with provider credentials and a custom model:

```json
{
  "agents": {
    "opencode": {
      "working_directory": "/workspace/project",
      "auth": {
        "anthropic": {
          "type": "api",
          "key": "${{ secret.ANTHROPIC_API_KEY }}"
        }
      },
      "config": {
        "model": "anthropic/claude-opus-4-8",
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
environment creation, after the VM cold-boots from its new writable overlay
and SSH is reachable. If any start action fails, environment creation fails and
the environment is recorded in an error state.

Start actions are useful for per-environment setup that should not be persisted
in the template overlay. Use them for work that should happen each time an
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

See [custom actions](/actions/custom-actions/) for custom action package
layout. Built-in actions are documented by category under
[Utility Tools](/actions/built-ins/utility-tools/) and
[Runtimes](/actions/built-ins/runtimes/).

## Secret References

Bastion resolves secret references in template strings before the config is sent
to VM orchestration. Store a secret first:

```sh
bastion secrets create --key PROJECT_NAME --value acme-app
```

Then reference it from template JSON:

```json
{
  "agents": {
    "opencode": {}
  },
  "actions": {
    "init": [
      {
        "run": "printf '%s\\n' '${{ secret.PROJECT_NAME }}' > /workspace/project.txt"
      }
    ]
  }
}
```

Use `${{ secret.KEY }}` for keyed secrets or `${{ secret.sec_xxxxxx }}` for a
secret ID. Secret keys cannot start with `sec_`, which is reserved for secret
IDs. If the secret does not exist, template creation or environment creation
fails.

The stored template config keeps the original reference syntax. `templates get`
does not return secret values. Resolution works anywhere a string appears in the
template JSON, including `agents.opencode`, `actions.init`, `actions.start`,
`run` commands, `working_directory`, action package inputs, and action package
context.

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

Creation requires an existing base. If no base has been built or imported,
Bastion returns a failed dependency error. See the [base guide](/guides/base/)
for setup instructions.

Creation may take several minutes for templates with package installs or other
expensive init work. Bastion streams init logs to stderr and writes the final
template metadata to stdout. Start action logs stream later during
environment creation.

Example response:

```json
{
  "id": "tpl_xxxxxx",
  "key": "dev",
  "baseContentAddress": "sha256:...",
  "createdAt": "<iso_timestamp>"
}
```

Unkeyed template responses omit `key`.

## List Templates

```sh
bastion templates list
```

Example response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "tpl_xxxxxx",
      "key": "dev",
      "baseContentAddress": "sha256:...",
      "createdAt": "<iso_timestamp>"
    }
  ]
}
```

The list response contains metadata only. `baseContentAddress` identifies the
base required to import or launch the template. Use `get` to inspect the full
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

## Export and Import Templates

Export a prepared template archive by key:

```sh
bastion templates export --key dev > dev-template.tar.zst
```

Export by ID:

```sh
bastion templates export --id tpl_xxxxxx > dev-template.tar.zst
```

The archive contains a manifest with the stored config and base content address,
plus the prepared qcow2 root disk overlay. Use it for backups or to replicate a
prepared template to another Bastion host without rerunning init actions.

Import with a new key:

```sh
bastion templates import --key dev-restored --file ./dev-template.tar.zst
```

Import without a key:

```sh
bastion templates import --file ./dev-template.tar.zst
```

Imports always create a new template ID. They do not preserve the exported ID or
key; pass `--key` when the restored template should have a human-friendly alias.

The destination must already have the exact base identified by the template
archive. Export and import the base before its templates:

```sh
# Source host
bastion base export > base.tar.zst
bastion templates export --key dev > dev-template.tar.zst

# Destination host
bastion base import --file ./base.tar.zst
bastion templates import --key dev --file ./dev-template.tar.zst
```

Exported configs keep secret reference syntax, but the prepared disk overlay can
contain values resolved during init actions. Treat exported archives as
sensitive backup material.

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

Removing a template also removes its immutable root disk overlay and prepared
metadata. A template cannot be removed while environment records still depend
on it.
