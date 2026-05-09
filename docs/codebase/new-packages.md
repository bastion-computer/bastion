Assume the following requirements when creating a new package in this monorepo:

- All packages MUST be created in the [packages](packages) directory.
- All packages MUST have a `lint`, `prettier`, and `typecheck` scripts.
  - `lint` runs `eslint ./src`.
  - `prettier` runs `prettier --check '**'`.
  - `typecheck` runs `tsc --noEmit`.
    - **Exception:** Astro-based packages (e.g. `@bastion/docs`) use `astro check` instead of `tsc --noEmit`, with `@astrojs/check` as a devDependency.
  - `eslint`, `prettier`, and `typescript` MUST therefore be instrumented for all new packages.
- All packages MUST have at least a `src` directory where the source code is stored.
  - **Exception:** Raw artifact packages that only export non-code assets, such as JSON files, do not need a `src` directory, `lint`, or `typecheck` scripts.
