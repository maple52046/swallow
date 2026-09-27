# Workflow

[English](../../../docs/en/guides/workflows.md) · [文件首頁](../README.md)

Workflow 是 operator intent 與 progress 的 durable record。建立 OS deployment、
Platform lifecycle action、Managed Software change 或 diagnostic operation 後，
request 會在全部工作完成前回傳；是否完成或失敗應以 Workflow 為準。

## 結構

- **Workflow：** End-to-end desired outcome 與 resource ownership。
- **Job：** 可重用、convergent 的 sub-goal。
- **Task：** 指派給一個 Runner 的 atomic、idempotent unit。
- **Runner：** Provisioner、Ansible 或 internal execution mechanism。

Wire model 仍保留部分 historical operation／step field names。Dashboard 與目前
`/api/v1/workflows` route 使用 canonical Workflow／Task language。

## Runtime topology

Temporal 是唯一 durable orchestration engine。可運作的 installation 需要：

- Temporal Server 與 PostgreSQL。
- `swallow-api worker`。
- 執行 Ansible Task 的 `swallow-api ansible-executor`。
- API 與其 MongoDB state。

不存在 in-process dispatcher fallback。

## 觀察工作

Workflow detail 會顯示：

- Overall state、intent、target resources、requester 與 timestamps。
- 依 execution structure 分組的 Job／Task。
- Task event、stdout/stderr log 與 artifact。
- State 允許時的 cancel、rerun 與 Task retry control。

Live Ansible event 可能早於 final Task state；terminal Workflow result 才是權威。

## Failure handling

1. 閱讀 failed Task 的 status reason 與 events。
2. 檢查 log／request ID，但不要暴露 credential。
3. 修正 external cause：provider availability、SSH known host、package source、
   runtime API 或 resource eligibility。
4. 支援時只 retry Task；需要重新評估整個 intent 時 rerun Workflow。

Expired／lost execution 可能變成 `indeterminate`；swallow 無法證明 external side
effect 是否發生，因此不會自動 retry。

## Cancellation

Cancellation 會記錄 intent，並要求 running orchestration 在安全邊界停止；無法撤銷
已完成的 external side effect。開始另一個 conflicting Workflow 前先檢查 resulting
Task states。

State transition 與 supported control 請見 active
[Workflows contract](../../../api-server/docs/development/api-contracts/api-server/workflows.md)。
