Assume the following requirements when creating a new package in this monorepo:

- All packages MUST be created in the [packages](packages) directory.
- All packages MUST have a `lint`, `prettier`, and `typecheck` scripts.
  - `lint` runs `eslint ./src`.
  - `prettier` runs `prettier --check '**'`.
  - `typecheck` runs `tsc --noEmit`.
  - `eslint`, `prettier`, and `typescript` MUST therefore be instrumented for all new packages.
- All packages MUST have at least a `src` directory where the source code is stored.
