---
title: Environments
description: Create, list, inspect, and remove Bastion VM environments.
---

An environment is a Cloud Hypervisor VM restored from a prepared template
snapshot. It has its own process space, network interface, SSH server, and tiny
qcow2 writable root disk overlay backed by the template's immutable prepared
root disk.

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

During creation, Bastion restores the template snapshot, starts DHCP for the
environment TAP network, waits for SSH, and writes the final JSON environment to
stdout. Init actions already ran during `bastion templates create`.

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

## Remove an Environment

```sh
bastion env remove --id env_xxxxxx
bastion env remove --key review-123
```

Removal asks `bastiond` to tear down the VM, cleans up stored VM metadata, and
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

## Creation Logs

`bastion env create` uses the host API streaming creation endpoint. API clients
receive newline-delimited JSON events with these types, though init log events
are normally emitted by `bastion templates create` because init runs during
template preparation:

| Event    | Meaning                                 |
| -------- | --------------------------------------- |
| `log`    | Creation progress output, when present. |
| `result` | Final environment record.               |
| `error`  | Final error and HTTP-style status code. |

The CLI converts `log` events to stderr and prints the `result` environment as
formatted JSON on stdout.
