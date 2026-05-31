---
title: Custom Queues
description: Publish durable tasks and process them with template functions.
---

Bastion queues store durable JSON tasks that are consumed by functions running in
environments. Use queues when an external service or webhook should hand work to
a Bastion environment without opening direct access to the environment.

## Create a Queue

Queues can be referenced by ID or by an optional unique key.

```sh
bastion queues create --key linear-task-queue
```

Publish tasks with arbitrary JSON data and optional retry settings:

```sh
bastion queues publish --key linear-task-queue \
  --data '{"issueId":"BAS-11","action":"sync"}' \
  --retry '{"max_attempts":5,"delay_ms":500,"backoff_multiplier":2,"max_delay_ms":30000,"jitter":true}'
```

If `retry` is omitted, Bastion applies a default retry policy. Failed tasks are
moved to the dead-letter state after all attempts are exhausted.

## Function Packages

Functions live under `<data-dir>/functions/<function_name>` on the host. A
function package contains a `manifest.json` and a TypeScript handler.

```text
~/.bastion/functions/linear_task/
├── manifest.json
└── index.ts
```

Example manifest:

```json title="manifest.json"
{
  "inputs": {
    "apiKey": {
      "type": "string",
      "description": "API key used by the handler.",
      "required": true
    }
  },
  "handler": "index.ts"
}
```

Example handler:

```ts title="index.ts"
export default async function handler({ inputs, data }) {
  console.log("processing", data.issueId, inputs.apiKey.length);
  return { processed: true, issueId: data.issueId };
}
```

Returning from the handler ACKs the task. The return value is stored as
`workerData` on the task. Throwing an error records the failure and schedules a
retry or moves the task to the dead-letter state.

## Template Functions

Add a `functions` object to a template. The function name must match the package
directory under `<data-dir>/functions`.

```json
{
  "functions": {
    "linear_task": {
      "trigger": {
        "type": "queue",
        "key": "linear-task-queue"
      },
      "with": {
        "apiKey": "${{ env.LINEAR_API_KEY }}"
      }
    }
  },
  "actions": {
    "init": []
  }
}
```

Only queue triggers are currently supported. Use exactly one of `id` or `key` in
the trigger.

When an environment is created from a template with functions, Bastion installs
Bun in the guest, copies the function package into the environment, and starts a
worker process. The function does not need polling or ACK boilerplate.

## Queue Proxy

Guest workers call a lightweight queue proxy bound to the VM TAP host IP instead
of the main Bastion API. The proxy uses `QUEUE_PROXY_PORT`, which defaults to
`3150`.

The main API still listens on `localhost:3148` by default and does not need to be
bound to `0.0.0.0` for queues to work.

## Task States

| State      | Meaning                                      |
| ---------- | -------------------------------------------- |
| `pending`  | The task is waiting for an available worker. |
| `leased`   | A worker has locked the task.                |
| `complete` | The worker ACKed the task.                   |
| `dead`     | Retries were exhausted.                      |

Inspect a task:

```sh
bastion queues tasks get --key linear-task-queue --task-id task_xxxxxx
```
