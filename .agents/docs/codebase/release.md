# Release

Releases are created by the GitHub Actions `Release` workflow when a version tag
matching `v*` is pushed.

## Procedure

1. Ensure `main` is green and contains the commit to release.
2. Create a version tag from the release commit, using a semver-style name such
   as `v0.1.0`.
3. Push the tag:

   ```sh
   git push origin v0.1.0
   ```

4. Watch the `Release` workflow in GitHub Actions.
5. Verify the generated GitHub Release page includes the expected release notes
   and uploaded assets.

## Release Workflow

The workflow builds the core package binaries with `BASTION_VERSION` set to the
tag name, so `bastion version` reports the released version instead of `dev`.

The current compatible release target is Linux x86_64, matching Bastion's current
host runtime support. The uploaded release assets are:

- `bastion_<tag>_linux_x86_64.tar.gz`
- `bastion_<tag>_linux_x86_64.tar.gz.sha256`

The archive contains both core binaries:

- `bastion`
- `bastiond`

The release page is generated automatically by GitHub Actions with generated
release notes.
