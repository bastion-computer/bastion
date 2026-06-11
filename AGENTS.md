# Agent

**Use this file and its linked documents as a wiki for this repository.**

IMPORTANT RULES:

1. Treat this file and the [.agents/docs](.agents/docs) directory as an up to date source of truth.

2. If you find any discrepancies or introduce changing patterns, you MUST suggest a relevant update to prevent staleness.

3. When planning tasks, ALWAYS think about the correct end to end feedback loop that will verify the correctness of any subsequent changes. For example:

- If asked to implement a new feature, how would I run it as a user? What is the expected inputs and outputs? After implementing the feature, does it do what is expected?
- If asked to fix a bug, how can I reproduce this bug as a user? After implementeing the fix, does the bug still occur when going through the reproducible steps?

## Table of Contents

- [Codebase](.agents/docs/codebase/)
  - [Runtime](.agents/docs/codebase/runtime.md) - Runtime conventions and APIs
  - [New Packages](.agents/docs/codebase/new-packages.md) - Requirements for creating new monorepo packages
  - [Release](.agents/docs/codebase/release.md) - Release workflow and tag procedure
  - [Testing](.agents/docs/codebase/testing.md) - Verification commands
- [Dev](.agents/docs/dev/)
  - [Drizzle](.agents/docs/dev/drizzle.md) - Development-only database debugging tools
- [Packages](.agents/docs/packages/)
  - [Core](.agents/docs/packages/core.md) - Go host API service and CLI
  - [Docs](.agents/docs/packages/docs.md) - Astro Starlight documentation site
