---
title: Templates
description: A guide to defining reusable sandbox environments with declarative configuration.
---

The template system allows developers to declaratively define the environment of their sandboxed VMs to reliably run an agent workflow.

## JSON Schema

Bastion templates are validated against a JSON schema with various options available for instrumenting the VM.

:::note
The full schema is available at <a href="/schemas/template.json" target="_blank" rel="noopener noreferrer">schemas/template.json</a>.
:::

### Primary fields

```json
{
  "resources": {},
  "env": {},
  "delegateCommands": {},
  "networkRules": {},
  "actions": {}
}
```

| Field              | Required | Description                                                                |
| ------------------ | -------- | -------------------------------------------------------------------------- |
| `resources`        | No       | VM resource limits such as vCPU, memory, and volume size.                  |
| `env`              | No       | Environment variables made available inside the VM.                        |
| `delegateCommands` | No       | Named command references that delegate execution to another template.      |
| `networkRules`     | No       | Ingress and egress network access rules for the VM.                        |
| `actions`          | Yes      | Ordered lifecycle actions to run during phases such as `init` and `start`. |

### Resources

Use `resources` to control the VM size allocated to sandboxes created from the template.

```json
{
  "resources": {
    "vcpu": 2,
    "memory": 2,
    "volume": 8
  }
}
```

| Field    | Required | Unit | Default | Description                 |
| -------- | -------- | ---- | ------- | --------------------------- |
| `vcpu`   | No       | vCPU | `2`     | Number of virtual CPUs.     |
| `memory` | No       | GiB  | `2`     | Memory allocated to the VM. |
| `volume` | No       | GiB  | `8`     | Volume size for the VM.     |

### Env

Use `env` to define environment variables made available inside sandboxes created from the template.

```json
{
  "env": {
    "NODE_ENV": "development",
    "PUBLIC_KEY": "${{ env.PUBLIC_KEY }}",
    "ANTHROPIC_API_KEY": "${{ secrets.ANTHROPIC_API_KEY }}"
  }
}
```

| Value type                | Syntax                 | Description                                                    |
| ------------------------- | ---------------------- | -------------------------------------------------------------- |
| Literal value             | `"development"`        | Passes the string value directly into the VM.                  |
| Host environment variable | `"${{ env.NAME }}"`    | Resolves the value from an environment variable on the host.   |
| Secret reference          | `"${{ secrets.NAME }}"` | Resolves the value from a configured bastion secret reference. |

### Delegate Commands

Use `delegateCommands` to proxy named commands to a separate sandbox based on a specified template. This is useful for sensitive operations that should be isolated in a template with narrower access, such as commands that require a private key.

```json
{
  "delegateCommands": {
    "ssh": "ssh-runner",
    "gpg": "tpl_e5f6g7h8"
  }
}
```

| Field              | Required | Description                                                                          |
| ------------------ | -------- | ------------------------------------------------------------------------------------ |
| Command name       | No       | Must start with a letter and can contain letters, numbers, underscores, and hyphens. |
| Template reference | Yes      | Must reference a template by generated ID matching `tpl_*` or by template key.       |

### Network Rules

Use `networkRules` to define which inbound and outbound network traffic is allowed for sandboxes created from the template.

```json
{
  "networkRules": {
    "ingress": [
      {
        "description": "Allow HTTP from trusted internal networks",
        "protocol": "tcp",
        "ports": [80],
        "sources": {
          "cidrs": ["10.0.0.0/24", "fd00:abcd::/64"],
          "templates": ["frontend-service", "tpl_a1b2c3d4"]
        }
      }
    ],
    "egress": [
      {
        "description": "Allow HTTPS to package registries",
        "protocol": "tcp",
        "portRange": {
          "from": 443,
          "to": 443
        },
        "destinations": {
          "cidrs": ["140.82.112.0/20"],
          "hosts": ["registry.npmjs.org"]
        }
      }
    ]
  }
}
```

| Field     | Required | Description                                    |
| --------- | -------- | ---------------------------------------------- |
| `ingress` | No       | Inbound rules. Each item is an ingress object. |
| `egress`  | No       | Outbound rules. Each item is an egress object. |

#### Ingress Objects

Ingress objects define inbound traffic allowed into the VM.

| Field         | Required      | Description                                                                                              |
| ------------- | ------------- | -------------------------------------------------------------------------------------------------------- |
| `description` | No            | Human-readable explanation for the rule.                                                                 |
| `protocol`    | Yes           | Either `tcp` or `udp`.                                                                                   |
| `ports`       | Conditionally | One or more individual ports from `1` to `65535`. Use either `ports` or `portRange`.                     |
| `portRange`   | Conditionally | Inclusive port range with `from` and `to` values from `1` to `65535`. Use either `ports` or `portRange`. |
| `sources`     | Yes           | Inbound source selector using `cidrs` and/or `templates`.                                                |

##### Sources

Sources define where ingress traffic can originate from. At least one of `cidrs` or `templates` is required.

| Field       | Required      | Description                                                              |
| ----------- | ------------- | ------------------------------------------------------------------------ |
| `cidrs`     | Conditionally | One or more IPv4 or IPv6 CIDR ranges.                                    |
| `templates` | Conditionally | One or more template references by generated ID matching `tpl_*` or key. |

#### Egress Objects

Egress objects define outbound traffic allowed from the VM.

| Field          | Required      | Description                                                                                              |
| -------------- | ------------- | -------------------------------------------------------------------------------------------------------- |
| `description`  | No            | Human-readable explanation for the rule.                                                                 |
| `protocol`     | Yes           | Either `tcp` or `udp`.                                                                                   |
| `ports`        | Conditionally | One or more individual ports from `1` to `65535`. Use either `ports` or `portRange`.                     |
| `portRange`    | Conditionally | Inclusive port range with `from` and `to` values from `1` to `65535`. Use either `ports` or `portRange`. |
| `destinations` | Yes           | Outbound destination selector using `cidrs` and/or `hosts`.                                              |

##### Destinations

Destinations define where egress traffic can be sent. At least one of `cidrs` or `hosts` is required.

| Field   | Required      | Description                                                                            |
| ------- | ------------- | -------------------------------------------------------------------------------------- |
| `cidrs` | Conditionally | One or more IPv4 or IPv6 CIDR ranges.                                                  |
| `hosts` | Conditionally | One or more hostnames, including optional wildcard subdomains such as `*.example.com`. |

### Actions

Use `actions` to define ordered lifecycle steps. The `init` phase is required, while `start` is optional.

:::note
See the [ecosystem page]() for a full list of published actions for common setups.
:::

```json
{
  "actions": {
    "init": [
      {
        "use": "github.com/bastion-computer/setup-node",
        "with": {
          "version": "24"
        }
      },
      {
        "use": "github.com/bastion-computer/checkout",
        "with": {
          "repository": "github.com/bastion-computer/bastion"
        }
      },
      {
        "use": "github.com/bastion-computer/setup-opencode",
        "with": {
          "anthropicApiKey": "${{ secrets.ANTHROPIC_API_KEY }}",
          "openaiApiKey": "${{ secrets.OPENAI_API_KEY }}"
        }
      }
    ],
    "start": [
      {
        "run": "npm run dev"
      }
    ]
  }
}
```

| Field   | Required | Description                                               |
| ------- | -------- | --------------------------------------------------------- |
| `init`  | Yes      | Ordered actions executed when the sandbox is initialized. |
| `start` | No       | Ordered actions executed when the sandbox starts.         |

#### Action Objects

Action objects define one executable step. Each action must use either `run` or `use`, but not both.

| Field  | Required      | Description                                                                    |
| ------ | ------------- | ------------------------------------------------------------------------------ |
| `run`  | Conditionally | Inline shell command to run directly in the VM. Cannot be combined with `use`. |
| `use`  | Conditionally | External action script reference. Cannot be combined with `run`.               |
| `with` | No            | Input object passed to a `use` action.                                         |

##### With Inputs

`with` inputs pass structured values to external `use` actions.

| Field       | Required | Description                                                                       |
| ----------- | -------- | --------------------------------------------------------------------------------- |
| Input name  | No       | Name of an input accepted by the external action.                                 |
| Input value | Yes      | Value passed to the external action. Can be a string, number, boolean, or `null`. |

## Create a template

Template definitions are immutable after creation. To change a template, create a new template and remove the old one.

```sh
bastion templates create $KEY --config '<json>'
```

```sh
bastion templates create $KEY --file ./template.json
```

`$KEY` is the unique label used to look up this template.

`--config` accepts an inline JSON template definition.

`--file` reads the template definition from a JSON file.

Exactly one of `--config` or `--file` is required. The provided template definition must match the template JSON schema.

Example:

```sh
bastion templates create node-dev --file ./template.json
```

```json
{
  "id": "tpl_xxxxxx",
  "key": "node-dev",
  "createdAt": "<iso_timestamp>"
}
```

## List all templates

```sh
bastion templates list [--limit] [--cursor]
```

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "tpl_xxxxxx",
      "key": "node-dev",
      "createdAt": "<iso_timestamp>"
    },
    {
      "id": "tpl_yyyyyy",
      "key": "python-agent",
      "createdAt": "<iso_timestamp>"
    }
  ]
}
```

`--limit` is an **optional** value that allows you to cap the number of returned entries. If more entries are available it will return a `cursor` timestamp. Defaults to 20.

`--cursor` is an **optional** timestamp for fetching entries created after this point in time. Defaults to `null`.

The list response returns template metadata only. Use `get` to inspect a template's full configuration.

## Get single template

```sh
bastion templates get [--id] [--key]
```

```json
// bastion templates get --id tpl_xxxxxx
// or
// bastion templates get --key node-dev
{
  "id": "tpl_xxxxxx",
  "key": "node-dev",
  "config": {
    "actions": {
      "init": [
        {
          "use": "github.com/bastion-computer/setup-node",
          "with": {
            "version": "24"
          }
        }
      ]
    }
  },
  "createdAt": "<iso_timestamp>"
}
```

This command must specify either an `--id` or `--key` value.

## Remove a template

```sh
bastion templates remove [--id] [--key]
```

```json
// bastion templates remove --id tpl_xxxxxx
// or
// bastion templates remove --key node-dev
{
  "id": "tpl_xxxxxx",
  "key": "node-dev",
  "config": {
    "actions": {
      "init": [
        {
          "use": "github.com/bastion-computer/setup-node",
          "with": {
            "version": "24"
          }
        }
      ]
    }
  },
  "createdAt": "<iso_timestamp>"
}
```

This command must specify either an `--id` or `--key` value.

Removing a template does not modify existing sandboxes that were already created from it.
