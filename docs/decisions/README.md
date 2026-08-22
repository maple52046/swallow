# 架構決策紀錄（ADR）

本目錄以輕量 ADR（Architecture Decision Record）記錄 swallow 重要且影響面廣的架構決策，
讓不熟悉脈絡的成員能快速理解「為什麼是這樣設計」。

> **與其他文件的關係**：domain 術語的權威定義在
> [`docs/development/glossaries/`](../development/glossaries/outline.md)；架構憲法在
> [`docs/development/architecture-spec.md`](../development/architecture-spec.md)；repository
> 結構契約在 [`docs/development/codebase-structure.md`](../development/codebase-structure.md)；
> 近期工作脈絡在 `docs/plans/`。ADR 只記錄 **決策本身**，不重述規則或實作細節。

## 何時新增 ADR

當一個決策滿足以下任一條件時，值得寫一筆 ADR：

- 影響多個 component 的邊界或互動方式。
- 有明顯的替代方案與取捨。
- 之後容易被質疑「為什麼不那樣做」。

反之，只影響單一 sub project 內部實作、且不改變 context 邊界或跨 component 契約的決策，
留在該 sub project 的文件即可，不需要 root ADR。

## 格式

每筆 ADR 用一個檔案，命名 `NNNN-short-title.md`（四位數流水號）。內容包含：

```md
# NNNN. 標題

- Status: Proposed | Accepted | Superseded（by NNNN）| Deprecated
- Date: YYYY-MM-DD

## Context
（決策的背景與問題）

## Decision
（實際決定）

## Alternatives considered
（考慮過的替代方案與否決原因）

## Consequences
（決策帶來的好處與代價）

## Current status
（目前實作狀態：Implemented / Partial / Planned）
```

> **重要**：不要為了補 ADR 而 **杜撰歷史理由**。若理由是由現有程式碼 / plan **推論** 而來、
> 而非當時明確記載，請在該筆標注「（inferred）」。

## 既有 ADR

| 文件 | 決定了什麼 |
| --- | --- |
| [`001-system-ownership-boundaries.md`](001-system-ownership-boundaries.md) | 哪個系統擁有哪些事實，以及 swallow 因此不得儲存或重建什麼 |
| [`002-server-identity.md`](002-server-identity.md) | server 如何跨站點與重裝維持身分，以及狀態為何是三個獨立軸而非單一值 |
| [`003-metrics-label-contract.md`](003-metrics-label-contract.md) | monitoring 拓撲，以及把 metrics 接回 server 的標籤集 |
| [`004-automation-via-awx.md`](004-automation-via-awx.md) | 長時間執行的 operation 由誰擁有、如何指定目標與觀察 |
| [`005-monorepo-consolidation.md`](005-monorepo-consolidation.md) | 平台從 gdcm submodule superproject 收斂為單一 swallow monorepo，並完成品牌改名 |

`001`–`004` 沿用先前的三位數命名，章節結構也與上方格式不同（Decision / Context /
Consequences / Rejected alternatives，沒有 Status 與 Date）。它們與本文件的格式對齊
尚未處理。`005` 起採用本文件定義的格式。

平台另有若干值得記錄的決策（例如 API contract 由 provider component 擁有），但這些理由
目前只存在於結構契約與實作中，尚未經確認。新增這些 ADR 時，請與知道當時脈絡的人確認，
或明確標注「（inferred）」。
