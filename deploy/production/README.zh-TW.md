# Compose installation assets

[English](README.md) · [Installation 選擇](../../docs/zh-TW/installation.md)

此目錄提供 Ubuntu 24.04 amd64 的 production-style Docker Compose installation
contract。swallow 尚在 active development，repository 目前未發布 stable SemVer
release；請將此流程視為 candidate／installation validation，而非 GA 承諾。

Image 不在這裡 build；`.env` 必須使用 promoted/candidate manifest 的 exact digest。
Dashboard/Nginx 是唯一 public service，API、MongoDB、Temporal、worker 與 executor
留在 internal network。TLS 必須使用 installation 提供的 CA material。

```bash
cp .env.example .env
./prepare-secrets.sh
# 安裝 secrets/tls.crt、secrets/tls.key，並填入 exact image references
set -a; source .env; set +a
./swallowctl preflight
./swallowctl install
./swallowctl doctor
```

`upgrade` 會先 backup；有 active Workflow 時拒絕，除非明確 `--force`。
Backup 包含 MongoDB、credential encryption key 與 job artifacts。Uninstall
預設保留 state，只有 `--purge-data` 會刪除。

Air-gap installation 必須驗證 `SHA256SUMS` 與 Sigstore bundle，再以
`docker load` 載入 OCI archive，且只能使用 `release-manifest.json` 的 reference。
Installer 不會 build 或默默改用 tag。

Secret source directory 權限是 `0700` 且排除於 git；透過 lifecycle tool 備份，
不要放進 source control。

每日排程 `./swallowctl backup`。Retention 是七份 daily 與四份 Sunday weekly。
Restore 會 force-recreate API／Dashboard，確保 restored credential key 被重新載入。

[Native](native/README.zh-TW.md) 仍是 incomplete preview，完整 Workflow topology
請使用 Compose。
