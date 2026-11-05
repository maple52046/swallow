# Workflow

[English](../../../docs/en/guides/workflows.md) · [文件首頁](../README.md)

Workflow 是 user intent 與 progress 的 durable record。建立 OS deployment、
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

swallow 的 store 保存每個 Workflow 的計畫與狀態；Temporal 是負責 durable 執行的 Workflow executor。可運作的
installation 需要：

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

playbook 執行中 Ansible executor 遺失時，Task 停在 `requires_attention`，原因為
`executor_outcome_unknown`。swallow 無法證明 playbook 的變更是否已發生，因此不會自動重跑；確認主機狀態後再重試
該 Task。

## Cancellation

Cancellation 會記錄 intent，並要求 running orchestration 在安全邊界停止；無法撤銷
已完成的 external side effect。開始另一個 conflicting Workflow 前先檢查 resulting
Task states。

取消不會改變已經結束的 OS 部署：已部署完成的 Server 保留結果，失敗的也一樣。取消若中斷了
正在進行的 OS 部署，swallow 會在 Workflow 結束前中止它，並把該 Server 收回 Ready（保留
static IP 綁定、不清磁碟）。該 Task 的時間軸會說明 Server 是否已收回，或為什麼保持原狀；
無法收回的 Server 可以用 Recover 處理。

## Change Plan

破壞性變更（例如 uninstall Platform）會經過一份 Change Plan：列出 swallow 要做的每個動作、哪些是破壞性的、
預期影響，以及阻擋事項。你在 dashboard 確認的對話框會顯示同樣的影響，確認就是核准這份 plan，所以變更會
立即開始；CLI 加上 `--approve` 時也一樣。plan 會留在 **Change Plans** 頁面，記錄決定了什麼、由誰決定，以及
由哪個 Workflow 執行。

以下情況 plan 會在 **Change Plans** 等 admin 整份核准或拒絕（在該頁，或以 `swallow plans approve`／
`swallow plans reject`）：送出時沒有核准（例如 CLI 沒有加 `--approve`）、有阻擋事項、由 swallow 自己提出，
或安全開關打開時。每份等待中的 plan 都會說明它為什麼在等。核准後 swallow 會再檢查一次並啟動 Workflow；
期間若變更又被修改，plan 會變成 superseded；若 plan 要做的事已經不同，會有一份新的 plan 等人核准。沒有人
決定的 plan 在 24 小時後到期。

頁面上方的 **Approval of confirmed changes** 卡片就是安全開關。打開 **Require a separate approval for every
Change Plan** 後，所有破壞性變更都會在這裡等待，即使是你確認過的；關掉只影響之後送出的變更。CLI 以
`swallow plans policy` 讀取與設定它。

頁面以名稱顯示 plan 提到的每個 Platform 與 Server，旁邊附上它的 ID，plan 的 ID 也有複製按鈕。plan 已核准時，
頁面會自動重新讀取，直到 plan 套用或被 superseded。詳見
[Change Plans contract](../../../api-server/docs/development/api-contracts/api-server/change-plans.md)。

State transition 與 supported control 請見 active
[Workflows contract](../../../api-server/docs/development/api-contracts/api-server/workflows.md)。
