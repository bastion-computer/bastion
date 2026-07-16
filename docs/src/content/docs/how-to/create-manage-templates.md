---
title: Create and manage templates
description: Create, inspect, replace, export, import, and remove immutable Bastion templates.
---

A Bastion template is immutable JSON plus a prepared qcow2 disk overlay. Create
one after the matching base exists, then use it to launch disposable
environments.

## Prerequisites

Before you create a template:

- Run `bastion system check` successfully on the VM host.
- Build or import the shared base.
- Create every Bastion secret referenced by the template.
- Ensure the host has enough CPU, memory, and disk to boot a temporary template
  VM.

The [template configuration reference](/reference/template-configuration/) and
public [template schema](/schemas/template.json) are the sources of truth for
fields and validation.

## Create a template file

1. Create `template.json`:

   ```sh
   cat > template.json <<'JSON'
   {
     "resources": {
       "vcpu": 2,
       "memory": 2,
       "volume": 20
     },
     "tunnels": {
       "web": 3000
     },
     "agents": {
       "opencode": {
         "working_directory": "/workspace/project"
       }
     },
     "actions": {
       "init": [
         {
           "use": "setup_bun",
           "with": {
             "version": "bun-v1.3.13"
           }
         },
         {
           "run": "mkdir -p /workspace/project && printf 'console.log(\"ready\")\\n' > index.ts",
           "working_directory": "/workspace/project"
         }
       ],
       "start": [
         {
           "run": "printf 'started\\n' > /workspace/project/start-status"
         }
       ]
     }
   }
   JSON
   ```

   Resource units are vCPUs, GiB of memory, and GiB of root volume. `init`
   actions run once while Bastion prepares the template. `start` actions run for
   every new environment.

2. Keep credentials out of the file. Use
   [Bastion secret references](/how-to/manage-secrets/) for sensitive values.

## Create and verify the template

1. Create a keyed template from the file:

   ```sh
   bastion templates create --key project-v1 --file ./template.json
   ```

   Use exactly one of `--file` or `--config`. Setup logs stream to stderr, and
   final metadata is written as JSON to stdout.

2. Confirm that the response has a generated `id`, the key `project-v1`, and a
   `baseContentAddress` beginning with `sha256:`.

3. Inspect the full stored configuration:

   ```sh
   bastion templates get --key project-v1
   ```

4. Launch a test environment and verify the initialized file:

   ```sh
   bastion env create --template-key project-v1 --key project-v1-check
   bastion ssh --key project-v1-check -- cat /workspace/project/index.ts
   ```

   Expected output:

   ```text
   console.log("ready")
   ```

## List templates

1. List one page of template metadata:

   ```sh
   bastion templates list --limit 100
   ```

2. If `cursor` is not `null`, request the next page:

   ```sh
   CURSOR="CURSOR"
   bastion templates list --limit 100 --cursor "$CURSOR"
   ```

   Replace `CURSOR` with the cursor from the previous response.

## Replace an immutable template

Templates cannot be edited. Create and verify a replacement before removing the
old version.

1. Modify `template.json` with the new configuration.

2. Create a replacement under a new key:

   ```sh
   bastion templates create --key project-v2 --file ./template.json
   ```

3. Create and verify replacement environments:

   ```sh
   bastion env create --template-key project-v2 --key project-v2-check
   bastion ssh --key project-v2-check -- test -f /workspace/project/index.ts
   ```

4. Preserve work from old environments, then remove those environments before
   removing `project-v1`:

   :::danger
   Environment removal permanently deletes each writable disk.
   :::

   ```sh
   bastion env remove --key project-v1-check
   bastion templates remove --key project-v1
   ```

5. Keep or remove `project-v2-check` according to your workflow.

## Export and import a template

:::caution
An exported template can contain secrets or credentials persisted by init
actions. Protect the archive as sensitive data.
:::

1. Export a prepared template:

   ```sh
   umask 077
   bastion templates export --key project-v2 > project-v2.tar.zst
   test -s project-v2.tar.zst
   ```

2. On the destination, import the exact matching base before the template.

3. Import the template with a new key:

   ```sh
   bastion templates import --key project-restored --file ./project-v2.tar.zst
   ```

   Import creates a new template ID and never preserves the exported key. The
   archive contains the config and prepared overlay, but no environment writable
   disks, cloud-init state, or VM memory.

See [Back up and restore Bastion artifacts](/how-to/back-up-and-restore/) for a
complete ordered procedure.

## Remove a template

:::caution
Removing a template permanently deletes its immutable overlay and metadata.
Export it first if you might need to restore it.
:::

1. Remove every dependent environment first:

   ```sh
   bastion env list --limit 100
   ENVIRONMENT_KEY="ENVIRONMENT_KEY"
   bastion env remove --key "$ENVIRONMENT_KEY"
   ```

   Replace `ENVIRONMENT_KEY` with each dependent environment key. Use an ID for
   unkeyed resources.

2. Remove the template:

   ```sh
   bastion templates remove --key project-v2
   ```

   Bastion rejects removal while an environment record still references the
   template.

For all template and action fields, see the
[template configuration reference](/reference/template-configuration/). For
command flags and output shapes, see the
[host CLI reference](/reference/cli/host/) and
[host API reference](/reference/api/host/). See
[Resource lifecycle](/explanation/resource-lifecycle/) for the immutable disk
and dependency model.
