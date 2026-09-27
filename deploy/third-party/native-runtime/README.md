# Native runtime media

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

This media supplies the complete Ubuntu 24.04 amd64 package closure needed by the native
Swallow path but not owned by MongoDB: Nginx, `python3-venv`, and their dependencies.
Place packages under `packages/`, pin their versions in the release media inventory,
and generate `SHA256SUMS` over every delivered file.

The native installer verifies this media and the separate MongoDB media before invoking
`apt-get --no-download`. It must succeed on a host with public apt access blocked.
