# Configuration reference

[English](../../../docs/en/reference/configuration.md) · [文件首頁](../README.md)

## API configuration

`swallow-api` 依下列順序 merge configuration，越上方優先度越高：

1. `SWALLOW_API_*` environment variable。
2. Command flag。
3. `--config` 指定的 file。
4. Development-oriented defaults。

完整 fields 以 annotated [configuration example](../../../api-server/docs/config-example.yaml)
與 [api-server README](../../../api-server/README.zh-TW.md) 為準。

正式環境需要特別注意：

- MongoDB URI/database 與 migration access。
- JWT signing secret 與 Session 效期（`accessTokenTTL` 15m、`refreshTokenTTL` 168h、`sessionMaxAge`
  720h；舊的 `jwtExpiryHours` 不再使用）。
- Bootstrap admin username/password。
- Base64 32-byte credential-encryption key。
- Machine bearer token。
- Reconcile、inventory 與 SSH key sync interval（`sshKeySyncInterval`，預設 5m）。
- Temporal address、namespace、task queue 與 start interval。
- Ansible runner command、playbook manifest/directory、runtime/artifact
  directory、retention 與 parallelism。
- Maximum uploaded OS image size。

未 restore matching encrypted data 就更換 credential key，會使既有 credentials
無法讀取。

### Boot Media

Boot Media 讓沒有 provisioner DHCP 的網路上的 Server BMC 掛載 swallow 建置的
Boot ISO；見 [OS provisioning](../guides/os-provisioning.md)。Builder 需要 base
URL、可寫入的 storage directory，以及 iPXE asset 與 packaging tool：

| 設定 | Environment variable | 意義 |
| --- | --- | --- |
| `api.bootMedia.baseURL` | `SWALLOW_API_BOOT_MEDIA_BASE_URL` | BMC 連到 swallow 的位址，例如 `http://10.0.0.5`，不含路徑。 |
| `api.bootMedia.dir` | `SWALLOW_API_BOOT_MEDIA_DIR` | 可寫入的 Boot ISO storage，預設 `/var/lib/swallow/boot-media`；file 存放於 `<dir>/<isoId>/swallow-ipxe.iso`。 |
| `api.bootMedia.ipxeDir` | `SWALLOW_API_IPXE_DIR` | Prebuilt iPXE asset（`ipxe.lkrn`、`ipxe.efi`、`genfsimg`、`VERSION`），預設 `/usr/share/swallow/ipxe`。 |
| `api.redfishProbeInterval` | `SWALLOW_API_REDFISH_PROBE_INTERVAL` | 對新加入或過期的 Server 進行 Redfish 偵測的頻率（預設 10m）。 |
| `api.redfishProbeMaxAge` | `SWALLOW_API_REDFISH_PROBE_MAX_AGE` | 一次偵測結果的有效期（預設 24h）。 |

每個 Boot ISO 都會以不帶 authentication、支援 HTTP byte range 的方式提供於
`<baseURL>/boot-media/ipxe/<isoId>/swallow-ipxe.iso`。許多 BMC 只接受 port 80
的 plain `http://`，因此 production Nginx 會在該 port 轉發 `/boot-media/`。
Production Compose 會讀取 `SWALLOW_BOOT_MEDIA_BASE_URL`、設定 API storage directory，
並將 named `boot-media` volume 掛載到 `/var/lib/swallow/boot-media`。

Container image 已包含 pinned iPXE asset 與所需 packaging tool。Native installation
會透過 tmpfiles 建立 Boot Media directory，但不會安裝這些 asset。若要在 native
環境建置，請在 `api.bootMedia.ipxeDir` 提供 `ipxe.lkrn`、`ipxe.efi`、`genfsimg`
與 `VERSION`，並安裝 `mtools`、`xorriso`、`isolinux` 與 `syslinux-common`。

Base URL 為空、storage directory 無法寫入，或缺少 asset／tool 時，Dashboard
會說明 builder unavailable 的原因並停用 **Build ISO**；既有 Boot ISO 仍會列出，
也可刪除。

`api.bootMedia.isoPath` 與 `SWALLOW_API_BOOT_MEDIA_ISO_PATH` 已退場；設定時只會
在 startup 顯示 warning，值會被忽略。固定的
`/boot-media/ipxe/swallow-ipxe.iso` route 已移除；production Compose 不再讀取
`SWALLOW_BOOT_MEDIA_DIR`，放在舊路徑的 hand-made file 也不會被提供。

## Dashboard configuration

`VITE_API_BASE_URL` 在 build／dev-server start 時選擇 API origin。Development
Compose 將它留空並使用 Vite same-origin proxy。更改 Vite environment variable
後需 restart／rebuild Dashboard。

Production 透過同一個 Nginx origin 以 plain HTTP（預設 port 80）提供 Dashboard 與 API；
MongoDB 與 Temporal 保持 internal，API、worker 與 executor 經 `egress` network 連 MAAS 與
Server。Installation 開放到受信任網路之外時，請在前面終結 TLS。

## CLI configuration

CLI resolution order：

1. Profile file。
2. `SWALLOW_*` environment variable。
3. Global flag。

`$SWALLOW_CONFIG` 可選擇另一個 profile path。常見 override 有
`SWALLOW_ENDPOINT`、`SWALLOW_TOKEN`、`SWALLOW_SITE`、
`SWALLOW_MACHINE_TOKEN`、`SWALLOW_INSECURE`。受控 lab 之外不可停用 TLS verification。

## Installation secrets

Compose／native assets 會定義 exact secret file contract。Secret directory 不得進
source control；credential key 必須與 MongoDB、artifact 一起備份。Production installation
會在 `production/secrets/` 產生所有 secrets，包含共置 MAAS 的 database password、admin
password 與 API key。
