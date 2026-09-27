# Ubuntu MAAS 3.6

[English](README.md) · [Third-party overview](../README.zh-TW.md)

MAAS 必須執行於 dedicated Ubuntu 24.04 host 或 VM，不與 swallow installation
共置。Production-style topology 需要 PostgreSQL、region controller 與 rack
controller；禁止使用 `maas-test-db`。

Connected installation 依 official MAAS 3.6 snap/channel procedure，並在 Site
compatibility record 保存 selected revision。Air-gap installation 需透過
`../offline-media-manifest.json` 定義的 local repository 交付 pinned packages，
驗證 checksum／signing metadata，並在隔離前同步所有 boot resources。

DNS、L2/DHCP/PXE routing、BMC access 與 PostgreSQL backup policy 是 Site operator
擁有的 MAAS installation inputs。

Official references:

- <https://maas.io/docs/release-notes>
- <https://maas.io/docs/how-to-install-maas>
