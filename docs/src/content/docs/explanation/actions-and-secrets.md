---
title: Actions and secrets
description: Understand when Bastion actions run, when secret references resolve, and where sensitive values can persist.
---

Templates combine reusable disk preparation with per-environment startup work.
Actions define that work, while secret references let the stored template keep
names instead of embedding secret values in its JSON configuration.

Neither mechanism is a sandbox inside the guest. Actions run as `root`, and a
resolved secret becomes ordinary data wherever an action or agent writes it.

## Ordered action phases

Bastion runs actions serially in their declared order. An action is either an
inline shell command or a reference to an action package. An action package is a
host-side directory that Bastion copies into the guest before executing its
manifest command.

The two phases have different persistence and timing:

| Phase           | Timing                                     | State affected                                    |
| --------------- | ------------------------------------------ | ------------------------------------------------- |
| `actions.init`  | Once while Bastion prepares a new template | Becomes part of the immutable template overlay    |
| `actions.start` | Once after each new environment cold-boots | Stays only in that environment's writable overlay |

Bastion writes the OpenCode agent configuration before init actions. During
environment creation, it starts the guest proxy, rewrites and restarts the agent
service, waits for agent health, and then runs start actions. This ordering lets
init prepare reusable files and lets start work against the final
environment-specific agent configuration.

An init failure prevents template registration. A start failure leaves the
environment in an error state when Bastion can persist the failure. Bastion
stops at the first failed action in either phase; it does not roll back commands
that already changed the guest.

Put expensive, stable setup in init when you want every environment to inherit
the result. Put work that must observe current external state or current secrets
in start. The cost of start work is paid for every environment, and its failure
can prevent that environment from becoming ready.

See [Create and manage templates](/how-to/create-manage-templates/) for template
task steps, [Create custom actions](/how-to/create-custom-actions/) for a
package workflow, [Action manifest](/reference/action-manifest/) for package
structure, and [Built-in actions](/reference/built-in-actions/) for supported
packages.

## Host coupling of action packages

The template names an action package; it does not embed that package in the
stored JSON definition. Bastion reads packages from the daemon's data directory
when the phase executes. Built-in packages are seeded when the daemon starts.
Custom packages remain local operational dependencies.

On one host, this means an upgrade or local package change can affect later
template or environment creation. In a cluster, every node that might execute a
custom start action needs a compatible package with the same name. A prepared
template archive contains the effects of init actions, but it does not make a
custom start package available on another node.

## Secret references

A template string can contain a reference such as `${{ secret.API_TOKEN }}` or
`${{ secret.sec_xxxxxx }}`. A keyed reference is human-readable and can resolve
to a newly stored secret that reuses the key. An ID reference binds the template
to one specific secret record.

Resolution traverses every string in the template JSON, including agent
configuration, shell commands, working directories, package inputs, and action
context. Bastion substitutes text; it does not apply shell escaping or
field-specific validation beyond validating the resulting template. You remain
responsible for how a substituted value behaves in a shell command or file.

The source template record keeps the reference expression rather than the
resolved value. Template inspection and template archive manifests therefore do
not reveal the value directly. Secret list responses also contain metadata only,
although an explicit secret get request returns the value.

See [Manage secrets](/how-to/manage-secrets/) for source-secret task steps, [Host
CLI](/reference/cli/host/) and [Host API](/reference/api/host/) for the resource
interfaces, and [Template configuration](/reference/template-configuration/)
for the expression syntax. Changing a cluster source secret does not refresh an
existing node derivative.

## Single-host resolution timing

On a single host, Bastion resolves the entire template at both template creation
and environment creation.

At template creation, every reference must exist, including a reference used
only by a start action. Bastion uses the resolved configuration to write agent
files and run init actions, but stores the original reference-bearing JSON.
Values written by agent setup or init can remain in the immutable template
overlay and in archives exported from that template.

At environment creation, the host resolves the stored configuration again. It
rewrites agent files and uses the newly resolved values for start actions. It
does not rerun init actions, so changing the secret behind an init reference does
not change files already baked into the template.

This distinction determines whether a new value takes effect:

- Agent configuration is refreshed for each environment and can receive the
  value resolved at environment creation.
- Start actions receive the value resolved at environment creation.
- Init output retains the value resolved when the template was created.

If any referenced secret is absent at either resolution point, the operation
fails before it can complete successfully.

## Cluster derivative timing

The cluster control plane stores source secret references in Postgres, but a VM
node does not resolve those source references for every environment. When the
control plane creates a template derivative on a node, it copies each current
source value into a node-local derivative secret and rewrites the derivative
template to reference that node-local secret ID.

An existing template derivative keeps its derivative secrets for its lifetime.
Replacing a source secret under the same key does not update those copies. A new
environment that reuses the existing derivative can therefore receive the old
value in refreshed agent configuration or start actions. A derivative created
later copies the source value that exists at that later time, so one cluster can
temporarily use different values on different nodes.

Removing a cluster source secret removes the Postgres source record, but it does
not call the node APIs to remove node-local derivative secrets. It also does not
scrub values already persisted in template overlays, environment disks, or
archives. Do not treat source-secret replacement or removal as a derivative
refresh or revocation mechanism.

## Sensitive copies

Bastion stores secret values as application-readable values in SQLite on a
single host and in Postgres in the cluster control plane. It does not add
application-level encryption at rest. In a cluster, the control plane also
creates node-local derivative secrets when it prepares a template derivative,
so a value can exist in both Postgres and one or more node SQLite databases.

Substitution can create additional copies. Values can appear in guest files,
the immutable template overlay, environment overlays, process arguments, action
output, and exported template archives. Bastion does not promise to redact
arbitrary action output. A context staging file is removed after its action, but
the action can copy its contents elsewhere.

Treat the following as sensitive:

- Host and cluster databases and their backups
- Base archives because they contain the guest SSH private key
- Template archives and S3 objects because init or agent setup can bake secrets
  into the overlay
- Environment disks and logs
- API traffic and streamed action output

Access control must come from your network, reverse proxy, operating system,
database, and object-storage configuration. [Security and operational
limits](/explanation/security-and-operational-limits/) describes the trust model
that follows from this design.
