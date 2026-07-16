---
title: Clusters and namespaces
description: Understand cluster source resources, node derivatives, namespace scope, and distributed-operation tradeoffs.
---

A Bastion cluster puts one control-plane API in front of multiple normal
Bastion hosts. Use it when one host does not provide enough capacity or when you
want one routing endpoint for environments on several hosts.

The cluster is an optional coordination layer, not a different VM runtime. Each
node still runs the single-host API and privileged daemon, and each environment
remains tied to one node.

## Control-plane relationships

The cluster depends on three kinds of state:

| Component                | State or work it owns                                                                    |
| ------------------------ | ---------------------------------------------------------------------------------------- |
| Cluster API and Postgres | Nodes, namespaces, source secrets and templates, environment routes, and status metadata |
| S3-compatible storage    | The global base archive and source template archives                                     |
| Linux nodes              | Imported bases, derivative resources, writable environment disks, and live VMs           |

Native cluster control-plane releases run on Linux x86_64 and macOS arm64. The
published `bastioncomputer/bastion` control-plane container runs on
`linux/amd64` and `linux/arm64`. Neither form launches VMs locally. Nodes that
host environments must run Linux x86_64 with KVM and `/dev/vhost-vsock` support.

Clients send resource and connection requests to the cluster API. The control
plane selects a node and calls that node's host API. Later SSH, OpenCode, and
tunnel traffic follows the environment route stored in Postgres. Clients do not
need direct node access, but the cluster API must be able to reach every node API.

See [Deploy and operate a cluster](/how-to/deploy-and-operate-cluster/) for the
setup and management workflow, and see [Host requirements and
configuration](/reference/host-requirements-and-configuration/) for process
settings.

## Source resources and derivatives

Postgres holds source resources, which are the records visible through a
namespace. A derivative is a node-local secret, template, or environment that
the control plane creates to perform work on one node.

Prepared template archives let the control plane reproduce a template
derivative without rerunning init actions. When a selected node does not already
have the derivative, the control plane loads the source archive from object
storage, creates node-local secrets for its references, rewrites those
references, and imports the resulting template. The source record remains the
stable cluster identity while node-local IDs remain implementation details.

This model avoids preparing the same template on every node in advance. It also
means Postgres, object storage, and node state can disagree after a partial
failure. Derivative cleanup is part of normal environment removal, but it is not
a garbage collector that continuously repairs all orphaned state.

Secret rotation follows the same derivative model. A template derivative keeps
the node-local derivative secrets created with it. Replacing a source secret
does not update that derivative, so a later environment that reuses it can still
receive the old value. A newly created derivative copies the current source
value. Removing the source secret does not call node APIs to delete node-local
copies. See [Actions and
secrets](/explanation/actions-and-secrets/#cluster-derivative-timing) for the
resolution consequences and [Manage secrets](/how-to/manage-secrets/) for
source-record operations.

## Placement and ownership

Environment creation checks each candidate node's reported utilization against
the template's declared vCPU, memory, and volume. It prefers a node that already
has the needed template derivative and otherwise considers registered nodes in
random order. If the checked nodes do not have enough capacity, creation fails
instead of intentionally overcommitting them.

Capacity is a point-in-time check, not a durable distributed reservation. A
node can change between the utilization check and VM creation, and concurrent
requests can race. Resource declarations therefore support placement decisions;
they are not a cluster-wide quota or a guarantee that an external workload will
not consume the same host capacity.

Placement does not skip a candidate whose utilization request fails. Bastion
aborts on the first candidate utilization error it encounters, even when another
registered node has enough capacity. An unavailable node can therefore block a
new environment operation before the scheduler considers a healthy candidate.

Base build and template preparation use a simpler policy: Bastion selects the
first registered node in creation order. It does not try another node if that
node is unavailable or the preparation request fails. One unavailable first
node can therefore block a base build or template creation across the cluster.

Once created, an environment belongs to its selected node. Its writable qcow2
overlay is not copied to Postgres, object storage, or another node. The control
plane has no automatic failover or rescheduling. If the node becomes
unavailable, the environment and its connection routes become unavailable too.

## Namespace scope

A namespace scopes source secrets, templates, environments, keys, and lists.
The same key can exist in different namespaces, and every namespace-scoped API
request resolves exactly one namespace by ID or key.

The base is deliberately outside namespaces. One content-addressed base backs
templates across the entire cluster, so replacing it can affect every
namespace. Nodes are also global control-plane resources rather than namespace
members.

Namespaces organize records and reduce accidental name collisions. They are not
an authorization boundary. The cluster API has no native authentication and
does not associate callers with namespaces. Any caller that can reach it can
select another namespace, read that namespace's secrets, or manage global nodes
and the base.

## Removal semantics

Normal environment removal first asks the owning node to remove its derivative
environment. The control plane then deletes the source record and, when no
environment uses the derivative template, attempts to remove that template and
its node-local derivative secrets.

Node removal does not drain, migrate, or reschedule environments. Database
constraints prevent removing a node while source environments still reference
it. Removing a node with no recorded environments still does not perform a
general scan for unrecorded node artifacts.

Namespace removal has intentionally narrower behavior than recursive resource
cleanup. It issues a database deletion and relies on Postgres constraints and
cascades for source records. It does not call node APIs to stop VMs or remove
derivative secrets and templates, and it does not delete the namespace's
template objects from S3. Remove resources through their normal lifecycle
before removing the namespace if you need operational cleanup.

See [Cluster CLI](/reference/cli/cluster/) and [Cluster
API](/reference/api/cluster/) for exact namespace selectors and interfaces.

## Availability limits

Bastion does not provide native cluster high availability. In particular, it
does not provide:

- Cluster API leader election or coordinated active-active operation
- Node failure detection that relocates running environments
- Automatic retries that complete an interrupted multi-node operation
- Candidate skipping when utilization checks fail
- Base or template preparation failover from the first registered node
- A node cordon or drain workflow
- Replication of environment writable overlays

Postgres and S3-compatible services can use their own availability features,
but those features do not add Bastion-level failover or reconcile node runtime
state.

## Partial operations

A cluster request can call several node APIs, read or write object storage, and
then update Postgres. These systems do not share a transaction. Bastion attempts
targeted cleanup on many error paths, but cleanup can also fail.

For example, a base fan-out can update some nodes before another node rejects
the import. An environment can be created on a node before its source record is
written. A resource can be removed remotely before a later database or
object-storage step fails. Forced base replacement has the widest impact
because nodes can temporarily hold different base content.

After an interrupted cluster operation, verify the source record, S3 object,
and affected node resources instead of assuming that the error rolled back all
work. Keep request IDs and streamed progress logs so you can identify which
remote steps completed.

Cluster exports also cover only bases and templates. A complete recovery plan
needs coordinated Postgres and object-storage backups, and no backup path
captures a running environment's writable disk or memory. See [Resource
lifecycle](/explanation/resource-lifecycle/#export-boundaries) for the artifact
boundaries.
