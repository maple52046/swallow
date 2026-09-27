# Docker CE prerequisite

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

Docker CE is a host prerequisite for the Compose installation, never a service in the
Swallow project. On connected Ubuntu 24.04 hosts, install Docker Engine and the Compose
plugin from Docker's signed apt repository; do not use the convenience script.

For disconnected hosts, mirror the exact packages listed by
`../offline-media-manifest.json`, preserve repository metadata and signing keys, verify
`SHA256SUMS`, then install from the local repository. Run `docker version` and
`docker compose version` before `deploy/production/swallowctl preflight`.

Official procedure: <https://docs.docker.com/engine/install/ubuntu/>
