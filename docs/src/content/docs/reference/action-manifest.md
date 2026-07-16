---
title: Action manifest
description: Package layout, exact manifest schema, inputs, context, execution behavior, and cluster requirements for custom Bastion actions.
---

An action package is a directory under `<DATA_DIR>/actions` containing a
`manifest.json` file and the files needed by its command. A template invokes the
package with a `use` action.

`DATA_DIR` represents the Bastion data directory used by the daemon, such as
`~/.bastion`.

## Package layout

```text
<DATA_DIR>/actions/setup_python/
|-- manifest.json
`-- install.sh
```

The directory name is the action name. It must start with an ASCII letter and
can contain ASCII letters, numbers, underscores, and hyphens. The equivalent
pattern is `^[A-Za-z][A-Za-z0-9_-]*$`.

All package entries copied to a guest must be regular files or directories.
Symbolic links and other special file types make action staging fail.

## Complete manifest schema

```json
{
  "inputs": {
    "version": {
      "type": "string",
      "description": "Python version to install.",
      "required": true
    }
  },
  "run": "sh ./install.sh"
}
```

The manifest accepts only these top-level fields:

| Field    | Type            | Required | Description                                                          |
| -------- | --------------- | -------- | -------------------------------------------------------------------- |
| `run`    | nonblank string | Yes      | Command executed from the copied package directory inside the guest. |
| `inputs` | object          | No       | Input names mapped to input definitions.                             |

Unknown top-level fields are invalid. In particular, the manifest has no
`defaults`, `environment`, `outputs`, or lifecycle field. An action implements
optional input defaults in its `run` command or scripts.

Each input definition accepts only:

| Field         | Type        | Required | Default | Description                                                |
| ------------- | ----------- | -------- | ------- | ---------------------------------------------------------- |
| `type`        | string enum | Yes      | None    | `string`, `number`, or `boolean`.                          |
| `description` | string      | No       | Empty   | Human-readable input documentation.                        |
| `required`    | boolean     | No       | `false` | Whether the invoking `with` object must contain the input. |

Input names must match `^[A-Za-z][A-Za-z0-9_]*$`. Unknown fields in an input
definition are invalid.

## Template invocation schema

A package is invoked with this template action shape:

```json
{
  "use": "setup_python",
  "with": {
    "version": "3.13"
  },
  "context": {
    "project": "example"
  }
}
```

| Field     | Type                    | Required | Description                                              |
| --------- | ----------------------- | -------- | -------------------------------------------------------- |
| `use`     | action name             | Yes      | Package directory name under `<DATA_DIR>/actions`.       |
| `with`    | object of scalar values | No       | Values validated against `manifest.json`.                |
| `context` | any JSON value          | No       | Structured data that bypasses manifest input validation. |

Values under `with` can only be JSON strings, numbers, or booleans. Bastion
rejects an undefined input, a missing required input, or a value whose JSON type
does not match the manifest.

Manifest input validation occurs when the action executes. For an `init` action,
that is template creation. For a `start` action, that is environment creation.

## Input environment variables

Bastion exposes each supplied `with` value as an environment variable. It adds
the `BASTION_INPUT_` prefix and uppercases the input name.

| Input name       | Environment variable           |
| ---------------- | ------------------------------ |
| `version`        | `BASTION_INPUT_VERSION`        |
| `python_version` | `BASTION_INPUT_PYTHON_VERSION` |
| `create_avd`     | `BASTION_INPUT_CREATE_AVD`     |

Strings retain their value. Numbers use their decoded JSON number text, and
booleans become `true` or `false`. Bastion sorts inputs by name and shell-quotes
the values in a temporary environment file before sourcing it.

Distinct input names can collide after uppercasing. For example, `token` and
`TOKEN` both become `BASTION_INPUT_TOKEN`. The manifest validator does not reject
this collision; the later assignment in sorted input-name order wins when the
file is sourced. Define input names that remain unique after uppercasing.

An omitted optional input does not create an environment variable. A package
script supplies any desired default, for example:

```sh
#!/usr/bin/env sh
set -eu

version="${BASTION_INPUT_VERSION:-latest}"
printf 'Installing %s\n' "$version"
```

## Context JSON

When an invocation contains `context`, Bastion serializes the resolved JSON data
to a temporary `.bastion-context.json` file in the guest package directory and
sets:

```text
BASTION_CONTEXT_FILE=/opt/bastion/actions/init-1-setup_python/.bastion-context.json
```

The path is representative; the lifecycle phase, action position, and package
name determine the actual directory. `BASTION_CONTEXT_FILE` is absent when the
invocation omits `context`.

Serialization preserves the JSON data model, but not the source's whitespace,
property order, or original number spelling. Secret substitution also decodes
and re-encodes the full template configuration before action execution.

The manifest does not validate context. The package is responsible for checking
its JSON type and fields. For example:

```sh
jq -e 'type == "object" and (.project | type == "string")' \
  "$BASTION_CONTEXT_FILE" >/dev/null
```

Secret expressions inside context strings resolve before the package runs. See
[secret references in template configuration](/reference/template-configuration/#secret-references).

## Execution behavior

Bastion executes an action package as follows:

1. Load and strictly decode `<DATA_DIR>/actions/<ACTION_NAME>/manifest.json`.
2. Validate the action name, manifest, and `with` values.
3. Write input and optional context files in the host staging copy, requesting
   mode `0600` for newly created files. Same-name files already supplied by the
   package retain their existing mode.
4. Copy the package to `/opt/bastion/actions/<PHASE>-<POSITION>-<ACTION_NAME>` in the guest.
5. Remove the sensitive files from the host staging copy immediately after the
   copy attempt.
6. In the guest, source and remove the input file before running the manifest
   command as `root`.
7. Keep the context file available during the command and install an `EXIT` trap
   that removes both sensitive guest files when the wrapper shell exits.

`ACTION_NAME`, `PHASE`, and `POSITION` are placeholders. `PHASE` is `init` or
`start`, and `POSITION` is the one-based action position.

The command runs under the guest shell with `set -eu` active in Bastion's
wrapper. A nonzero command exit fails the containing template or environment
operation. Command output becomes `log` events in the operation's NDJSON stream.
See [environment states and streams](/reference/environment-states-and-streams/#ndjson-operation-streams).

The wrapper attempts the cleanup described above, but the copied package
directory remains on the guest disk. An interrupted process or package command
that bypasses shell-exit handling can leave temporary data behind. Package
scripts can also persist their inputs elsewhere.

> **Warning:** Do not assume action inputs or context remain ephemeral. They can
> appear in process arguments, logs, package-created files, template overlays,
> environment disks, and exported archives. See
> [Security and operational limits](/explanation/security-and-operational-limits/).

## Built-in seeding and name ownership

`bastion start daemon` seeds the built-in packages into
`<DATA_DIR>/actions`. On every daemon start, Bastion removes and replaces each
directory whose name matches a built-in action.

Use a unique name for a custom package. A custom package that uses a built-in
name is overwritten. The complete reserved set is listed in the
[built-in actions reference](/reference/built-in-actions/).

## Cluster availability

Action packages are node-local; the cluster control plane does not distribute
them.

Built-in packages are available after each node's daemon seeds them. For custom
packages:

| Lifecycle phase | Cluster requirement                                                                                                                   |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `actions.init`  | The package must exist on the node selected to prepare the source template. Its resulting files are captured in the template archive. |
| `actions.start` | The package must exist with compatible contents on every node that can be selected for an environment.                                |

If a scheduled node lacks a package, environment creation fails when Bastion
tries to read that action's manifest.
