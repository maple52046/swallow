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

Boot Media（BMC 為沒有 provisioner DHCP 的網路上的 Server 掛載的 iPXE ISO，見
[OS provisioning](../guides/os-provisioning.md)）是選用功能，兩個值都設定後才會啟用：

| 設定 | Environment variable | 意義 |
| --- | --- | --- |
| `api.bootMedia.isoPath` | `SWALLOW_API_BOOT_MEDIA_ISO_PATH` | API 提供的 iPXE ISO 檔案。 |
| `api.bootMedia.baseURL` | `SWALLOW_API_BOOT_MEDIA_BASE_URL` | BMC 連到 swallow 的位址，例如 `http://10.0.0.5`，不含路徑。 |
| `api.redfishProbeInterval` | `SWALLOW_API_REDFISH_PROBE_INTERVAL` | 對新加入或過期的 Server 進行 Redfish 偵測的頻率（預設 10m）。 |
| `api.redfishProbeMaxAge` | `SWALLOW_API_REDFISH_PROBE_MAX_AGE` | 一次偵測結果的有效期（預設 24h）。 |

BMC 會以不帶帳密的方式掛載 `<baseURL>/boot-media/ipxe/swallow-ipxe.iso`，並在每次開機時重新讀取。
許多 BMC 只接受 port 80 的 `http://`，因此 production Nginx 會在 port 80 轉發 `/boot-media/`（不會導向
HTTPS）。Production Compose 會讀取 `SWALLOW_BOOT_MEDIA_BASE_URL`，並掛載 `SWALLOW_BOOT_MEDIA_DIR`（預設
`./boot-media`，內含 `swallow-ipxe.iso`）。ISO 通常內嵌 provisioner rack 的位址，因此是安裝專屬的檔案。

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
