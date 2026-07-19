---
title: Environments
description: Create, list, inspect, and remove Bastion VM environments.
---

An environment is a Cloud Hypervisor VM with a writable qcow2 root disk overlay
backed by an immutable template overlay, which is itself backed by the shared
base. It has its own process space, network interface, and fresh cloud-init state.

## Create an Environment

Create from a keyed template:

```sh
bastion env create --template-key dev
```

Create from a template ID:

```sh
bastion env create --template-id tpl_xxxxxx
```

Use `--template-id` for unkeyed templates.

Assign an optional unique environment key:

```sh
bastion env create --template-key dev --key review-123
```

Attach tags with repeated `--tag` flags:

```sh
bastion env create --template-key dev --tag repo:bastion --tag agent:review
```

During creation, Bastion creates the writable overlay, starts DHCP for the
environment TAP network, writes fresh cloud-init media, cold-boots the VM, waits
for SSH, runs any `actions.start` steps, and writes the final JSON environment to
stdout. Init actions already ran during template creation.

Example response:

```json
{
  "id": "env_xxxxxx",
  "key": "review-123",
  "status": "running",
  "templateId": "tpl_xxxxxx",
  "tags": ["repo:bastion", "agent:review"],
  "createdAt": "<iso_timestamp>",
  "updatedAt": "<iso_timestamp>"
}
```

Unkeyed environment responses omit `key`.

### Creation Logs

API clients receive newline-delimited JSON events with these types:

| Event    | Meaning                                 |
| -------- | --------------------------------------- |
| `log`    | Creation progress output, when present. |
| `result` | Final environment record.               |
| `error`  | Final error and HTTP-style status code. |

The CLI converts `log` events to stderr and prints the `result` environment as
formatted JSON on stdout.

## List Environments

```sh
bastion env list
```

Limit and paginate results:

```sh
bastion env list --limit 50 --cursor '<cursor>'
```

Filter by tags:

```sh
bastion env list --tag repo:bastion --tag agent:review
```

Repeated tag filters are combined with `AND`, so only environments containing
all requested tags are returned.

Example response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "env_xxxxxx",
      "key": "review-123",
      "status": "running",
      "templateId": "tpl_xxxxxx",
      "tags": ["repo:bastion", "agent:review"],
      "createdAt": "<iso_timestamp>",
      "updatedAt": "<iso_timestamp>"
    }
  ]
}
```

## Get an Environment

```sh
bastion env get --id env_xxxxxx
bastion env get --key review-123
```

`get` reconciles the stored environment record with the daemon before returning
the response.

## Preview Tunnels

List registered tunnel URLs for a running environment:

```sh
bastion env tunnels --id env_xxxxxx
bastion env tunnels --key review-123
```

Example response:

```json
{
  "entries": [
    {
      "name": "frontend",
      "port": 3000,
      "url": "http://localhost:3148/v1/environments/env_xxxxxx/tunnels/frontend"
    }
  ]
}
```

Open the `url` in a host browser to reach the service running on the matching
guest localhost port. The URL uses the same host API base URL that the CLI uses,
including values configured with `bastion client set api-url`.

For web apps that require absolute routes from the origin, start a local proxy
for the tunnel instead:

```sh
bastion proxy --env-key review-123 --name frontend
```

The command prints a local URL such as `http://localhost:3000` and logs proxied
requests to stderr. By default, it uses the tunnel's guest port locally; if that
port is unavailable, it falls back to a free port and logs the fallback. Open the
local URL in your browser; requests for absolute paths like `/assets/app.js` are
forwarded to the named tunnel. Use `--host`, for example `--host 0.0.0.0`, to
serve the proxy somewhere other than `localhost`.

## Remove an Environment

```sh
bastion env remove --id env_xxxxxx
bastion env remove --key review-123
```

Removal asks the daemon to tear down the VM, cleans up stored VM metadata, and
deletes the environment record.

Example response:

```json
{
  "id": "env_xxxxxx",
  "key": "review-123",
  "status": "removed",
  "templateId": "tpl_xxxxxx",
  "tags": ["repo:bastion", "agent:review"],
  "createdAt": "<iso_timestamp>",
  "updatedAt": "<iso_timestamp>"
}
```

## Statuses

Environment status is derived from Cloud Hypervisor runtime state.

| Status     | Meaning                                                             |
| ---------- | ------------------------------------------------------------------- |
| `creating` | Bastion has created the environment record and is launching the VM. |
| `running`  | The VM is live and reachable.                                       |
| `paused`   | Cloud Hypervisor reports the VM as paused.                          |
| `stopped`  | No live VM is present.                                              |
| `error`    | Launch, init, reconciliation, or removal failed.                    |
| `removed`  | Returned by the remove command after successful deletion.           |

When a failure is persisted, responses include `lastError`.

The `/v1/utilization` API counts environments as capacity-consuming when either
their environment status or live VM state is `creating`, `running`, or `paused`.
This includes a live VM whose persisted environment status is temporarily stale.
An environment is excluded only when neither its record nor VM state is one of
those active states.
