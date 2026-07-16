---
title: Create custom actions
description: Package reusable guest setup as a custom Bastion action.
---

Create a custom action when multiple templates need the same setup logic. An
action package is a directory under `BASTION_DATA_DIR/actions` with a strict
`manifest.json` and the files its command uses.

## Prerequisites

Before you begin:

- Start the Bastion host API and daemon at least once so the data and actions
  directories exist.
- Identify the data directory used by both services. The installer records it in
  `/etc/default/bastion`; the default is the installer's user's `~/.bastion`.
- Write the package as the service user, or ensure that user can read every file.

Custom action commands run as `root` inside the guest. Review scripts with the
same care as other privileged provisioning code.

## Create an action package

1. Set the data and action directories. Adjust `BASTION_DATA_DIR` if your
   services use another path:

   ```sh
   BASTION_DATA_DIR="${BASTION_DATA_DIR:-$HOME/.bastion}"
   ACTION_DIR="$BASTION_DATA_DIR/actions/write_marker"
   mkdir -p "$ACTION_DIR"
   ```

   `write_marker` starts with a letter and uses only supported action-name
   characters.

2. Create the manifest:

   ```sh
   cat > "$ACTION_DIR/manifest.json" <<'JSON'
   {
     "inputs": {
       "message": {
         "type": "string",
         "description": "Text to write into the guest marker file.",
         "required": true
       }
     },
     "run": "sh ./run.sh"
   }
   JSON
   ```

   Manifests accept only `run` and optional `inputs`. Input types are `string`,
   `number`, or `boolean`. Unknown fields, unknown inputs, missing required
   inputs, and type mismatches fail the lifecycle action.

3. Create the action script:

   ```sh
   cat > "$ACTION_DIR/run.sh" <<'SH'
   #!/usr/bin/env sh
   set -eu

   install -d -m 0755 /opt/bastion-custom
   printf '%s\n' "$BASTION_INPUT_MESSAGE" > /opt/bastion-custom/marker.txt
   SH
   chmod 0750 "$ACTION_DIR/run.sh"
   ```

   Bastion exposes the `message` input as `BASTION_INPUT_MESSAGE`. It uppercases
   input names and adds the `BASTION_INPUT_` prefix.

4. Do not name a custom package after a built-in action. The daemon replaces
   built-in action directories whenever it starts.

## Use the action in a template

1. Create `custom-action-template.json`:

   ```sh
   cat > custom-action-template.json <<'JSON'
   {
     "agents": {
       "opencode": {}
     },
     "actions": {
       "init": [
         {
           "use": "write_marker",
           "with": {
             "message": "custom action ready"
           }
         }
       ]
     }
   }
   JSON
   ```

2. Create a template after a base exists:

   ```sh
   bastion templates create --key custom-action-check --file ./custom-action-template.json
   ```

   Bastion validates the manifest, stages the package, copies it into the guest,
   and runs its manifest command. You do not need to restart the daemon after
   adding a uniquely named custom package.

3. Create an environment and verify the result:

   ```sh
   bastion env create --template-key custom-action-check --key custom-action-check-1
   bastion ssh --key custom-action-check-1 -- cat /opt/bastion-custom/marker.txt
   ```

   Expected output:

   ```text
   custom action ready
   ```

## Pass structured context

When scalar inputs are not enough, add arbitrary JSON as `context` in the
template action:

```json
{
  "use": "write_marker",
  "with": {
    "message": "custom action ready"
  },
  "context": {
    "project": "example",
    "features": ["api", "web"]
  }
}
```

When context is present, Bastion sets `BASTION_CONTEXT_FILE` to a temporary JSON
file in the guest action directory. Read that file from the script. Bastion
removes the staged input and context files after the action exits, but the action
can persist anything it copies elsewhere.

:::caution
Secret references work in `with` and `context`, but the resolved values cross
into the guest and can be persisted by your script. Never print secrets to
action logs. Treat prepared template archives as sensitive when init actions use
secrets.
:::

## Update or remove an action

Actions have no registry record or version field. Templates read the package
from disk when the relevant lifecycle phase runs.

1. To update init behavior safely, create a new action directory name or update
   the package and create a replacement immutable template.

2. If an action appears in `actions.start`, keep the package installed on every
   host that can create an environment from that template.

3. In a cluster, install an identical package in the configured data directory
   on every VM host. Template preparation and environment scheduling can use
   different nodes.

4. Remove test resources in dependency order:

   :::caution
   Removing the test environment deletes its writable disk. The remaining
   commands delete the prepared template, custom package, and local template
   file.
   :::

   ```sh
   bastion env remove --key custom-action-check-1
   bastion templates remove --key custom-action-check
   rm -rf -- "$ACTION_DIR"
   rm ./custom-action-template.json
   ```

For package and input rules, see the
[action manifest reference](/reference/action-manifest/). For supported package
names, see the [built-in actions reference](/reference/built-in-actions/). See
[Actions and secrets](/explanation/actions-and-secrets/) for lifecycle timing
and [Create and manage templates](/how-to/create-manage-templates/) for
immutable template replacement.
