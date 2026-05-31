---
title: API Reference
description: Local HTTP API endpoints exposed by Bastion.
---

The host API is served by `bastion start` on `http://localhost:3148` by default.
The CLI is a client for this API.

## Health

```http
GET /v1/health
```

Response:

```json
{
  "status": "ok"
}
```

## Templates

### Create Template

```http
POST /v1/templates
Content-Type: application/json
```

Request with an optional unique key:

```json
{
  "key": "dev",
  "config": {
    "actions": {
      "init": []
    }
  }
}
```

Request without a key:

```json
{
  "config": {
    "actions": {
      "init": []
    }
  }
}
```

Response:

```json
{
  "id": "tpl_xxxxxx",
  "key": "dev",
  "createdAt": "<iso_timestamp>"
}
```

If no key was provided, the `key` field is omitted from responses.

### List Templates

```http
GET /v1/templates?limit=20&cursor=<cursor>
```

Response:

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

### Get Template

```http
GET /v1/templates/tpl_xxxxxx
GET /v1/templates/by-key/dev
```

The by-key route only works for templates that have a key.

Response:

```json
{
  "id": "tpl_xxxxxx",
  "key": "dev",
  "config": {
    "actions": {
      "init": []
    }
  },
  "createdAt": "<iso_timestamp>"
}
```

### Remove Template

```http
DELETE /v1/templates/tpl_xxxxxx
DELETE /v1/templates/by-key/dev
```

The by-key route only works for templates that have a key. The response is the
removed template record.

## Queues

### Create Queue

```http
POST /v1/queues
Content-Type: application/json
```

Request with an optional unique key:

```json
{
  "key": "linear-task-queue"
}
```

Response:

```json
{
  "id": "que_xxxxxx",
  "key": "linear-task-queue",
  "createdAt": "<iso_timestamp>",
  "updatedAt": "<iso_timestamp>"
}
```

### List Queues

```http
GET /v1/queues?limit=20&cursor=<cursor>
```

Response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "que_xxxxxx",
      "key": "linear-task-queue",
      "createdAt": "<iso_timestamp>",
      "updatedAt": "<iso_timestamp>"
    }
  ]
}
```

### Get Queue

```http
GET /v1/queues/que_xxxxxx
GET /v1/queues/by-key/linear-task-queue
```

### Remove Queue

```http
DELETE /v1/queues/que_xxxxxx
DELETE /v1/queues/by-key/linear-task-queue
```

Removing a queue removes its tasks.

### Publish Task

```http
POST /v1/queues/que_xxxxxx/tasks
POST /v1/queues/by-key/linear-task-queue/tasks
Content-Type: application/json
```

Request:

```json
{
  "retry": {
    "max_attempts": 5,
    "delay_ms": 500,
    "backoff_multiplier": 2,
    "max_delay_ms": 30000,
    "jitter": true
  },
  "data": {
    "issueId": "BAS-11"
  }
}
```

If `retry` is omitted, Bastion applies a default retry policy.

Response:

```json
{
  "id": "task_xxxxxx",
  "queueId": "que_xxxxxx",
  "status": "pending",
  "retry": {
    "max_attempts": 5,
    "delay_ms": 500,
    "max_delay_ms": 30000,
    "backoff_multiplier": 2,
    "jitter": true
  },
  "data": {
    "issueId": "BAS-11"
  },
  "attempts": 0,
  "availableAt": "<iso_timestamp>",
  "createdAt": "<iso_timestamp>",
  "updatedAt": "<iso_timestamp>"
}
```

### Get Task

```http
GET /v1/queues/que_xxxxxx/tasks/task_xxxxxx
GET /v1/queues/by-key/linear-task-queue/tasks/task_xxxxxx
```

Completed tasks include `workerData` when the handler returned data. Failed
tasks include `lastError`; tasks that exhaust retries have status `dead`.

### Worker Endpoints

The queue proxy and host API expose worker endpoints for generated function
workers:

```http
POST /v1/queues/que_xxxxxx/lease
POST /v1/queues/que_xxxxxx/tasks/task_xxxxxx/ack
POST /v1/queues/que_xxxxxx/tasks/task_xxxxxx/fail
```

The same endpoints are available under `/v1/queues/by-key/:key`. A lease returns
`204 No Content` when no task is available.

## Environments

### Create Environment

```http
POST /v1/environments
Content-Type: application/json
Accept: application/x-ndjson
```

Request by template key:

```json
{
  "templateKey": "dev",
  "tags": ["repo:bastion", "agent:review"]
}
```

Request with an optional environment key:

```json
{
  "key": "review-123",
  "templateId": "tpl_xxxxxx",
  "tags": ["repo:bastion", "agent:review"]
}
```

Request by template ID:

```json
{
  "templateId": "tpl_xxxxxx"
}
```

Successful and failed environment creation both stream newline-delimited JSON.

Log event:

```json
{ "type": "log", "log": "..." }
```

Result event:

```json
{
  "type": "result",
  "environment": {
    "id": "env_xxxxxx",
    "key": "review-123",
    "status": "running",
    "templateId": "tpl_xxxxxx",
    "tags": ["repo:bastion", "agent:review"],
    "createdAt": "<iso_timestamp>",
    "updatedAt": "<iso_timestamp>"
  }
}
```

If no environment key was provided, the `key` field is omitted from environment
responses. `templateKey` only works for keyed templates; use `templateId` for
unkeyed templates.

Error event:

```json
{
  "type": "error",
  "error": "launch environment vm: ...",
  "status": 424
}
```

### List Environments

```http
GET /v1/environments?limit=20&cursor=<cursor>&tag=repo:bastion&tag=agent:review
```

Repeated `tag` filters are combined with `AND`.

Response:

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

### Get Environment

```http
GET /v1/environments/env_xxxxxx
GET /v1/environments/by-key/review-123
```

Response:

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

### Remove Environment

```http
DELETE /v1/environments/env_xxxxxx
DELETE /v1/environments/by-key/review-123
```

Response:

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

### Open SSH Stream

```http
POST /v1/environments/env_xxxxxx/ssh
Connection: Upgrade
Upgrade: bastion-ssh
Content-Type: application/json
```

Request:

```json
{
  "command": ["uname", "-a"],
  "pty": false
}
```

Interactive clients can request a PTY and include terminal metadata:

```json
{
  "pty": true,
  "term": "xterm-256color",
  "width": 120,
  "height": 40
}
```

The upgraded connection uses Bastion SSH frames for stdin, stdout, stderr,
terminal resize, exit status, and errors. Prefer the `bastion ssh` CLI unless
you are implementing a custom client.

## Pagination

List endpoints accept:

| Query    | Default | Description                                                         |
| -------- | ------- | ------------------------------------------------------------------- |
| `limit`  | `20`    | Maximum number of entries. Values above `100` are clamped to `100`. |
| `cursor` | empty   | Cursor returned by the previous page.                               |

Responses use this shape:

```json
{
  "cursor": null,
  "entries": []
}
```

If another page exists, `cursor` is a string value to pass to the next request.

## Errors

Non-streaming errors use this shape:

```json
{
  "error": "template not found"
}
```

Domain errors map to these statuses:

| Status | Meaning                                                        |
| ------ | -------------------------------------------------------------- |
| `400`  | Invalid request or validation failure.                         |
| `404`  | Resource not found.                                            |
| `409`  | Conflict, such as duplicate non-null keys or in-use templates. |
| `424`  | Failed dependency, such as a VM runtime issue.                 |
| `500`  | Unexpected server error.                                       |

## Daemon API

`bastiond` also exposes an internal API over its Unix socket. The host API uses
it for `/v1/health`, `POST /v1/vms`, `GET /v1/vms/:id`, and
`DELETE /v1/vms/:id`. Treat this API as an implementation detail unless you are
working on Bastion itself.
