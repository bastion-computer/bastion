---
title: How Bastion works
description: Understand the processes, trust boundaries, and optional cluster control plane that make up Bastion.
---

Bastion turns a declarative template into an isolated virtual machine (VM) for a
coding agent. It separates the HTTP interface from privileged VM operations so
that most request handling does not run as `root`.

This separation limits which process holds host privileges. It does not make a
reachable Bastion API safe for untrusted callers. The host and cluster APIs have
no native authentication or TLS, so every caller that can reach either API is
effectively a Bastion administrator. See [Security and operational
limits](/explanation/security-and-operational-limits/) before exposing an API
beyond its default loopback address.

## Single-host model

A single-host installation has three main participants:

| Participant             | Responsibility                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------ |
| CLI or other API client | Sends resource and connection requests over HTTP.                                    |
| Host API                | Stores metadata in SQLite, applies lifecycle rules, and proxies environment traffic. |
| Privileged daemon       | Manages disks, networking, Cloud Hypervisor processes, and VM cleanup.               |

The host API listens on `localhost:3148` by default. It runs as an unprivileged
process and calls the daemon through a Unix socket. The daemon, also called
`bastiond`, runs with host root privileges because KVM, TAP networking, network
address translation, and VM process management require them. The Cloud
Hypervisor child processes that `bastiond` launches also run with host root
privileges. Unix permissions on the daemon socket determine which local process
can request those operations.

The request path is:

1. A client sends an HTTP request to the host API.
2. The host API validates the resource operation and reads or writes SQLite.
3. When the operation needs VM work, the host API calls the daemon over its Unix
   socket.
4. The daemon creates or removes artifacts and controls Cloud Hypervisor.

This path explains why the API and daemon must use the same Bastion data
directory and compatible socket ownership. It also explains why an API process
can restart independently of running VM processes: the daemon and Cloud
Hypervisor own the live runtime, while SQLite holds the API's durable metadata.
The API reconciles environment records with daemon state when it reads them.

For setup procedures, see [Install, update, or remove
Bastion](/how-to/install-update-remove/). For listen addresses, paths, and socket
ownership, see [Host requirements and
configuration](/reference/host-requirements-and-configuration/).

## Guest boundary

Each environment is a cold-booted Cloud Hypervisor VM with its own kernel, root
file system, process tree, and network interface. Code running as `root` in one
guest does not run as `root` on the host or in another guest.

Bastion uses two paths into a running guest:

- The host API carries SSH sessions over an upgraded HTTP connection, then
  connects to the guest with the private key associated with the base.
- A guest proxy carries OpenCode and named HTTP tunnel traffic over virtio-vsock.
  Vsock is a host-to-guest transport that does not require exposing the guest
  service on the host network.

The API remains the client-facing entry point for both paths. As a result, API
reachability controls access not only to lifecycle operations, but also to root
SSH sessions and proxied guest services. See [Connect to
environments](/how-to/connect-to-environments/) for task steps and the [Host
API](/reference/api/host/) reference for the exact proxy interfaces.

## Disk model

Bastion uses qcow2 copy-on-write disk layers. A copy-on-write layer reads
unchanged blocks from a backing image and stores only its own changed blocks.
The shared base contains template-independent guest software, each template
captures reusable setup, and each environment receives a fresh writable layer.

This design makes environment creation cheaper than copying a complete disk. It
also creates a strict dependency chain: an environment depends on its template,
and a template depends on the exact base identified by its content address.
Replacing a base does not migrate its templates.

See [Resource lifecycle](/explanation/resource-lifecycle/) for the complete
relationship. Follow [Manage the base](/how-to/manage-base/), [Create and manage
templates](/how-to/create-manage-templates/), and [Manage
environments](/how-to/manage-environments/) for lifecycle procedures.

## Optional cluster control plane

The cluster control plane adds placement and routing above normal single-host
installations. It does not replace the host API or daemon. Every cluster node is
still a Linux host running both processes.

The cluster API stores source records and routing information in Postgres. It
stores the global base archive and prepared template archives in S3-compatible
object storage. When it places an environment, it creates node-local derivative
resources and records which node owns them. A derivative is a node-local copy
created from a cluster source resource.

Clients call the cluster API, and the cluster API calls each selected node's
host API. The cluster API never controls a node's daemon directly. SSH, OpenCode,
and tunnel requests follow the recorded route through the cluster API to the
owning node.

This extra layer lets one API address multiple hosts, but it also turns one
request into several network and storage operations. Those operations are not a
single distributed transaction. A failure can leave work completed on only
some nodes or in only some stores. [Clusters and
namespaces](/explanation/clusters-and-namespaces/) describes the resulting
tradeoffs, and [Deploy and operate a
cluster](/how-to/deploy-and-operate-cluster/) covers setup and operation.

## Platform roles

Native release archives target Linux x86_64 and macOS arm64. The Linux release
can run the client, cluster control plane, host API, and VM runtime. The macOS
release can run the client and cluster control plane, but it cannot run the host
API, privileged daemon, system setup, or VM runtime.

The published `bastioncomputer/bastion` control-plane container targets
`linux/amd64` and `linux/arm64`. Container availability on Linux arm64 does not
add Linux arm64 VM-host support.

A VM node requires Linux x86_64 with readable and writable KVM and
`/dev/vhost-vsock`. A cloud VM also needs nested virtualization when it acts as a
Bastion node. This split works because the cluster API coordinates remote VM
nodes and does not host environments itself. See [Run a cluster with Docker
Compose](/how-to/run-cluster-with-docker-compose/) for the published container
workflow.

Use [Get started](/tutorials/get-started/) for the supported single-host workflow
and the [Host CLI](/reference/cli/host/) reference for platform-specific command
availability.
