# Swallow installation assets

This directory contains the environment orchestration, release tooling, and third-party
media contracts used to install and run Swallow.

`deploy/` is retained as an internal repository path for compatibility. It is not the
product term: installing, starting, upgrading, restoring, or uninstalling the Swallow
control plane is an **installation**. **Deployment** is reserved for asking a provisioner
such as MAAS to install an operating system on a managed server.

- `dev/` — the source-based development golden path.
- `testing/` — digest-pinned candidate installations with isolated data.
- `production/` — supported Compose and native installation lifecycle.
- `release/` — candidate assembly and SemVer promotion tooling.
- `third-party/` — separately versioned prerequisites and offline media contracts.
