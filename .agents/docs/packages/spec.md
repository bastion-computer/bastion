This package contains Bastion's raw JSON Schema data type artifacts.

## Overview

The spec package lives in `spec/` and owns files that need to be consumed outside a single TypeScript runtime. This includes JSON Schema data types for Bastion template and custom action configuration.

The package is consumed by `@bastion/docs` for raw schema routes. Consumers should import the raw files directly from stable package paths.

## Files

| File | Purpose |
| ---- | ------- |
| `data-types/template.json` | JSON Schema data type for template configuration. |
| `data-types/action.json` | JSON Schema data type for custom action configuration. |

## Tasks

| Task | Command | Purpose |
| ---- | ------- | ------- |
| `format:check` | `prettier --check '**'` | Check formatting without writing files. |
| `format:write` | `prettier --write '**'` | Rewrite formatting locally. |

## Data Types

JSON Schema data type artifacts belong in `data-types/`. Do not place public JSON Schema files in `core`; core should only own runtime implementation logic and API/CLI types.

When adding a new public data type, add the JSON file under `data-types/`. The package export pattern exposes files as `@bastion/spec/data-types/*.json`.
