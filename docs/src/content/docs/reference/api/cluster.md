---
title: Cluster API
description: Cluster control plane routes, namespace prefixes, node and namespace management, orchestration, proxying, and response differences.
---

`bastion start cluster` serves the cluster control plane at
`http://localhost:3150` by default. It stores source resources in Postgres,
stores base and template archives in S3-compatible storage, and creates
derivative resources on registered Linux host API nodes.

> **Important:** The native cluster API and host node APIs provide no
> authentication, authorization, or TLS. Protect all of them with trusted
> networking and an external boundary that provides authentication and TLS. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

## HTTP conventions

The cluster API uses the same Bastion resource JSON and timestamp conventions,
`X-Request-ID` behavior, error body, pagination, NDJSON event types, archive
media types, and binary-response limitations as the
[host API HTTP conventions](/reference/api/host/#http-conventions), except where
this page states a difference. Proxied guest responses remain arbitrary and are
not converted to Bastion field or timestamp conventions.

If the caller sends a nonempty `X-Request-ID`, the cluster echoes it on normal
HTTP responses. Otherwise it generates a UUID. The control plane does not
automatically forward the client's request ID to node calls. Do not depend on
the header on a hijacked `bastion-ssh` upgrade response.

Route notation uses these common placeholders:

| Placeholder      | Meaning                                     |
| ---------------- | ------------------------------------------- |
| `{NODE_ID}`      | `node_`-prefixed node UUID.                 |
| `{NAMESPACE_ID}` | `ns_`-prefixed namespace UUID.              |
| `{RESOURCE_ID}`  | Source `sec_`, `tpl_`, or `env_` ID.        |
| `{RESOURCE_KEY}` | User-provided key, URL path-escaped.        |
| `{CURSOR}`       | Pagination cursor from a previous response. |
| `{PATH...}`      | Optional remaining proxy path.              |

Braces identify placeholders and are not literal path characters.

Keys can pass resource validation with `/`, but router by-key parameters occupy
one path segment and cannot reliably represent such keys, even as `%2F`. Use
slash-free node, namespace, secret, template, and environment keys. Use a
generated ID route for an existing affected resource. If a namespace key is
affected, select that namespace by ID.

### Pagination and errors

Node, namespace, secret, template, and environment lists accept `limit` and
`cursor`. `limit` defaults to `20`, nonpositive or unparsable values become
`20`, and values above `100` become `100`. Responses use:

```json
{
  "cursor": null,
  "entries": []
}
```

Entries are ordered by creation time ascending. A non-null cursor is the last
returned creation timestamp and should be passed unchanged.

Normal errors use:

```json
{
  "error": "invalid input: namespace is required"
}
```

Error text can include wrapped control-plane or node operation context. Clients
should branch on status and documented fields rather than parse message text.

| Status | Name                  | Meaning                                                                                                                                      |
| ------ | --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `400`  | Bad Request           | Invalid JSON, selector, key, tag, URL, config, archive, or query.                                                                            |
| `404`  | Not Found             | Source resource, node, namespace, derivative route, agent, or tunnel is absent.                                                              |
| `409`  | Conflict              | Duplicate key, duplicate singleton base, or duplicate resource.                                                                              |
| `424`  | Failed Dependency     | A route-specific archive-store, base, node API, capacity, derivative, cleanup, aggregate health, or aggregate utilization dependency failed. |
| `500`  | Internal Server Error | Unexpected Postgres, S3, persistence, or server failure. Some database deletion constraints currently surface with this status.              |
| `502`  | Bad Gateway           | Reverse proxy could not reach a node.                                                                                                        |

Streaming operations keep HTTP `200 OK` after the stream starts and put the
effective status in a terminal `error` event. See
[NDJSON operation streams](/reference/environment-states-and-streams/#ndjson-operation-streams).

Binary base and template exports can return a truncated body after `200 OK` if
S3-compatible storage or the client connection fails after bytes are committed.
Use the [binary archive response](/reference/api/host/#binary-archive-responses)
and [archive trust](/reference/api/host/#archive-validation-and-trust) rules.

## Namespace route prefixes

Source secrets, templates, and environments require one of two prefixes:

```text
/v1/namespaces/{NAMESPACE_ID}
/v1/namespaces/by-key/{NAMESPACE_KEY}
```

For example, the two create-secret routes are:

```http
POST /v1/namespaces/{NAMESPACE_ID}/secrets
POST /v1/namespaces/by-key/{NAMESPACE_KEY}/secrets
```

The cluster router also registers unprefixed `/v1/secrets`, `/v1/templates`, and
`/v1/environments` route families. They are not usable cluster resource routes:
the service returns `400 Bad Request` because no namespace selector is present.

The base, health API, utilization API, node management, and namespace management
are global and do not use a namespace prefix.

## Cluster health API

### Get aggregate health

```http
GET /v1/health
```

Statuses: `200 OK`, `424 Failed Dependency`, `500 Internal Server Error`.

Response:

```json
{
  "status": "ok"
}
```

The control plane calls `GET /v1/health` on every registered node in creation
order. With no nodes, it returns `ok`. If any node call fails or returns a status
other than `ok`, the request returns `424 Failed Dependency`; no partial health
object is returned.

## Cluster utilization API

### Get aggregate utilization

```http
GET /v1/utilization
```

Statuses: `200 OK`, `424 Failed Dependency`, `500 Internal Server Error`.

Response:

```json
{
  "vcpu": { "total": 32, "used": 6, "available": 26 },
  "memory": {
    "total": 68719476736,
    "used": 8589934592,
    "available": 60129542144
  },
  "volume": {
    "total": 2199023255552,
    "used": 64424509440,
    "available": 2134598746112
  }
}
```

The control plane calls each node's utilization route and adds `total`, `used`,
and `available` independently for vCPU, memory, and volume. Memory and volume
are bytes. With no nodes, all values are zero. A failed node request returns
`424 Failed Dependency` instead of a partial aggregate.

## Cluster base routes

The base is global across all namespaces.

### Get cluster base metadata

```http
GET /v1/base
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

Response shape matches the
[host base metadata](/reference/api/host/#get-base-metadata).

### Build the cluster base

```http
POST /v1/base/build?force=true
Accept: application/x-ndjson
```

`force` defaults to `false`. An invalid boolean returns `400 Bad Request` before
streaming. Build requires configured archive storage and at least one node.

The control plane selects the first node by creation time, builds and exports
the base there, validates its content address, imports it concurrently into all
other nodes, stores it as `base/base.tar.zst`, and records cluster metadata.

The operation returns the standard base `log`, `result`, and `error` events.
Control-plane logs use a `cluster:` prefix. Once started, HTTP status remains
`200 OK`. Effective `409 Conflict` means a base exists without force. Effective
`424 Failed Dependency` covers a missing archive store, no registered node, a
node build/export/import failure, or a fan-out content-address mismatch. Invalid
input uses `400 Bad Request`; other storage or persistence failures use `500
Internal Server Error`.

> **Warning:** A forced multi-node replacement has no public transaction or
> rollback operation. The selected build node is replaced before fan-out, and
> concurrent imports can succeed on only some other nodes. A later S3 or
> Postgres failure can leave every node updated while cluster metadata still
> describes the old base. Success can invalidate templates in every namespace.
> Review
> [cluster operation limits](/explanation/security-and-operational-limits/).

### Import the cluster base

```http
POST /v1/base/import?force=true
Content-Type: application/vnd.bastion.base+tar+zstd
Accept: application/x-ndjson
```

Import requires configured archive storage. It validates the uploaded archive,
imports it concurrently into all current nodes, stores it, and records metadata.
It can run with no registered nodes; future node creation synchronizes the
stored base.

The event shapes and `force` behavior match cluster base build. Fan-out and
storage are non-atomic in the same way. Archive structure and content-address
validation do not prove authenticity; verify a trusted out-of-band digest or
signature before upload.

### Export the cluster base

```http
GET /v1/base/export
Accept: application/vnd.bastion.base+tar+zstd
```

Statuses: `200 OK`, `404 Not Found`, `424 Failed Dependency`, and `500 Internal
Server Error`. The control plane streams the stored S3-compatible object without
calling a node. `424` means archive storage is not configured. The response
media type and archive layout match the
[host base archive](/reference/api/host/#export-the-base-archive). A storage read
failure after bytes are written can leave `200 OK` with a partial archive.

## Cluster node management routes

The complete node route set is:

| Method   | Route                                     | Successful status | Response              |
| -------- | ----------------------------------------- | ----------------- | --------------------- |
| `POST`   | `/v1/cluster/nodes`                       | `200 OK` NDJSON   | Terminal node result. |
| `GET`    | `/v1/cluster/nodes`                       | `200 OK`          | Page of nodes.        |
| `GET`    | `/v1/cluster/nodes/{NODE_ID}`             | `200 OK`          | One node.             |
| `GET`    | `/v1/cluster/nodes/by-key/{RESOURCE_KEY}` | `200 OK`          | One node.             |
| `DELETE` | `/v1/cluster/nodes/{NODE_ID}`             | `200 OK`          | Deleted node.         |
| `DELETE` | `/v1/cluster/nodes/by-key/{RESOURCE_KEY}` | `200 OK`          | Deleted node.         |

### Create a node

```http
POST /v1/cluster/nodes
Content-Type: application/json
Accept: application/x-ndjson
```

Request:

```json
{
  "key": "node-a",
  "url": "http://node-a.internal:3148"
}
```

`key` is optional, nonblank, unique, and cannot begin with reserved prefix
`node_`. For `url`, use an absolute `http` or `https` URL with a host and an
optional base path. Do not include user information, a query, or a fragment
because the control plane appends `/v1/...` paths by string concatenation.
Current validation does not reject every unsupported component.

Malformed JSON returns `400 Bad Request` before streaming. After the stream
starts, the terminal result is:

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

If a cluster base exists, the control plane loads it from archive storage and
imports it with force on the proposed node before inserting the node record. If
no base exists, it records the node without a separate health check.

After streaming starts, `400 Bad Request` covers an invalid key or URL and `409
Conflict` covers a duplicate key. Base synchronization can produce `424 Failed
Dependency` when archive storage is absent or the node reports a dependency
failure; other S3, node transport, decode, or Postgres errors generally produce
`500 Internal Server Error`. A base can be installed on the proposed node even
if the later Postgres insert fails, leaving an unregistered changed host.

### List nodes

```http
GET /v1/cluster/nodes?limit=20&cursor={CURSOR}
```

Response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "node_6ba7b810-9dad-41d1-80b4-00c04fd430c8",
      "key": "node-a",
      "url": "http://node-a.internal:3148",
      "createdAt": "2026-07-16T12:00:00Z"
    }
  ]
}
```

Unkeyed nodes omit `key`.

### Get a node

```http
GET /v1/cluster/nodes/{NODE_ID}
GET /v1/cluster/nodes/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

### Delete a node

> **Warning:** Node deletion does not migrate environments, stop VMs, call the
> node, or clean node-local derivative templates and secrets. Source environment
> references can prevent deletion, and that database constraint can currently
> surface as `500 Internal Server Error`. Review
> [cluster deletion limits](/explanation/security-and-operational-limits/).

```http
DELETE /v1/cluster/nodes/{NODE_ID}
DELETE /v1/cluster/nodes/by-key/{RESOURCE_KEY}
```

A successful response is the removed node record. The route only deletes the
Postgres node row.

## Cluster namespace management routes

The complete namespace management route set is:

| Method   | Route                                          | Successful status | Response            |
| -------- | ---------------------------------------------- | ----------------- | ------------------- |
| `POST`   | `/v1/cluster/namespaces`                       | `201 Created`     | New namespace.      |
| `GET`    | `/v1/cluster/namespaces`                       | `200 OK`          | Page of namespaces. |
| `GET`    | `/v1/cluster/namespaces/{NAMESPACE_ID}`        | `200 OK`          | One namespace.      |
| `GET`    | `/v1/cluster/namespaces/by-key/{RESOURCE_KEY}` | `200 OK`          | One namespace.      |
| `DELETE` | `/v1/cluster/namespaces/{NAMESPACE_ID}`        | `200 OK`          | Deleted namespace.  |
| `DELETE` | `/v1/cluster/namespaces/by-key/{RESOURCE_KEY}` | `200 OK`          | Deleted namespace.  |

### Create a namespace

```http
POST /v1/cluster/namespaces
Content-Type: application/json
```

Request:

```json
{
  "key": "team-a"
}
```

`key` is optional, nonblank, unique, and cannot begin with reserved prefix
`ns_`. Statuses: `201 Created`, `400 Bad Request`, `409 Conflict`, and
`500 Internal Server Error`.

Response:

```json
{
  "id": "ns_6f1ed002-ab9d-4f78-9b05-2f1f7cce3210",
  "key": "team-a",
  "createdAt": "2026-07-16T12:00:00Z"
}
```

### List namespaces

```http
GET /v1/cluster/namespaces?limit=20&cursor={CURSOR}
```

Response:

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "ns_6f1ed002-ab9d-4f78-9b05-2f1f7cce3210",
      "key": "team-a",
      "createdAt": "2026-07-16T12:00:00Z"
    }
  ]
}
```

### Get a namespace

```http
GET /v1/cluster/namespaces/{NAMESPACE_ID}
GET /v1/cluster/namespaces/by-key/{RESOURCE_KEY}
```

Statuses: `200 OK`, `404 Not Found`, `500 Internal Server Error`.

### Delete a namespace

> **Warning:** Namespace deletion cascades source secrets, templates,
> environments, tags, and derivative mappings in Postgres. It does not first
> stop derivative VMs, remove node-local derivatives, or delete namespace
> template archive objects from S3-compatible storage. Review
> [cluster deletion limits](/explanation/security-and-operational-limits/).

```http
DELETE /v1/cluster/namespaces/{NAMESPACE_ID}
DELETE /v1/cluster/namespaces/by-key/{RESOURCE_KEY}
```

A successful response is the removed namespace record after the database
cascade.

## Namespaced secret routes

Apply either namespace prefix to every suffix in this table:

| Method   | Suffix                           | Successful status | Response                        |
| -------- | -------------------------------- | ----------------- | ------------------------------- |
| `POST`   | `/secrets`                       | `201 Created`     | Source secret metadata.         |
| `GET`    | `/secrets`                       | `200 OK`          | Page of source secret metadata. |
| `GET`    | `/secrets/{RESOURCE_ID}`         | `200 OK`          | Source secret including value.  |
| `GET`    | `/secrets/by-key/{RESOURCE_KEY}` | `200 OK`          | Source secret including value.  |
| `DELETE` | `/secrets/{RESOURCE_ID}`         | `200 OK`          | Deleted source metadata.        |
| `DELETE` | `/secrets/by-key/{RESOURCE_KEY}` | `200 OK`          | Deleted source metadata.        |

Request and response shapes match the
[host secret routes](/reference/api/host/#secret-routes). IDs remain `sec_`
prefixed. Keys are unique within a namespace and cannot begin with `sec_`.

The control plane does not copy a source secret to every node when it is created.
It creates unkeyed node-local derivative secrets only while preparing a source
template or restoring a derivative template. Deleting a source secret does not
scrub already prepared node-local state.

## Namespaced template routes

Apply either namespace prefix to every suffix:

| Method   | Suffix                                    | Successful status | Response                           |
| -------- | ----------------------------------------- | ----------------- | ---------------------------------- |
| `POST`   | `/templates`                              | `200 OK` NDJSON   | Terminal source template metadata. |
| `GET`    | `/templates`                              | `200 OK`          | Page of source metadata.           |
| `POST`   | `/templates/import?key={RESOURCE_KEY}`    | `201 Created`     | Imported source metadata.          |
| `GET`    | `/templates/{RESOURCE_ID}/export`         | `200 OK`          | Template archive.                  |
| `GET`    | `/templates/{RESOURCE_ID}`                | `200 OK`          | Full source template.              |
| `GET`    | `/templates/by-key/{RESOURCE_KEY}/export` | `200 OK`          | Template archive.                  |
| `GET`    | `/templates/by-key/{RESOURCE_KEY}`        | `200 OK`          | Full source template.              |
| `DELETE` | `/templates/{RESOURCE_ID}`                | `200 OK`          | Deleted full source template.      |
| `DELETE` | `/templates/by-key/{RESOURCE_KEY}`        | `200 OK`          | Deleted full source template.      |

Request, metadata, full-record, archive, and NDJSON shapes match the
[host template routes](/reference/api/host/#template-routes). Configuration must
match the [template schema](/reference/template-configuration/).

Template create requires archive storage, a cluster base, and a registered node.
The control plane selects the first node in creation order, creates derivative
secrets, prepares a derivative template, exports it, cleans temporary node
resources, rewrites the archive manifest to source metadata, stores the archive,
and then records the source template. Cleanup after an intermediate failure is
best effort, so an error can leave derivative node resources.

After streaming starts, create can report effective `400 Bad Request` for
config, key, or secret-reference validation; `409 Conflict` for a duplicate key;
`424 Failed Dependency` for a missing archive store, base, or node, or for a
node derivative or cleanup operation; and `500 Internal Server Error` for other
S3, Postgres, or rewrite failures.

Template import requires archive storage and an exact cluster base content
address but does not require a current node. It validates and rewrites the
uploaded archive under a new source ID and optional new key. These checks do not
authenticate the archive; verify a trusted out-of-band digest or signature
before upload.

Import returns `400 Bad Request` for invalid archive, schema, key, or base
address; `404 Not Found` for an unknown namespace; `409 Conflict` for a duplicate
key; `424 Failed Dependency` when archive storage or the cluster base is absent;
and `500 Internal Server Error` for other S3 or Postgres failures.

Template export reads the stored source archive without calling a node and can
produce a partial binary response after `200 OK`.

Template deletion begins a Postgres transaction, deletes the source row inside
that transaction, deletes the S3 object, and then commits Postgres. It is not an
atomic cross-store transaction. If S3 deletion fails, Postgres rolls back. If S3
deletion succeeds and the Postgres commit fails, the source row can remain
without its archive. The route returns `424 Failed Dependency`, rather than the
host API's `409 Conflict`, while source environments still use the template or
when archive storage is not configured. S3 and commit failures generally return
`500 Internal Server Error`.

## Namespaced environment routes

Apply either namespace prefix to every suffix:

| Method   | Suffix                                                                  | Successful status         | Response                                |
| -------- | ----------------------------------------------------------------------- | ------------------------- | --------------------------------------- |
| `POST`   | `/environments`                                                         | `200 OK` NDJSON           | Terminal source environment.            |
| `GET`    | `/environments`                                                         | `200 OK`                  | Page of reconciled source environments. |
| `GET`    | `/environments/{RESOURCE_ID}`                                           | `200 OK`                  | Reconciled source environment.          |
| `GET`    | `/environments/by-key/{RESOURCE_KEY}`                                   | `200 OK`                  | Reconciled source environment.          |
| `GET`    | `/environments/{RESOURCE_ID}/tunnels`                                   | `200 OK`                  | Registered tunnel metadata.             |
| `GET`    | `/environments/by-key/{RESOURCE_KEY}/tunnels`                           | `200 OK`                  | Registered tunnel metadata.             |
| `DELETE` | `/environments/{RESOURCE_ID}`                                           | `200 OK`                  | Removed source environment.             |
| `DELETE` | `/environments/by-key/{RESOURCE_KEY}`                                   | `200 OK`                  | Removed source environment.             |
| `POST`   | `/environments/{RESOURCE_ID}/ssh`                                       | `101 Switching Protocols` | Bastion SSH frames.                     |
| `ANY`    | `/environments/{RESOURCE_ID}/agents/{AGENT_NAME}[/{PATH...}]`           | Upstream dependent        | Proxied agent response.                 |
| `ANY`    | `/environments/by-key/{RESOURCE_KEY}/agents/{AGENT_NAME}[/{PATH...}]`   | Upstream dependent        | Proxied agent response.                 |
| `ANY`    | `/environments/{RESOURCE_ID}/tunnels/{TUNNEL_NAME}[/{PATH...}]`         | Upstream dependent        | Proxied tunnel response.                |
| `ANY`    | `/environments/by-key/{RESOURCE_KEY}/tunnels/{TUNNEL_NAME}[/{PATH...}]` | Upstream dependent        | Proxied tunnel response.                |

`ANY` represents all HTTP methods accepted by the router. `{AGENT_NAME}` is
currently limited to `opencode`.

Environment create requests and source response shapes match the
[host environment routes](/reference/api/host/#environment-routes). Exactly one
source `templateId` or `templateKey` is required. Keys are unique within the
namespace. Repeated `tag` list filters use AND semantics.

After streaming starts, create can report effective `400 Bad Request` for
selectors, keys, tags, config, or resource resolution; `404 Not Found` for the
namespace or source template; `409 Conflict` for a duplicate environment or
derivative; `424 Failed Dependency` for archive storage, capacity, node
utilization, or node derivative operations; and `500 Internal Server Error` for
other Postgres or S3 failures.

The cluster process resolves omitted template resources in its own environment,
independently of node host APIs and daemons. Explicitly set all three resource
fields for consistent scheduling and VM sizing. The scheduler first checks nodes
with an existing derivative for the source template, then registered nodes in
randomized order. A node must have enough vCPU, memory, and volume after
accounting for the greater of node-reported use and control-plane source
allocation. If none fits, the stream ends with effective `424 Failed
Dependency`.

List and get reconcile each source record with its node derivative. A missing
derivative becomes `stopped`; another node error returns `424 Failed Dependency`.
Environment statuses are defined in the
[environment state reference](/reference/environment-states-and-streams/#environment-statuses),
and source-record visibility is defined under
[cluster persistence](/reference/environment-states-and-streams/#cluster-persistence).

Removal first calls the node to delete the derivative environment, then deletes
the source record. When no other source environment uses that template
derivative, the control plane also attempts to remove the derivative template
and its derivative secrets.

Deletion is not atomic. A source-row deletion failure can follow successful node
environment removal, leaving a source that reconciles to `stopped`. Cleanup of
the now-unused derivative runs after source deletion; it can return `424 Failed
Dependency` even though the source record is already gone. The cluster does not
persist `removing` or `error` for these removal failures.

## Environment proxy and SSH routes

For agent and tunnel requests, the cluster resolves the source environment to a
node URL and derivative environment ID. It rewrites the path to the node's
`/v1/environments/{DERIVATIVE_ENVIRONMENT_ID}/...` route, preserves incoming
request query values except parameters named `namespace-id` and `namespace-key`,
and prefixes the registered node's optional base path.

The reverse proxy preserves methods, bodies, response statuses, streaming, and
HTTP upgrades. It uses the registered node URL's scheme, host, optional base
path. Registered node URLs must not contain a query. A node transport failure
returns `502 Bad Gateway` before response bytes are committed; a later failure
can leave an upstream status with a truncated body or upgraded stream. The node
then applies its own running-state, agent, tunnel, and guest vsock checks.

Before proxying, source or derivative lookup can return `404 Not Found`, state
or node dependency failures can return `424 Failed Dependency`, and other
control-plane failures can return `500 Internal Server Error`.

SSH is available only through the source environment ID route, not by key:

```http
POST /v1/namespaces/{NAMESPACE_ID}/environments/{RESOURCE_ID}/ssh
Connection: Upgrade
Upgrade: bastion-ssh
Content-Type: application/json
```

or:

```http
POST /v1/namespaces/by-key/{NAMESPACE_KEY}/environments/{RESOURCE_ID}/ssh
Connection: Upgrade
Upgrade: bastion-ssh
Content-Type: application/json
```

The request shape, `101 Switching Protocols` response, and binary frame format
match the [SSH protocol](/reference/environment-states-and-streams/#ssh-upgrade-stream).
The control plane opens a second `bastion-ssh` upgrade to the owning node and
copies the byte stream in both directions without changing frames. The node does
not verify the guest host key, and command arrays do not preserve argument
boundaries.

Before the client upgrade, malformed JSON returns `400 Bad Request`; missing
source or derivative resources return `404 Not Found`; node or state dependency
failures return `424 Failed Dependency`; and other control-plane or node
handshake failures return `500 Internal Server Error`.
