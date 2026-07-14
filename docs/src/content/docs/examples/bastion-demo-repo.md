---
title: Issue tracker demo repo
description: A practical Bastion tutorial built around a small Bun and TypeScript app.
---

The standalone
[bastion-demo repository](https://github.com/bastion-computer/bastion-demo) is a
small issue tracker app for showing Bastion in a realistic coding-agent workflow.
It uses Bun and TypeScript, has no external package dependencies, and includes a
Bastion template plus prompt files for parallel sessions.

Use it when you want a practical walkthrough that covers:

- Installing Bastion and building its shared base.
- Creating a reusable template.
- Creating parallel environments from that template.
- Attaching over SSH, OpenCode, and `bastion mux`.
- Running several coding sessions at the same time with prompts for bug fixes,
  refactors, features, and test coverage.
- Configuring remote CLI access with `bastion client set api-url`,
  `bastion client config`, and `bastion client remove api-url`.

## Start The Tutorial

Clone the demo repository:

```sh
git clone https://github.com/bastion-computer/bastion-demo.git
cd bastion-demo
```

Then follow the repository README:

```sh
bastion base build
bastion templates create --key bastion-demo --file bastion/template.json
bastion env create --template-key bastion-demo --key demo-fix-bug --tag demo
bastion mux
```

`bastion base build` is a one-time host setup step. It installs common guest
components into the shared base; the demo template then adds Bun, Git, the
project checkout, and project-specific OpenCode configuration in its lightweight
overlay.

The template installs Bun, clones the demo app into `/workspace/bastion-demo`,
runs the tests, configures OpenCode to start in the project directory, and sets
interactive SSH shells to the same directory.

## Parallel Session Prompts

The repository includes prompt files for separate environments:

| Prompt                                   | Purpose                     |
| ---------------------------------------- | --------------------------- |
| `prompts/01-fix-whitespace-title-bug.md` | Fix a validation bug.       |
| `prompts/02-refactor-api-router.md`      | Refactor the API router.    |
| `prompts/03-add-labels-feature.md`       | Add a product feature.      |
| `prompts/04-add-api-tests.md`            | Expand API test coverage.   |
| `prompts/05-add-search-filter.md`        | Improve frontend filtering. |

Create one Bastion environment per prompt, then attach an OpenCode session to
each environment. The environments are isolated VMs, so agents can run tests,
modify files, start servers, and change local state without colliding.
