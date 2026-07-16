---
title: Environment states and streams
description: Environment status transitions, reconciliation, NDJSON operation events, and the Bastion SSH framing protocol.
---

Environment records expose lifecycle state through `status`, optional
`lastError`, and timestamps. Long-running creation operations use
newline-delimited JSON (NDJSON). SSH uses a separate upgraded, framed byte
stream.

## Environment response shape

```json
{
  "id": "env_550e8400-e29b-41d4-a716-446655440000",
  "key": "review-123",
  "status": "running",
  "templateId": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "tags": ["repo:bastion", "agent:review"],
  "createdAt": "2026-07-16T12:00:00Z",
  "updatedAt": "2026-07-16T12:01:30Z"
}
```

| Field        | Type             | Description                                                   |
| ------------ | ---------------- | ------------------------------------------------------------- |
| `id`         | string           | Generated `env_`-prefixed UUID v4.                            |
| `key`        | string           | Optional user-provided key; omitted for unkeyed environments. |
| `status`     | string           | Current lifecycle state.                                      |
| `templateId` | string           | Source template ID.                                           |
| `tags`       | array of strings | Tags in creation order; empty when no tags were supplied.     |
| `createdAt`  | RFC 3339 string  | Creation time in UTC.                                         |
| `updatedAt`  | RFC 3339 string  | Last persisted state-change time in UTC.                      |
| `lastError`  | string           | Most recent persisted runtime error; omitted when empty.      |

## Environment statuses

| Status     | Meaning                                                                                          |
| ---------- | ------------------------------------------------------------------------------------------------ |
| `creating` | VM creation is in progress.                                                                      |
| `running`  | The VM is live, guest setup completed, and managed services are ready.                           |
| `paused`   | Cloud Hypervisor reports the VM as paused. Bastion has no public pause or resume route.          |
| `stopped`  | No live VM is present.                                                                           |
| `error`    | A persisted host or derivative runtime failed. `lastError` normally describes the failure.       |
| `removing` | Host removal is in progress.                                                                     |
| `removed`  | Removal completed. This value is returned in a delete response, not stored as a listable record. |

Template creation uses a temporary VM but does not expose an environment record
or these statuses.

## Host and cluster persistence

The host API and cluster control plane persist environment operations at
different points. Do not infer cluster visibility from host behavior.

### Host persistence

The host API inserts an environment record with `creating` before asking the
daemon to launch it, so concurrent reads can observe creation. Validation,
template lookup, and secret-resolution failures that occur before that insert do
not create a record. A launch or guest-setup failure after insertion normally
changes the stored record to `error` and writes `lastError`.

Host deletion first persists `removing`. A daemon removal failure changes the
record to `error` and retains it. On success, the host deletes VM metadata and
the environment record; only the delete response contains `removed`. If VM
metadata or record deletion fails after daemon cleanup, the request returns an
error and a surviving record can remain in `removing`.

Host list and get requests reconcile stored state with the daemon. They can
persist `paused`, `stopped`, `error`, and a changed `lastError`. When the daemon
reports `stopped`, reconciliation also removes stale VM metadata.

### Cluster persistence

The cluster control plane creates the derivative template and environment on a
node before inserting the source environment. A normal cluster create therefore
does not expose a source record in `creating`. Failure before the insert returns
an error without a source environment record; cleanup of newly created
derivatives is best effort. The inserted source starts with the derivative's
returned status and `lastError`.

Cluster list and get requests copy a derivative's status and error into the
source record. A missing derivative becomes `stopped`. Another node error
returns `424 Failed Dependency` without replacing the source status with
`error`. A cluster source reaches `error` only by mirroring a derivative that is
already in that state.

Cluster deletion does not persist `removing` or an error state. It first asks
the node to delete the derivative environment. If that fails, the source record
remains. If source deletion then fails, the source can remain while its missing
derivative later reconciles to `stopped`. After source deletion, the control
plane tries to clean an unused derivative template and secrets. That cleanup can
fail after the source is already gone, so an error response does not prove that
deletion was rolled back. Only the response of a fully successful delete
contains `removed`.

### Host status-dependent operations

| Operation                         | Accepted state                                                                                           |
| --------------------------------- | -------------------------------------------------------------------------------------------------------- |
| List or get environment           | Any stored state; the request attempts reconciliation first unless the state is `removing` or `removed`. |
| List or proxy a registered tunnel | `running` only.                                                                                          |
| Proxy the OpenCode agent          | `running` only.                                                                                          |
| Open host API SSH                 | `running` or `paused`; a paused guest may not make useful progress until resumed outside the public API. |
| Remove environment                | Any state that can be loaded and reconciled; runtime removal failures change the record to `error`.      |

Cluster tunnel and OpenCode requests require the source to reconcile to
`running`. Cluster SSH ultimately uses the node host API's `running` or `paused`
rule. See
[namespaced environment routes](/reference/api/cluster/#namespaced-environment-routes)
for cluster-specific failures.

## Capacity accounting

Cluster environment creation selects a node only when all resolved resource
dimensions fit. Capacity-consuming states are `creating`, `running`, and
`paused`.

Host utilization counts a resource when either the persisted environment status
or its live VM state is one of those three active states. This avoids reporting
capacity as free while one representation is temporarily stale.

## NDJSON operation streams

These public operations stream `application/x-ndjson`:

| API              | Operation          | Result field  |
| ---------------- | ------------------ | ------------- |
| Host and cluster | Build base         | `base`        |
| Host and cluster | Import base        | `base`        |
| Host and cluster | Create template    | `template`    |
| Host and cluster | Create environment | `environment` |
| Cluster only     | Create node        | `node`        |

The response also sets `Cache-Control: no-cache`. Every event is one JSON object
followed by a newline. A `log` string can itself contain one or more newline
characters because it represents a write chunk, not necessarily one rendered
log line.

Once streaming starts, the HTTP response status is `200 OK`. A later logical
failure cannot change that status, so the final `error` event carries the
effective status code. Clients must read through a terminal `result` or `error`
event rather than treating HTTP `200` as operation success.

A malformed JSON request or invalid query parameter detected before streaming
starts can still return a normal `400 Bad Request` JSON error. A connection that
ends before `result` or `error` is incomplete and must not be treated as success.

### Log events

```json
{ "type": "log", "log": "installing packages\n" }
```

| Field  | Type          | Description                       |
| ------ | ------------- | --------------------------------- |
| `type` | literal `log` | Event discriminator.              |
| `log`  | string        | Progress or guest command output. |

Cluster operations include control-plane messages prefixed with `cluster:` and
can interleave them with node-level logs. Concurrent base synchronization can
also interleave log chunks from multiple nodes.

### Result events

Exactly one operation-specific object is present on success.

Base result:

```json
{
  "type": "result",
  "base": {
    "contentAddress": "sha256:0123456789abcdef...",
    "createdAt": "2026-07-16T12:00:00Z",
    "updatedAt": "2026-07-16T12:00:00Z"
  }
}
```

Template result:

```json
{
  "type": "result",
  "template": {
    "id": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "key": "dev",
    "baseContentAddress": "sha256:0123456789abcdef...",
    "createdAt": "2026-07-16T12:00:00Z"
  }
}
```

Environment result:

```json
{
  "type": "result",
  "environment": {
    "id": "env_550e8400-e29b-41d4-a716-446655440000",
    "status": "running",
    "templateId": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "tags": [],
    "createdAt": "2026-07-16T12:00:00Z",
    "updatedAt": "2026-07-16T12:01:30Z"
  }
}
```

Cluster node result:

```json
{
  "type": "result",
  "node": {
    "id": "node_6ba7b810-9dad-41d1-80b4-00c04fd430c8",
    "key": "node-a",
    "url": "http://node-a.internal:3148",
    "createdAt": "2026-07-16T12:00:00Z"
  }
}
```

Optional `key` fields are omitted when the resource has no key.

### Error events

```json
{
  "type": "error",
  "error": "failed dependency: launch environment vm: ...",
  "status": 424
}
```

| Field    | Type            | Description                       |
| -------- | --------------- | --------------------------------- |
| `type`   | literal `error` | Event discriminator.              |
| `error`  | string          | Human-readable operation error.   |
| `status` | integer         | Effective HTTP-style status code. |

The effective status depends on the route and where orchestration failed. Use
the canonical [host API errors](/reference/api/host/#normal-errors) or
[cluster API errors](/reference/api/cluster/#pagination-and-errors) for status
meanings and each route section for narrower cases.

### CLI stream handling

The CLI writes `log` values to stderr, renders the terminal result as indented
JSON on stdout, and returns an error for a terminal `error` event. Base and
template archive progress also goes to stderr so stdout remains safe for binary
redirection on export. An interrupted binary export can still leave a partial
file on stdout; a nonempty file is not proof of success.

## SSH upgrade stream

SSH does not use NDJSON. A client sends a JSON request to the environment SSH
route with an HTTP upgrade:

> **Warning:** Guest host keys are not verified. The host API uses the stored
> private key for client authentication but accepts any SSH host key presented
> at the guest address. Keep the API, daemon, VM network, and cluster node path
> inside a trusted boundary.

```http
POST /v1/environments/{ENVIRONMENT_ID}/ssh HTTP/1.1
Connection: Upgrade
Upgrade: bastion-ssh
Content-Type: application/json
```

`ENVIRONMENT_ID` represents an `env_`-prefixed environment ID. The API has no
by-key SSH route; clients resolve a key to an ID first.

Command request:

```json
{
  "command": ["uname", "-a"]
}
```

Interactive PTY request:

```json
{
  "pty": true,
  "term": "xterm-256color",
  "width": 120,
  "height": 40
}
```

| Request field | Type             | Default                          | Description                                                   |
| ------------- | ---------------- | -------------------------------- | ------------------------------------------------------------- |
| `command`     | array of strings | Interactive shell                | Remote command elements joined into one shell command string. |
| `pty`         | boolean          | `false`                          | Request a pseudo-terminal.                                    |
| `term`        | string           | `xterm` when `pty` is true       | Terminal type.                                                |
| `width`       | integer          | `80` when missing or nonpositive | Initial terminal columns.                                     |
| `height`      | integer          | `24` when missing or nonpositive | Initial terminal rows.                                        |

The command array does not preserve argument boundaries. The server joins its
elements with single spaces and passes the resulting string to the guest SSH
shell without protocol-level quoting. For example, one element that contains a
space becomes indistinguishable from two elements. A custom client must apply
correct guest-shell quoting to every argument and must never interpolate
untrusted input into the command string. Prefer an interactive shell or a
purpose-built guest API when safe quoting is not possible.

After validation and SSH metadata lookup, the API responds:

```http
HTTP/1.1 101 Switching Protocols
Connection: Upgrade
Upgrade: bastion-ssh
```

If validation or environment lookup fails before the upgrade, the route returns
a normal JSON error and does not switch protocols.

### SSH frame format

Each frame has a five-byte header followed by its payload:

| Header bytes          | Encoding                                                           |
| --------------------- | ------------------------------------------------------------------ |
| Byte `0`              | Unsigned frame type.                                               |
| Bytes `1` through `4` | Unsigned 32-bit payload length in network byte order (big-endian). |
| Remaining bytes       | Payload of the declared length.                                    |

The maximum payload is 1 MiB (`1 << 20` bytes). Oversized frames are invalid.

| Type | Name      | Direction     | Payload                                                                              |
| ---- | --------- | ------------- | ------------------------------------------------------------------------------------ |
| `1`  | stdin     | Client to API | Raw stdin bytes.                                                                     |
| `2`  | stdin EOF | Client to API | Empty payload.                                                                       |
| `3`  | resize    | Client to API | JSON object such as `{"width":120,"height":40}`. Nonpositive dimensions are ignored. |
| `4`  | stdout    | API to client | Raw remote stdout bytes.                                                             |
| `5`  | stderr    | API to client | Raw remote stderr bytes.                                                             |
| `6`  | exit      | API to client | JSON object such as `{"code":0}`.                                                    |
| `7`  | error     | API to client | API-side SSH error text.                                                             |

The cluster API opens the same upgrade to the derivative environment's node and
copies frames in both directions without changing their contents. Prefer
`bastion ssh` unless implementing a custom client; the CLI manages terminal raw
mode, resize signals, stream separation, and exit frames. A remote nonzero exit
currently makes the top-level `bastion` process exit with status `1` rather than
the remote status.
