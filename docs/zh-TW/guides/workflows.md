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

Rerun 會讓 intent 收斂到它描述的目標狀態。每個 ensure Task 都會重跑，並先檢查目前的實際
狀態：已用指定 image 部署且 SSH 可連的 Server 幾秒內完成，並更新它的 deployment 結果；
failed 的 Server 會先收回 Ready 再重新部署。Platform 的設定 playbook 也會重跑，所需時間與平常相同。

Expired／lost execution 可能變成 `indeterminate`；swallow 無法證明 external side
effect 是否發生，因此不會自動 retry。

## Cancellation

Cancellation 會記錄 intent，並要求 running orchestration 在安全邊界停止；無法撤銷
已完成的 external side effect。開始另一個 conflicting Workflow 前先檢查 resulting
Task states。

取消不會改變已經結束的 OS 部署：已部署完成的 Server 保留結果，失敗的也一樣。取消若中斷了
正在進行的 OS 部署，swallow 會在 Workflow 結束前中止它，並把該 Server 收回 Ready（保留
static IP 綁定、不清磁碟）。該 Task 的時間軸會說明 Server 是否已收回，或為什麼保持原狀；
無法收回的 Server 可以用 Recover 處理。

State transition 與 supported control 請見 active
[Workflows contract](../../../api-server/docs/development/api-contracts/api-server/workflows.md)。
