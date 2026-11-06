# Testing environment

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

Testing environment 不 build 或 bind-mount source。從 [`publish.sh`](../release/publish.sh)
產生的 `release-manifest.json` 把 exact image references（API、Dashboard、MongoDB、PostgreSQL、
Temporal Server、Prometheus、prom-label-proxy、Pulp 與 Pulp web）複製到 `.env`，再執行：

```bash
./prepare.sh
docker compose --env-file .env up -d
./seed.sh
```

拓樸就是 production Compose file 加上獨立的 secrets，以 plain HTTP 在
`SWALLOW_HTTP_PORT`（預設 18080）提供服務。PostgreSQL 發佈在 loopback
`SWALLOW_POSTGRES_HOST_PORT`（`.env.example` 為 15432），因此不會與同一台主機上的
production installation 衝突。Testing 不安裝 MAAS，它的 Prometheus 也只收集 swallow 自己的
metrics：exporter discovery 的 job 由 production installation 的 `swallowctl` 渲染。

Site 的 Local Repository（Pulp）與 production 相同，發佈在每個介面的 `SWALLOW_PULP_PORT`
（`.env.example` 為 18081，因此不會與預設都是 8080 的 production installation 或開發環境衝突）。
production 由 `swallowctl` 在共用的 PostgreSQL 建立 Pulp 的 role 與 database；這裡由一次性的
`pulp-db` service 在每次 `up` 時建立，排在 `pulp-init` 遷移 database 之前。`seed.sh` 比照 production
的 `bootstrap.sh`，把 Pulp 登錄為 testing Site 的 `repository` Integration，並寫入 Local Repository
的預設來源 `ubuntu`。Integration 的 `contentOrigin` 就是 Pulp 自己的 origin
`http://<SWALLOW_ADDRESS>:<SWALLOW_PULP_PORT>`，`SWALLOW_ADDRESS` 沒有設定時是 `localhost`：要讓 Server
從這個 stack 取得內容時，先在 `.env` 設定 `SWALLOW_ADDRESS` 再 `up`。之後 Pulp 從網際網路同步這些來源，
`seed.sh` 不等它完成。

設定 `COMPOSE_PROJECT_NAME` 可在同一台主機上執行多個互相隔離的 stack；用完請以
`docker compose down --volumes` 移除。每個 stack 都發佈自己的 Pulp port，所以每個 stack 要有各自的
`SWALLOW_HTTP_PORT`、`SWALLOW_PULP_PORT` 與 `SWALLOW_POSTGRES_HOST_PORT`。

Generated testing secrets 保存在 `deploy/testing/secrets/`，不重用 production
path；其中包含 Pulp 的 database 密碼、admin 密碼（`repository` Integration 的 credential）與 secret key，
產生方式與 production 的 `prepare-secrets.sh` 相同。`prepare.sh` 與 `seed.sh` 都可重複執行。`seed.sh` 會先在一次性 API container 中建立
deployment key（`swallow-api deployment-key ensure`，即 production `swallowctl install` 的步驟），
遵循 `COMPOSE_PROJECT_NAME`，並檢查 write-only automation credential 的 contract。
