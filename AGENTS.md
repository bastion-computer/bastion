# Agent

**Use this file and its linked documents as a wiki for this repository.**

IMPORTANT RULES:

1. Treat this file and the [.agents/docs](.agents/docs) directory as an up to date source of truth.

2. If you find any discrepancies or introduce changing patterns, you MUST suggest a relevant update to prevent staleness.

3. Use red/green TDD for behavior changes. First prove the current failure or missing behavior with a failing automated test, regression test, or reproducible user-level command. Then implement the smallest fix and rerun the targeted check to prove it is green.

4. Verify every change end to end before finishing. When planning tasks, ALWAYS identify the user-facing workflow that proves the change works, then run that workflow or the closest automated equivalent. For example:

- If asked to implement a new feature, how would I run it as a user? What is the expected inputs and outputs? After implementing the feature, does it do what is expected?
- If asked to fix a bug, how can I reproduce this bug as a user? After implementing the fix, does the bug still occur when going through the reproducible steps?

5. The E2E test suite is a core guard rail for this codebase. Always aim for complete coverage and ensure they are always passing.

6. Once plans have been approved, you have full ownership of implementation. You are expected to work autonomously, and over longer durations if neccessary, to deliver work that is considered done according to the agreed upon spec and plan.

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
