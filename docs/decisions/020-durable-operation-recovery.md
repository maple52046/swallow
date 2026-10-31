# 020. Durable Operation 失敗回復：以新 execution 重跑，不刪 Platform

- Status: Accepted; the mark-only reconciler and the parked lease renewal superseded by [ADR 072](072-store-driven-workflow-execution.md) (a lost execution is resumed by re-dispatch; ownership needs no renewal); rerun unchanged
- Date: 2026-09-07
- 現行規格：[workflow-execution.md](../development/workflow-execution.md) §6 `requires_attention` 停等、§7 Rerun：以新 Workflow 收斂、§10 派發與接續

## Context

Platform 部署是 Temporal 編排的多步驟 Operation（[ADR 016](016-temporal-operation-orchestration.md)、
[ADR 017](017-workflow-job-task-runner-model.md)）。一個部署走到 `requires_attention`（等 operator
對某個可重試的失敗 Step 重試）時，若主機重開機，等待迴圈 `awaitRetryStep` 內的續租活動會失敗，舊碼直接
回傳錯誤，上層 `finishCanceled` 把 Operation 打成終態 `canceled`。

之後這個 Operation 永遠無法恢復：

- Temporal Workflow ID 固定為 `swallow-operation/{id}` 且採 `REJECT_DUPLICATE`；
- Starter 只啟動 `startState=pending` 的紀錄，從不重啟已 `started` 者；
- v3 的 repair 只做 per-step 重試 signal，execution 一旦消失就回 409。

結果 operator 只能「刪 Platform → 重新部署」。這在 v1 從頭部署尚可接受，但一旦未來加上部署後的驗證
Step，「驗證機制本身出錯」就會逼使用者刪掉整個 Platform，無法接受。需求是：Operation 要能撐過主機
重開機，且回復不得刪 Platform。

## Decision

沿用既有 uninstall 失敗重試已採用的「**新 Operation + `RetryOfOperationId` lineage**」模式
（[ADR 010](010-cluster-lifecycle-actions.md)），而不是對抗 Temporal 的固定 WorkflowID 與 reject-duplicate。
分三層：

1. **Rerun 原語（免刪 Platform 的回復）**：把失敗、終態或 execution 已消失的 Operation 以全新 execution
   （新 ID，因此是新 WorkflowID）重跑同一意圖，`RetryOfOperationId` 指回原 Operation，封存 secrets 複製到新
   Operation。原 Operation 若非終態，先轉為終態讓 targets 釋出，並留作診斷；平台 lifecycle 跟隨較新的
   Operation。對外為 `POST /workflows/{id}/rerun`。原決定保留已成功的 Step、不重跑其副作用（尤其不重新部署
   已部署的節點）；這條沿用規則後由 [ADR 060](060-cancel-and-rerun-converge.md) 改為收斂式。
2. **等待期撐過短暫中斷**：等待 operator 重試時的續租失敗只記 log 並繼續等待；唯一離開條件是收到重試
   signal 或真正的取消。
3. **Lost-execution reconciler**：週期性偵測 Mongo 仍在前進、但 Temporal execution 已關閉或不存在的
   Operation，將其標為 `requires_attention`（lifecycle 顯示 `deploy_failed` 可 repair）。**只標記、不自動
   rerun**；rerun 一律是明確的 operator 動作。

## Alternatives considered

- **同一個 Operation 換新 WorkflowID / 重設 `startState=pending` 再讓 Starter 重跑：** 否決——要嘛破壞
  「一個 Operation ↔ 一個 WorkflowID（終身）」的不變式，要嘛得放寬 reject-duplicate，兩者都會侵蝕
  Starter 「只前進、duplicate-start 安全」的簡單性；且仍無法乾淨處理 targets 釋出與 lineage。新 Operation
  + `RetryOfOperationId` 已是 uninstall 失敗重試在用的既有模式，重用它一致且風險低。
- **reconciler 直接自動 rerun：** 否決——回復可能重跑具副作用的 provisioning，應由 operator 明確決定；
  自動化會把「暫時性中斷」與「真正需要人介入」混為一談。reconciler 只負責讓卡住的 Operation 變成可
  repair 的可視狀態。
- **reconciler 把遺失的 Operation 直接標為 `canceled`（終態）而非 `requires_attention`：** 否決——終態會
  失去「這是待處理、可回復」的語意；rerun 本身會在需要時才把原 Operation 轉終態以釋出 targets。
- **提高續租 activity 的重試次數而非改為繼續等待：** 否決——任何有上限的重試都撐不過多分鐘的重開機；
  「失敗即繼續等待」才是真正耐受中斷的作法。

## Consequences

- 失敗、被取消、或 execution 遺失的部署都能在**同一個 Platform** 上以新 execution 回復，不需刪除
  Platform；未來加上驗證 Step 後，驗證失敗同樣可 repair 而非砍平台。rerun 是平台型別無關的原語，k0s 與
  Slurm 皆適用。
- **租約弱化取捨**：等待期續租失敗後繼續等待，代表 Operation 可能短暫沒有有效 lease。這是安全的，因為
  parked 期間沒有 host 變更，且任何 Step 在真正產生副作用前都會重新 validate/acquire lease（fencing
  token）。若 lease 已被他人取得，重試該 Step 會因 fenced 而失敗，Operation 仍可用 rerun 以全新 lease
  回復。
- **replay 安全**：改動只影響「未來的續租失敗事件」所走的分支；已記錄續租失敗的 history 早已結束，不會
  replay 出不同命令，無需 workflow 版本切換。
- reconciler 需要專用的 Mongo 索引，才能跳過大量已結束的歷史紀錄；它與 Starter 同時跑在 API 與 worker
  兩個 process，每次寫入都是 idempotent，重複 sweep 安全。
