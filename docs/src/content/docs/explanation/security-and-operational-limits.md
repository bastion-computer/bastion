---
title: Security and operational limits
description: Understand Bastion's trust model, supported hosts, sensitive data, and unsupported availability behavior.
---

Bastion assumes that you operate it inside a trusted administrative boundary.
It isolates coding workloads in VMs, but it does not provide a complete identity,
network-security, high-availability, or backup system around those VMs.

Use these limits to decide where you run Bastion and which controls you must
provide outside the product.

## API trust boundary

The host API and cluster API have no native authentication, authorization, or
TLS. They serve plain HTTP directly. The default loopback listen addresses
reduce accidental exposure on a new installation, but changing a listen address
can make an API reachable without adding any protection.

Every caller that can reach an API is effectively a Bastion administrator. A
reachable caller can read stored secrets, create or remove resources, open root
SSH sessions in environments, proxy agent and tunnel traffic, and trigger
privileged VM operations through the host API. On the cluster API, a caller can
also select any namespace and manage global nodes and the base.

Do not expose either API directly to an untrusted network. Put remote access
behind controls that provide TLS and authenticate and authorize callers, such as
a private network or VPN. The CLI cannot add arbitrary authentication headers or
configure a client certificate, so any reverse proxy must use a transparent
network identity and preserve HTTP upgrade traffic. Restrict node APIs so that
the cluster control plane and intended operators can reach them. The
[Use remote access with
Tailscale](/how-to/remote-access-with-tailscale/) shows one network-boundary
pattern; it does not change Bastion's native API behavior.

The CLI accepts `http` and `https` API URLs, and cluster nodes can be registered
with either scheme. An `https` URL only works when an external component
actually provides TLS. Bastion's API server does not load certificates or
terminate TLS itself.

## Privileged daemon boundary

The daemon runs with host root privileges and listens on a local Unix socket
rather than a TCP port. The Cloud Hypervisor child processes it launches also
run with host root privileges. The daemon owns disk preparation, TAP interfaces,
DHCP, network address translation, and runtime cleanup. The host API calls it
only when a resource operation requires privileged work.

Unix socket ownership is therefore a host security control. Only the host API
service account and intended administrators should be able to connect. The same
configured ownership also protects per-VM proxy sockets and allows the host API
to read the guest SSH private key. Misconfigured ownership can either break SSH
and tunnels or grant an unintended local process access to daemon operations.

See [How Bastion works](/explanation/how-bastion-works/) for the process
relationship and [Host requirements and
configuration](/reference/host-requirements-and-configuration/) for socket and
data-directory settings.

## VM isolation boundary

Each environment has a separate Cloud Hypervisor VM, guest kernel, process tree,
root disk overlay, and TAP network. Actions and SSH sessions run as `root` inside
the guest. Guest root is not host root, but all environments still share the
host kernel's KVM and networking facilities and consume the same physical CPU,
memory, and storage.

Treat VM isolation as one layer in your security design, not as a promise of
hostile multi-tenant hardening. Keep the Linux host, Cloud Hypervisor assets,
kernel, and Bastion release updated through your normal operations. Limit guest
network access when agents should not reach internal services or the public
internet.

The base carries a guest SSH key that is reused by its descendant environments.
The host API controls access to that key and proxies SSH, so API compromise also
means guest access.

## Secret and artifact sensitivity

Bastion stores single-host secrets as application-readable values in SQLite and
cluster source secrets as application-readable values in Postgres. The control
plane can also copy values into node-local SQLite databases as derivative
secrets. Bastion does not add application-level encryption to these values.

Template references reduce values embedded in source JSON, but resolution can
write values into agent files, action output, immutable template overlays, and
environment disks. Base archives include the guest SSH private key. Template
archives can include values persisted during agent setup or init actions. Treat
both archive types and cluster S3 objects as sensitive even when their manifests
contain only references.

Protect data directories and backups with operating-system permissions and
storage encryption. Require encrypted transport and access controls for
Postgres and S3-compatible storage. Bastion does not request object-level
encryption or coordinate key management for you.

See [Actions and secrets](/explanation/actions-and-secrets/) for resolution and
persistence details and [Host API](/reference/api/host/) for the interfaces that
return secret values.

## Supported execution roles

Distribution support and VM-host support are different:

| Delivery or role                  | Supported platform           | Scope                                                           |
| --------------------------------- | ---------------------------- | --------------------------------------------------------------- |
| Native release                    | Linux x86_64                 | Client, cluster control plane, host API, daemon, and VM runtime |
| Native release                    | macOS arm64                  | Client and cluster control plane only                           |
| Published control-plane container | `linux/amd64`, `linux/arm64` | Cluster control plane only                                      |
| VM node                           | Linux x86_64                 | Host API, daemon, and Cloud Hypervisor VM runtime               |

The VM node role also requires all of the following:

- Linux on x86_64
- Read and write access to `/dev/kvm`
- `/dev/vhost-vsock`
- Host networking and image utilities required by system setup
- Nested virtualization when the Bastion host is itself a VM

The published Linux arm64 container does not make Linux arm64 a supported VM
node. A macOS or containerized control plane still needs one or more supported
Linux x86_64 nodes before it can create an environment. See [Run a cluster with
Docker Compose](/how-to/run-cluster-with-docker-compose/) for the container
workflow.

See the [Get started prerequisites](/tutorials/get-started/#prerequisites) for a
first host workflow and [Host requirements and
configuration](/reference/host-requirements-and-configuration/#linux-vm-host-requirements)
for the complete host checks.

## Environment lifecycle limits

Bastion creates and removes environments. It reports stopped, paused, and error
states during reconciliation, but the public lifecycle has no restart, resume,
checkpoint, migration, or clone operation for an existing environment.

An environment's writable qcow2 overlay stays on its assigned host. Bastion does
not replicate or export it. If you remove the environment or lose the host disk,
changes made after template creation are not recoverable through Bastion.

Base and template archives contain disk artifacts, not VM memory snapshots.
Template import requires the exact base content address and creates a new
template identity. A forced base replacement can make existing templates
unusable rather than upgrading them.

See [Resource lifecycle](/explanation/resource-lifecycle/) for the dependency
model. Follow [Manage the base](/how-to/manage-base/) and [Create and manage
templates](/how-to/create-manage-templates/) for supported archive workflows.

## Cluster availability limits

The cluster control plane schedules new environments and routes to their owning
nodes. It does not provide high availability, failover, rescheduling, or node
drain.

A failed node makes its environments unavailable. Bastion does not recreate
them elsewhere because their writable disks exist only on that node. Aggregate
health and utilization also fail when a registered node cannot answer; these
endpoints report a problem but do not repair it.

New operations do not consistently route around unavailable nodes. Environment
placement aborts on the first candidate utilization error instead of skipping
that candidate. Base build and template preparation select the first registered
node and do not fail over to another node. One unavailable node can therefore
block new work even when other nodes are healthy.

Node removal does not migrate workloads. Namespace removal does not recursively
stop node VMs or delete derivative artifacts and S3 objects. Clean up resources
through their normal APIs before removing a namespace or node.

Cluster orchestration spans Postgres, S3-compatible storage, and multiple node
APIs without a distributed transaction. Some steps can complete before another
step fails, especially during base synchronization and derivative creation or
cleanup. [Clusters and
namespaces](/explanation/clusters-and-namespaces/#partial-operations) explains
how to reason about partial state.

## Backup limits

Base and template export endpoints are not complete installation backups. They
exclude host configuration, SQLite metadata, environment overlays, VM memory,
and live process state. The cluster API also does not export Postgres as a whole
or produce a consistent snapshot with its S3 bucket.

Plan backups at the infrastructure layer:

- Coordinate a single-host data and configuration backup with base and template
  exports.
- Coordinate Postgres and S3 backups for a cluster.
- Store archive encryption keys and access policy outside the Bastion data being
  protected.
- Assume that active environment work is disposable unless the workload writes
  important state to an external version-control or storage system.

No Bastion restore operation reconstructs a lost environment's writable state.
Test recovery with disposable resources and verify base content addresses before
you depend on exported templates. Structural archive validation and base content
addresses do not authenticate an archive's publisher. Verify authenticity
outside Bastion before importing guest disk content.

Follow [Back up and restore](/how-to/back-up-and-restore/) for the supported
artifact procedure.
