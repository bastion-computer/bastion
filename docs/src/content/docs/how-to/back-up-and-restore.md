---
title: Back up and restore
description: Export and restore Bastion base and template artifacts in dependency order.
---

Use Bastion archives to preserve the shared base and prepared templates without
rerunning template init actions.

:::caution
Base and template archives are not a full disaster-recovery backup. They do not
include environment writable disks, environment records, the host SQLite
database, cluster Postgres, S3 bucket state, or independent secret records.
Imported templates receive new IDs, and keys must be assigned again.
:::

## Back up host artifacts

1. Choose a protected archive directory and restrict new files:

   ```sh
   ARCHIVE_DIR="$HOME/bastion-backup"
   umask 077
   mkdir -p "$ARCHIVE_DIR"
   ```

:::danger
Base archives contain the guest SSH private key. Template overlays can contain
credentials resolved during init. Encrypt archives outside Bastion and limit
access according to your secret-handling policy.
:::

2. Export the base first:

   ```sh
   bastion base export > "$ARCHIVE_DIR/base.tar.zst"
   test -s "$ARCHIVE_DIR/base.tar.zst"
   ```

   The base archive contains its manifest, prepared root disk, cloud-init seed,
   and guest SSH private key.

3. List templates and choose each key or ID to preserve:

   ```sh
   bastion templates list --limit 100
   : > "$ARCHIVE_DIR/templates.tsv"
   ```

4. Export each selected template to a safe, fixed filename and record the source
   key separately. For example:

   ```sh
   TEMPLATE_KEY="project"
   TEMPLATE_ARCHIVE="template-001.tar.zst"
   : "${TEMPLATE_KEY:?set TEMPLATE_KEY}"
   : "${TEMPLATE_ARCHIVE:?set TEMPLATE_ARCHIVE}"
   bastion templates export --key "$TEMPLATE_KEY" \
     > "$ARCHIVE_DIR/$TEMPLATE_ARCHIVE"
   test -s "$ARCHIVE_DIR/$TEMPLATE_ARCHIVE"
   printf '%s\t%s\n' "$TEMPLATE_ARCHIVE" "$TEMPLATE_KEY" \
     >> "$ARCHIVE_DIR/templates.tsv"
   ```

   `TEMPLATE_KEY` is the source template key, and `TEMPLATE_ARCHIVE` is a safe
   local filename that does not contain resource input. Use a new sequential
   filename such as `template-002.tar.zst` for each additional template. For an
   unkeyed template, set `TEMPLATE_ID` to its generated `tpl_` ID, export with
   `--id "$TEMPLATE_ID"`, and record the ID in the inventory instead.

5. Record checksums after every export:

   ```sh
   (cd "$ARCHIVE_DIR" && sha256sum ./*.tar.zst ./templates.tsv > SHA256SUMS)
   ```

   Running `sha256sum` inside the archive directory records relative paths, so
   the directory can be moved before restore.

6. Store a separate inventory of the keys you want to assign during restore and
   the external secret-manager entries each template needs. Bastion archive
   commands do not export independent secrets.

## Restore host artifacts

1. Install Bastion on the destination Linux x86_64 VM host, initialize system
   assets, and verify services:

   ```sh
   bastion system init --with-utilities
   bastion system check
   sudo systemctl is-active bastiond.service bastion-api.service
   ```

2. Verify archive integrity:

   ```sh
   ARCHIVE_DIR="$HOME/bastion-backup"
   (cd "$ARCHIVE_DIR" && sha256sum -c SHA256SUMS)
   ```

3. Import the base before any template:

   ```sh
   bastion base import --file "$ARCHIVE_DIR/base.tar.zst"
   bastion base get
   ```

   The restored content address must match the original base.

4. Recreate required secrets from your external secret manager. For example:

   :::caution
   The current CLI has no stdin or file input for secret values. It passes the
   value in its argument vector, where same-user or privileged processes,
   tracing, audit, or command-accounting systems can record it.
   :::

   ```bash
   read -rsp 'Service token: ' SECRET_VALUE
   printf '\n'
   bastion secrets create --key SERVICE_TOKEN --value "$SECRET_VALUE"
   unset SECRET_VALUE
   ```

5. Read `templates.tsv`, then import each safe archive filename and assign its
   intended key:

   ```sh
   TEMPLATE_KEY="project"
   TEMPLATE_ARCHIVE="template-001.tar.zst"
   bastion templates import \
     --key "$TEMPLATE_KEY" \
     --file "$ARCHIVE_DIR/$TEMPLATE_ARCHIVE"
   ```

   The response contains a new generated template ID. Import does not preserve
   the source ID or key.

6. Create a disposable environment and verify the restored layer:

   ```sh
   bastion env create --template-key "$TEMPLATE_KEY" --key restore-check
   bastion env get --key restore-check
   ```

7. After verification, remove the disposable environment:

   :::caution
   Environment removal deletes its writable disk.
   :::

   ```sh
   bastion env remove --key restore-check
   ```

## Replace an existing base during restore

:::danger
Importing with `--force` replaces the singleton base and can make every existing
template unusable. Remove dependent environments and templates or retain the
matching old base before continuing. In a cluster, this affects all namespaces.
:::

After completing that cleanup, run:

```sh
bastion base import --force --file "$ARCHIVE_DIR/base.tar.zst"
```

## Back up cluster state

You can export the global base and namespace templates through the cluster API.

1. Initialize and validate a protected archive directory and every cluster
   selection variable:

   ```sh
   umask 077
   ARCHIVE_DIR="$HOME/bastion-cluster-backup"
   CLUSTER_API_URL="https://cluster.example.com"
   NAMESPACE_KEY="team-a"
   TEMPLATE_KEY="project"
   CLUSTER_TEMPLATE_ARCHIVE="cluster-template-001.tar.zst"
   : "${ARCHIVE_DIR:?set ARCHIVE_DIR}"
   : "${CLUSTER_API_URL:?set CLUSTER_API_URL}"
   : "${NAMESPACE_KEY:?set NAMESPACE_KEY}"
   : "${TEMPLATE_KEY:?set TEMPLATE_KEY}"
   : "${CLUSTER_TEMPLATE_ARCHIVE:?set CLUSTER_TEMPLATE_ARCHIVE}"
   mkdir -p "$ARCHIVE_DIR"
   ```

   `CLUSTER_API_URL` is the private cluster API URL. `NAMESPACE_KEY` and
   `TEMPLATE_KEY` select the source resource. `CLUSTER_TEMPLATE_ARCHIVE` is a
   safe local filename independent of those resource keys.

2. Export the global base and selected template, then record the template
   inventory:

   ```sh
   bastion --api-url "$CLUSTER_API_URL" base export \
     > "$ARCHIVE_DIR/cluster-base.tar.zst"
   bastion --api-url "$CLUSTER_API_URL" \
     --namespace-key "$NAMESPACE_KEY" \
     templates export --key "$TEMPLATE_KEY" \
     > "$ARCHIVE_DIR/$CLUSTER_TEMPLATE_ARCHIVE"
   test -s "$ARCHIVE_DIR/cluster-base.tar.zst"
   test -s "$ARCHIVE_DIR/$CLUSTER_TEMPLATE_ARCHIVE"
   printf '%s\t%s\t%s\n' \
     "$CLUSTER_TEMPLATE_ARCHIVE" "$NAMESPACE_KEY" "$TEMPLATE_KEY" \
     > "$ARCHIVE_DIR/cluster-templates.tsv"
   ```

   Repeat the template export with sequential safe filenames and append each
   mapping to `cluster-templates.tsv`.

3. Generate and immediately verify relocatable cluster checksums:

   ```sh
   (
     cd "$ARCHIVE_DIR"
     sha256sum ./*.tar.zst ./cluster-templates.tsv > CLUSTER_SHA256SUMS
     sha256sum -c CLUSTER_SHA256SUMS
   )
   ```

For actual cluster disaster recovery, also use your database and object-storage
tools to back up Postgres and the configured S3-compatible bucket, including
`base/` and `templates/` objects. Preserve cluster configuration and credentials.
Bastion does not provide a coordinated cluster backup, point-in-time restore, or
environment failover workflow.

See [Manage the base](/how-to/manage-base/),
[Create and manage templates](/how-to/create-manage-templates/), and the
[host CLI reference](/reference/cli/host/) for archive command details. See the
[resource lifecycle export boundaries](/explanation/resource-lifecycle/#export-boundaries)
for what these artifacts omit.
