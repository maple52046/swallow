# Testing environment

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

Testing environment 不 build 或 bind-mount source。從 [`publish.sh`](../release/publish.sh)
產生的 `release-manifest.json` 把 exact image references（API、Dashboard、MongoDB、Temporal
PostgreSQL 與 Temporal Server；Temporal UI 只在 `diagnostics` profile 需要）複製到 `.env`，
再執行：

```bash
./prepare.sh
docker compose --env-file .env up -d
./seed.sh
```

拓樸就是 production Compose file 加上獨立的 secrets，以 plain HTTP 在
`SWALLOW_HTTP_PORT`（預設 18080）提供服務。PostgreSQL 發佈在 loopback
`SWALLOW_POSTGRES_HOST_PORT`（`.env.example` 為 15432），因此不會與同一台主機上的
production installation 衝突。Testing 不安裝 MAAS。

設定 `COMPOSE_PROJECT_NAME` 可在同一台主機上執行多個互相隔離的 stack；用完請以
`docker compose down --volumes` 移除。

Generated testing secrets 保存在 `deploy/testing/secrets/`，不重用 production
path。`prepare.sh` 與 `seed.sh` 都可重複執行。`seed.sh` 會先在一次性 API container 中建立
deployment key（`swallow-api deployment-key ensure`，即 production `swallowctl install` 的步驟），
遵循 `COMPOSE_PROJECT_NAME`，並檢查 write-only automation credential 的 contract。
