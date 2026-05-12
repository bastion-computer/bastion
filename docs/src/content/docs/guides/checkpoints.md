---
title: Checkpoints
description: A guide to saving and reusing sandbox VM state.
---

Checkpoints are saved sandbox states that include memory, CPU, and disk. They can be used to restore a long running session, branch parallel workflows from a checkpoint, or provide a known saved state for delegated commands.

## Create a checkpoint

```sh
bastion checkpoints create $KEY --sandbox $SANDBOX_ID
```

```json
{
  "id": "chk_xxxxxx",
  "key": "dev-env/branch01",
  "source": {
    "type": "sandbox",
    "id": "sbx_xxxxxx"
  },
  "status": "pending",
  "createdAt": "<iso_timestamp>"
}
```

`$KEY` is the unique label used to reference this checkpoint (in the example response its given the value `dev-env/branch01`).

`--sandbox` is the sandbox ID to capture.

Checkpoint creation is asynchronous. A newly created checkpoint starts in `pending` while bastion captures the sandbox state.

:::note
Sandboxes must be paused before creating a checkpoint so the captured memory, CPU, and disk state is consistent.
:::

## List all checkpoints

```sh
bastion checkpoints list [--limit] [--cursor]
```

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "chk_xxxxxx",
      "key": "dev-env/branch01",
      "source": {
        "type": "sandbox",
        "id": "sbx_xxxxxx"
      },
      "status": "ok",
      "createdAt": "<iso_timestamp>"
    },
    {
      "id": "chk_yyyyyy",
      "key": "staging/branch01",
      "source": {
        "type": "sandbox",
        "id": "sbx_yyyyyy"
      },
      "status": "pending",
      "createdAt": "<iso_timestamp>"
    }
  ]
}
```

`--limit` is an **optional** value that allows you to cap the number of returned entries. If more entries are available it will return a `cursor` timestamp. Defaults to 20.

`--cursor` is an **optional** timestamp for fetching entries created after this point in time. Defaults to `null`.

Use the resulting checkpoint ID or key with `bastion sandbox create --from checkpoint` to initialize a new sandbox from the saved state.

## Get single checkpoint

```sh
bastion checkpoints get [--id] [--key]
```

```json
// bastion checkpoints get --id chk_xxxxxx
// or
// bastion checkpoints get --key dev-env/branch01
{
  "id": "chk_xxxxxx",
  "key": "dev-env/branch01",
  "source": {
    "type": "sandbox",
    "id": "sbx_xxxxxx"
  },
  "status": "ok",
  "createdAt": "<iso_timestamp>"
}
```

This command must specify either an `--id` or `--key` value.

## Remove a checkpoint

```sh
bastion checkpoints remove [--id] [--key]
```

```json
// bastion checkpoints remove --id chk_xxxxxx
// or
// bastion checkpoints remove --key dev-env/branch01
{
  "id": "chk_xxxxxx",
  "key": "dev-env/branch01",
  "source": {
    "type": "sandbox",
    "id": "sbx_xxxxxx"
  },
  "status": "ok",
  "createdAt": "<iso_timestamp>"
}
```

This command must specify either an `--id` or `--key` value.

Removing a checkpoint deletes the saved VM state and prevents creating new sandboxes from that checkpoint. It does not remove any running sandboxes that were already created from it.
