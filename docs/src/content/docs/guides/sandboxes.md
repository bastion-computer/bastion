---
title: Sandboxes
description: A guide to creating and managing isolated VM environments for agents.
---

Sandboxes are isolated [Firecracker microVMs](https://firecracker-microvm.github.io/) where agents run with full access to their own Linux environment while remaining separated from the host system.

Sandboxes can be created from a template or from a checkpoint. Creating from a template initializes a fresh VM using declarative configuration, while creating from a checkpoint restores a saved VM state including memory, CPU, and disk.

## Create a sandbox

```sh
bastion sandbox create --from [template|checkpoint] [--id] [--key]
```

```json
// bastion sandbox create --from template --id tpl_xxxxxx
// or
// bastion sandbox create --from template --key dev-env
{
  "id": "sbx_xxxxxx",
  "status": "pending",
  "source": {
    "type": "template",
    "id": "tpl_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

```json
// bastion sandbox create --from checkpoint --id chk_xxxxxx
// or
// bastion sandbox create --from checkpoint --key dev-env/branch01
{
  "id": "sbx_yyyyyy",
  "status": "pending",
  "source": {
    "type": "checkpoint",
    "id": "chk_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

This command must specify a `--from` value and either an `--id` or `--key` value.

`--from` is the source type used to initialize the sandbox. It must be either `template` or `checkpoint`.

`--id` references the selected source by its generated ID.

`--key` references the selected source by its user-defined key.

Sandbox creation is asynchronous. A newly created sandbox starts in `pending` while bastion initializes the VM. Creating from a template runs the template lifecycle actions, while creating from a checkpoint restores the saved VM state.

## List all sandboxes

```sh
bastion sandbox list [--limit] [--cursor]
```

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "sbx_xxxxxx",
      "status": "running",
      "source": {
        "type": "template",
        "id": "tpl_xxxxxx"
      },
      "createdAt": "<iso_timestamp>"
    },
    {
      "id": "sbx_yyyyyy",
      "status": "paused",
      "source": {
        "type": "checkpoint",
        "id": "chk_xxxxxx"
      },
      "createdAt": "<iso_timestamp>"
    }
  ]
}
```

`--limit` is an **optional** value that allows you to cap the number of returned entries. If more entries are available it will return a `cursor` timestamp. Defaults to 20.

`--cursor` is an **optional** timestamp for fetching entries created after this point in time. Defaults to `null`.

## Get single sandbox

```sh
bastion sandbox get $SANDBOX_ID
```

```json
{
  "id": "sbx_xxxxxx",
  "status": "running",
  "source": {
    "type": "template",
    "id": "tpl_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

Use the sandbox ID returned by `create` or `list` to fetch a single sandbox.

## Pause a sandbox

```sh
bastion sandbox pause $SANDBOX_ID
```

```json
{
  "id": "sbx_xxxxxx",
  "status": "paused",
  "source": {
    "type": "template",
    "id": "tpl_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

Pausing a sandbox stops VM execution while preserving its state so a checkpoint can be created.

> _See the extended guide on [checkpoints](/guides/checkpoints) for creating and managing saved VM state._

## Remove a sandbox

```sh
bastion sandbox remove $SANDBOX_ID
```

```json
{
  "id": "sbx_xxxxxx",
  "status": "paused",
  "source": {
    "type": "template",
    "id": "tpl_xxxxxx"
  },
  "createdAt": "<iso_timestamp>"
}
```

Removing a sandbox tears down the VM and removes it from the sandbox list.
