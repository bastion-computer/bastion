---
title: Template configuration
description: Exact JSON schema, lifecycle behavior, resources, agents, tunnels, actions, and secret references for Bastion templates.
---

A template configuration is the JSON value in a template's `config` field.
Bastion validates it against the public
[template JSON Schema](/schemas/template.json) before preparing a template and
again after resolving secret references.

Template keys and IDs are resource metadata supplied to the CLI or API; they are
not fields in the configuration JSON.

## Complete top-level schema

| Field       | Type   | Required | Description                                                               |
| ----------- | ------ | -------- | ------------------------------------------------------------------------- |
| `agents`    | object | Yes      | Agent servers configured in the guest. Must contain `opencode`.           |
| `actions`   | object | Yes      | Ordered `init` actions and optional `start` actions. Must contain `init`. |
| `resources` | object | No       | vCPU, memory, and root-volume allocation.                                 |
| `tunnels`   | object | No       | Named guest localhost HTTP ports.                                         |

Unknown top-level fields are invalid. The smallest valid configuration is:

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

## Resource configuration

`resources` accepts only these fields:

| Field    | Type    | Unit              | Minimum | Omitted value                           |
| -------- | ------- | ----------------- | ------- | --------------------------------------- |
| `vcpu`   | integer | Virtual CPU count | `1`     | `BASTION_VM_CPUS`, then `2`             |
| `memory` | integer | GiB               | `1`     | `BASTION_VM_MEMORY_BYTES`, then `2 GiB` |
| `volume` | integer | GiB               | `1`     | `20 GiB`                                |

Unknown resource fields are invalid. Each host API, daemon, and cluster process
resolves an omitted field independently. The host API uses its result for
utilization accounting, the daemon uses its result for the actual VM, and the
cluster process uses its result for scheduling. Their results can differ.

For cluster templates, explicitly set vcpu, memory, and volume through all three
fields. Explicit values make scheduler accounting and node VM sizing consistent
across process environments; omitted defaults are not a portable allocation
contract.

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

See
[VM sizing configuration](/reference/host-requirements-and-configuration/#vm-sizing-configuration)
for the canonical precedence and process-boundary rules.

## Tunnel configuration

`tunnels` maps a name to a guest TCP port:

```json
{
  "agents": {
    "opencode": {}
  },
  "tunnels": {
    "frontend": 3000,
    "backend": 8080
  },
  "actions": {
    "init": []
  }
}
```

| Constraint   | Value                      |
| ------------ | -------------------------- |
| Name pattern | `^[A-Za-z][A-Za-z0-9_-]*$` |
| Port type    | Integer                    |
| Port range   | `1` through `65535`        |

The port identifies a service listening on `localhost` inside the guest.
Bastion reaches it through the guest proxy over vsock, so the service does not
need to bind to a host-reachable guest interface. Tunnel entries are returned in
name order.

Tunnel routes proxy arbitrary HTTP methods, request paths, query strings,
responses, and HTTP connection upgrades. See the
[host API tunnel routes](/reference/api/host/#environment-tunnel-routes) and
[cluster API proxy behavior](/reference/api/cluster/#environment-proxy-and-ssh-routes).

## Agent configuration

`agents` must contain exactly one supported field, `opencode`. Unknown agent
names are invalid.

`agents.opencode` accepts:

| Field               | Type            | Required | Default                | Description                                             |
| ------------------- | --------------- | -------- | ---------------------- | ------------------------------------------------------- |
| `working_directory` | nonempty string | No       | `/root`                | Directory created for and used by the OpenCode service. |
| `auth`              | object          | No       | No auth file written   | Provider names mapped to OpenCode auth records.         |
| `config`            | object          | No       | No config file written | OpenCode configuration written to `opencode.json`.      |

Unknown fields in `agents.opencode` are invalid. Bastion writes `auth` to
`/root/.local/share/opencode/auth.json`, writes `config` to
`/root/.config/opencode/opencode.json`, and sets both files to mode `0600`.

The base contains the pinned OpenCode binary. During template preparation,
Bastion writes the configuration and enables `bastion-opencode.service` before
`actions.init`. During environment creation, it rewrites the configuration,
starts the service, and makes up to 60 health API attempts. Each attempt has a
one-second connection timeout and a two-second request timeout, and Bastion
sleeps one second after a failed attempt. This is retry behavior, not a hard
60-second elapsed deadline. Bastion runs `actions.start` after a successful
health response.

The managed service always listens on `127.0.0.1`. Its default port is `4096`.
When `config.server.port` is present, it must be an integer from `1` through
`65535`; Bastion uses that port for both the service and API proxy. The rest of
the `config` object is passed to OpenCode without Bastion-specific schema
validation.

### OpenCode API auth records

`auth` maps each nonempty provider name to exactly one of these record shapes.
Unknown fields in a record are invalid.

API key record:

| Field      | Type                      | Required |
| ---------- | ------------------------- | -------- |
| `type`     | Literal `"api"`           | Yes      |
| `key`      | string                    | Yes      |
| `metadata` | object with string values | No       |

```json
{
  "type": "api",
  "key": "${{ secret.OPENAI_API_KEY }}",
  "metadata": {
    "organization": "example"
  }
}
```

OAuth record:

| Field           | Type                                 | Required |
| --------------- | ------------------------------------ | -------- |
| `type`          | Literal `"oauth"`                    | Yes      |
| `refresh`       | string                               | Yes      |
| `access`        | string                               | Yes      |
| `expires`       | integer greater than or equal to `0` | Yes      |
| `accountId`     | string                               | No       |
| `enterpriseUrl` | string                               | No       |

```json
{
  "type": "oauth",
  "refresh": "${{ secret.OAUTH_REFRESH_TOKEN }}",
  "access": "${{ secret.OAUTH_ACCESS_TOKEN }}",
  "expires": 1780000000,
  "accountId": "account-123"
}
```

Well-known record:

| Field   | Type                  | Required |
| ------- | --------------------- | -------- |
| `type`  | Literal `"wellknown"` | Yes      |
| `key`   | string                | Yes      |
| `token` | string                | Yes      |

```json
{
  "type": "wellknown",
  "key": "provider-key",
  "token": "${{ secret.PROVIDER_TOKEN }}"
}
```

### Complete OpenCode example

```json
{
  "agents": {
    "opencode": {
      "working_directory": "/workspace/project",
      "auth": {
        "openai": {
          "type": "api",
          "key": "${{ secret.OPENAI_API_KEY }}"
        }
      },
      "config": {
        "model": "openai/gpt-5.5",
        "permission": "allow",
        "server": {
          "port": 4097
        }
      }
    }
  },
  "actions": {
    "init": []
  }
}
```

## Lifecycle action configuration

`actions` accepts only:

| Field   | Type             | Required | Execution                                                    |
| ------- | ---------------- | -------- | ------------------------------------------------------------ |
| `init`  | array of actions | Yes      | Once, while Bastion prepares the immutable template overlay. |
| `start` | array of actions | No       | Once for each new environment, after a fresh cold boot.      |

Actions execute in array order as `root`. A failed action stops the lifecycle
operation. Template creation does not register a reusable template when an init
action fails. Host and cluster environment creation persist failures
differently; see
[host and cluster persistence](/reference/environment-states-and-streams/#host-and-cluster-persistence).

Each array entry must be exactly one of a run action or a use action. Mixing
their fields is invalid.

### Run action schema

| Field               | Type            | Required | Description                                           |
| ------------------- | --------------- | -------- | ----------------------------------------------------- |
| `run`               | string          | Yes      | Shell command executed in the guest.                  |
| `working_directory` | nonempty string | No       | Directory Bastion creates before running the command. |

Unknown fields are invalid. Although the JSON Schema permits an empty `run`
string, it does not identify an executable action and fails during orchestration.

```json
{
  "run": "mise install",
  "working_directory": "/workspace/project"
}
```

Without `working_directory`, Bastion passes `run` to the guest shell. With a
working directory, Bastion creates it, changes into it, and invokes the command
with `sh -c`.

### Use action schema

| Field     | Type           | Required | Description                                             |
| --------- | -------------- | -------- | ------------------------------------------------------- |
| `use`     | string         | Yes      | Action package name.                                    |
| `with`    | object         | No       | Scalar inputs declared by the package manifest.         |
| `context` | any JSON value | No       | Structured JSON exposed through `BASTION_CONTEXT_FILE`. |

The `use` value must match `^[A-Za-z][A-Za-z0-9_-]*$`. Input names under
`with` must match `^[A-Za-z][A-Za-z0-9_]*$`, and each value must be a string,
number, or boolean. Unknown fields are invalid.

```json
{
  "use": "write_env_file",
  "with": {
    "path": "/workspace/project"
  },
  "context": {
    "NODE_ENV": "development",
    "API_TOKEN": "${{ secret.API_TOKEN }}"
  }
}
```

The package manifest validates `with` at execution time. `context` has no
manifest-defined schema. See the
[action manifest reference](/reference/action-manifest/) and
[built-in actions reference](/reference/built-in-actions/).

## Secret references

Bastion recognizes secret expressions in every JSON string value in the
configuration:

```text
${{ secret.SECRET_KEY }}
${{ secret.sec_16fd2706-8baf-433b-82eb-8c7fada847da }}
```

`SECRET_KEY` represents a secret key. The `sec_16fd...` value is a representative
generated secret ID. Optional whitespace is allowed immediately inside `${{`
and `}}`, but the secret reference itself cannot contain whitespace or `}`.

Only the `secret` expression namespace is supported. A recognized expression
with another namespace is invalid. A missing secret also makes the operation
invalid.

Substitution visits JSON string values in objects and arrays. It never rewrites
object property names, even when a property name contains expression syntax.

The stored template keeps the expression, not the resolved value. Bastion
resolves the full configuration before template preparation and again before
each environment launch:

| Location                                                    | Resolution consequence                                                                  |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `actions.init` and agent setup during template creation     | Resolved values can persist in the immutable template overlay and its archives.         |
| `actions.start` and agent setup during environment creation | Values resolve again from the current secret records for each environment.              |
| Other strings                                               | Values resolve whenever the containing configuration is used in those lifecycle phases. |

> **Warning:** Resolved secrets can be written into guest disks, configuration
> files, action context, command output, and exported template archives. Treat
> templates, environments, logs, and archives as sensitive. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

Cluster source templates retain namespace secret references. The control plane
creates unkeyed derivative secrets on a selected node and rewrites references to
their derivative IDs before node-local orchestration.

## Template lifecycle and immutability

Template preparation performs these phases:

1. Validate the source configuration.
2. Resolve secrets and validate the resolved configuration.
3. Create a qcow2 overlay backed by the current base.
4. Cold-boot a temporary VM with fresh cloud-init media.
5. Configure the OpenCode agent.
6. Run `actions.init` in order.
7. Prepare the guest disk for cloning, stop the VM, and retain the overlay as immutable.

The template records the exact base `contentAddress` as
`baseContentAddress`. Environment creation and template import require the
current base to match. Templates have no update operation; create a new template
to change configuration or init state.

Template archives contain a manifest and the immutable qcow2 overlay. They do
not contain cloud-init media or VM memory state. Init-resolved values may still
exist in the overlay. Archive structure and base-address checks do not establish
authenticity; follow the
[archive validation and trust requirements](/reference/api/host/#archive-validation-and-trust)
before importing an archive.
