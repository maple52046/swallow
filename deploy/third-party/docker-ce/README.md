# Docker CE prerequisite

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

Docker CE is a host prerequisite for the Compose installation, never a service in the
Swallow project. On a connected Ubuntu 24.04 host, `swallowctl install` installs Docker
Engine and the Compose plugin from Docker's signed apt repository when they are missing; it
never uses the convenience script. A host that already runs Docker keeps its installation.

For disconnected hosts, mirror the exact packages listed by
`../offline-media-manifest.json`, preserve repository metadata and signing keys, verify
`SHA256SUMS`, then install from the local repository before running `swallowctl install`.

Official procedure: <https://docs.docker.com/engine/install/ubuntu/>
