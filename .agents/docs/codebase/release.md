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

## Prereleases

Tags with a `-rc.*` suffix, such as `v0.12.0-rc.1`, create GitHub
prerelease releases. They are not marked as the repository latest release.

The public installer uses stable latest releases by default. To install the
latest prerelease for testing, pass `--experimental`:

```sh
curl -fsSL https://bastion.computer/install.sh | bash -s -- --experimental
```

## Release Workflow

The workflow builds the core package binaries with `BASTION_VERSION` set to the
tag name, so `bastion version` reports the released version instead of `dev`.

The compatible release targets are Linux x86_64 for host runtime support and
macOS Apple silicon for client-only CLI support. The uploaded core release
assets are:

- `bastion_<tag>_linux_x86_64.tar.gz`
- `bastion_<tag>_linux_x86_64.tar.gz.sha256`
- `bastion_<tag>_darwin_arm64.tar.gz`
- `bastion_<tag>_darwin_arm64.tar.gz.sha256`

The Linux archive contains the host runtime binary and the guest proxy installed into templates:

- `bastion`
- `bastion-guest-proxy`

The macOS archive contains the client CLI only:

- `bastion`

The release page is generated automatically by GitHub Actions with generated
release notes.

The workflow also builds and pushes a multi-platform Docker image to Docker Hub
for Linux x86_64 and Apple silicon-compatible Linux arm64 hosts. The Docker Hub
tags are:

- `bastioncomputer/bastion:<tag>` for every release tag
- `bastioncomputer/bastion:latest` for stable releases only

Prerelease tags with `-rc.*` are pushed with their explicit tag but are not
published as `latest`.
