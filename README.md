# bastion

Bastion is an open source platform to deploy virtual computers for AI agents.

**See the full documentation at [bastion.computer](https://bastion.computer).**

## Dev Environment

The following section is for setting up a local dev environment for contributing to the codebase.

### Prerequisites

- [mise](https://mise.jdx.dev/) for tool and task management.

All required runtimes, compilers, and build tools are managed by [`mise.toml`](./mise.toml); you do not need to install them globally.

### Setup

Install the required tools and JavaScript dependencies from the repository root:

```sh
mise install
```

List available tasks:

```sh
mise tasks --all
```

### Local Testing

Run the same checks used by CI from the repository root:

```sh
mise run format:check
mise run lint
mise run build
mise run typecheck
mise run test
```

Use `format:write` to rewrite formatting locally:

```sh
mise run format:write
```

Start local development services:

```sh
mise run dev:up
```

This opens a `bastion-dev` tmux session with separate panes for the core API, dev db explorer, docs site, and an interactive shell in the repository root.

To stop the dev environment, exit the panes or run `mise run dev:down`.

### Bastion CLI Commands

With the dev server running, run CLI commands from a local dev build through mise:

```sh
mise run dev:bastion templates list
```

Examples:

```sh
mise run dev:bastion secrets list
mise run dev:bastion templates create dev-env --config '{"actions":{"init":[]}}'
mise run dev:bastion sandbox create --from template --key dev-env
```

The task defaults to `http://localhost:3148`. Set `BASTION_API_URL` to target another host API:

```sh
BASTION_API_URL=http://localhost:3148 mise run dev:bastion templates list
```
