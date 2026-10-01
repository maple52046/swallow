# swallow installation assets

[繁體中文](README.zh-TW.md) · [Installation guide](../docs/en/installation.md)

This directory contains the environment orchestration, release tooling, and
third-party media contracts used to install and run swallow.

`deploy/` is retained as an internal repository path for compatibility. It is
not the product term: installing, starting, upgrading, restoring, or
uninstalling swallow is an **Installation**. **Deployment** is reserved for
asking a provisioner such as MAAS to install an operating system on a managed
Server.

- `dev/` — source-based development golden path.
- `testing/` — digest-pinned release validation with isolated data.
- `production/` — the single-VM production installation (`swallowctl install`: Compose
  stack, co-located MAAS, and bootstrap) and native preview assets.
- `release/` — workstation release publication (`publish.sh`); the project has no CI.
- `third-party/` — separately versioned prerequisites and offline media contracts.

swallow is in active development and has no stable SemVer release. The native
path remains an incomplete preview until its Temporal/PostgreSQL systemd
packaging is implemented.
