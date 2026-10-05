# Installation 選擇

[English](../en/installation.md) · [文件首頁](README.md)

swallow 正在積極開發中。請依需求選擇 installation path。

| 目標 | 路徑 | Build source | Persistent data | 狀態 |
| --- | --- | ---: | ---: | --- |
| 給 operator 使用 | [Production installation（單一 VM）](../../deploy/production/README.zh-TW.md) | 否 | Volumes 與 backups | 正式支援的安裝方式 |
| 評估或開發 | [Development Compose](../../deploy/dev/README.zh-TW.md) | 是 | Local volumes | Contributor 使用 |
| 驗證 release | [Testing Compose](../../deploy/testing/README.zh-TW.md) | 否 | Isolated volumes | Release validation |
| 不使用 container 安裝 | [Native Ubuntu preview](../../deploy/production/native/README.zh-TW.md) | 否 | Native paths | 尚未完成的 preview，不發佈 |

## Production installation（單一 VM）

一個指令把乾淨的 Ubuntu 24.04 VM 變成可用的 swallow。這裡的「production」指的是
installation contract（digest-pinned images、產生的 secrets、lifecycle tooling），
不是多機拓樸：swallow 與它的 MAAS 共用同一台 VM。

`swallowctl install` 完成時：

- Dashboard 與 API 在 **HTTP port 80** 提供服務，`admin` 帳號可登入；
- MAAS 3.6（region+rack）在同一台 VM 上執行，已是 Site `default` 的 provisioner
  Integration，且已同步；
- 官方 `ubuntu/noble` amd64 image 已 complete，出現在 **Provisioning → Images**；
- Temporal、worker 與 Ansible executor 已串接，Deployment Key 已建立，Site automation
  已啟用；
- `swallow` CLI 已安裝在 `/usr/local/bin/swallow`。

### 前置條件

- 一個已發佈的 swallow release：GitHub Release 上的 assets（Compose bundle、
  `release-manifest.json` 與 `SHA256SUMS`），以及 public 的 GHCR package；否則請先在 VM 上
  `docker login ghcr.io`。Repository 裡的 `release-manifest.example.json` 只有 placeholder
  digest，無法拿來安裝。
- 一台可 `sudo` 的乾淨 Ubuntu 24.04 amd64 VM。建議起始規格 4 vCPU、16 GiB RAM、
  100 GiB disk。
- 能連到 `ghcr.io`（swallow 自己的 images）、Docker Hub（官方 MongoDB、PostgreSQL 與
  Temporal images）、`download.docker.com`、Snap Store 與 `images.maas.io`。
- 未被占用的 port：80（Dashboard）、5240（MAAS），以及 loopback 5432（MAAS 用的 PostgreSQL）。
- 事先不要安裝 MAAS；installer 會拒絕不是它建立的 MAAS。缺少 Docker 時會從 Docker apt
  repository 安裝。

### 安裝

從 GitHub release 下載 Compose bundle 與 checksums，再到 `production/` 目錄執行 installer：

```bash
sha256sum --ignore-missing -c SHA256SUMS
tar --zstd -xf swallow-compose-<version>.tar.zst
cd production
sudo ./swallowctl install --profile production --release-manifest ../release-manifest.json
```

Installer 會把 release 的 image digests 寫入 `.env`、產生 secrets、啟動 stack、建立
Deployment Key、安裝 MAAS、匯入 `ubuntu/noble`（數百 MB）、在 swallow 註冊 MAAS，最後執行
`swallowctl doctor`。重複執行是安全的。

### 安裝後

1. 開啟 `http://<vm-address>/`，以 `admin` 登入；密碼在
   `production/secrets/bootstrap-admin-password`（用 `sudo cat` 讀取）。
2. 從 account menu（**SSH keys**）加入自己的 SSH Access Key。Installer 不會代為建立。
3. Commission 機器前，在 MAAS 設定 PXE network 與 DHCP
   （`http://<vm-address>:5240/MAAS`，帳號 `admin`，密碼在
   `production/secrets/maas-admin-password`）。DNS、DHCP/PXE routing 與 BMC access
   由 site 負責。
4. 隨時可執行 `sudo ./swallowctl doctor` 重新檢查所有串接。

Upgrade、backup、restore 與 uninstall 請見
[production installation reference](../../deploy/production/README.zh-TW.md)；third-party
components 請見 [third-party guide](../../deploy/third-party/README.zh-TW.md)。

## Development Compose

給 contributor 的最短路徑。Source 以 bind mount 掛入、toolchain 在 container 內，
API／Dashboard 變更會自動 reload。Default credentials 與 encryption material
刻意只適合本機，不能在共享環境使用。

## Testing Compose

Testing 使用 release manifest 的 exact image digests，不掛 source，也不使用本機 build tag。
它用來驗證 release，不適合保存長期 operator data。

## Native preview

Native bundle 目標是 Ubuntu 24.04 amd64，但 native Temporal／PostgreSQL systemd
packaging 尚未完成。缺少完整 Temporal topology 時 Workflow 無法執行，因此 release 目前不發佈
native bundle。

## 正式環境安全基線

- Installation 提供 plain HTTP。要開放到受信任的管理網路之外前，請在 port 80 前面加上
  TLS terminator。
- 保留產生的 secrets：`production/secrets/` 權限為 0700，不得進 source control。
- 只用含 immutable image digests 的 release manifest 安裝。
- 排程 `swallowctl backup` 並演練 `restore`。Backup 包含 MongoDB、credential key、
  Workflow artifacts 與 MAAS database；deployment key 的私鑰以 credential key 加密，
  兩者必須一起還原。
