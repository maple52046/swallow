# 疑難排解

[English](../en/troubleshooting.md) · [文件首頁](README.md)

## 本機環境無法啟動

```bash
cd deploy/dev
docker compose ps
docker compose logs api-server worker ansible-executor temporal
```

- Docker socket permission error 通常表示目前 login session 沒有 `docker` group。
- API restart loop 通常是 build 或 config validation error。
- API healthy 但 Workflow 卡住時，通常是 Temporal、worker 或 Ansible executor 不可用。

## `swallowctl doctor` 回報失敗

在 `/opt/swallow/production/`（安裝目錄）執行 `sudo ./swallowctl doctor`；每一行 `FAIL` 代表
一個串接。重新執行 release 的 `install.sh` 是安全的，中途停下的安裝會接續完成。

- **Image pull 被拒：** 若是 `ghcr.io` 的 image，代表 GHCR package 是 private；請設為
  public，或安裝前先 `docker login ghcr.io`。若是 `docker.io` 的 image，代表遇到 Docker Hub
  的匿名拉取限制；稍後重跑，或先 `docker login` Docker Hub。
- **Worker 沒有 poll task queue：** 查看 `docker compose logs worker`；它需要 MongoDB、
  database schema 與 Temporal。
- **MAAS 沒有 complete 的 `ubuntu/noble`：** VM 連不到 `images.maas.io`，或仍在匯入中。
  請重新執行 `sudo ./local-maas.sh ensure-image`。
- **MAAS Integration 未同步：** 該行會顯示 Integration 最後的錯誤。API 透過 `.env` 裡的
  `SWALLOW_MAAS_URL` 連 MAAS（有 `install.sh` 之前建立的安裝使用
  `http://host.docker.internal:5240/MAAS`）；請確認 MAAS 在該位址的 port 5240 有回應、
  `secrets/maas-api-key` 是最新的，再重新執行 `sudo ./bootstrap.sh apply`。

## Login 失敗

- 確認 endpoint 指向 API root，沒有重複附加 `/api/v1`。
- Development default 是 `admin` / `admin`。Production installation 產生的 admin
  password 在 `/opt/swallow/production/secrets/bootstrap-admin-password`。
- CLI profile 可被 environment variable 或 flag 覆寫；診斷時先明確指定
  `--endpoint`。
- Token 過期後需重新 login；consumer 不應解析 JWT expiry。

## Integration 沒有 Server

1. 檢查 Site／Integration 已 enable 且配對正確。
2. 閱讀 `sync.lastError` 與 `sync.lastSucceededAt`。
3. 驗證 MAAS network reachability 與 write-only credential。
4. Trigger reconcile，或等待 configured interval。
5. Server 不支援 manual create。

## Server 持續停在 Releasing 或 Inspecting

`releasing`、`inspecting`、`testing` 與 `deploying` 都是進行中的 OS
provisioning state，預期會在不需要另一個 operator action 的情況下改變。
Deployment 欄位的 spinner 會標示這種狀況，但不代表 provider 一定仍有進度。

若有顯示執行時間，這些 provider state 會從 swallow 第一次觀察到它時起算；
swallow OS deployment 則改從 deployment 開始時起算。它不是 provider 確切的
transition time，也不能證明仍有進度。沒有執行時間代表
起始時間未知，不代表工作尚未開始。

1. 將滑鼠停在 Deployment state 上，查看目前細節。
2. 檢查 provisioning observation time，以及 Integration 的 sync error 與最後成功時間。
3. Refresh Server，執行一次 targeted provider read。
4. 查看 Server 的 provider event，確認 external lifecycle action 仍有進度或已失敗。

```bash
swallow servers refresh server1
swallow servers events server1 --limit 50
swallow -o json servers get server1
```

如果 state 變成 `failed`、`broken` 或 `rescue`，代表需要 operator 處理，而不是
繼續等待。各 state 與 recovery 提示見
[Server 與 infrastructure](guides/servers-and-infrastructure.md#判讀-deployment-欄位)。

## 更換 key 後 credential 失效

Stored integration／automation credential 使用 `api.credentialKey` 加密。更換
key 後既有 ciphertext 無法解密。請從 backup restore matching key，或重新輸入每個
affected credential。

## Workflow pending 或卡住

- 確認 API、Temporal Server/PostgreSQL、worker 與 Ansible executor health。
- 檢查 Workflow current Task、event 與 log。
- 檢查 Site lease conflict、Server lock、SSH known host、resolved SSH user
  與 external provider availability。
- 第一個 Workflow terminal 或安全 cancel 前，不要開始 conflicting Workflow。

## Monitoring 是空的

- 確認 selected Site 已設定 metrics Integration。
- 驗證 Prometheus 可使用 machine token 呼叫 service discovery（若 swallow 前面有 TLS
  terminator，也要信任它的 CA）。
- 確認 exporter 已安裝，且 discovered target 可連線。
- Missing metric data 是 unknown，不是 failure 證據。

## 保留 request ID

API error 內的 opaque request ID 與 `X-Request-ID` header 相同。對照 log 時，
請連同精確時間、Site、resource ID 與 Workflow ID 一起提供。

環境特定說明另見 [development](../../deploy/dev/README.zh-TW.md) 與
[Compose installation](../../deploy/production/README.zh-TW.md) references。
