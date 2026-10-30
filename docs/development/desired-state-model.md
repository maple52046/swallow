# Desired-state 模型設計與規範

本文件是 swallow 管理受管資源的**上位模型**：哪些狀態由 swallow 擁有、provider 如何把它實現出來、
差異如何收斂、破壞性變更如何經 operator 核准，以及執行狀態與 store 必須遵守的規則。所有開發都必須遵守；
功能規格（[os-provisioning.md](os-provisioning.md)、[platform-deployment.md](platform-deployment.md)、
[software-deployment.md](software-deployment.md) 等）在此模型之下描述各功能的細節。

> **本文件不重述決策**：為什麼由 swallow 擁有 desired state、為什麼採混合收斂，見
> [decision 069](../decisions/069-swallow-owns-desired-state.md)。core、provider adapter 與擴充模組的邊界見
> [core-and-extensions.md](core-and-extensions.md)（[decision 070](../decisions/070-core-providers-and-extensions.md)）。

> **目標模型與現況**：本文件描述目標模型與規則，不描述遷移路線。尚未遷移的功能維持現行行為，現行行為
> 仍以各功能規格為準；某個功能遷移到本模型時，在同一個變更中更新它的規格。

---

## 1. 目的與適用範圍

- **讀者**：設計新資源、新欄位或新動作，修改既有功能的所有權或收斂行為，或審查這類變更的開發者與 AI agent。
- **必須依本模型的工作**：
  - 新功能：新的受管資源、新欄位、新的改變外部世界的動作。
  - 修改既有功能時，只要變更觸及**所有權**（某個欄位由誰寫入、誰是真相）或**收斂行為**（何時、如何改變
    外部世界），就要把該部分依本模型重做。只修錯誤、文字或不改所有權的行為，不需要遷移。
- **不在範圍內**：swallow 永不擁有的事實（§3.4）；單一功能的 Task 組成與 provider 細節（見各功能規格）；
  HTTP 路由與欄位（以 api-server 擁有的 API 契約為準）。

## 2. 核心概念

語言以 glossary 為準：[Desired State](glossaries/terms/desired-state.md)、
[Observed State](glossaries/terms/observed-state.md)、[Condition](glossaries/terms/condition.md)、
[Drift](glossaries/terms/drift.md)、[Convergence](glossaries/terms/convergence.md)、
[Change Plan](glossaries/terms/change-plan.md)、[Destructive Change](glossaries/terms/destructive-change.md)、
[Provider Interface](glossaries/terms/provider-interface.md)、[Provider Adapter](glossaries/terms/provider-adapter.md)。

- **Desired State（spec）**：operator 要某個受管資源成為的樣子。swallow 擁有，store 是唯一真相來源。
- **Observed State（status）**：provider 回報的實際樣子，帶來源（哪個 Integration）與觀察時間。
- **Derived**：由 desired 與 observed 推導出的值（例如 Server Type、Allocation State），可隨時重算。
- **generation 與 observedGeneration**：spec 每次改變，資源的 `generation` 加一；controller 依某一版 spec
  完成一輪收斂後，把該版寫入 `observedGeneration`。兩者相等表示「目前的 status 是針對最新 spec 的結果」。
- **Condition**：status 中描述資源狀況的一組具名判斷（例如是否已實現、是否 drift、是否需要處理）。
- **Drift**：observed state 在 swallow 沒有要求的情況下偏離了 desired state。
- **Convergence**：讓 observed state 趨近 desired state 的持續過程。
- **Controller**：負責一種資源的收斂迴圈；比對 desired 與 observed，決定要直接收斂、產生 Change Plan，
  還是回報 drift。
- **Provider Interface 與 Provider Adapter**：core 定義的、和某類外部系統溝通的介面（port），以及實作它的
  adapter（例如 MAAS 是 OS provisioning 的 adapter）。規則見 [core-and-extensions.md](core-and-extensions.md) §3。
- **Destructive Change**：實現它會造成資料遺失、讓資源離開服務、中斷 workload，或無法以套回舊 spec 復原的變更。
- **Change Plan**：破壞性變更在執行前的預覽，經 operator 核准後才執行。
- **Hold**：暫停某個資源的收斂；觀察與 drift 回報照常進行。

這裡的 Convergence 和既有的「reconcile」不同：現有的 provider reconcile 是把 provider inventory 同步進
swallow，屬於**觀察**（寫入 observed state）；Convergence 是依 desired state **改變外部世界**。

## 3. 所有權分類

### 3.1 每個欄位都必須分類

每個儲存的欄位恰好屬於以下一類，並在 domain 型別、API 契約與功能規格中可以看出它屬於哪一類（spec 或
status 的位置本身就是標示）。新功能在實作前，必須先列出每個欄位的類別。

| 類別 | 誰寫入 | 規則 |
| --- | --- | --- |
| desired | API（operator、dashboard、CLI），或擁有它的上層資源 | 唯一真相來源；寫入者必須唯一且可追溯 |
| observed | 對應 provider adapter 的觀察路徑 | 帶來源與觀察時間；永遠不是寫入外部系統的依據 |
| derived | 由 desired 與 observed 計算 | 可以快取，但必須能重算；不得成為另一個 desired 欄位的唯一來源 |

### 3.2 desired

- 只由 API 寫入，或由擁有它的上層資源寫入。例如 Platform 的 spec 決定其 Server 的角色；這時該欄位的寫入者
  是 Platform，Server 自己的 API 不得再寫它。
- secret 也是 desired：以封存的形式保存，只寫不讀（write-only），不進入 Workflow history 或 log。
- 不是每個 observed 欄位都有 desired 對應：硬體 inventory、GPU inventory、provider 自動計算的 tag 只有
  observed。

### 3.3 observed

- 只由 provider adapter 的觀察路徑寫入，帶來源 Integration 與觀察時間，沿用
  [Staleness](glossaries/terms/staleness.md) 的規則：provider 無法連線時，資料只是變舊，不會變成「已知是壞的」。
- 永遠不拿 observed 值寫回 provider，也不把 observed 值當成新的 desired（除非 operator 明確 adopt，見 §6.3）。

### 3.4 swallow 永不擁有的事實

以下事實由各自的系統擁有，swallow 即時讀取或深度連結，不保存、不收斂，也不產生 drift（與
[decision 001](../decisions/001-system-ownership-boundaries.md) 相同）：

- metrics（時間序列）、alert 規則的評估與 firing 狀態、dashboard。
- workload 的排程與佇列（Slurm、Kubernetes 的工作）。
- 受管 Platform 內部的資源：Kubernetes 物件、Slurm 的 partition、node 狀態與 job。
- 硬體的歷史紀錄（swallow 只觀察目前的硬體）。

## 4. Resource 模型

### 4.1 形狀

每個受管資源由以下部分組成：

- **identity**：swallow 核發的 id，不使用 hostname、IP 或 provider id 當身分。
- **spec**：desired 欄位，以及 `generation`。
- **status**：observed 與 derived 欄位、`observedGeneration`，以及 `conditions[]`。

### 4.2 Condition

每個 Condition 有 `type`、`status`（`true | false | unknown`）、`reason`（短代碼）、`message`（給人看的說明）、
`lastTransitionTime` 與 `observedGeneration`。所有資源共用以下 type；功能可以增加自己的 type，但不得改變
這些 type 的意義。值集以 glossary 的 [Condition](glossaries/terms/condition.md) 為準。

| type | `true` 的意思 | 常見的 `false`／`unknown` reason |
| --- | --- | --- |
| `realized` | observed state 已符合目前的 spec | `converging`、`capability_missing`、`awaiting_approval`、`failed`、`provider_unreachable` |
| `drifted` | 有 observed 欄位在 swallow 沒有要求的情況下偏離 spec | — |
| `requires_attention` | controller 已停止自動嘗試，需要 operator 處理 | — |
| `held` | 收斂被暫停（§6.4） | — |

`realized=false` 加上 reason `capability_missing` 表示 provider 沒有能力實現某個欄位；desired 值照樣保留。

### 4.3 現有資源的目標分類

下表是遷移時的起點，不是現行行為。每個資源遷移時，在其功能規格中逐欄定案；表中標「待定」的項目在遷移時
決定，不得在其他地方先行決定。

| 資源 | desired | observed | 備註 |
| --- | --- | --- | --- |
| Server | OS 部署意圖（OS Image、Deploy Target、網路意圖、Deployment Template 參照）、Zone／Pool placement、operator 指派的 tag、Boot Media 設定、Server Default User、Power Configuration | OS Provisioning State、硬體與 GPU inventory、位址、power 狀態、membership、health、provider 自動計算的 tag | 目前 tag 與 Power Configuration 在 capable provider 上由 provider 擁有；power on／off 是 desired power 狀態還是保留為動作：待定；Server Lock 改為 swallow 的 hold 還是維持 provider 擁有：待定 |
| Platform | type、拓樸、各 Server 的 Node Role、功能開關（GPU Operator、Omnistat、Device Metrics Exporter 等）與版本 | 叢集 membership、連線狀態、各功能的狀態讀回 | Platform Lifecycle State 改由 conditions 與 Workflow 推導；Platform Claim 是由 spec 的 target 推導的欄位，與 spec 在同一個 transaction 寫入 |
| Managed Software（Software Assignment） | kind、variant 或 role、版本 | 已套用的狀態與版本 | 現行設計已有 desired 與 last-applied，最接近本模型 |
| Zone／Pool | 存在與否、名稱、說明 | provisioner 中的實現狀態 | 現行設計已是 swallow 擁有、有能力時實現 |
| SSH Key | 公鑰與用途 | 各 provisioner 的同步狀態 | 現行設計已接近本模型 |
| OS Image | swallow 的命名與 `defaultUser` 等 metadata | provider 擁有的 artifact 本身 | 現行的 Provider Data Overlay 改為 desired metadata，overlay 概念隨遷移淘汰 |
| Deployment Template | 可重用的部署意圖 | — | 只有 desired；它是 Server desired state 的輸入，自己不被實現 |
| Provisioner 設定（HTTP Proxy、Subnet DNS） | 設定值 | provider 目前的值 | 目前是 provider 擁有、swallow write-through；遷移後外部的改動成為 drift |

## 5. 收斂模式

### 5.1 判斷準則

一個變更只要實現它會造成以下任一結果，就是**破壞性**：

1. 抹除或取代主機上的資料（重裝 OS、清除磁碟）。
2. 讓 Server 離開服務或離開 Platform（release、recover、uninstall、移除成員、變更 Node Role）。
3. 中斷正在執行的 workload（重開機或關閉使用中的主機、會重啟 Platform daemon 的重設定）。
4. 無法以套回舊 spec 的方式復原。

其他變更是**非破壞性**的。無法確定時歸為破壞性。功能規格可以把某個變更降級為非破壞性，但必須寫出理由；
降級是規格變更，要經過審查。

### 5.2 初版分類

| 非破壞性：controller 自動收斂 | 破壞性：先產生 Change Plan |
| --- | --- |
| tag、Zone／Pool placement、名稱與說明 | 在已部署的 Server 上重新部署 OS |
| Boot Media 設定（在下一次開機前套用） | Release、Recover |
| SSH Key 同步、Subnet DNS、HTTP Proxy | 建立 Platform（安裝到 target）、uninstall Platform |
| Power Configuration 的參數 | 變更 Node Role、從 Platform 移除 Server |
| 在 `ready` 的 Server 上部署 OS | 卸載 Managed Software |
| 安裝 Managed Software | 關閉使用中主機的電源 |

灰色地帶（Platform 與 Managed Software 的版本升級、為 Platform 加入新成員）在對應功能遷移時依 §5.1 定案。

### 5.3 非破壞性收斂

- controller 發現差異就收斂，不需要核准。
- 短的動作由 controller 直接經 provider interface 執行；長的動作（例如部署 OS、安裝軟體）以 Workflow 執行，
  Workflow 由 controller 建立，不需要核准。
- 收斂失敗時依 §7.5 退避，最後轉為 `requires_attention`。

### 5.4 Change Plan

- **內容**：target 資源；每個資源要做的動作（從目前的 observed 到 desired、是否破壞性、原因）；計算時依據的
  spec `generation` 與 observed 的觀察時間；預期影響（例如哪些 Server 會離開服務）；阻擋事項（preflight 失敗）。
- **狀態**：`pending_approval → approved → applied`，或結束於 `rejected`、`superseded`、`expired`。值集以 glossary
  的 [Change Plan](glossaries/terms/change-plan.md) 為準。
- **核准**：由有權限的 operator 核准整份 plan；第一版不支援部分核准。核准後 plan 內容不可變。
- **過時偵測**：apply 前重新計算。spec 的 generation 已改變，或 observed 的變化讓動作清單不同時，plan 轉為
  `superseded`，要重新核准；不影響動作清單的觀察變化不會讓 plan 失效。
- **到期**：plan 有有限的有效期間，因為它依據的 observed 會變舊。
- **一個資源同時只有一份未完成的 plan**：新的 spec 變更會讓尚未 apply 的 plan 轉為 `superseded`。
- **apply**：產生一個 Workflow，Workflow 記錄 plan id；plan 記錄 Workflow id。

## 6. Drift 政策

### 6.1 偵測與回報

- controller 每次比對時檢查 drift：observed 曾經符合 spec（或 swallow 沒有進行中的收斂），之後卻在 swallow
  沒有要求的情況下改變。
- drift 以 `drifted` condition（列出偏離的欄位）與 timeline event（偵測到、消失）回報。
- 只有 observed 的事實（例如硬體）改變不是 drift，而是新的觀察；它可能影響其他 condition（例如部署資格）。

### 6.2 預設不自動修正

- drift 只回報，不自動修正。
- 某個欄位可以在其功能規格中明確宣告為 auto-correct，條件是修正它屬於非破壞性，而且規格寫出理由。

### 6.3 外部操作

有人直接在 provider 或 Platform 上操作（例如 MAAS UI、`kubectl`、手動 SSH）時，swallow 不會默默接受新值，
而是回報 drift。operator 有兩個選擇：

- **adopt**：把 spec 改成目前的 observed 值。這是一般的 spec 寫入，走同樣的 API 與審計。
- **reapply**：重新實現 desired state。非破壞性時直接收斂，破壞性時產生 Change Plan。

swallow 永不擁有的事實（§3.4）不會產生 drift。

### 6.4 Hold

- 以下情況暫停收斂：Server 在 Rescue Mode、Server 被 lock、operator 明確 hold、資源被進行中的 Workflow
  以 lease 佔用。
- hold 期間觀察與 drift 回報照常進行；controller 不執行收斂動作。Change Plan 可以產生，但要等 hold 解除才能
  apply；明確允許的回復動作（例如對 rescue 中的 Server 做 Recover）除外。
- hold 以 `held` condition 與原因呈現。

## 7. Controller 規範

### 7.1 一種資源一個 controller

controller 框架由 core 提供；資源的 spec schema 可以由擴充模組定義（見
[core-and-extensions.md](core-and-extensions.md) §6），但收斂迴圈的規則一律相同。

### 7.2 level-triggered 與 idempotent

- controller 依**目前的** desired 與 observed 決定動作，不依事件內容；事件只負責喚醒它。漏掉事件不得遺失工作。
- 同樣的輸入執行兩次，結果與執行一次相同。

### 7.3 只透過 provider interface 改變外部世界

controller 不直接呼叫廠商 SDK 或 API，也不知道是哪個 adapter 在實現它（見
[core-and-extensions.md](core-and-extensions.md) §3）。

### 7.4 每個欄位只有一個寫入者

- spec：API 或擁有它的上層資源（§3.2）。
- observed：provider adapter 的觀察路徑。
- conditions 與 `observedGeneration`：該資源的 controller。

兩條路徑寫同一個欄位是設計錯誤，不得以「後寫者勝」或修復 sweep 處理。

### 7.5 觸發、退避與失敗

- **觸發來源**：store 的 change feed（spec 與 observed 的變化）、定期 resync，以及 operator 要求的立即收斂。
- **退避**：失敗時採有上限的指數退避。
- **放棄**：超過嘗試次數或時間上限時，設定 `requires_attention` condition 與正規化的錯誤，停止自動嘗試，
  直到 spec 改變或 operator 要求重試。
- **並行**：controller 不對被進行中的 Workflow 以 lease 佔用的資源動作（§6.4）。

### 7.6 執行位置

controller 不在 API process 執行。同一種 controller 同一時間只有一個 active instance，以 store 中的 lease 保證。

## 8. Provider 在收斂中的角色

介面與 adapter 的一般規則見 [core-and-extensions.md](core-and-extensions.md) §3。在收斂中：

- adapter 宣告自己的 capability。有能力就實現 desired state；沒有能力時，controller 回報 `realized=false`、
  reason `capability_missing`，desired 值照樣保留。swallow 不再為沒有能力的 provider 建立同形狀的 fallback
  （取代 [decision 031](../decisions/031-provider-capability-first-with-swallow-owned-fallback.md)）。
- adapter 只回報 observed state，永遠不寫 desired state，也不決定政策。
- adapter 把錯誤分成暫時性、永久性與缺少能力三類，由 controller 決定退避或放棄。
- 目前以 write-through 方式設定的 provider 設定（HTTP Proxy、Subnet DNS、Power Configuration）遷移後是 swallow
  的 desired 欄位；從 provider 讀回的值是 observed，兩者不同時就是 drift。

## 9. 執行與 Workflow executor

- **Workflow 是 swallow 擁有的執行狀態**。Workflow 與其 Task 的狀態、attempt 與結果以 store 中的紀錄為唯一
  真相。Workflow 的語意——Task DAG、attempt、重試、取消、fencing、等待 operator——由 core 定義（目前的行為見
  [workflow-execution.md](workflow-execution.md)）。
- **Workflow executor 是 provider interface**，目前的 adapter 是 Temporal。executor 負責執行與 durable 等待；
  Temporal 的型別與概念（workflow context、activity options、history event）不得越過 adapter。
- **狀態轉換只有一個寫入者**：core 的 Workflow service。executor adapter 的 activity 呼叫它回報進度；API 只寫入
  intent 與命令（例如取消要求），不直接改寫狀態。
- **受理與啟動**：API 在同一個 transaction 中寫入 Workflow intent 與相關的 domain 變更；把 Workflow 交給 executor
  的動作由 store 的 outbox 或 change feed 驅動，不靠多個 process 輪詢。
- **execution 遺失**：executor 沒有某個未結束 Workflow 的 execution 時，swallow 依 store 中的狀態重新派發；已成功的
  Task 因為是 idempotent 的 ensure 且結果已記錄，不會重做。
- **來源**：Workflow 由三種方式產生：operator 的明確要求、controller 的非破壞性收斂（§5.3）、核准的 Change Plan
  （§5.4）。

## 10. Store 規範

### 10.1 規則以 port 表達

store 規則與資料庫引擎無關：

- **transaction port**：
  - 跨文件或跨資料表的不變式必須在同一個 transaction 內完成，例如 Platform 與其 Server 上的 Platform Claim。
  - transaction 的 callback 不得有外部寫入（provider 呼叫、HTTP、檔案），而且必須可以重跑，因為資料庫可能在
    暫時性錯誤時重試整個 transaction。
  - 事件在 commit 之後才發布；transaction 失敗時不得有任何事件送出。
- **change feed port**：store 的變化通知，是 controller 與事件流（例如 SSE）的觸發來源，讓不同 process 的寫入
  都能被看到。

### 10.2 目前的實作

- MongoDB single-node replica set，以多文件 transaction 實作 transaction port，以 change stream 實作 change feed port。
- 所有 process 在啟動時檢查 store 支援 transaction 與 change feed，不支援時拒絕啟動。

### 10.3 禁止

- 不得以補償（先寫再刪）或修復 sweep 取代 transaction。
- 不得讓資料庫特有的型別或語意越過 infra 層，例如 `bson` 型別、Mongo 的 update operator、change stream 的
  resume token。日後遷移到 PostgreSQL 時，應該只需重寫 infra adapter。

### 10.4 測試

- domain 與 application 的 unit test 以 fake transaction 驗證 use case 確實在 transaction 內寫入，以及事件只在
  成功後發布。記憶體 fake 無法 rollback，所以「失敗時沒有任何寫入」由下面的整合測試驗證。
- infra 的整合測試在 replica set 上執行。

## 11. API 與 UI 慣例

### 11.1 API

- desired state 以宣告式資源表達：`PUT`（整份）或 `PATCH`（部分）寫入 spec，回應同時帶 spec 與 status。
- 寫入 spec 可以帶預期的 `generation`；不符時回 `409 conflict`，避免覆蓋別人的變更。
- 會造成破壞性變更的 spec 寫入，回應 `202 Accepted` 與產生的 Change Plan，而不是直接執行。
- Change Plan 有自己的讀取、核准與拒絕 API。
- action endpoint 只保留給沒有自然 desired state 的動作，例如測試連線、取消或重試 Workflow、要求立即觀察。
  為了方便，API 可以提供「寫入 spec 並產生 plan」的單一呼叫，但它的效果必須等同於寫入 spec。
- 既有資源在遷移時調整契約，依 [api-contracts.md](api-contracts.md) 的流程處理相容期間。

### 11.2 dashboard 與 CLI

- 呈現 desired 與 observed 的差異、conditions，以及 drift 和 adopt／reapply 的選項。
- 待核准的 Change Plan 有核准畫面，破壞性動作與預期影響必須清楚標示。
- CLI 提供相同的能力，一律經由 API。

## 12. 新增一種 resource 的檢查清單

- [ ] 列出每個欄位，並標明 desired、observed 或 derived（§3）。
- [ ] 確認沒有任何欄位屬於 swallow 永不擁有的事實（§3.4）。
- [ ] desired 欄位的寫入者唯一（API 或哪個上層資源）。
- [ ] observed 欄位標明來自哪個 provider interface，帶來源與觀察時間。
- [ ] 定義 spec 與 status 的形狀、`generation` 與 conditions（§4）。
- [ ] 依 §5.1 把每一種變更分類為破壞性或非破壞性；灰色地帶寫出理由。
- [ ] 定義哪些欄位（如果有）是 auto-correct，並寫出理由（§6.2）。
- [ ] 定義 hold 的情況（§6.4）。
- [ ] 需要的 provider capability 已在 provider interface 上宣告，缺少時回報 `capability_missing`（§8）。
- [ ] 跨文件的不變式在一個 transaction 內完成，沒有補償或修復 sweep（§10）。
- [ ] API 契約、glossary 與 dashboard 的呈現已依 §11 更新。
- [ ] 新的程式碼放在 core、adapter 或模組的哪一處已依 [core-and-extensions.md](core-and-extensions.md) §1 判斷。

## 13. 設計理由與相關文件

- 為什麼由 swallow 擁有 desired state、為什麼採混合收斂、為什麼 Temporal 只負責執行：
  [decision 069](../decisions/069-swallow-owns-desired-state.md)。
- 被取代的 capability-first fallback：[decision 031](../decisions/031-provider-capability-first-with-swallow-owned-fallback.md)；
  仍然有效的「swallow 永不擁有」清單：[decision 001](../decisions/001-system-ownership-boundaries.md)。
- core、provider adapter 與擴充模組：[core-and-extensions.md](core-and-extensions.md)、
  [decision 070](../decisions/070-core-providers-and-extensions.md)。
- 目前的 Workflow 執行行為：[workflow-execution.md](workflow-execution.md)。
