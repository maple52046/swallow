# 020. Durable Operation 失敗回復：以新 execution 重跑，不刪 Platform

- Status: Accepted
- Date: 2026-09-07

## Context

Platform 部署是 Temporal 編排的多步驟 Operation（[ADR 016](016-temporal-operation-orchestration.md)、
[ADR 017](017-workflow-job-task-runner-model.md)）。一個部署走到 `requires_attention`（等 operator
對某個可重試的失敗 Step 重試）時，若主機重開機，等待迴圈 `awaitRetryStep` 內每 `leaseDuration/3` 的
續租活動會失敗，舊碼直接回傳錯誤，上層 `finishCanceled` 把 Operation 打成終態 `canceled`。

之後這個 Operation 永遠無法恢復：

- Temporal Workflow ID 固定為 `swallow-operation/{id}` 且採 `REJECT_DUPLICATE`；
- Starter 只啟動 `startState=pending` 的紀錄，從不重啟已 `started` 者；
- v3 的 repair 只做 per-step 重試 signal，execution 一旦消失就回 409。

結果 operator 只能「刪 Platform → 重新部署」。這在 v1 從頭部署尚可接受，但一旦未來加上部署後的驗證
Step，「驗證機制本身出錯」就會逼使用者刪掉整個 Platform，無法接受。需求是：Operation 要能撐過主機
重開機，且回復不得刪 Platform。

## Decision

沿用既有 uninstall 失敗重試已採用的「**新 Operation + `RetryOfOperationId` lineage**」模式，而不是對抗
Temporal 的固定 WorkflowID 與 reject-duplicate。分三層：

**1. Rerun 原語（免刪 Platform 的回復）。** 新增 `WorkflowService.Rerun`：把失敗/終態/execution 已消失
的 v3 Operation 以**全新 execution** 重跑。它 clone 原 Operation 的 Steps，**保留已 `succeeded`/`skipped`
的 Step**（fresh execution 的 ready/complete 掃描把它們當作已滿足的相依，故不重跑其副作用，尤其
`provision-os` 不會重炸已部署節點），其餘 reset 為 `pending`；透過 secret repo 的 `CloneForOperation`
把封存 secrets 複製到新 Operation 並重映射每個 Step 的 `SecretRefs`；設 `RetryOfOperationId = 原 ID`、
配發新 UUID（因此是新 WorkflowID），交由 Starter 啟動。原 Operation 若非終態（`requires_attention`），
先 best-effort 取消其（可能已死的）execution 再把投影強制轉為 `canceled`，讓 targets 立即釋出，新
Operation 才能宣告同一批 targets。平台 lifecycle 以 `requestedAt` 最新的 deploy Operation 為準，新
Operation 自然接管平台狀態；原 Operation 留作診斷。對外為 `POST /workflows/{id}/rerun`。

**2. 等待期撐過短暫中斷。** `awaitRetryStep` 的續租失敗改為記 log 後繼續等待，不再結束 Operation；唯一
離開條件是收到重試 signal 或 `ctx` 被真正取消。

**3. Lost-execution reconciler。** 週期性 sweep 偵測「Mongo 仍為前進中狀態（pending/waiting_dependency/
running/waiting_external）、`startState=started`、但 Temporal execution 已 `Describe` 為關閉/NotFound」
的 Operation，將其標為 `requires_attention`（lifecycle 顯示 `deploy_failed` 可 repair）。**只標記、不
自動 rerun**；rerun 一律是明確的 operator 動作。

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
  Platform；未來加上驗證 Step 後，驗證失敗同樣可 repair 而非砍平台。
- 對 k0s 與 Slurm 皆適用：rerun 是平台型別無關的原語，clone 沿用原 Operation 凍結的 Steps 與意圖。
- **租約弱化取捨**：等待期續租失敗後繼續等待，代表 Operation 可能短暫沒有有效 lease。這是安全的，因為
  parked 期間沒有 host 變更，且任何 Step 在真正產生副作用前都會重新 validate/acquire lease（fencing
  token）。若 lease 已被他人取得，重試該 Step 會因 fenced 而失敗，Operation 仍可用 rerun 以全新 lease
  回復。
- **replay 安全**：`awaitRetryStep` 的改動只影響「未來的續租失敗事件」所走的分支；任何已記錄續租失敗的
  history 早已跑完舊的結束路徑而不再是 open，故沒有 open history 會 replay 出不同命令，無需 workflow
  版本切換。
- 新增 Mongo 索引 `operation_v3_recovery (schemaVersion, startState, status)` 支撐 reconciler sweep，
  使其跳過大量「已 started 且終態」的歷史紀錄。
- reconciler 與 Starter 同時跑在 API 與 worker 兩個 process；每次寫入都是 idempotent 的狀態更新，重複
  sweep 安全。

## Current status

Implemented，並於 dev lab 驗證。Rerun 原語、`POST /workflows/{id}/rerun`、`awaitRetryStep` 硬化、
lost-execution reconciler，以及 dashboard Repair 的 rerun fallback 均已實作；一個因主機重開機而卡住的
HA Slurm 部署以 rerun 回復並完成，全程未刪 Platform。

## Related

- [ADR 016](016-temporal-operation-orchestration.md)、[ADR 017](017-workflow-job-task-runner-model.md)
  — Temporal 編排與 Workflow/Job/Task/Runner 模型與 fencing lease。
- [ADR 010](010-cluster-lifecycle-actions.md) — Uninstall/Delete 的界線；本 ADR 沿用其「新 Operation +
  lineage」的失敗重試模式並延伸到 deploy 回復。
- [`docs/development/platform-deployment.md`](../development/platform-deployment.md) §4.7 — 冪等、失敗、
  重試與回復語意。
