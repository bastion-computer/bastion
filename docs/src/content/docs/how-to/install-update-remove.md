---
title: Install, update, or remove Bastion
description: Install and update Bastion releases, or safely remove an installation.
---

Use the release installer to install or update Bastion. Removal is manual because
Bastion does not provide an uninstall command.

## Check platform support

Bastion release installation supports these targets:

| Target              | Installed capability                                                                       |
| ------------------- | ------------------------------------------------------------------------------------------ |
| Linux x86_64        | CLI, cluster control plane, host API, privileged daemon, guest proxy, and systemd services |
| macOS Apple silicon | CLI and cluster control plane; no host API, daemon, guest proxy, or VM hosting             |

A Linux VM host also requires read/write access to `/dev/kvm` and the
`/dev/vhost-vsock` device. If Linux runs inside another VM, enable nested
virtualization.

## Install the latest release

:::caution
The installer downloads release archives, verifies their SHA-256 checksums, and
uses `sudo` to install binaries. On Linux, it also writes and enables systemd
services. Review `https://bastion.computer/install.sh` before running it if your
security policy requires source review.
:::

1. Run the stable installer:

   ```sh
   curl -fsSL https://bastion.computer/install.sh | bash
   ```

   By default, binaries are installed in `/usr/local/bin`. On Linux, the
   installer creates and starts `bastion-api.service` and `bastiond.service`.

2. Confirm the version:

   ```sh
   bastion version
   ```

3. On Linux, confirm both services are enabled and active:

   ```sh
   sudo systemctl is-enabled bastiond.service bastion-api.service
   sudo systemctl is-active bastiond.service bastion-api.service
   ```

4. On a new Linux VM host, install and verify its runtime dependencies:

   :::caution
   `--with-utilities` allows Bastion to install missing operating-system packages
   without prompting.
   :::

   ```sh
   bastion system init --with-utilities
   bastion system check
   ```

   This initializes source assets; it does not build the shared base. See
   [Manage the base](/how-to/manage-base/) before creating templates.

## Install a prerelease

1. If you intentionally want the latest GitHub prerelease, pass
   `--experimental`:

   ```sh
   curl -fsSL https://bastion.computer/install.sh | bash -s -- --experimental
   ```

2. Confirm the installed version:

   ```sh
   bastion version
   ```

Use prereleases only where you can tolerate interface and behavior changes.

## Update Bastion

1. Review the current service configuration before updating:

   ```sh
   sudo cat /etc/default/bastion
   ```

   The installer preserves this file during updates. It replaces the systemd
   unit definitions with the current release versions and restarts both
   services.

2. Run the same installer used for a stable installation:

   ```sh
   curl -fsSL https://bastion.computer/install.sh | bash
   ```

   The installer reports the old and new versions when an update is available.
   The installed Linux unit uses `KillMode=process`, so restarting `bastiond`
   does not terminate its Cloud Hypervisor child processes. API connections are
   interrupted while the services restart.

3. Verify the update:

   ```sh
   bastion version
   sudo systemctl is-active bastiond.service bastion-api.service
   curl -fsS http://localhost:3148/v1/health
   ```

   The health response is `{"status":"ok"}`.

## Remove Bastion

1. Capture and verify the installation metadata before deleting any service
   configuration:

   ```sh
   sudo systemctl cat bastion-api.service
   sudo systemctl show bastion-api.service --property=User --value
   command -v bastion
   command -v bastion-guest-proxy
   ```

   Read the exact `EnvironmentFile` path from the unit; it reflects a custom
   `BASTION_SERVICE_ENV_FILE` used during installation. Inspect that file before
   removing it, then record the service `User`, `BASTION_DATA_DIR`, and binary
   directory. Set and verify them in the same shell that you will use for
   removal:

   ```sh
   SERVICE_ENV_FILE="/exact/path/from/EnvironmentFile"
   sudo cat "$SERVICE_ENV_FILE"
   SERVICE_USER="YOUR_SERVICE_USER"
   BASTION_DATA_DIR="/absolute/path/to/bastion-data"
   INSTALL_DIR="/absolute/path/to/install-directory"
   test -n "$SERVICE_ENV_FILE" && sudo test -f "$SERVICE_ENV_FILE"
   test -n "$SERVICE_USER" && id "$SERVICE_USER"
   test -n "$BASTION_DATA_DIR" && test "$BASTION_DATA_DIR" != "/"
   sudo test -d "$BASTION_DATA_DIR"
   test -x "$INSTALL_DIR/bastion"
   ```

   Replace all four example values with the captured values. The installer can
   use a custom service environment file, service user, data directory, and
   install directory; do not infer them after deleting the configuration.

2. If you need the base or prepared templates later, export them before removing
   resources. Follow [Back up and restore Bastion artifacts](/how-to/back-up-and-restore/).

3. List all environments and preserve any work you need:

   ```sh
   bastion env list --limit 100
   ```

4. Remove each environment by its key or ID:

   :::danger
   Removing an environment permanently deletes its writable VM disk. Stopping
   the systemd services first is not a substitute: the daemon unit deliberately
   leaves Cloud Hypervisor child processes running across restarts.
   :::

   ```sh
   ENVIRONMENT_KEY="ENVIRONMENT_KEY"
   bastion env remove --key "$ENVIRONMENT_KEY"
   ```

   Replace `ENVIRONMENT_KEY` with the key from `bastion env list`. If an
   environment has no key, set `ENVIRONMENT_ID="ENVIRONMENT_ID"` to its generated
   ID and run `bastion env remove --id "$ENVIRONMENT_ID"`. Repeat until the list
   is empty.

   :::caution
   The next steps stop Bastion, remove its service definitions, and remove its
   installed executables. Confirm that the environment list is empty first.
   :::

5. Stop and disable the Linux services:

   ```sh
   sudo systemctl disable --now bastion-api.service bastiond.service
   ```

6. Remove the service units and the captured environment file:

   ```sh
   sudo rm -f /etc/systemd/system/bastion-api.service
   sudo rm -f /etc/systemd/system/bastiond.service
   sudo rm -f -- "$SERVICE_ENV_FILE"
   sudo systemctl daemon-reload
   ```

   Remove `SERVICE_ENV_FILE` only if it is dedicated to Bastion. If you supplied
   a pre-existing shared file, remove its Bastion settings instead.

7. Remove the installed binaries:

   ```sh
   sudo rm -f "$INSTALL_DIR/bastion" "$INSTALL_DIR/bastion-guest-proxy"
   ```

   A macOS installation contains only `bastion`; set `INSTALL_DIR` from
   `command -v bastion` and skip the Linux service steps.

## Remove Bastion-managed files

If you want to keep templates, metadata, and client configuration, leave the
captured data directory in place. To remove Bastion-managed data and downloaded
runtime assets, delete that exact verified directory:

:::danger
The following command permanently deletes the SQLite database, secrets, base,
templates, environment artifacts, custom actions, and downloaded runtime
assets. Use the exact `BASTION_DATA_DIR` captured before removing the service
configuration. Do not run it with an empty, root, or unverified value.
:::

```sh
test -n "$BASTION_DATA_DIR"
test "$BASTION_DATA_DIR" != "/"
sudo test -d "$BASTION_DATA_DIR"
sudo rm -rf -- "$BASTION_DATA_DIR"
```

The installer uses an existing service account and does not create it, so do not
delete `SERVICE_USER` solely because you removed Bastion. Operating-system
packages installed by `bastion system init --with-utilities` also remain. Remove
those packages with your system package manager only after confirming that no
other software needs them.

For installer behavior and runtime commands, see the
[host CLI reference](/reference/cli/host/) and
[host requirements and configuration](/reference/host-requirements-and-configuration/).
Review [Security and operational limits](/explanation/security-and-operational-limits/)
before exposing an installed API.
