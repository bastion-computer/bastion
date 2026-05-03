---
title: Snapshots
description: A guide to saving and reusing sandbox VM state.
---

Snapshots are saved sandbox states that include memory, CPU, and disk. They can be used to restore a long running session, branch parallel workflows from a checkpoint, or provide a known saved state for delegated commands.

## Create a snapshot

```sh
bastion snapshots create $KEY --sandbox $SANDBOX_ID
```

```json
{
  "id": "snp_xxxxxx",
  "key": "checkpoint",
  "source": {
    "type": "sandbox",
    "id": "sbx_xxxxxx"
  },
  "status": "pending",
  "createdAt": "<iso_timestamp>"
}
```

`$KEY` is the unique label used to reference this snapshot (in the example response its given the value `checkpoint`).

`--sandbox` is the sandbox ID to capture.

Snapshot creation is asynchronous. A newly created snapshot starts in `pending` while bastion captures the sandbox state.

:::note
Sandboxes must be paused before creating a snapshot so the captured memory, CPU, and disk state is consistent.
:::

## List all snapshots

```sh
bastion snapshots list [--limit] [--cursor]
```

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "snp_xxxxxx",
      "key": "checkpoint",
      "source": {
        "type": "sandbox",
        "id": "sbx_xxxxxx"
      },
      "status": "ok",
      "createdAt": "<iso_timestamp>"
    },
    {
      "id": "snp_yyyyyy",
      "key": "staging",
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

Use the resulting snapshot ID or key with `bastion sandbox create --from snapshot` to initialize a new sandbox from the saved state.

## Remove a snapshot

```sh
bastion snapshots remove [--id] [--key]
```

```json
// bastion snapshots remove --id snp_xxxxxx
// or
// bastion snapshots remove --key checkpoint
{
  "id": "snp_xxxxxx",
  "key": "checkpoint",
  "source": {
    "type": "sandbox",
    "id": "sbx_xxxxxx"
  },
  "status": "ok",
  "createdAt": "<iso_timestamp>"
}
```

This command must specify either an `--id` or `--key` value.

Removing a snapshot deletes the saved VM state and prevents creating new sandboxes from that snapshot. It does not remove any running sandboxes that were already created from it.
