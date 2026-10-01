# Ubuntu MAAS 3.6

[English](README.md) · [Third-party overview](../README.zh-TW.md)

Production installation 在同一台 Ubuntu 24.04 VM 上與 swallow 共置 MAAS。
`swallowctl install` 透過 [`local-maas.sh`](../../production/local-maas.sh) 安裝並管理它；
請勿事先手動安裝 MAAS，installer 會拒絕不是由它建立的 MAAS snap。

Installer 會：

- 安裝 MAAS `3.6/stable` snap，並初始化為 `region+rack`；
- 將 MAAS 狀態存在 Compose PostgreSQL instance 內獨立的 `maas` role 與 `maasdb`
  database（只開 loopback；不使用 `maas-test-db`）；
- 建立 MAAS `admin` 帳號（`secrets/maas-admin-password`），並把 API key 存到
  `secrets/maas-api-key`；
- 選取官方 `images.maas.io` 的 `ubuntu/noble` amd64，並等到 boot resource complete；
- 把這個 MAAS 註冊為 Site 的 provisioner Integration（`http://host.docker.internal:5240/MAAS`）。

機器透過 `http://<primary IPv4>:5240/MAAS` 連 MAAS；如需其他位址，請在第一次安裝前
設定 `SWALLOW_MAAS_URL`。不會安裝第三方或 custom OS image。

DNS、L2/DHCP/PXE routing 與 BMC access 仍是 Site 擁有的輸入：commission 機器前，
請在 MAAS 的 PXE network 啟用 DHCP。

Official references:

- <https://maas.io/docs/release-notes>
- <https://maas.io/docs/how-to-install-maas>
