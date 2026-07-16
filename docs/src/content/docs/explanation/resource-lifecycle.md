---
title: Resource lifecycle
description: Understand how Bastion turns system assets into bases, templates, and running environments.
---

Bastion separates reusable disk state from per-environment state. The resource
chain is a source root file system, a base, a template, and an environment. Each
step has a different lifetime and reason to change.

## Copy-on-write layers

Bastion uses qcow2, a disk format that supports copy-on-write backing files. A
child disk reads unchanged blocks from its parent and writes changed blocks to
itself. The resulting relationship is:

| Layer               | Purpose                                                          | Mutability after creation |
| ------------------- | ---------------------------------------------------------------- | ------------------------- |
| System rootfs       | Downloaded Ubuntu source image used to construct the base.       | Managed system asset      |
| Base                | Shared, template-independent guest software and access material. | Immutable backing image   |
| Template overlay    | Reusable result of template initialization.                      | Immutable backing image   |
| Environment overlay | Files changed by one running environment.                        | Writable until removal    |

The layers save disk space and preparation time, but they are not independent
copies. Removing or replacing a backing layer makes its descendants unusable.
Bastion records a base content address on every template and checks for an exact
match before importing a template or launching an environment.

A content address is a SHA-256 identifier calculated from the base artifacts.
It detects that a different base is present; it does not encrypt the artifacts,
identify their publisher, or make different bases compatible.

## Base preparation

System initialization downloads Cloud Hypervisor, a guest kernel and initramfs,
an Ubuntu rootfs, and a pinned OpenCode asset. It generates the guest SSH key
locally. These are source assets, not a usable Bastion base.

Base construction copies and boots the source rootfs in a temporary VM. Bastion
installs the guest proxy, OpenCode, and other template-independent components,
syncs the guest file system, and stores the resulting root disk as the singleton
base. The base also carries cloud-init seed material and the private SSH key used
to reach guests.

There is one base per single-host data directory and one global base per
cluster. Keeping common software in one layer makes templates smaller. The
tradeoff is broad coupling: forcing a different base into place can invalidate
every template that records the old content address. In a cluster, that coupling
crosses all namespaces.

See [Install, update, or remove Bastion](/how-to/install-update-remove/) for
source-asset preparation and [Manage the base](/how-to/manage-base/) for build,
replacement, import, and export procedures.

## Template preparation

A template is both a JSON definition and an immutable disk overlay. During
template creation, Bastion creates the overlay on the current base and boots a
temporary VM. It writes agent configuration, runs the ordered `actions.init`
steps, removes machine-specific cloud-init and machine identity state, tears down
the temporary VM, and retains the overlay as read-only.

This work happens once for that template. Package installation, repository
cloning, and other reusable setup can therefore be paid once instead of once per
environment. The tradeoff is staleness: changes captured by init remain fixed
until you create a new template. Bastion does not edit an existing template or
replay init actions in existing templates.

If agent setup or an init action fails, template creation fails and Bastion does
not register a reusable template record. Bastion attempts cleanup, but streamed
output can still contain sensitive or operational details from the failed guest.

See [Create and manage templates](/how-to/create-manage-templates/) for task
steps, [Template configuration](/reference/template-configuration/) for the
exact definition, and [Actions and
secrets](/explanation/actions-and-secrets/) for lifecycle timing.

## Environment creation

An environment starts with a new writable qcow2 overlay backed by the selected
template. Bastion also creates fresh cloud-init media, TAP networking, a DHCP
lease, and a Cloud Hypervisor process. It cold-boots the guest rather than
restoring VM memory.

After SSH becomes available, Bastion starts the guest proxy, refreshes and
restarts configured agent services, and runs the ordered `actions.start` steps.
Only then does a successful create operation report the environment as running.
Start actions run once per environment creation, not during template creation.

Changes in the environment overlay belong only to that environment. They do not
flow back into its template or appear in sibling environments. This isolation
is useful for parallel agents, but it means removing an environment permanently
removes its writable state.

If launch or a start action fails after the environment record exists, Bastion
records an error state and `lastError` where possible. It does not automatically
retry the launch or reschedule the environment. The current API also has no
start or restart operation for a stopped environment; create a new environment
when you need a fresh instance.

See [Manage environments](/how-to/manage-environments/) for creation,
inspection, and removal procedures. [Environment states and
streams](/reference/environment-states-and-streams/) defines the exact status
and operation-stream behavior.

## Dependency and removal rules

Environment records prevent removal of the template they reference. Remove
dependent environments before removing a template. Base replacement has no
equivalent recursive migration: a forced replacement succeeds at the base layer
but leaves old templates unable to launch because their content addresses no
longer match.

In a cluster, source records add another level. The control plane creates
node-local templates, secrets, and environments as derivatives. Normal
environment removal tries to remove the node environment and then cleans an
unused template derivative. A node failure can prevent that cleanup.

Namespace removal is different from resource-by-resource removal. It issues a
database deletion and relies on Postgres constraints and cascades; it does not
walk the namespace and invoke node or object-storage cleanup. See [Clusters and
namespaces](/explanation/clusters-and-namespaces/) before treating namespace
deletion as cleanup.

## Export boundaries

Bastion exports bases and prepared templates, not complete hosts or live
environments.

A base archive contains the prepared root disk, cloud-init seed, metadata, and
guest SSH private key. A template archive contains its manifest and immutable
qcow2 overlay. It does not include its backing base, cloud-init media, or VM
memory, so restore the exact base before importing the template.

Bastion validates archive structure, required entries, and metadata before an
import. Base validation also recomputes the base content address. These checks
reject malformed or inconsistent input, but they do not authenticate who
created an archive. The archive formats have no signature or publisher trust
chain. Verify provenance and authenticity outside Bastion before importing an
archive, because an accepted archive supplies guest disk content that Bastion
later boots.

There is no environment export. Environment writable overlays, live processes,
and memory state are outside the backup format. Imports also create a new
template ID and do not preserve the exported key unless you assign one.

These exports are artifact backups, not a full control-plane backup. A complete
single-host recovery plan must also account for SQLite and configuration. A
cluster recovery plan must coordinate Postgres, S3-compatible storage, and
external configuration. The cluster API does not provide a one-command
cluster-wide backup or restore transaction.

Resource operations are also not atomic across API metadata, local artifacts,
object storage, and node APIs. Bastion attempts cleanup after failures, but a
failed operation can leave an artifact without a record or a record whose remote
artifact changed. Verify each affected store after an interrupted operation.

For the supported artifact workflow, see [Back up and
restore](/how-to/back-up-and-restore/).
