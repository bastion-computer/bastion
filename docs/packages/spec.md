This package contains Bastion's raw JSON Schema data type artifacts.

## Overview

The spec package lives in `packages/spec/` and owns files that need to be consumed outside a single TypeScript runtime. This includes JSON Schema data types for Bastion template and custom action configuration.

The package is consumed by `@bastion/docs` for raw schema routes. Consumers should import the raw files directly from stable package paths.

## Files

| File | Purpose |
| ---- | ------- |
| `data-types/template.json` | JSON Schema data type for template configuration. |
| `data-types/action.json` | JSON Schema data type for custom action configuration. |

## Scripts

| Script | Command | Purpose |
| ------ | ------- | ------- |
| `prettier` | `prettier --check '**'` | Check formatting. |

## Data Types

JSON Schema data type artifacts belong in `data-types/`. Do not place public JSON Schema files in `@bastion/core`; core should only own runtime business logic and Valibot schemas.

When adding a new public data type, add the JSON file under `data-types/`. The package export pattern exposes files as `@bastion/spec/data-types/*.json`.
