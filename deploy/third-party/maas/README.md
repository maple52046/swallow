# Ubuntu MAAS 3.6

[繁體中文](README.zh-TW.md) · [Third-party overview](../README.md)

MAAS runs on a dedicated Ubuntu 24.04 host or VM. It is never co-located with the
Swallow control plane. Production requires PostgreSQL plus region and rack controllers;
`maas-test-db` is prohibited.

For connected installations, follow the official MAAS 3.6 snap/channel procedure and
record the selected revision in the site compatibility record. For air-gapped
installations, deliver the pinned packages through the local repository described by
`../offline-media-manifest.json`, validate its checksums and signing metadata, and
pre-synchronize all required boot resources before network isolation.

DNS, L2/DHCP/PXE routing, BMC access, and the PostgreSQL backup policy are MAAS
installation inputs owned by the site operator.

Official references:

- <https://maas.io/docs/release-notes>
- <https://maas.io/docs/how-to-install-maas>
