---
title: Glossary
description: Definitions for Bastion resources, runtime components, cluster concepts, protocols, and storage terms.
---

## Action

One ordered lifecycle step in `actions.init` or `actions.start`. A run action
executes an inline shell command. A use action invokes an action package. See
[template actions](/reference/template-configuration/#lifecycle-action-configuration).

## Action package

A directory under `<DATA_DIR>/actions` that contains `manifest.json` and the
files used by its command. Bastion embeds built-in packages and also supports
user-created packages. See the
[action manifest reference](/reference/action-manifest/).

## Agent

A managed service inside an environment. The current template schema requires
the `opencode` agent. Bastion configures and starts its server and proxies it
through the API.

## API URL

The base URL used by client commands, such as `http://localhost:3148` for a host
or `http://localhost:3150` for a cluster control plane. The native APIs speak
plain HTTP and provide no authentication or TLS. An `https` API URL refers to an
external TLS endpoint in front of Bastion. A client or node API URL can include
a base path. Do not include user information, a query, or a fragment because
Bastion appends product routes by string concatenation; current validation does
not reject every unsupported component.

## Archive

A zstd-compressed tar stream used to export or import a base or prepared
template. A base archive contains its root disk, cloud-init seed, and SSH private
key. A template archive contains its manifest and qcow2 overlay. Archives can
contain sensitive resolved values. Structural validation and content addresses
do not authenticate an archive; import only from a trusted source after
verifying an out-of-band digest or signature.

## Base

The singleton, template-independent prepared root-disk layer used by every template
on a host or cluster. A cluster base is global across namespaces. See the
[base CLI commands](/reference/cli/host/#bastion-base-commands).

## Base content address

A `sha256:` identifier calculated from the canonical base artifacts. Templates
store it as `baseContentAddress`. Template import and environment launch require
the current base to match exactly. It detects content changes but does not prove
the base's origin or authenticity.

## Bastion daemon

The root process started by `bastion start daemon`, also called `bastiond`. It
performs privileged Cloud Hypervisor, disk, TAP, guest setup, and VM cleanup
operations behind a Unix socket. It is Linux x86_64 only.

## Client

The `bastion` command when it calls a host or cluster API. Client commands can
run on Linux x86_64 or macOS arm64. Local client overrides are stored in
`<DATA_DIR>/client.json`.

## Cluster

A control plane plus registered Bastion host API nodes. The control plane stores
source state in Postgres, archives in S3-compatible storage, schedules
environments, and proxies client traffic to owning nodes.

## Cluster control plane

The service started by `bastion start cluster`. It runs on Linux x86_64 and
macOS arm64 and listens on `localhost:3150` by default. It does not itself host
VMs. See the [cluster API reference](/reference/api/cluster/).

## Cold boot

Starting a VM from disk and fresh cloud-init media without restoring VM memory
state. Bastion cold-boots template preparation VMs and environments.

## Cursor

The pagination value returned in a list response. Pass a non-null cursor to the
next request. Current cursors are creation timestamps, but clients should treat
them as opaque strings.

## Data directory

The host directory selected by `--data-dir` or `BASTION_DATA_DIR`, defaulting to
`~/.bastion`. It contains host SQLite metadata, actions, runtime assets, the
base, templates, environments, and local client config. The cluster control
plane instead uses Postgres and S3-compatible storage for source state.

## Derivative

A node-local secret, template, or environment created by the cluster control
plane from a namespace source resource. Derivative IDs are implementation
routing details and differ from source IDs. Clients normally see only source
resources.

## Environment

A VM created from an immutable template with a new writable disk overlay and
fresh cloud-init state. It has an `env_`-prefixed ID, optional key, status, source
template ID, and tags. See
[environment states](/reference/environment-states-and-streams/#environment-statuses).

## Guest

The Ubuntu operating system running inside a Bastion VM, as distinct from the
Linux host operating system. Lifecycle actions run as `root` in the guest.

## Host

A Linux x86_64 machine with KVM and vhost-vsock support that runs the host API,
daemon, and Cloud Hypervisor VMs. See
[Linux VM host requirements](/reference/host-requirements-and-configuration/#linux-vm-host-requirements).

## Host API

The service started by `bastion start api`. It stores one host's resource
metadata in SQLite, listens on `localhost:3148` by default, and delegates
privileged VM operations to the daemon. See the
[host API reference](/reference/api/host/).

## ID

A generated resource identifier made from a type prefix, underscore, and UUID
v4. Public prefixes include `sec_`, `tpl_`, `env_`, `node_`, and `ns_`. IDs are
globally generated; cluster lookups still enforce namespace ownership for
namespaced resources.

## Init action

An action in `actions.init`. Bastion runs init actions once while preparing a
template. Their file system changes and resolved values can persist in the
immutable template overlay and archive.

## Key

An optional user-provided nonblank resource alias. Keys are unique within their
scope: host-wide on one host API, per namespace for cluster secrets, templates,
and environments, and cluster-wide for nodes and namespaces. A by-key route does
not work for an unkeyed resource. Use slash-free keys because router by-key paths
cannot reliably address a key that contains `/`; use the generated ID route for
an existing affected resource.

## Namespace

A cluster scope for source secrets, templates, environments, keys, and lists.
It has an `ns_`-prefixed ID and optional key. The base, nodes, health API, and
utilization API are global rather than namespaced.

## NDJSON

Newline-delimited JSON. Long-running base, template, environment, and cluster
node operations emit one JSON event per line. Event types are `log`, `result`,
and `error`. See
[NDJSON operation streams](/reference/environment-states-and-streams/#ndjson-operation-streams).

## Node

A registered Bastion host API URL that the cluster control plane can use to host
derivative resources and environments. A node must ultimately be a Linux x86_64
VM host even when the control plane runs on macOS.

## OpenCode

The currently supported managed agent. Bastion installs a pinned OpenCode guest
binary into the base, configures a systemd service from the template, and proxies
its HTTP server through environment agent routes.

## Overlay

A qcow2 disk layer backed by another disk. Bastion uses a shared prepared base,
an immutable template overlay backed by that base, and a writable environment
overlay backed by the template.

```text
base root disk
`-- immutable template overlay
    `-- writable environment overlay
```

## Resource

A public API object: base, secret, template, environment, node, or namespace.
Most resources have IDs and optional keys. The base is a singleton without an ID
or key.

## Secret

A stored nonempty string value with a `sec_`-prefixed ID and optional key.
Templates reference secrets with `${{ secret.KEY }}` or a secret ID. List and
delete responses omit the value; get returns it.

## Source resource

The client-visible secret, template, or environment stored by the cluster
control plane in a namespace. The control plane creates node-local derivatives
as needed while preserving source IDs and keys at the cluster API.

## Start action

An action in `actions.start`. Bastion runs start actions after each environment
cold-boots and after managed guest services start. Start actions do not change
the immutable template overlay.

## Template

An immutable configuration and prepared qcow2 overlay backed by one exact base.
It has a `tpl_`-prefixed ID, optional key, `baseContentAddress`, and JSON config.
See the [template configuration reference](/reference/template-configuration/).

## Tunnel

A template-registered name and guest localhost HTTP port. The host API reaches
the port through the guest proxy over vsock and exposes an HTTP reverse-proxy
route. Tunnel proxying supports arbitrary methods, paths, and connection
upgrades.

## vCPU

A virtual CPU allocated to a template preparation VM or environment. Template
`resources.vcpu` controls the allocation and cluster capacity requirement when
set. If omitted, the host API, daemon, and cluster process resolve defaults
independently.

## VM

A Cloud Hypervisor virtual machine managed by the daemon. Environments are
public VM-backed resources; temporary template and base VMs are orchestration
details rather than environment records.

## vsock

Virtual socket transport between a host and guest without exposing a guest TCP
port on the host network. Bastion uses Cloud Hypervisor vsock and a guest proxy
for OpenCode and registered HTTP tunnels.
