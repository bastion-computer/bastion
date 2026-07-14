---
title: Base
description: Build and manage the shared base image used by Bastion templates.
---

The base is the singleton, template-agnostic root disk that every Bastion
template uses as its backing image. Building it once keeps template creation and
template archives small because common guest setup does not need to be repeated
for every template.

Bastion's disk layers are:

```text
system rootfs --copied and prepared--> base rootfs

base rootfs
└── immutable template overlay
    └── writable environment overlay
```

`bastion system init` installs the source runtime assets, including the Ubuntu
rootfs and pinned OpenCode asset. `bastion base build` boots that rootfs,
installs the guest proxy, OpenCode, and other template-agnostic dependencies, and
stores the prepared base under `<data-dir>/base`.

Templates and environments boot normally with fresh cloud-init media. Base,
template, and environment artifacts do not contain VM memory snapshots.

## Build the Base

Initialize the system assets and start the host API and daemon before building
the base. The installer starts both services by default.

```sh
bastion system init --with-utilities
bastion system check
bastion base build
```

Build logs are written to stderr. The final metadata is written to stdout:

```json
{
  "contentAddress": "sha256:...",
  "createdAt": "<iso_timestamp>",
  "updatedAt": "<iso_timestamp>"
}
```

The content address identifies the exact base artifacts. Each template records
this value as `baseContentAddress`, and Bastion verifies it before importing a
template or launching an environment.

Only one base can exist in a data directory. A second build returns a conflict
unless replacement is explicitly requested:

```sh
bastion base build --force
```

Replacing a base with different content makes templates created from the old
base unusable. Restore the matching base or recreate those templates before
launching more environments. Remove dependent environments and templates before
replacing a base that is no longer needed.

## Inspect the Base

Return the current base metadata:

```sh
bastion base get
```

The command returns `not found` until a base has been built or imported. There
is no separate base remove command.

## Export and Import

Export the base to a zstd-compressed tar archive:

```sh
bastion base export > base.tar.zst
```

The archive contains a manifest, prepared root disk, cloud-init seed, and guest
SSH private key. Treat it as sensitive backup material.

Import a base into a host that already has its system assets and services set
up:

```sh
bastion base import --file ./base.tar.zst
```

Use `--force` only when intentionally replacing an existing base:

```sh
bastion base import --force --file ./base.tar.zst
```

Template archives contain only the template manifest and its root disk overlay,
so the matching base must be available first. Back up and restore resources in
this order:

```sh
# Source host
bastion base export > base.tar.zst
bastion templates export --key dev > dev-template.tar.zst

# Destination host
bastion base import --file ./base.tar.zst
bastion templates import --key dev --file ./dev-template.tar.zst
```

## Cluster Base

The cluster base is global and is not scoped to a namespace. After registering
at least one node, build it through the cluster API:

```sh
bastion --api-url http://cluster.internal:3150 base build
```

The control plane builds the base on one node, stores its archive in the
configured S3-compatible bucket, and imports it into the other registered nodes.
When a base already exists, registering another node synchronizes that base
before the node is added to the cluster.

You can also establish the cluster base from an archive:

```sh
bastion --api-url http://cluster.internal:3150 \
  base import --file ./base.tar.zst
```

Cluster `base build` and `base import` stream control-plane and node progress to
stderr. Because the base is shared by every namespace, `--force` replacement
affects templates across the entire cluster.
