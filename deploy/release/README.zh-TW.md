# Release 與發佈

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

swallow 沒有 CI：release 由工作機執行 [`publish.sh`](publish.sh) 發佈。

## Images

GHCR package `ghcr.io/<owner>/swallow` 只放由此 repository build 的 images，tag 為
`<component>-<version>`。Runtime services 直接使用官方 images：

| Image | 來源 |
| --- | --- |
| `api`、`dashboard`、`cli` | 由此 repository build（linux/amd64），推送到 GHCR |
| MongoDB、PostgreSQL、Temporal Server | Docker Hub 上官方的 `mongo`、`postgres` 與 `temporalio/auto-setup`，固定在 [`runtime-images.env`](runtime-images.env) |

Installation 從不使用 tag：`release-manifest.json` 以 digest 固定每個 image。請審慎地檢查並
更新 `runtime-images.env`；PostgreSQL image 同時承載共置 MAAS 的 database，必須維持
PostgreSQL 14 以上。選配的 Temporal UI 不屬於 release；operator 需要時，自行在
`production/.env` 設定它的 digest。

## 發佈 release

請在 x86_64 Linux 上發佈。所有 image 都以一般的 `docker build` 與 `docker push` 原生 build 成
linux/amd64，不需要 buildx 或模擬：Docker Engine，或 BuildKit 有在執行的 nerdctl 即可。發佈機器
另外需要 git、jq、zstd、python3、放在 `PATH` 上的
[crane](https://github.com/google/go-containerregistry/tree/main/cmd/crane)，以及具備
`write:packages` 權限的 GitHub token。發佈前請先跑過各 component 的測試（見各 component
README）。

```bash
go install github.com/google/go-containerregistry/cmd/crane@v0.22.1   # 只需一次；把 Go 的 bin 加進 PATH
echo "$GHCR_TOKEN" | docker login ghcr.io -u <github-user> --password-stdin
git switch main && git pull           # 只發佈已 commit 的程式碼
deploy/release/publish.sh 0.1.0       # 加上 --github-release 會一併建立 GitHub Release（需要 gh）
```

`publish.sh` 會先執行 [`validate.sh`](validate.sh) 與文件檢查，拒絕 tag 已存在的版本，確認每個
官方 runtime image 都有 linux/amd64，接著 build 並推送 `api`、`dashboard`、`cli`，再以
[`assemble-release.sh`](assemble-release.sh) 在 `out/release/<version>/` 組出 artifacts：

- `swallow-compose-<version>.tar.zst`：單一 VM installation（含 CLI 的 `production/`、
  `testing/` 與 manifest）；
- `install.sh`：這個版本的一鍵安裝與升級腳本，由 [`install.sh`](install.sh) 填入版本後產生；
- `swallow-linux-amd64`：CLI；
- `release-manifest.json`、`offline-media-manifest.json` 與 `SHA256SUMS`。

Release 只包含 Compose installation
（[ADR 050](../../docs/decisions/050-compose-only-release-artifacts.md)）：native packaging
完成前不出 native bundle；在支援離線安裝（含 MAAS images）之前，也不出離線 image 封存檔。

第一次推送後，請把 GHCR package 設為 public，否則每個 installation 在 `swallowctl install`
前都必須先 `docker login ghcr.io`。沒有加 `--github-release` 時，請手動把 artifacts 附到
GitHub Release。Release 附有 SHA-256 checksums，但沒有簽章與 SBOM。
