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
- 所選 Task 的失敗摘要或等待通知，接著是它的 Task event、stdout/stderr log 與 artifact。
- Workflow 與 Task 狀態變化的時間軸（timeline）。
- State 允許時的 cancel、rerun 與 Task retry control。

頁面會選取第一個 running、failed 或停在 `requires_attention` 的 Task；有 error 的 Task 會開在
**Stderr** 分頁。Workflow 仍在進行時，頁面每隔幾秒重新讀取一次。

等待 swallow 以外事物的 running Task 可以回報它觀察到的狀態。這時 Task 會顯示一則標題為
**Waiting:** 加上最新觀察的通知，並列出已等待的時間與預算（budget）的比較、預算還剩多少、已觀察幾次，
以及最後一次觀察的時間；沒有預算的等待只顯示已等待的時間。Steps 表格中該 Task 的 **Reason** 欄位
顯示同一個觀察。這些數字是最後一次觀察時的值，不是持續走動的時鐘。Runner 不回報等待狀態的 Task
不會顯示這則通知；attempt 結束時通知就會消失。

Live Ansible event 與等待通知可能早於 final Task state，兩者都只供參考；terminal Workflow result 才是權威。

## Failure handling

失敗或停在 `requires_attention` 的 Task，會在分頁上方顯示失敗摘要，依你需要的順序呈現：

- 發生了什麼：error message 的第一行，作為摘要的標題（完整的失敗輸出在 **Stderr** 分頁）。
- **Affected**：受影響的 Server 或 Platform，連到它的頁面；Runner 知道是 Task 的哪個 target 時才顯示。
- **Next step**：下一步該做什麼；Runner 有記錄時才顯示。
- error code，以及有 stage 時的 stage。

**Show diagnostic details** 會展開摘要背後的證據：failure class 與它的意思、失敗的 operation 與
endpoint、放棄的那次等待嘗試的次數與它相對預算跑了多久、它最後的觀察、保留換行的 cause，以及 provider
在失敗前後記錄的 target 事件。只列出 Runner 記錄到的項目。等待放棄時，它最後的觀察與數字會從等待通知
移到這裡。swallow 儲存失敗前，會把這些文字中的 secret 遮蔽為 `[REDACTED]`，其餘的 cause 照樣保留；
即使如此，仍應把這些細節視為管理者資訊。在 swallow 保存這些細節之前記錄的 error，或由不記錄它們的
Runner 產生的 error，只顯示 message 與 code，沒有 **Show diagnostic details**。

在改動任何東西之前，failure class 告訴你重試是否值得：

- **Transient**：狀況預期會自行消失，例如主機仍在開機，或 API 短暫無法連線。不做任何變更就重試 Task
  也可能成功。
- **Permanent**：在某些東西改變之前會一再發生，例如 credential 被拒絕、image 不存在或輸入無效。先修正
  原因；不做變更的重試會再次失敗。
- **Unknown**：swallow 無法分類這個失敗，包括結果無法證明的 attempt。重試前先檢查 target。

failure class 不決定能不能重試：只有 error 為 retryable 時，Task 才會出現 retry 按鈕，與 class 無關。

Task 只保留最新的 error，所以重試之後，較早的 attempt 要在時間軸上看。記錄某次 attempt 失敗的事件會
顯示 attempt 編號、error code、failure class，以及有 endpoint 時的 endpoint，下面接著該次 attempt 的
message 第一行。
在 swallow 保存 failure class 之前記錄的事件沒有 failure class。

處理失敗：

1. 閱讀 failed Task 的失敗摘要；需要證據時，展開它的 diagnostic details，並查看 **Stderr** 分頁與它的
   events。
2. 依 failure class 決定先重試，還是先修正原因。
3. 檢查 log／request ID，但不要暴露 credential。
4. 修正 external cause：provider availability、SSH known host、package source、
   runtime API 或 resource eligibility。
5. 支援時只 retry Task；需要重新評估整個 intent 時 rerun Workflow。

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
立即開始；CLI 加上 `--approve` 時也一樣。plan 會留在 **Activity → Change Plans** 分頁，記錄決定了什麼、由誰決定，以及
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

State transition、supported control，以及 Task 失敗細節與等待通知的每個欄位，請見 active
[Workflows contract](../../../api-server/docs/development/api-contracts/api-server/workflows.md)。
