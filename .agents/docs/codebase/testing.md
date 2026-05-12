ALWAYS verify the code has been linted, format-checked, built, typechecked where supported, and tested from the repository root via the following commands:

- `mise run lint`
- `mise run format:check`
- `mise run build`
- `mise run typecheck`
- `mise run test`

Use `mise run format:write` to rewrite formatting locally. CI uses `mise run format:check`.
