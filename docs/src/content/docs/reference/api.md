---
title: API Reference
description: Local HTTP API endpoints exposed by Bastion.
---

The host API is served by `bastion start api` on `http://localhost:3148` by default.
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

## Utilization

```http
GET /v1/utilization
```

Returns host capacity and current allocations for live environments. `memory` and
`volume` values are bytes. `vcpu.total` is calculated from host CPU topology as
physical CPUs x cores per CPU x threads per core.

Used capacity includes environments in `creating`, `running`, and `paused`
states. It excludes `stopped`, `error`, and removed environments.

Response:

```json
{
  "vcpu": {
    "total": 16,
    "used": 2,
    "available": 14
  },
  "memory": {
    "total": 34359738368,
    "used": 2147483648,
    "available": 32212254720
  },
  "volume": {
    "total": 1099511627776,
    "used": 17179869184,
    "available": 1082331758592
  }
}
```

## Secrets

Secrets store sensitive values that templates can reference with
`${{ secret.KEY }}` or `${{ secret.sec_xxxxxx }}`. List and remove responses
return metadata only. `GET` returns the value.

### Create Secret

```http
POST /v1/secrets
Content-Type: application/json
```

Request with an optional unique key. Secret keys cannot start with the reserved
`sec_` ID prefix:

```json
{
  "key": "OPENAI_API_KEY",
  "value": "sk_xxxxxx"
}
```

Response:

```json
{
  "id": "sec_xxxxxx",
  "key": "OPENAI_API_KEY",
  "createdAt": "<iso_timestamp>"
}
```

If no key was provided, the `key` field is omitted from responses.

### List Secrets

```http
GET /v1/secrets?limit=20&cursor=<cursor>
```

Response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "sec_xxxxxx",
      "key": "OPENAI_API_KEY",
      "createdAt": "<iso_timestamp>"
    }
  ]
}
```

### Get Secret

```http
GET /v1/secrets/sec_xxxxxx
GET /v1/secrets/by-key/OPENAI_API_KEY
```

Response:

```json
{
  "id": "sec_xxxxxx",
  "key": "OPENAI_API_KEY",
  "value": "sk_xxxxxx",
  "createdAt": "<iso_timestamp>"
}
```

### Remove Secret

```http
DELETE /v1/secrets/sec_xxxxxx
DELETE /v1/secrets/by-key/OPENAI_API_KEY
```

The response is the removed secret metadata without the secret value.

## Templates

### Create Template

```http
POST /v1/templates
Content-Type: application/json
Accept: application/x-ndjson
```

Request with an optional unique key:

```json
{
  "key": "dev",
  "config": {
    "agents": {
      "opencode": {}
    },
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
    "agents": {
      "opencode": {}
    },
    "actions": {
      "init": []
    }
  }
}
```

Successful and failed template creation both stream newline-delimited JSON.

Log event:

```json
{ "type": "log", "log": "..." }
```

Result event:

```json
{
  "type": "result",
  "template": {
    "id": "tpl_xxxxxx",
    "key": "dev",
    "createdAt": "<iso_timestamp>"
  }
}
```

If no key was provided, the `key` field is omitted from responses.

Error event:

```json
{
  "type": "error",
  "error": "prepare template vm: ...",
  "status": 424
}
```

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
    "agents": {
      "opencode": {}
    },
    "actions": {
      "init": []
    }
  },
  "createdAt": "<iso_timestamp>"
}
```

### Export Template

```http
GET /v1/templates/tpl_xxxxxx/export
GET /v1/templates/by-key/dev/export
Accept: application/vnd.bastion.template+tar+zstd
```

The response body is a zstd-compressed tar archive containing the template config
and prepared VM artifacts. Use the by-key route only for templates that have a
key.

### Import Template

```http
POST /v1/templates/import?key=dev-restored
Content-Type: application/vnd.bastion.template+tar+zstd
```

The `key` query parameter is optional. Imports create a new template ID and do
not preserve the exported ID or key.

Response:

```json
{
  "id": "tpl_xxxxxx",
  "key": "dev-restored",
  "createdAt": "<iso_timestamp>"
}
```

If no key was provided, the `key` field is omitted from the response.

### Remove Template

```http
DELETE /v1/templates/tpl_xxxxxx
DELETE /v1/templates/by-key/dev
```

The by-key route only works for templates that have a key. The response is the
removed template record.

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
If the template defines `actions.start`, those action logs are emitted as log
events before the result event.

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

### Proxy OpenCode Agent

```http
GET /v1/environments/env_xxxxxx/agents/opencode
GET /v1/environments/env_xxxxxx/agents/opencode/<path>
POST /v1/environments/env_xxxxxx/agents/opencode/<path>
GET /v1/environments/by-key/review-123/agents/opencode
GET /v1/environments/by-key/review-123/agents/opencode/<path>
POST /v1/environments/by-key/review-123/agents/opencode/<path>
```

All HTTP methods are proxied to the environment's OpenCode server. The
environment must be running and its template must define `agents.opencode`.

### List Tunnels

```http
GET /v1/environments/env_xxxxxx/tunnels
GET /v1/environments/by-key/review-123/tunnels
```

Response:

```json
{
  "entries": [
    {
      "name": "frontend",
      "port": 3000
    }
  ]
}
```

The environment must be running and its template must define a top-level
`tunnels` object.

### Proxy Tunnel

```http
GET /v1/environments/env_xxxxxx/tunnels/frontend
GET /v1/environments/env_xxxxxx/tunnels/frontend/<path>
POST /v1/environments/env_xxxxxx/tunnels/frontend/<path>
GET /v1/environments/by-key/review-123/tunnels/frontend
GET /v1/environments/by-key/review-123/tunnels/frontend/<path>
POST /v1/environments/by-key/review-123/tunnels/frontend/<path>
```

All HTTP methods are proxied over the environment vsock device to
`localhost:<registered-port>` inside the guest. The host API validates that the
tunnel name is registered on the template before connecting.

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

The daemon also exposes an internal API over its Unix socket. The host API uses
it for `/v1/health`, `POST /v1/vms`, `GET /v1/vms/:id`, and
`DELETE /v1/vms/:id`. Treat this API as an implementation detail unless you are
working on Bastion itself.
