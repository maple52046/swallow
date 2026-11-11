# 架構決策紀錄（ADR）

本目錄以輕量 ADR（Architecture Decision Record）記錄 swallow 重要且影響面廣的架構決策，
讓不熟悉脈絡的成員能快速理解「為什麼是這樣設計」。

> **與其他文件的關係**：domain 術語的權威定義在
> [`docs/development/glossaries/`](../development/glossaries/outline.md)；架構憲法在
> [`docs/development/architecture-spec.md`](../development/architecture-spec.md)；repository
> 結構契約在 [`docs/development/codebase-structure.md`](../development/codebase-structure.md)；
> 現行行為在 `docs/development/` 的設計規格；近期工作脈絡在 `docs/plans/`。ADR 只記錄
> **決策本身**，不重述規則或實作細節。

## 何時新增 ADR

ADR 只記錄會約束未來工作的決策。符合以下任一條件才寫：

- 改變 component 邊界、跨 component 的互動方式，或某個事實由誰擁有。
- 建立之後的功能都必須遵守的規則（例如 [069](069-swallow-owns-desired-state.md)、
  [064](064-upstream-patches-are-the-exception.md)）。
- 引入、更換或否決重大的技術依賴或 runtime 拓撲（例如 [072](072-store-driven-workflow-execution.md)、
  [040](040-single-vm-production-installation.md)）。

決議改變時更新原本那份 ADR（見下方「ADR 的更新」），不另開一份去取代它；只有新的問題才新增 ADR。
單一功能的設計——包括它的參數、流程與否決過的方案——寫在 `docs/development/` 對應規格的「設計理由」，
不寫 ADR。判斷不了時這樣想：三個月後做另一個功能的人也必須遵守它，就是 ADR；只有修改這個功能的人
需要知道，就屬於規格。只影響單一 component 內部實作的決策，留在該 component 的文件。

## 引用方式

- 文件與程式註解以「decision NNN」引用 ADR，不寫「ADR NNN」「ADR-NNN」或只有編號的連結。每份文件第一次
  引用某份 ADR 時附上標題並連結，例如「[decision 069](069-swallow-owns-desired-state.md)（swallow owns desired
  state）」；之後只寫「decision 069」。下方的索引不受此限。
- ADR、現行規格與程式註解不使用 AR-xx、DF-xx 這類架構檢視的追蹤編號，改寫成自足的說明。

## ADR 與規格的分工

- ADR 回答「為什麼」：背景、現行的決議、否決的方案與取捨。正文永遠描述現行的決議。
- 「現在怎麼運作」寫在規格（`docs/development/` 的設計文件、glossary、API 契約）。行為改變只更新規格；
  決議本身改變時才更新 ADR。

## ADR 的更新

- 同一個問題只有一份 ADR。決議改變時改寫那份 ADR 的正文，讓它描述新的決議，並在最後的修訂紀錄加一行。
- 修訂紀錄是 ADR 的最後一節（英文 ADR 用 `## Revision history`，中文 ADR 用 `## 修訂紀錄`），每次修訂一行，
  只增加、不刪除：

  ```md
  - 2026-11-02（commit abc1234）：一句話說明改了什麼、為什麼。
  ```

  commit 是實作或觸發這次修訂的 commit；ADR 與實作在同一個 commit 時無法事先知道 hash，就只寫日期。不寫成
  段落，細節留在 git 歷史。
- 讀 ADR 時讀 Context 與 Decision，需要時再讀 Alternatives considered 與 Consequences；除非任務與決議的演變有關，
  不讀修訂紀錄。
- 2026-11-02 起，修訂同一個問題的 ADR 已合併成一份，沿用現行那份的編號；被併入的編號列在下方「已併入的編號」，
  引用一律寫現行的編號。

## 格式

每筆 ADR 用一個檔案，命名 `NNN-short-title.md`（三位數流水號）。內容包含：

```md
# NNN. 標題

- Status: Proposed | Accepted | Deprecated
- Date: YYYY-MM-DD（第一次 Accepted 的日期）
- 現行規格：`docs/development/<spec>.md` §<章節>（若決策的現行行為由規格描述）

## Context
（決策當時的背景與問題）

## Decision
（實際決定；只有當數值或路徑「就是」決策時才寫進來）

## Alternatives considered
（考慮過的替代方案與否決原因）

## Consequences
（決策帶來的好處與代價）

## Revision history
- YYYY-MM-DD（commit abc1234）：一句話說明改了什麼。
```

> **重要**：不要為了補 ADR 而 **杜撰歷史理由**。若理由是由現有程式碼 / plan **推論** 而來、
> 而非當時明確記載，請在該筆標注「（inferred）」。

## 既有 ADR

「併入的舊編號」列出 2026-11-02 合併進這份的 ADR；細節與現行規則見各 ADR 指向的規格。

| ADR | 決定了什麼 | 併入的舊編號 |
| --- | --- | --- |
| [002](002-server-identity.md) | Server 以 opaque identity 跨站點與重裝維持身分，狀態分為三個獨立軸 | — |
| [003](003-metrics-label-contract.md) | monitoring 拓撲與把 metrics 接回 Server 的標籤契約；swallow 只提供有上限的基本用量圖表 | — |
| [005](005-monorepo-consolidation.md) | 由 submodule superproject 收斂為單一 swallow monorepo | — |
| [007](007-cluster-deployment-ownership.md) | swallow 擁有建立 Kubernetes Platform 的意圖，角色由伺服器端指派，HA 採 k0s 內建負載平衡 | — |
| [008](008-overview-read-model.md) | overview 是跨 context 的唯讀聚合視圖 | — |
| [009](009-deployment-template-ownership.md) | Deployment Template 只擁有可重用的 OS 部署意圖，不擁有 image、automation 或執行生命週期 | — |
| [010](010-cluster-lifecycle-actions.md) | Platform 的移除：Uninstall 改變部署過的主機並在成功時刪除紀錄，只刪紀錄（`recordOnly`）不碰主機；同時 release 時直接 release，略過 platform 軟體的 uninstall | 022 |
| [011](011-flexible-k0s-topologies.md) | k0s 支援單機、非 HA 多節點與 HA 三種拓樸，不新增 Node Role | — |
| [013](013-server-lock-protection.md) | provider 擁有的 Server Lock 是所有 provider 與主機變更的共同防護 | — |
| [015](015-platform-term-disambiguation.md) | swallow（小寫）指系統本身；Platform 專指 swallow 部署的受管 runtime 聚合，取代 Cluster | 014 |
| [019](019-slurm-platform-deployment.md) | Slurm 以 per-daemon 角色部署，套件由 image 提供，slurmrestd credential 為選用 | — |
| [021](021-platform-type-specific-management.md) | Platform 詳情為共用外殼加各 type 專屬視圖，Slurm 另有唯讀即時叢集讀取 | — |
| [023](023-slurm-ha-shared-state-provisioning.md) | Slurm HA 的共享 StateSaveLocation 由 swallow 自動佈署，不由 user 提供 | — |
| [024](024-slurm-login-and-workload-storage.md) | Slurm 新增 login 角色與選用的 workload 共享檔案系統，與 controller state 分離 | — |
| [026](026-slurm-minimum-resource-policy.md) | Slurm 部署採單一選用的全系統 CPU／記憶體／儲存資格門檻，前後端都檢查 | — |
| [027](027-os-image-upload.md) | swallow 可驅動 provisioner 上傳 OS image，但不持有 artifact，custom 與否由 provider 判定 | — |
| [028](028-ephemeral-kubernetes-deployment.md) | Kubernetes 接受 ephemeral（RAM）OS，以開機後主機相容性檢查把關 | — |
| [032](032-self-deployed-platform-management.md) | swallow 只管理自己佈署的 Platform，runtime 狀態以 live explorer 讀取、不保存副本 | — |
| [033](033-provider-recovery-policy.md) | provider recovery 由 swallow 擁有的單一政策決定，provisioner 只執行選中的 primitive；明確重試失敗的 OS 部署會先 Recover 再重新部署 | 059 |
| [035](035-os-image-verification-and-deploy-target.md) | 以 Deploy Target 為部署模式詞彙，custom image 須依 target 經 swallow 驗證才能部署 | — |
| [036](036-provisioning-lifecycle-integrity.md) | `allocated` 可回復、驗證採借用歸還、投影以 provider 狀態為準 | — |
| [037](037-cli-component.md) | 新增 `cli/` component，API 服務 binary 改名 `swallow-api` | — |
| [038](038-software-deployment.md) | 單一 Managed Software 是獨立於 Platform 的 deploy target，並以 Job 組入其他 Workflow | — |
| [039](039-ssh-key-management-and-default-user.md) | swallow 擁有 SSH Key；automation 只以 Deployment Key 登入；登入帳號依序取 Server Default User（設定前先驗證）與 OS Image 的預設帳號 | 041、045 |
| [040](040-single-vm-production-installation.md) | 單一 VM 安裝與發佈：Compose 與 swallowctl 安裝的 MAAS 共置、共用一個 PostgreSQL；只發佈 Compose installation，GHCR 只放 swallow 建置的 image，install.sh 一行安裝與升級 | 050、051、052 |
| [042](042-authentication-sessions-and-api-keys.md) | 登入建立 Session（短效 access token 與輪替的 refresh token），User 可建立 API Key | — |
| [043](043-docker-host-management.md) | Docker CE 以 `enableApi` 開放 Engine API，由 api-server 代為即時管理主機 Docker，不保存 Docker 物件 | — |
| [044](044-docker-registry-credentials.md) | 私有 registry 帳密由 swallow 以 installation 範圍加密保存，每次 pull 依 image 解析並隨請求送出 | — |
| [046](046-os-deployment-progress-and-stall.md) | 部署中投影 provider stage，同一 stage 25 分鐘未推進即要求處理 | — |
| [047](047-redfish-boot-media.md) | Boot Media：swallow 依 provisioner 以固定範本建置 Boot ISO，經 BMC（Redfish）掛載並導向開機；UEFI 走 ISO 自己的 GRUB，可以原地 rebuild | 049、057 |
| [048](048-os-provisioning-generic-states.md) | swallow 定義通用的 OS Provisioning State 詞彙，provider 只做對應 | — |
| [053](053-server-enrollment-and-automatic-inspection.md) | Server Enrollment 引導 network boot 與 existing OS；硬體檢視改為自動啟動的 durable Workflow | — |
| [054](054-provisioner-power-configuration.md) | Power Configuration 歸 provisioner、swallow 即時 write-through；以 power adapter 建模並診斷缺少的 driver | — |
| [055](055-libvirt-virtual-machine-enrollment.md) | libvirt VM 以 domain 名稱經 Hypervisor enroll；Boot Media 新增 libvirt method | — |
| [058](058-deployment-dhcp-and-subnet-dns.md) | swallow 擁有部署網路意圖，provider adapter 實現 `automatic`、`dhcp`、`static` 三種定址模式 | 012、018 |
| [061](061-recovery-supersedes-blocking-work.md) | Workflow 的回復與停止：rerun 依現況收斂並保留原 Workflow；取消不改寫已完成的結果並把被中斷的部署帶回 Ready；Release、Recover 與 uninstall 取代佔用 Server 的工作 | 020、060 |
| [062](062-swallow-managed-gpu-operator.md) | swallow 以 Platform Job 安裝 AMD GPU Operator，只用 image driver，由 NFD 決定納管節點 | — |
| [063](063-omnistat-slurm-observability.md) | Slurm 部署一律設定 AMD GPU GRES，Omnistat 為選用功能並組入 Prometheus／Grafana；它的 hardware counters 由部署精靈預設要求、API 省略時關閉，無法收集的節點只關掉 counters，不讓部署失敗 | — |
| [064](064-upstream-patches-are-the-exception.md) | 優先選擇可用的上游版本，自行維護 patch 是例外 | — |
| [065](065-control-plane-ntp-and-post-provision-time-sync.md) | Installation 在主機提供 NTP server，每台 provision 的 Server 都指向它，Platform 只檢查時間 | — |
| [066](066-slurm-job-hooks-and-device-metrics-exporter.md) | Slurm 以單一 Prolog/Epilog dispatcher 執行 hook、預設共用節點，並提供 DME、OCI、PMIx 選項 | — |
| [067](067-platform-claim-on-server.md) | Platform 對 Server 的佔用存為 Platform Claim，被佔用的 Server 不能直接 Release 或刪除 | — |
| [068](068-device-metrics-exporter-managed-software.md) | AMD Device Metrics Exporter 成為可單台安裝的 Managed Software，Slurm 部署改為組入同一 kind | — |
| [069](069-swallow-owns-desired-state.md) | 所有權邊界：swallow 擁有 desired state 與執行狀態，provider 收斂到它；swallow 不保存也不重建哪些事實；Temporal 只執行，store 需要 transaction | 001、031 |
| [070](070-core-providers-and-extensions.md) | core 只有領域模型、Workflow 與 provider interface；外部系統都是可抽換的 adapter，平台與 GPU 廠商邏輯放在擴充模組，交付為發行版 | — |
| [071](071-provisioner-realized-desired-state.md) | 由 provisioner 實現的欄位（tag、Zone／Pool、HTTP Proxy、Subnet DNS、SSH Key、OS Image metadata）是 swallow 的 desired state，由 controller 收斂；overlay 退場 | 025、029、056 |
| [072](072-store-driven-workflow-execution.md) | Workflow 的執行：Workflow／Job／Task／Runner 模型、store 驅動的 Temporal 執行、swallow 以 ansible-runner 自行執行 Ansible 並即時串流 task 事件 | 006、016、017、030 |
| [073](073-hold-os-package-maintenance-during-automation.md) | swallow 改套件的 playbook 執行期間暫緩 OS 的自動套件維護：只停 timer、不停用，以有期限的租約歸還，由內容測試強制 | — |
| [074](074-retire-schema-v2-and-deprecated-aliases.md) | 退役 schema-v2 Operation 紀錄（schema v10 刪除）與「保留一個 release」的 alias：`/operations`、`/clusters`、`clusterId` 等欄位、request `ephemeral` 與同步的部署路由 | — |
| [075](075-module-contract-v1.md) | 模組契約 v1：每個模組以 `module.json` 宣告自己並以一個 root Go package 提供 hook，composition root 為三個 process 組出同一個 registry，模組之間只經由 core 的 port 依賴 | — |
| [076](076-change-plans.md) | Change Plan 是 convergence context 的獨立文件，每個資源最多一份未完成；破壞性的 spec 寫入與 plan 同一個 transaction，整份核准，由資源的 controller 重算後 apply 成 Workflow，24 小時到期 | — |
| [077](077-task-diagnostics-and-wait-policy.md) | Task 失敗帶有兩層的 Task Diagnostic，存在 store、經 API 提供，遮蔽 secret 但保留原因，並以結構化 log 關聯；每個等待宣告觀察來源、Failure Class 分類與預算，等待中發佈觀察，permanent 立即失敗 | — |
| [078](078-local-repository.md) | 每個 Site 以模式 `mode` 決定部署怎麼使用它的 Local Repository：`upstream`（預設，從模組宣告的上游取得並驗證）、`cache`（有就從它取得，其餘從上游，不等同步）或 `offline`（只從它取得）。Local Repository 是 provider 類別（目前是 Pulp 3），與控制平面共置、每個 installation 都安裝並以 `repository` Integration 登錄；swallow 擁有它必須包含的內容（模組宣告與 Site 的來源），由 controller 收斂；`offline` 的 Site 在它沒有 ready 時拒絕部署，不退回上游 | — |
| [079](079-release-built-host-artifacts.md) | 沒有上游發佈的主機 binary（第一個是 Omnistat 的 rocprof extension）由 swallow 的 release 工具建置：每個 ROCm minor 在 glibc 最低的 builder 建一次、只建 CPython 3.12，通過 glibc 與 import 的檢查才以獨立的 tag 發佈，再宣告成 file artifact；先上傳、後宣告，發佈 release 前確認每個宣告的檔案都取得到 | — |
| [080](080-host-toolchains-probe-first.md) | 主機的工具鏈是需求：任何 CPython 3.12（OS 套件）與任何 uv（缺少時由 file artifact 提供）；元件先探測、不能用才安裝，安裝內容仍以 hash lock 固定；swallow 使用、不覆寫預先烘進 image 的版面；主機經 `artifact_sources` 寫的節點設定（`/etc/uv/uv.toml`）取得 Site 選擇的來源，其他 role 不設 index | — |

### 已併入的編號

程式、文件與 commit 訊息中的舊編號，對應到現行的 ADR：

| 舊編號 | 現行 ADR |
| --- | --- |
| 001 | [069](069-swallow-owns-desired-state.md) |
| 006 | [072](072-store-driven-workflow-execution.md) |
| 012 | [058](058-deployment-dhcp-and-subnet-dns.md) |
| 014 | [015](015-platform-term-disambiguation.md) |
| 016 | [072](072-store-driven-workflow-execution.md) |
| 017 | [072](072-store-driven-workflow-execution.md) |
| 018 | [058](058-deployment-dhcp-and-subnet-dns.md) |
| 020 | [061](061-recovery-supersedes-blocking-work.md) |
| 022 | [010](010-cluster-lifecycle-actions.md) |
| 025 | [071](071-provisioner-realized-desired-state.md) |
| 029 | [071](071-provisioner-realized-desired-state.md) |
| 030 | [072](072-store-driven-workflow-execution.md) |
| 031 | [069](069-swallow-owns-desired-state.md) |
| 041 | [039](039-ssh-key-management-and-default-user.md) |
| 045 | [039](039-ssh-key-management-and-default-user.md) |
| 049 | [047](047-redfish-boot-media.md) |
| 050 | [040](040-single-vm-production-installation.md) |
| 051 | [040](040-single-vm-production-installation.md) |
| 052 | [040](040-single-vm-production-installation.md) |
| 056 | [071](071-provisioner-realized-desired-state.md) |
| 057 | [047](047-redfish-boot-media.md) |
| 059 | [033](033-provider-recovery-policy.md) |
| 060 | [061](061-recovery-supersedes-blocking-work.md) |

`002`、`003` 的章節結構與上方格式不同（Decision 在 Context 之前、Rejected Alternatives），當時也未記錄
`Date`，依「不得杜撰歷史理由」原則保留為 unrecorded。`005` 起採用本文件定義的格式。（原 `004`
記錄的 AWX 執行模型已由 `006` 取代並移除。）`034` 從未指派，不代表有遺失或未索引的 ADR。

swallow 另有若干值得記錄的決策（例如 API contract 由 provider component 擁有），但這些理由
目前只存在於結構契約與實作中，尚未經確認。新增這些 ADR 時，請與知道當時脈絡的人確認，
或明確標注「（inferred）」。
