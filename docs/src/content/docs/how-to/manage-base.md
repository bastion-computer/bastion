---
title: Manage the base
description: Build, inspect, export, import, and safely replace the shared Bastion base.
---

Bastion keeps one template-independent base per host or cluster. Every template
records the base's content address and requires that exact base when it launches
an environment.

## Prerequisites

Before you manage a host base, ensure that:

- The Linux x86_64 host has KVM and vhost-vsock support.
- The host API and daemon are running against the same data directory and Unix
  socket.
- `bastion system check` reports `[ok]` for every dependency.

If dependencies are missing, run:

```sh
bastion system init --with-utilities
bastion system check
```

## Build a base

1. Confirm that the API and daemon are active:

   ```sh
   sudo systemctl is-active bastiond.service bastion-api.service
   ```

2. Build the base:

   ```sh
   bastion base build
   ```

   Progress is written to stderr. The final JSON contains a `contentAddress`
   beginning with `sha256:` and creation timestamps.

3. Read the stored metadata:

   ```sh
   bastion base get
   ```

   A second build returns a conflict. Do not use `--force` simply to make the
   command repeatable.

## Export a base

:::caution
A base archive contains the guest SSH private key. Store it as sensitive backup
material, restrict file permissions, and encrypt it outside Bastion when your
policy requires encryption at rest.
:::

1. Restrict newly created files and export the archive:

   ```sh
   umask 077
   bastion base export > base.tar.zst
   ```

2. Verify that the archive is nonempty and record a checksum:

   ```sh
   test -s base.tar.zst
   sha256sum base.tar.zst > base.tar.zst.sha256
   ```

## Import a base

1. Initialize system assets and start services on the destination host before
   importing the base.

2. Verify the archive checksum:

   ```sh
   sha256sum -c base.tar.zst.sha256
   ```

3. Import the archive:

   ```sh
   bastion base import --file ./base.tar.zst
   ```

4. Confirm that its content address matches the source:

   ```sh
   bastion base get
   ```

5. Import prepared templates only after the matching base is present. Follow
   [Back up and restore Bastion artifacts](/how-to/back-up-and-restore/) for the
   full ordering.

## Replace a base

:::danger
`bastion base build --force` and `bastion base import --force` replace the only
base. Templates prepared from a different content address become unusable. In a
cluster, the base is global, so replacement affects every namespace and node.
:::

1. Before exporting templates or removing resources, export and checksum the old
   base to a protected directory:

   ```sh
   umask 077
   BACKUP_DIR="$HOME/bastion-base-replacement"
   mkdir -p "$BACKUP_DIR"
   bastion base export > "$BACKUP_DIR/old-base.tar.zst"
   test -s "$BACKUP_DIR/old-base.tar.zst"
   (cd "$BACKUP_DIR" && sha256sum ./old-base.tar.zst > SHA256SUMS)
   : > "$BACKUP_DIR/templates.tsv"
   ```

   Keep this archive while any old template backup might need it. A template
   archive is not usable without its exact base content address.

2. Export each old template to a safe filename and record its key separately:

   ```sh
   TEMPLATE_KEY="TEMPLATE_KEY"
   TEMPLATE_ARCHIVE="old-template-001.tar.zst"
   bastion templates export --key "$TEMPLATE_KEY" \
     > "$BACKUP_DIR/$TEMPLATE_ARCHIVE"
   test -s "$BACKUP_DIR/$TEMPLATE_ARCHIVE"
   printf '%s\t%s\n' "$TEMPLATE_ARCHIVE" "$TEMPLATE_KEY" \
     >> "$BACKUP_DIR/templates.tsv"
   (cd "$BACKUP_DIR" && sha256sum ./*.tar.zst ./templates.tsv > SHA256SUMS)
   ```

   Replace `TEMPLATE_KEY` with the template key. Use sequential filenames for
   additional archives; do not use arbitrary resource keys as path names.

3. List environments and remove every environment that depends on templates you
   will discard:

   ```sh
   bastion env list --limit 100
   ENVIRONMENT_KEY="ENVIRONMENT_KEY"
   bastion env remove --key "$ENVIRONMENT_KEY"
   ```

   Replace `ENVIRONMENT_KEY` with each environment key. Use a quoted
   `ENVIRONMENT_ID` variable with `--id` for unkeyed environments.

4. Remove each old template only after its environments are gone:

   ```sh
   bastion templates remove --key "$TEMPLATE_KEY"
   ```

5. Verify the archived base and templates before force replacement:

   ```sh
   (cd "$BACKUP_DIR" && sha256sum -c SHA256SUMS)
   ```

6. Replace the base by building it again:

   ```sh
   bastion base build --force
   ```

   Or replace it with an archive:

   ```sh
   bastion base import --force --file ./base.tar.zst
   ```

7. Recreate templates against the replacement base and verify a new environment
   before deleting old backup archives.

## Manage a cluster base

The cluster base is global and does not use a namespace selector.

1. Set `CLUSTER_API_URL` to the URL reachable through your transparently
   authenticated private network:

   ```sh
   CLUSTER_API_URL="https://cluster.example.com"
   ```

2. After registering at least one prepared node, build the base:

   ```sh
   bastion --api-url "$CLUSTER_API_URL" base build
   ```

   The control plane builds on one node, stores `base/base.tar.zst` in the
   configured S3-compatible bucket, and imports it into the other registered
   nodes.

3. Confirm cluster metadata:

   ```sh
   bastion --api-url "$CLUSTER_API_URL" base get
   ```

The APIs do not implement native authentication or TLS. Prefer a restricted
private network such as Tailscale. The CLI cannot add arbitrary authentication
headers or present a configured client certificate. If a transparent HTTP proxy
terminates TLS, it must pass upgraded connections and long-lived streams. See
[Deploy and operate a cluster](/how-to/deploy-and-operate-cluster/) and the
[host CLI reference](/reference/cli/host/) and
[cluster CLI reference](/reference/cli/cluster/) for command options. See
[Resource lifecycle](/explanation/resource-lifecycle/) for the base dependency
model and export boundaries.
