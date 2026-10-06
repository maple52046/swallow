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

## OS provisioning compatibility

Client 只能對 swallow 定義的
[`provisioning.state`](../../development/glossaries/terms/os-provisioning-state.md)
分支。不得對 `providerState` 分支：它只是 display-only provider label，例如
MAAS 的 *Commissioning*，而且不同 provisioner 可能使用不同用語。

2026-10-04 的 provisioning vocabulary 變更對 API client 是 breaking change：

- State value `commissioning` 更名為 `inspecting`。
- `POST /api/v1/servers/{id}/commission` 更名為
  `POST /api/v1/servers/{id}/inspect`；舊 route 已移除，沒有 alias。
- `commissioningStatus` field 保留原名，因為它承載 provider 上次的 inspection
  result label。

更名前寫入的 stored projection 會以 `inspecting` 回傳。在下一次 reconcile
重寫前，以 `provisioningState=inspecting` filter 也會比對到這些 record。

Optional RFC 3339 `provisioning.stateSince` field 記錄 swallow 第一次觀察到目前
`provisioning.state` 的時間。State 不變時此值保持不變；state 改變時，它會移到
新的 observation time。起始時間未知時會省略此 field，包括舊 projection 尚未
再次觀察到該 state 的情況；client 必須接受它不存在。這是 swallow 的 observation
timestamp，不是 provider 確切的 transition time。

自 2026-10-06 起，`POST /api/v1/servers/{id}/inspect` 改為執行 `inspect-hardware`
Workflow，不再是單一 provider 呼叫。`202` response 保留 provisioning snapshot，並新增
`workflowId` 與 `resumed`；請追蹤 Workflow，不要只輪詢 Server state。從 `deployed`、
`allocated`、`rescue`、`retired` 或進行中的 state 執行 Inspect，以及其他 Workflow
佔用該 Server 時執行 Inspect，現在會回 `409 conflict`。provisioner Integration 的
`settings.autoInspect: "false"` 會關閉新納管 Server 的自動檢視。詳見
[Server Enrollment contract](../../../api-server/docs/development/api-contracts/api-server/server-enrollment.md)，
其中也定義了 existing OS 的 enrollment bundle。

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
