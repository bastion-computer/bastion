---
title: Secrets
description: A guide to referencing secret values on the bastion platform.
---

The secret system allows developers to map sensitive environment variables to a reference value. It works alongside the template and proxy systems so actual secrets remain obfuscated inside the sandbox boundary and only resolved during outbound calls at the host level.

Secret references are immutable after creation. To change the environment variable, or allowed hosts, remove the existing reference and bind a new one. This keeps changes explicit and avoids ambiguous updates to security-sensitive fields.

## Create a secret reference

```sh
bastion secrets bind $KEY:$ENV_VAR --allow-host
```

`$KEY` is the unique label used to reference this secret in templates, and `$ENV_VAR` is the host environment variable it maps to.

`--allow-host` is **required** at least once. It registers a hostname with wildcard support so that only outbound calls to this destination are allowed to resolve the secret. This flag can be used multiple times if there is more than one allowed host. Below are examples of hostname matching.

- `bastion.computer` matches only `bastion.computer`.
- `*.bastion.computer` matches `api.bastion.computer`.
- `*.bastion.computer` does not match `bastion.computer`.
- `*.bastion.computer` does not match `x.y.bastion.computer`.
- `*` matches any host.

Example:

```sh
bastion secrets bind API_KEY:BASTION_API_KEY \
    --allow-host "bastion.computer" \
    --allow-host "*.bastion.computer"
```

```json
{
  "id": "sec_xxxxxx",
  "key": "API_KEY",
  "env": "BASTION_API_KEY",
  "allowHosts": ["bastion.computer", "*.bastion.computer"],
  "createdAt": "<iso_timestamp>"
}
```

This secret can then be referenced in templates.

```
${{ secrets.API_KEY }}
```

## List all secret references

```sh
bastion secrets list [--limit] [--cursor]
```

```json
{
  "cursor": null,
  "entries": [
    {
      "id": "sec_xxxxxx",
      "key": "API_KEY",
      "env": "BASTION_API_KEY",
      "allowHosts": ["bastion.computer", "*.bastion.computer"],
      "createdAt": "<iso_timestamp>"
    },
    {
      "id": "sec_yyyyyy",
      "key": "PUBLIC_KEY",
      "env": "BASTION_PUBLIC_KEY",
      "allowHosts": [],
      "createdAt": "<iso_timestamp>"
    },
    {
      "id": "sec_zzzzzz",
      "key": "PRIVATE_KEY",
      "env": "BASTION_PRIVATE_KEY",
      "allowHosts": ["bastion.computer", "*.bastion.computer"],
      "createdAt": "<iso_timestamp>"
    }
  ]
}
```

`--limit` is an **optional** value that allows you to cap the number of returned entries. If more entries are available it will return a `cursor` timestamp. Defaults to 20.

`--cursor` is an **optional** timestamp for fetching entries created after this point in time. Defaults to `null`.

## Get single secret reference

```sh
bastion secrets get [--id] [--key]
```

```json
// bastion secrets get --id sec_xxxxxx
// or
// bastion secrets get --key API_KEY
{
  "id": "sec_xxxxxx",
  "key": "API_KEY",
  "env": "BASTION_API_KEY",
  "allowHosts": ["bastion.computer", "*.bastion.computer"],
  "createdAt": "<iso_timestamp>"
}
```

This command must specify either an `--id` or `--key` value.

## Resolve a single secret reference

```sh
bastion secrets resolve [--id] [--key]
```

```json
// bastion secrets resolve --id sec_xxxxxx
// or
// bastion secrets resolve --key API_KEY
{
  "value": "<secret_value>"
}
```

This command must specify either an `--id` or `--key` value.

`resolve` reads the mapped environment variable from the host and returns the actual secret value. It is intended for explicit debugging and inspection. Secret values are never returned by `list` or `get`.

## Remove a secret reference

```sh
bastion secrets remove [--id] [--key]
```

```json
// bastion secrets remove --id sec_xxxxxx
// or
// bastion secrets remove --key API_KEY
{
  "id": "sec_xxxxxx",
  "key": "API_KEY",
  "env": "BASTION_API_KEY",
  "allowHosts": ["bastion.computer", "*.bastion.computer"],
  "createdAt": "<iso_timestamp>"
}
```

This command must specify either an `--id` or `--key` value.

Removing a secret reference does not modify or unset the underlying host environment variable.
