# API integration

[English](../../../docs/en/reference/api-integration.md) · [文件首頁](../README.md)

`api-server` component 擁有 swallow HTTP API。External client 必須使用 active
provider-owned contracts，不得從 private Go type 或 Dashboard traffic 推測 fields。

## Base URL 與 authentication

Route 位於 `/api/v1`。Login 會將 username／password 換成 opaque bearer token：

```bash
API=https://swallow.example/api/v1
TOKEN=$(curl --fail --silent \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"REDACTED"}' \
  "$API/auth/login" | jq -r .accessToken)

curl --fail --silent \
  -H "Authorization: Bearer $TOKEN" \
  "$API/auth/me"
```

JWT 必須視為 opaque。Identity 與 role 從 `/auth/me` 讀取，收到 `401` 後重新
authenticate。

Prometheus discovery 等 machine endpoint 可接受 configured machine bearer token；
它不是 operator endpoint 的替代 login。

## Error

Error 使用統一 envelope：

```json
{
  "error": {
    "code": "validation_error",
    "message": "human-readable detail",
    "requestId": "opaque-correlation-id"
  }
}
```

Client 只對 `code` 分支，不解析 `message`，並保留 `requestId` 供 log correlation。
Shared contract 定義 code 與 HTTP status mapping。

## Pagination 與 timestamp

大型 list endpoint 接受 `page`、`pageSize`，回傳 `items`、`total`、
`page`、`pageSize`。Timestamp 使用 ISO 8601 UTC。Bounded collection 若回傳
plain array，會由個別 contract 明確說明。

## Identity 與 staleness

- 使用 opaque resource ID，不以 hostname／address 當 integration key。
- 保守處理 unknown enum value。
- 保留 `null` 的 unknown 語意。
- 以 cached provider data 決策前先讀 sync／observation timestamp。
- Credential 是 write-only；使用 `hasCredential`，不要期待 redacted value。

## Contract directory

依序閱讀：

1. [HTTP conventions](../../../api-server/docs/development/api-contracts/api-server/conventions.md)
2. [Active contract outline](../../../api-server/docs/development/api-contracts/api-server/outline.md)
3. 要整合 resource 的特定 active contract。

不得對 Planned entry 實作。`/operations` 與 `/clusters` 是 deprecated migration
surface；新 client 使用 `/workflows` 與 `/platforms`。

Shell automation 可使用具備廣泛 operator coverage、能輸出 lossless
JSON／YAML 的 [swallow CLI](cli.md)。Managed Software 目前需使用 Dashboard
或 HTTP API。
