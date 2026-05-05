---
title: Custom Actions
description: A guide to creating reusable bastion actions for template lifecycle steps.
---

Custom actions are reusable steps that can be referenced from a template's `init` or `start` procedures via the `use` directive. Use a custom action when a setup step should be packaged once and shared across templates instead of being repeated as an inline `run` command.

## Requirements

Every custom action must include a `bastion.json` file at the repository root. This file gives the consuming bastion instance the information it needs to run the step.

:::note
The full schema is available at <a href="/schemas/action.json" target="_blank" rel="noopener noreferrer">schemas/action.json</a>.
:::

Other than `bastion.json`, custom actions have no restrictions on programming languages or frameworks. An action can be a shell script, compiled binary, Node.js package, Python program, Go command, or any other executable entrypoint that can run inside the sandbox.

## Action config

The minimal action config defines a `runs.command` entrypoint.

```json
{
  "$schema": "https://bastion.computer/schemas/action.json",
  "runs": {
    "command": "./setup.sh"
  }
}
```

| Field         | Required | Description                                                           |
| ------------- | -------- | --------------------------------------------------------------------- |
| `name`        | No       | Human-readable action name.                                           |
| `description` | No       | Short explanation of what the action does.                            |
| `inputs`      | No       | Inputs accepted from the template action's `with` object.             |
| `runs`        | Yes      | Execution command used by bastion to run the step inside the sandbox. |

### Runs

`runs.command` is executed from the action root. Inputs passed from a template's `with` object are made available to this command as environment variables.

```json
{
  "runs": {
    "command": "./setup.sh"
  }
}
```

| Field     | Required | Description                                                 |
| --------- | -------- | ----------------------------------------------------------- |
| `command` | Yes      | Command to execute from the action root inside the sandbox. |

The command can call any runtime, script, or binary included by the action or already available in the sandbox.

### Inputs

Use `inputs` to declare the keys and values the action accepts from a template's `with` object. Each input must define a `type`.

```json
{
  "inputs": {
    "version": {
      "type": "string",
      "description": "Node.js version to install.",
      "required": true
    }
  }
}
```

Input keys must start with a letter and can only contain letters, numbers, and underscores.

| Field         | Required | Description                                                     |
| ------------- | -------- | --------------------------------------------------------------- |
| `type`        | Yes      | Input value type: `string`, `number`, or `boolean`.             |
| `description` | No       | Human-readable explanation for the input.                       |
| `required`    | No       | Whether templates must provide this input. Defaults to `false`. |

### Input environment variables

When bastion runs an action, each value from the template action's `with` object is exposed as an environment variable using the `BASTION_INPUT_` prefix. The input name is uppercased to produce the environment variable name.

| Input name     | Environment variable         |
| -------------- | ---------------------------- |
| `version`      | `BASTION_INPUT_VERSION`      |
| `node_version` | `BASTION_INPUT_NODE_VERSION` |

String, number, and boolean input values are stringified before they are added to the action environment.

## Example action

An action repository can use any project layout, but a small action might look like this:

```text
setup-node/
├── bastion.json
└── setup.sh
```

```json title="bastion.json"
{
  "$schema": "https://bastion.computer/schemas/action.json",
  "name": "setup-node",
  "description": "Install Node.js in a sandbox.",
  "inputs": {
    "version": {
      "type": "string",
      "description": "Node.js version to install.",
      "required": true
    }
  },
  "runs": {
    "command": "./setup.sh"
  }
}
```

```sh title="setup.sh"
#!/usr/bin/env sh
set -eu

node_version="$BASTION_INPUT_VERSION"

printf 'Installing Node.js %s\n' "$node_version"
```

## Use an action in a template

Templates reference custom actions with `use`. Values under `with` are passed to the action as declared inputs.

```json
{
  "actions": {
    "init": [
      {
        "use": "github.com/bastion-computer/setup-node",
        "with": {
          "version": "24"
        }
      }
    ]
  }
}
```
