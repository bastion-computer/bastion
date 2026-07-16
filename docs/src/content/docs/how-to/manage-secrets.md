---
title: Manage secrets
description: Store, reference, inspect, rotate, and remove Bastion secrets safely.
---

Bastion secrets let template strings refer to values without embedding those
values in template JSON. Use a keyed secret when the same template definition
must work across environments.

:::caution
Bastion stores secret values as plaintext in the host SQLite database or cluster
Postgres database. The APIs have no native authentication or TLS, and
`bastion secrets get` returns the value. Restrict database and API access, add
a transparently authenticated private network such as Tailscale, and use
narrowly scoped credentials. The CLI cannot add arbitrary authentication headers
or present a configured client certificate.
:::

## Create a secret

The CLI currently accepts the value only through `--value`; it has no stdin or
file alternative. To avoid writing it into shell history, read it into a
temporary Bash variable.

1. Read the value without echoing it:

   ```bash
   read -rsp 'Secret value: ' SECRET_VALUE
   printf '\n'
   ```

2. Create a keyed secret and immediately clear the variable:

   ```bash
   bastion secrets create --key SERVICE_TOKEN --value "$SECRET_VALUE"
   unset SECRET_VALUE
   ```

   `SECRET_VALUE` is the temporary shell variable. `SERVICE_TOKEN` is the
   stable secret key used by templates. The CLI process must still receive the
   secret in its argument vector. Same-user or privileged processes can
   potentially read it through process inspection while the command runs, and
   tracing, audit, or command-accounting systems can record it.

3. Confirm that the create response contains only `id`, `key`, and `createdAt`.
   It must not contain `value`.

Secret keys are optional and unique. A key cannot start with `sec_`, which is
reserved for generated secret IDs.

## Reference a secret

1. Add the keyed reference to a template string:

   ```json
   {
     "agents": {
       "opencode": {
         "auth": {
           "service": {
             "type": "api",
             "key": "${{ secret.SERVICE_TOKEN }}"
           }
         }
       }
     },
     "actions": {
       "init": []
     }
   }
   ```

2. Create the template only after the referenced secret exists:

   ```sh
   bastion templates create --key project --file ./template.json
   ```

Bastion preserves the reference in stored template JSON. It resolves references
when it prepares a template and again when it creates an environment. Values
used by agent setup or `actions.init` can be persisted in the prepared template
disk. Values used by agent refresh or `actions.start` are resolved again for the
new environment. Treat template disks and exports as sensitive.

You can also reference a generated ID, for example
`${{ secret.SECRET_ID }}`, where `SECRET_ID` is a value beginning with `sec_`.
Keyed references are easier to reproduce across hosts.

## List and inspect secrets

1. List metadata without values:

   ```sh
   bastion secrets list --limit 100
   ```

2. Only when you need the plaintext value, get one secret:

   :::caution
   The next command writes the secret value to stdout. Do not run it in a
   recorded terminal, CI log, or shared shell.
   :::

   ```sh
   bastion secrets get --key SERVICE_TOKEN
   ```

3. Prefer `list` rather than `get` for inventory and monitoring.

## Rotate a secret

Bastion has no secret update command. Recreate the same key to rotate a value.

1. Ensure no template or environment creation is running. A reference fails to
   resolve while the key is absent.

2. Remove the old value:

   ```sh
   bastion secrets remove --key SERVICE_TOKEN
   ```

3. Read and create the replacement value:

   ```bash
   read -rsp 'New secret value: ' SECRET_VALUE
   printf '\n'
   bastion secrets create --key SERVICE_TOKEN --value "$SECRET_VALUE"
   unset SECRET_VALUE
   ```

4. Create a new environment and verify the credential there. Existing
   environments do not change. Data persisted by an old `actions.init` step also
   remains unchanged because init does not rerun for an existing template.

For rotation without a gap, create a versioned key, create a replacement
immutable template that references it, verify replacement environments, and
then remove the old environments, template, and secret in that order.

## Remove a secret

:::caution
Secret removal does not inspect template references and does not scrub values
already written into prepared templates or environments. Future template or
environment creation fails if it still needs the removed reference.
:::

1. Remove dependent environments and templates, or update your replacement
   template to use another secret.

2. Remove the secret by key:

   ```sh
   bastion secrets remove --key SERVICE_TOKEN
   ```

   For an unkeyed secret, set `SECRET_ID="SECRET_ID"` to its generated `sec_`
   ID, then run `bastion secrets remove --id "$SECRET_ID"`.

## Manage a cluster secret

Select exactly one cluster namespace before using the same commands:

```sh
CLUSTER_API_URL="https://cluster.example.com"
TEAM_KEY="TEAM_KEY"
bastion --api-url "$CLUSTER_API_URL" \
  --namespace-key "$TEAM_KEY" \
  secrets list
```

Replace `TEAM_KEY` with the namespace key. Cluster secrets are source resources
in Postgres. Bastion creates node-local derivative secrets when needed. Namespace
deletion is not a recursive cleanup operation; remove environments, templates,
and secrets through their resource commands before removing the namespace.

See the [host CLI reference](/reference/cli/host/),
[cluster CLI reference](/reference/cli/cluster/),
[host API reference](/reference/api/host/), and
[template configuration reference](/reference/template-configuration/) for
exact fields and response shapes. [Actions and
secrets](/explanation/actions-and-secrets/) explains resolution timing and
sensitive copies.
