# Release 與發佈

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

swallow 沒有 CI：release 由工作機執行 [`publish.sh`](publish.sh) 發佈。

## Images

所有 installation images 都在同一個 GHCR package `ghcr.io/<owner>/swallow`，tag 為
`<component>-<version>`：

| Component | 來源 |
| --- | --- |
| `api`、`dashboard`、`cli` | 由此 repository build（linux/amd64） |
| `mongo`、`temporal-postgres`、`temporal-server`、`temporal-ui` | 固定在 [`runtime-images.env`](runtime-images.env) 的 upstream images，只鏡像 linux/amd64 |

Installation 從不使用 tag：`release-manifest.json` 以從 registry 讀回的 digest 固定每個
image。請審慎地檢查並更新 `runtime-images.env`；Temporal PostgreSQL image 同時承載共置 MAAS 的
database，必須維持 PostgreSQL 14 以上。

## 發佈 release

請在 Ubuntu 24.04 上發佈（native bundle 的 Python wheels 依 host Python 而定）。發佈機器需要
Docker（含 buildx）、git、jq、zstd、含 pip 的 python3，以及具備 `write:packages` 權限的
GitHub token。發佈前請先跑過各 component 的測試（見各 component README）。

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u <github-user> --password-stdin
git switch main && git pull           # 只發佈已 commit 的程式碼
deploy/release/publish.sh 0.1.0       # 加上 --github-release 會一併建立 GitHub Release（需要 gh）
```

`publish.sh` 會先執行 [`validate.sh`](validate.sh) 與文件檢查，拒絕 tag 已存在的版本，接著
build 並推送 `api`、`dashboard`、`cli`，以 [`mirror-runtime-images.sh`](mirror-runtime-images.sh)
鏡像 runtime images，再以 [`assemble-release.sh`](assemble-release.sh) 在
`out/release/<version>/` 組出 artifacts：

- `swallow-compose-<version>.tar.zst`：單一 VM installation（含 CLI 的 `production/`、
  `testing/` 與 manifest）；
- `swallow-native-<version>.tar.zst`：native preview bundle；
- `swallow-oci-<version>.tar`：給 air-gap `docker load` 用的 runtime images；
- `swallow-linux-amd64`：CLI；
- `release-manifest.json`、`offline-media-manifest.json` 與 `SHA256SUMS`。

第一次推送後，請把 GHCR package 設為 public，否則每個 installation 在 `swallowctl install`
前都必須先 `docker login ghcr.io`。沒有加 `--github-release` 時，請手動把 artifacts 附到
GitHub Release。Release 附有 SHA-256 checksums，但沒有簽章與 SBOM。
