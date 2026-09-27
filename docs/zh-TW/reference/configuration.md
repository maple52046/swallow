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
- JWT signing secret 與 expiry。
- Bootstrap admin username/password。
- Base64 32-byte credential-encryption key。
- Machine bearer token。
- Reconcile/inventory interval。
- Temporal address、namespace、task queue 與 start interval。
- Ansible runner command、playbook manifest/directory、runtime/artifact
  directory、retention 與 parallelism。
- Maximum uploaded OS image size。

未 restore matching encrypted data 就更換 credential key，會使既有 credentials
無法讀取。

## Dashboard configuration

`VITE_API_BASE_URL` 在 build／dev-server start 時選擇 API origin。Development
Compose 將它留空並使用 Vite same-origin proxy。更改 Vite environment variable
後需 restart／rebuild Dashboard。

Production 透過同一個 Nginx/TLS origin 提供 Dashboard 與 API；MongoDB、Temporal、
worker 與 executor 保持 internal。

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
source control；credential key 必須與 MongoDB、artifact 一起備份，CA material
依選定 installation path 安裝。
