# Testing environment

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

Testing environment 不 build 或 bind-mount source。將 candidate release manifest
內三個 exact image reference 複製到 `.env`，再執行：

```bash
./prepare.sh
docker compose --env-file .env up -d
./seed.sh
```

同一份 Compose file 供 shared environment 與 CI 使用。CI 以唯一 project name
啟動，完成後刪除 volumes。Self-signed TLS 只允許 testing；正式 installation
必須提供 CA material。

Generated testing secrets 保存在 `deploy/testing/secrets/`，不重用 production
path。`prepare.sh` 與 `seed.sh` 都可重複執行。`seed.sh` 會先在一次性 API container 中建立
deployment key（`swallow-api deployment-key ensure`，即 production `swallowctl install` 的步驟），並遵循
`COMPOSE_PROJECT_NAME`。

Candidate workflow 會用剛 build 的 API／Dashboard exact digests 啟動 topology，
等待 readiness、seed、驗證 reported candidate version，最後清除 project 與 volume。
