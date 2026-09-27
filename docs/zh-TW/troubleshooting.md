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

## Login 失敗

- 確認 endpoint 指向 API root，沒有重複附加 `/api/v1`。
- Development default 是 `admin` / `admin`；其他 installation 使用自己的
  bootstrap configuration。
- CLI profile 可被 environment variable 或 flag 覆寫；診斷時先明確指定
  `--endpoint`。
- Token 過期後需重新 login；consumer 不應解析 JWT expiry。

## Integration 沒有 Server

1. 檢查 Site／Integration 已 enable 且配對正確。
2. 閱讀 `sync.lastError` 與 `sync.lastSucceededAt`。
3. 驗證 MAAS network reachability 與 write-only credential。
4. Trigger reconcile，或等待 configured interval。
5. Server 不支援 manual create。

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
- 驗證 Prometheus 可使用 machine token 呼叫 service discovery，並信任 swallow CA。
- 確認 exporter 已安裝，且 discovered target 可連線。
- Missing metric data 是 unknown，不是 failure 證據。

## 保留 request ID

API error 內的 opaque request ID 與 `X-Request-ID` header 相同。對照 log 時，
請連同精確時間、Site、resource ID 與 Workflow ID 一起提供。

環境特定說明另見 [development](../../deploy/dev/README.zh-TW.md) 與
[Compose installation](../../deploy/production/README.zh-TW.md) references。
