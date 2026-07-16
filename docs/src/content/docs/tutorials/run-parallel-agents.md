---
title: Run parallel agents
description: Run two isolated OpenCode sessions against the Bastion demo issue tracker.
---

In this tutorial, you prepare the `bastion-demo` issue tracker once, create two
isolated environments from it, and run separate coding agents at the same time.
One agent fixes input validation while the other expands API test coverage.

The tutorial contains the complete Bastion workflow. You do not need to follow
the demo repository's README.

## Prerequisites

Before you begin, you need:

- A Linux x86_64 Bastion VM host with KVM and vhost-vsock support.
- Bastion installed, its services running, and a base already built. Complete
  [Get started](/tutorials/get-started/) first on a new host.
- The `opencode` CLI on the machine where you run Bastion commands.
- An OpenAI API key that can use the model configured by the demo template.
- Capacity for two environments. Each one requests 2 vCPUs, 2 GiB of memory,
  and a 20 GiB volume.

The Bastion host and cluster APIs provide no native authentication or TLS. Keep
the API on `localhost` for this tutorial.

## Create the demo template

The guest checkout in this tutorial is pinned to `bastion-demo` commit
`627337df1d57bbe83a4136ae511e7088900f7d24`. At that commit, the project has no
package dependencies, and `bun test` passes without running `bun install`.

1. Create and enter a local tutorial directory:

   ```sh
   mkdir bastion-parallel-tutorial
   cd bastion-parallel-tutorial
   ```

2. Create `bastion-demo-template.json` locally:

   ```sh
   cat > bastion-demo-template.json <<'JSON'
   {
     "resources": {
       "vcpu": 2,
       "memory": 2,
       "volume": 20
     },
     "tunnels": {
       "tracker": 3000
     },
     "agents": {
       "opencode": {
         "working_directory": "/workspace/bastion-demo",
         "auth": {
           "openai": {
             "type": "api",
             "key": "${{ secret.OPENAI_API_KEY }}"
           }
         },
         "config": {
           "model": "openai/gpt-5.5",
           "permission": "allow"
         }
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
           "run": "apt-get update && apt-get install -y --no-install-recommends git"
         },
         {
           "run": "git clone https://github.com/bastion-computer/bastion-demo.git bastion-demo && git -C bastion-demo checkout --detach 627337df1d57bbe83a4136ae511e7088900f7d24",
           "working_directory": "/workspace"
         },
         {
           "run": "bun test",
           "working_directory": "/workspace/bastion-demo"
         },
         {
           "use": "set_default_ssh_directory",
           "with": {
             "path": "/workspace/bastion-demo"
           }
         }
       ],
       "start": [
         {
           "run": "set -eu\nnohup bun run start > /tmp/bastion-demo.log 2>&1 &\nfor i in $(seq 1 30); do\n  if curl -fsS http://127.0.0.1:3000/api/health >/dev/null; then\n    exit 0\n  fi\n  sleep 1\ndone\ncat /tmp/bastion-demo.log >&2\nexit 1",
           "working_directory": "/workspace/bastion-demo"
         }
       ]
     }
   }
   JSON
   ```

   The template performs this complete setup:
   - Requests 2 vCPUs, 2 GiB of memory, and a 20 GiB volume.
   - Installs Bun 1.3.13 with the built-in `setup_bun` action.
   - Installs Git, clones the repository into `/workspace/bastion-demo`, checks
     out the exact commit above, and runs `bun test` without installing
     dependencies.
   - Starts OpenCode in `/workspace/bastion-demo` with an API key from
     `${{ secret.OPENAI_API_KEY }}`.
   - Starts the tracker for every environment and waits for `/api/health` to
     respond before the start action succeeds.
   - Exposes the app's guest-local port `3000` as the `tracker` tunnel.

   Template init work becomes one immutable layer. Each environment receives a
   separate writable layer, process tree, network, and checkout.

## Store the provider key

:::caution
Bastion stores secrets as plaintext in its SQLite database. The API has no
native authentication or TLS, and `bastion secrets get` returns the value. Keep
the API private and use a narrowly scoped key.

The CLI currently accepts the value only through `--value`; it has no stdin or
file alternative. The following Bash commands avoid shell history, but the
value remains visible in the CLI argument vector while the command runs.
Same-user or privileged processes can potentially read it through process
inspection, and tracing, audit, or command-accounting systems can record it.
:::

1. Read the key without echoing it, create the Bastion secret, and clear the
   shell variable:

   ```bash
   read -rsp 'OpenAI API key: ' SECRET_VALUE
   printf '\n'
   bastion secrets create --key OPENAI_API_KEY --value "$SECRET_VALUE"
   unset SECRET_VALUE
   ```

   `SECRET_VALUE` is a temporary shell variable. The response contains secret
   metadata, but not the value.

## Prepare the demo template

1. Create the reusable template from the local file:

   ```sh
   bastion templates create --key bastion-demo --file ./bastion-demo-template.json
   ```

   This can take several minutes. Setup and test logs appear on stderr. The
   final JSON on stdout contains a generated `id`, the key `bastion-demo`, and a
   `baseContentAddress` that begins with `sha256:`.

2. Confirm the stored configuration:

   ```sh
   bastion templates get --key bastion-demo
   ```

   The output retains the `${{ secret.OPENAI_API_KEY }}` reference and does not
   contain the resolved key. The prepared template disk can contain values used
   during init, so treat the template and its exports as sensitive.

## Create two isolated environments

1. Create an environment for the validation fix:

   ```sh
   bastion env create --template-key bastion-demo --key demo-fix-bug --tag demo --tag task:fix-bug
   ```

2. Create another environment for the API tests:

   ```sh
   bastion env create --template-key bastion-demo --key demo-tests --tag demo --tag task:tests
   ```

3. List both environments:

   ```sh
   bastion env list --tag demo
   ```

   The response contains two entries with `status` set to `running`. Both use
   the same source template, but their writable disks and running processes are
   independent. In this template, `running` also means the start action received
   a successful response from the tracker's `/api/health` endpoint.

## Start the agents

1. In one terminal, attach OpenCode to the first environment:

   ```sh
   bastion opencode --key demo-fix-bug
   ```

   Give the agent this task:

   ```text
   Users can create issues whose title is only spaces. Reproduce this with a
   failing test, then fix create and update validation so blank or
   whitespace-only titles return 400. Keep the app dependency-free, preserve
   the API response shape, and run bun test before finishing.
   ```

2. At the same time, in another terminal, attach OpenCode to the second
   environment:

   ```sh
   bastion opencode --key demo-tests
   ```

   Give this agent a different task:

   ```text
   Expand HTTP API coverage for GET /api/health, unknown API routes, invalid
   JSON, updating an unknown issue, and deleting an unknown issue. Keep the app
   dependency-free, use isolated temporary data files, and run bun test before
   finishing.
   ```

   The agents can edit files and run the same ports without colliding because
   each session targets a different VM.

## Verify the results

After both agents finish, verify each environment independently.

1. Run the test suite in the validation environment:

   ```sh
   bastion ssh --key demo-fix-bug -- "cd /workspace/bastion-demo && bun test && git status --short && git diff --stat 627337df1d57bbe83a4136ae511e7088900f7d24"
   ```

2. Run the same checks in the test-coverage environment:

   ```sh
   bastion ssh --key demo-tests -- "cd /workspace/bastion-demo && bun test && git status --short && git diff --stat 627337df1d57bbe83a4136ae511e7088900f7d24"
   ```

   Bastion joins command elements after `--` with spaces before starting the SSH
   command. Pass a compound remote shell command as one quoted argument so your
   local shell does not consume `&&` or remove the grouping. Each command must
   print a passing Bun test summary. Inspect `git status` and the diff summary;
   the exact files and whether the agent committed changes depend on the agent's
   implementation.

3. Inspect the full diff in each environment before you preserve or merge it:

   ```sh
   bastion ssh --key demo-fix-bug -- "cd /workspace/bastion-demo && git diff 627337df1d57bbe83a4136ae511e7088900f7d24"
   bastion ssh --key demo-tests -- "cd /workspace/bastion-demo && git diff 627337df1d57bbe83a4136ae511e7088900f7d24"
   ```

4. In a third terminal, proxy the validation environment's tracker service:

   ```sh
   bastion proxy --env-key demo-fix-bug --name tracker
   ```

   Open the printed `http://localhost:PORT` URL. `PORT` is the guest tunnel port
   unless that local port is occupied, in which case Bastion chooses a free
   port. Press `Ctrl-C` when you finish previewing the app.

## Preserve and clean up

:::danger
Removing these environments permanently deletes both agents' writable disks.
Push or export all work you need before removing them.
:::

1. If you have not pushed the changes, archive each complete checkout on your
   local machine:

   ```sh
   bastion ssh --key demo-fix-bug -- "tar -C /workspace -czf - bastion-demo" > demo-fix-bug.tar.gz
   bastion ssh --key demo-tests -- "tar -C /workspace -czf - bastion-demo" > demo-tests.tar.gz
   test -s demo-fix-bug.tar.gz && test -s demo-tests.tar.gz
   ```

2. Remove both environments before removing their template:

   ```sh
   bastion env remove --key demo-fix-bug
   bastion env remove --key demo-tests
   bastion templates remove --key bastion-demo
   rm ./bastion-demo-template.json
   ```

3. Remove the provider key if no other template needs it:

   ```sh
   bastion secrets remove --key OPENAI_API_KEY
   ```

The shared base remains available for your next template. To scale this pattern,
create one keyed environment per task and use repeated `--tag` flags to group
work. See [Manage environments](/how-to/manage-environments/) and
[Connect to environments](/how-to/connect-to-environments/) for the individual
commands. See [Actions and secrets](/explanation/actions-and-secrets/) for why
the provider key is resolved again for each environment.
