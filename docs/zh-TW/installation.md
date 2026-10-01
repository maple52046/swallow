# Installation 選擇

[English](../en/installation.md) · [文件首頁](README.md)

swallow 正在積極開發中，目前沒有穩定 SemVer release。請依需要驗證的目標選擇
installation path。

| 目標 | 路徑 | Build source | Persistent data | 狀態 |
| --- | --- | ---: | ---: | --- |
| 評估或開發 | [Development Compose](../../deploy/dev/README.zh-TW.md) | 是 | Local volumes | 建議 evaluation 使用 |
| 驗證 candidate | [Testing Compose](../../deploy/testing/README.zh-TW.md) | 否 | Isolated volumes | CI/release validation |
| 驗證 installation lifecycle | [Production Compose assets](../../deploy/production/README.zh-TW.md) | 否 | Backups 與 retained state | Active-development assets |
| 不使用 container 安裝 | [Native Ubuntu preview](../../deploy/production/native/README.zh-TW.md) | 否 | Native paths | 尚未完成的 preview |

## Development Compose

這是最短的可執行路徑。Source 以 bind mount 掛入、toolchain 在 container 內，
API／Dashboard 變更會自動 reload。Default credentials 與 encryption material
刻意只適合本機，不能在共享環境使用。

## Testing Compose

Testing 使用 candidate 的 exact image digests，不掛 source，也不使用本機 build tag。
它服務 CI 與 candidate acceptance，不適合保存長期 operator data。

## Compose installation assets

Production 目錄說明 digest-pinned images、TLS、secrets、preflight、backup、restore、
upgrade、doctor 與 uninstall。由於目前尚無 stable release，應把它視為可驗證的
現行 installation contract，而非 general-availability 承諾。

Docker CE、MAAS、MongoDB media 與 per-Site Prometheus 等 prerequisites 分開交付；
請見 [third-party guide](../../deploy/third-party/README.zh-TW.md)。

## Native preview

Native bundle 目標是 Ubuntu 24.04 amd64，但 native Temporal／PostgreSQL systemd
packaging 尚未完成。缺少完整 Temporal topology 時 Workflow 無法執行；
要驗證完整產品請使用 Compose。

## 正式環境安全基線

不得沿用 development defaults。實際 installation 至少需要：

- CA-issued TLS 與權限受限的 secret files。
- 唯一的 JWT、machine-token、bootstrap-admin、MongoDB 與 credential-encryption secrets。
- 含 immutable image digests 的 exact release/candidate manifest。
- 涵蓋 MongoDB、credential key 與 Workflow artifacts 的 backup／restore drill（deployment key 的私鑰
  以 credential key 加密存於 MongoDB，兩者必須一起還原；`swallowctl install` 與 `upgrade` 會建立此 key，
它不存在時 OS 與 Platform 佈署會被拒絕）。
- Dedicated MAAS、per-Site monitoring，以及清楚的 network／retention ownership。
