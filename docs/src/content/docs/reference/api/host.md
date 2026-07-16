---
title: Host API
description: Routes, request and response shapes, statuses, pagination, streams, proxying, and SSH framing for one Bastion host.
---

`bastion start api` serves the host API at `http://localhost:3148` by default.
The API manages one Linux x86_64 VM host and calls the privileged daemon through
a Unix socket.

> **Important:** The native API does not provide authentication, authorization,
> or TLS.
> Do not expose it directly to an untrusted network. Add those controls in a
> reverse proxy or other trusted boundary. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

## HTTP conventions

All routes use the `/v1` prefix. JSON request bodies use
`Content-Type: application/json`. Bastion resource and error response fields use
lower camel case. This convention does not apply to template configuration,
archive internals, NDJSON `log` text, or arbitrary responses from proxied guest
services. Documented Bastion resource timestamp fields are RFC 3339 UTC strings
and can include fractional seconds; the API does not rewrite timestamps in
proxied responses.

Route examples use these common placeholders:

| Placeholder        | Meaning                                       |
| ------------------ | --------------------------------------------- |
| `{SECRET_ID}`      | `sec_`-prefixed secret UUID.                  |
| `{TEMPLATE_ID}`    | `tpl_`-prefixed template UUID.                |
| `{ENVIRONMENT_ID}` | `env_`-prefixed environment UUID.             |
| `{RESOURCE_KEY}`   | User-provided resource key, URL path-escaped. |
| `{TUNNEL_NAME}`    | Template tunnel name.                         |
| `{AGENT_NAME}`     | Agent name; currently only `opencode`.        |
| `{PATH...}`        | Optional remaining proxied path.              |

The braces are notation and are not literal path characters.

Resource validation permits `/` in a key, but a slash-containing key cannot be
reliably represented by the router's single-segment by-key routes, even as
`%2F`. Use slash-free keys. For an existing affected resource, use its generated
slash-free ID on routes that accept an ID.

### Request IDs

Normal JSON, NDJSON, archive, and HTTP proxy responses include `X-Request-ID`.

If a request supplies a nonempty `X-Request-ID`, Bastion echoes it unchanged.
Otherwise Bastion generates a UUID. The same value appears as `request_id` in
the structured request log.

Do not depend on this header after a connection is hijacked for
`101 Switching Protocols`. The native SSH upgrade response and raw guest-proxy
upgrade response do not reliably include middleware response headers.

### Pagination

Secret, template, and environment list routes accept:

| Query    | Default | Behavior                                                                       |
| -------- | ------- | ------------------------------------------------------------------------------ |
| `limit`  | `20`    | Nonpositive or unparsable values become `20`; values above `100` become `100`. |
| `cursor` | Empty   | Return records created after this cursor.                                      |

List responses use:

```json
{
  "cursor": null,
  "entries": []
}
```

When another page exists, `cursor` is the creation timestamp of the final entry
in the current page. Clients should still treat it as an opaque string and pass
it unchanged. Entries are ordered by creation time ascending.

### Normal errors

Registered nonstreaming routes normally return:

```json
{
  "error": "not found: template not found"
}
```

Error text is descriptive and can include wrapped operation context. Clients
should branch on status and documented fields rather than parse message text.

| Status | Name                  | Meaning                                                                                                                                                                                           |
| ------ | --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `400`  | Bad Request           | Invalid JSON, query value, identifier selection, key, tag, config, or archive.                                                                                                                    |
| `404`  | Not Found             | Resource, agent, or tunnel does not exist.                                                                                                                                                        |
| `409`  | Conflict              | Duplicate key, existing base, or host template in use.                                                                                                                                            |
| `424`  | Failed Dependency     | A required base is absent during template preparation/import, guest initialization reports a dependency failure, or a tunnel, agent, or SSH operation lacks a ready state or connection metadata. |
| `500`  | Internal Server Error | Other persistence, file system, daemon, VM readiness, or server failures.                                                                                                                         |
| `502`  | Bad Gateway           | An environment HTTP proxy could not reach or complete its guest upstream.                                                                                                                         |

Streaming routes return logical errors as terminal NDJSON events after HTTP
streaming begins. See
[NDJSON operation streams](/reference/environment-states-and-streams/#ndjson-operation-streams).

### Binary archive responses

Base and template export routes write archives directly to the response. If a
failure is known before any bytes are written, the route can return its mapped
JSON error status. If reading or writing fails after bytes are committed, the
response can retain `200 OK`, end as a truncated archive, and contain no JSON
error. Clients must treat transport completion and archive validation as part of
success. CLI redirection can leave a partial file after a failed command.

### Archive validation and trust

Base import validates compression and tar structure, safe and unique entry
names, required regular files, the manifest format, and a SHA-256 content
address computed from canonical base artifacts. Template import validates its
structure, required manifest and overlay, manifest format, JSON configuration,
and declared base content address. Template archives do not carry a digest of
the overlay.

These structural checks and content addresses detect some corruption but do not
prove who created an archive. An attacker can create a new archive and recompute
its content address. Import only from a trusted source. Verify the exact archive
with an out-of-band digest or signature before upload. Protect base archives as
credentials because they include the guest SSH private key.

## Host health API

### Get host API health

```http
GET /v1/health
```

Successful status: `200 OK`.

Response:

```json
{
  "status": "ok"
}
```

This route reports only that the host API process is serving requests. It does
not call the daemon, inspect KVM, or test a VM.

## Host utilization API

### Get host utilization

```http
GET /v1/utilization
```

Statuses: `200 OK`, `500 Internal Server Error`.

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
    "used": 21474836480,
    "available": 1078036791296
  }
}
```

`memory` and `volume` values are bytes. `vcpu.total` uses online host CPU
topology when available and falls back to the Go runtime CPU count. Memory total
comes from host `MemTotal`. Volume total is the size of the file system containing
the Bastion data directory.

Used values are declared template allocations for active environments, not
live guest telemetry. An environment counts when either its record or live VM
state is `creating`, `running`, or `paused`. Available never falls below zero.
The host API resolves omitted resource fields in its own process; its accounting
can differ from daemon VM sizing. See
[VM sizing configuration](/reference/host-requirements-and-configuration/#vm-sizing-configuration).

## Base routes

The base is a singleton and is not keyed.

### Get base metadata

```http
GET /v1/base
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

Response:

```json
{
  "contentAddress": "sha256:0123456789abcdef...",
  "createdAt": "2026-07-16T12:00:00Z",
  "updatedAt": "2026-07-16T12:00:00Z"
}
```

### Build the base

```http
POST /v1/base/build?force=true
Accept: application/x-ndjson
```

`force` is optional and defaults to `false`. It accepts Go boolean values such
as `true` and `false`; an invalid value returns `400 Bad Request` before the
stream starts. The request body is not used.

The response is always `application/x-ndjson` once the operation starts and has
HTTP status `200 OK`. It emits `log` events followed by either:

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

or a terminal error such as:

```json
{
  "type": "error",
  "error": "conflict: base already exists",
  "status": 409
}
```

Without `force=true`, an existing base produces effective `409 Conflict`. Other
build failures normally produce effective `500 Internal Server Error`.

> **Warning:** Forced replacement is not atomic. Bastion prepares or extracts a
> candidate first, but installation removes the existing base directory before
> renaming and finalizing the candidate. A rename, content-address, or metadata
> failure can leave no installed base. There is no rollback route.

### Import a base archive

```http
POST /v1/base/import?force=true
Content-Type: application/vnd.bastion.base+tar+zstd
Accept: application/x-ndjson
```

The request body is the archive. `force` has the same parsing and replacement
behavior as base build. The NDJSON result and error shapes are also identical.
Malformed, unsupported, or content-address-invalid archives produce a terminal
error with effective status `400 Bad Request`. An existing base without force
produces `409 Conflict`; other import failures normally produce `500 Internal
Server Error`. A matching content address does not establish authenticity; see
[archive validation and trust](#archive-validation-and-trust).

### Export the base archive

```http
GET /v1/base/export
Accept: application/vnd.bastion.base+tar+zstd
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

The response `Content-Type` is
`application/vnd.bastion.base+tar+zstd`. The zstd-compressed tar contains:

| Entry           | Contents                          |
| --------------- | --------------------------------- |
| `manifest.json` | Base archive format and metadata. |
| `rootfs.img`    | Prepared root disk.               |
| `cidata.img`    | Prepared base cloud-init seed.    |
| `ssh_key`       | Guest SSH private key.            |

> **Warning:** The archive contains a private key accepted by guests created
> from the base. Protect it as sensitive material. A response that terminates
> after writing some bytes can be a
> [partial binary response](#binary-archive-responses).

## Secret routes

Secret keys are optional. An explicitly supplied key must be nonblank, unique,
and must not begin with reserved prefix `sec_`. Values must be nonempty.

### Create a secret

```http
POST /v1/secrets
Content-Type: application/json
```

Request:

```json
{
  "key": "OPENAI_API_KEY",
  "value": "sk-example"
}
```

`key` is optional. Statuses: `201 Created`, `400 Bad Request`, `409 Conflict`,
`500 Internal Server Error`.

Response omits the value:

```json
{
  "id": "sec_16fd2706-8baf-433b-82eb-8c7fada847da",
  "key": "OPENAI_API_KEY",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

### List secret metadata

```http
GET /v1/secrets?limit=20&cursor={CURSOR}
```

Statuses: `200 OK`, `500 Internal Server Error`.

Response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "sec_16fd2706-8baf-433b-82eb-8c7fada847da",
      "key": "OPENAI_API_KEY",
      "createdAt": "2026-07-16T12:00:00Z"
    }
  ]
}
```

List never returns secret values.

### Get a secret

```http
GET /v1/secrets/{SECRET_ID}
GET /v1/secrets/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

Response includes the value:

```json
{
  "id": "sec_16fd2706-8baf-433b-82eb-8c7fada847da",
  "key": "OPENAI_API_KEY",
  "value": "sk-example",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

### Delete a secret

```http
DELETE /v1/secrets/{SECRET_ID}
DELETE /v1/secrets/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

The response is the deleted metadata and omits `value`. Deletion does not modify
stored template JSON, prepared disks, running environments, or archives that may
already contain the resolved value.

## Template routes

Template configuration follows the
[exact template schema](/reference/template-configuration/). Keys are optional,
nonblank, and unique when present. Every prepared or imported template records
the current base content address.

### Create a template

```http
POST /v1/templates
Content-Type: application/json
Accept: application/x-ndjson
```

Request:

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

`key` is optional. Malformed JSON returns a normal `400 Bad Request` before
streaming. Once the body binds successfully, the route returns `200 OK` with
`application/x-ndjson`, even if validation or preparation later fails.

Successful terminal event:

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

Terminal errors can report `400 Bad Request` for configuration, key, or secret
reference validation; `409 Conflict` for a duplicate key; `424 Failed
Dependency` when the base is absent or guest initialization fails; and `500
Internal Server Error` for other daemon, readiness, storage, or persistence
failures. Log events contain guest setup and init-action output.

### List template metadata

```http
GET /v1/templates?limit=20&cursor={CURSOR}
```

Statuses: `200 OK`, `500 Internal Server Error`.

Response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
      "key": "dev",
      "baseContentAddress": "sha256:0123456789abcdef...",
      "createdAt": "2026-07-16T12:00:00Z"
    }
  ]
}
```

### Get a template

```http
GET /v1/templates/{TEMPLATE_ID}
GET /v1/templates/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

Response:

```json
{
  "id": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "key": "dev",
  "config": {
    "agents": {
      "opencode": {}
    },
    "actions": {
      "init": []
    }
  },
  "baseContentAddress": "sha256:0123456789abcdef...",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

The stored config retains secret expression syntax rather than resolved values.

### Import a template archive

```http
POST /v1/templates/import?key={RESOURCE_KEY}
Content-Type: application/vnd.bastion.template+tar+zstd
```

`key` is optional. The body is a prepared template archive. Statuses are `201
Created`; `400 Bad Request` for archive, manifest, schema, ID, or base-address
mismatch validation; `409 Conflict` for a duplicate key; `424 Failed Dependency`
when the host base is absent; and `500 Internal Server Error` for other daemon,
file, or persistence failures.

The current base must exactly match the archive's `baseContentAddress`. Import
creates a new template ID and creation time and never preserves the archived ID
or key. Response:

```json
{
  "id": "tpl_45c48cce-2e2d-4f77-92f0-a4f6509237c9",
  "key": "dev-restored",
  "baseContentAddress": "sha256:0123456789abcdef...",
  "createdAt": "2026-07-16T13:00:00Z"
}
```

### Export a template archive

```http
GET /v1/templates/{TEMPLATE_ID}/export
GET /v1/templates/by-key/{RESOURCE_KEY}/export
Accept: application/vnd.bastion.template+tar+zstd
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

The response `Content-Type` is
`application/vnd.bastion.template+tar+zstd`. The archive contains
`manifest.json` and `rootfs.img`. It contains neither cloud-init media nor VM
memory state. The manifest contains template ID, optional key, source config,
and base content address.

The response can be truncated after `200 OK`; see
[binary archive responses](#binary-archive-responses). The template archive has
no overlay digest or signature.

### Delete a template

```http
DELETE /v1/templates/{TEMPLATE_ID}
DELETE /v1/templates/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `409 Conflict`, `500 Internal Server Error`.

The host returns `409 Conflict` while an environment record references the
template. A successful response is the full deleted template record, including
`config`.

> **Warning:** Deletion is not atomic. Bastion deletes the SQLite row before it
> asks the daemon to remove the prepared overlay. If artifact removal fails, the
> route returns `500 Internal Server Error`, the template is no longer listable,
> and prepared files can remain. There is no API rollback.

## Environment routes

Environment records and statuses are defined in
[Environment states and streams](/reference/environment-states-and-streams/).
Environment keys are optional, nonblank, and unique when present. Tags must be
nonblank strings.

### Create an environment

```http
POST /v1/environments
Content-Type: application/json
Accept: application/x-ndjson
```

Request by template ID:

```json
{
  "key": "review-123",
  "templateId": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
  "tags": ["repo:bastion", "agent:review"]
}
```

Request by template key:

```json
{
  "templateKey": "dev"
}
```

Exactly one of `templateId` and `templateKey` is required. `key` and `tags` are
optional. Malformed JSON returns a normal `400 Bad Request`. Once streaming
starts, the HTTP status is `200 OK` and the terminal event determines success.

Successful terminal event:

```json
{
  "type": "result",
  "environment": {
    "id": "env_550e8400-e29b-41d4-a716-446655440000",
    "key": "review-123",
    "status": "running",
    "templateId": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
    "tags": ["repo:bastion", "agent:review"],
    "createdAt": "2026-07-16T12:00:00Z",
    "updatedAt": "2026-07-16T12:01:30Z"
  }
}
```

Terminal errors can report `400 Bad Request` for selectors, keys, tags, resolved
configuration, or secret references; `404 Not Found` for the template; `409
Conflict` for a duplicate environment key; `424 Failed Dependency` when guest
service or `actions.start` initialization reports a dependency failure; and `500
Internal Server Error` for other launch, readiness, daemon, or persistence
failures. Guest proxy, OpenCode, and `actions.start` output appears as `log`
events.

The host inserts `creating` before launch and normally retains post-insert
failures as `error`. See
[host persistence](/reference/environment-states-and-streams/#host-persistence).

### List environments

```http
GET /v1/environments?limit=20&cursor={CURSOR}&tag={TAG}&tag={TAG}
```

Successful status: `200 OK`.

Repeated `tag` values use AND semantics. Repeated duplicate filters are
deduplicated. Matching is exact. The route reconciles each returned environment
with the daemon before responding.

An empty tag filter returns `400 Bad Request`. Reconciliation or persistence
failures return `500 Internal Server Error`.

Response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "env_550e8400-e29b-41d4-a716-446655440000",
      "key": "review-123",
      "status": "running",
      "templateId": "tpl_7c9e6679-7425-40de-944b-e07fc1f90ae7",
      "tags": ["repo:bastion", "agent:review"],
      "createdAt": "2026-07-16T12:00:00Z",
      "updatedAt": "2026-07-16T12:01:30Z"
    }
  ]
}
```

### Get an environment

```http
GET /v1/environments/{ENVIRONMENT_ID}
GET /v1/environments/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

The route reconciles persisted and daemon state before returning the environment
record. A nonempty runtime error appears as `lastError`.

### Delete an environment

```http
DELETE /v1/environments/{ENVIRONMENT_ID}
DELETE /v1/environments/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

The host records `removing`, asks the daemon to stop and clean the VM, removes VM
metadata, and deletes the environment record. A successful response contains
the former record with `status: "removed"` and a new `updatedAt`. A daemon
removal failure leaves the stored record in `error` with `lastError`. A later
metadata or SQLite deletion failure can return `500 Internal Server Error` after
runtime cleanup has already progressed; the operation has no transaction across
the daemon and SQLite.

## Environment tunnel routes

### List registered tunnels

```http
GET /v1/environments/{ENVIRONMENT_ID}/tunnels
GET /v1/environments/by-key/{RESOURCE_KEY}/tunnels
```

Statuses: `200 OK`, `404 Not Found`, `424 Failed Dependency`,
`500 Internal Server Error`.

The environment must be `running`. Response entries are sorted by name and do
not contain public URLs:

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

`424 Failed Dependency` means the environment is not `running` or the stored
template tunnel configuration cannot be interpreted.

### Proxy a registered tunnel

```http
ANY /v1/environments/{ENVIRONMENT_ID}/tunnels/{TUNNEL_NAME}
ANY /v1/environments/{ENVIRONMENT_ID}/tunnels/{TUNNEL_NAME}/{PATH...}
ANY /v1/environments/by-key/{RESOURCE_KEY}/tunnels/{TUNNEL_NAME}
ANY /v1/environments/by-key/{RESOURCE_KEY}/tunnels/{TUNNEL_NAME}/{PATH...}
```

`ANY` means all HTTP methods accepted by the router. Bastion validates that the
environment is running and that the template registers `TUNNEL_NAME`, then
connects over the VM's vsock guest proxy to that port on guest localhost.

Before proxying, a missing environment or tunnel returns `404 Not Found`; a
nonrunning environment or missing connection metadata returns `424 Failed
Dependency`; and other lookup failures return `500 Internal Server Error`.

The path after the tunnel name becomes the upstream path; an absent path becomes
`/`. The method, query, body, and ordinary headers pass through. Upstream status,
headers, and body pass back to the client. The proxy disables upstream HTTP
keep-alives.

Requests with `Connection: Upgrade` and a nonempty `Upgrade` header are forwarded
as raw upgraded connections. This supports protocols such as WebSocket when the
guest service supports them. A guest-proxy dial or transport failure returns
`502 Bad Gateway`, usually as plain text, when headers have not already been
committed. If an upstream response or upgrade fails after bytes are written, the
client can instead receive the upstream status and a truncated body or raw
stream.

## Environment agent routes

### Proxy an OpenCode agent

```http
ANY /v1/environments/{ENVIRONMENT_ID}/agents/opencode
ANY /v1/environments/{ENVIRONMENT_ID}/agents/opencode/{PATH...}
ANY /v1/environments/by-key/{RESOURCE_KEY}/agents/opencode
ANY /v1/environments/by-key/{RESOURCE_KEY}/agents/opencode/{PATH...}
```

These routes use the same HTTP and upgrade proxy behavior as registered
tunnels. The environment must be `running`, its template must define
`agents.opencode`, and its OpenCode `config.server.port` must be a valid integer
from `1` through `65535`. The default is `4096`.

Other `{AGENT_NAME}` values return `404 Not Found`. Before proxying, a
nonrunning environment, missing connection metadata, or invalid stored port
returns `424 Failed Dependency`; other lookup failures return `500 Internal
Server Error`.

## Environment SSH route

### Open an SSH stream

```http
POST /v1/environments/{ENVIRONMENT_ID}/ssh
Connection: Upgrade
Upgrade: bastion-ssh
Content-Type: application/json
```

There is no by-key SSH route. Resolve a key with the environment get route first.

Send the canonical
[SSH request body](/reference/environment-states-and-streams/#ssh-upgrade-stream),
which supports a command array or PTY settings.

Before upgrade, malformed JSON returns `400 Bad Request`, a missing environment
returns `404 Not Found`, and a nonready environment or missing SSH metadata
returns `424 Failed Dependency`. Other lookup or server-hijack failures return
`500 Internal Server Error` when the server can still write an HTTP response. A
successful route returns `101 Switching Protocols` with `Upgrade: bastion-ssh`.
Private-key reads, guest dial and handshake, and session startup happen after
that upgrade; those failures use a type `7` error frame and close the stream
while the HTTP status remains `101`.

The environment must be `running` or `paused` and have host, port, user, and key
metadata. The framed protocol carries stdin, stdin EOF, terminal resize, stdout,
stderr, exit status, and API errors. See the complete
[SSH frame format](/reference/environment-states-and-streams/#ssh-frame-format).

The API does not verify the guest SSH host key, and command arrays become one
shell string without preserved argument boundaries. See
[SSH upgrade security](/reference/environment-states-and-streams/#ssh-upgrade-stream)
before implementing a custom client.

## Host and daemon API boundary

The routes on this page are the public host API. `bastion start daemon` exposes a
separate implementation API over its Unix socket for base, template, and VM
operations. It is not a network API or a supported external integration
surface.
