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
- 引入、更換或否決重大的技術依賴或 runtime 拓撲（例如 [016](016-temporal-operation-orchestration.md)、
  [040](040-single-vm-production-installation.md)）。

單一功能的設計——包括它的參數、流程與否決過的方案——寫在 `docs/development/` 對應規格的「設計理由」，
不寫 ADR。判斷不了時這樣想：三個月後做另一個功能的人也必須遵守它，就是 ADR；只有修改這個功能的人
需要知道，就屬於規格。只影響單一 component 內部實作的決策，留在該 component 的文件。

## ADR 與規格的分工

- ADR 回答「為什麼」：背景、決定、否決的方案與取捨。Accepted 後凍結，不追加 amendment、Revision
  或實作狀態。
- 「現在怎麼運作」寫在規格（`docs/development/` 的設計文件、glossary、API 契約）。行為改變只更新規格。
- 決策本身被推翻或改變時，新增一份 ADR，在下方索引的「關係」欄寫明它 refine 或 supersede 哪一份，
  並把舊 ADR 的 Status 改為 `Superseded by NNN` 或 `Refined by NNN`。這與更新「現行規格」連結，
  是 Accepted ADR 唯一允許的修改。
- 2026-10-30 起，原本兼作規格的功能型 ADR 已縮減為決策核心，細節移入規格，原文保留在 git 歷史；
  ADR 編號不變，程式碼與文件中的「decision NNN」引用仍然有效。在此之前建立的修訂關係記錄在索引的
  「關係」欄。

## 格式

每筆 ADR 用一個檔案，命名 `NNN-short-title.md`（三位數流水號）。內容包含：

```md
# NNN. 標題

- Status: Proposed | Accepted | Superseded by NNN | Refined by NNN | Deprecated
- Date: YYYY-MM-DD
- 現行規格：`docs/development/<spec>.md` §<章節>（若決策的現行行為由規格描述）

## Context
（決策當時的背景與問題）

## Decision
（實際決定；只有當數值或路徑「就是」決策時才寫進來）

## Alternatives considered
（考慮過的替代方案與否決原因）

## Consequences
（決策帶來的好處與代價）
```

> **重要**：不要為了補 ADR 而 **杜撰歷史理由**。若理由是由現有程式碼 / plan **推論** 而來、
> 而非當時明確記載，請在該筆標注「（inferred）」。

## 既有 ADR

「關係」欄只列這份 ADR 修訂了哪些較早的 ADR；細節與現行規則見各 ADR 指向的規格。

| ADR | 決定了什麼 | 關係 |
| --- | --- | --- |
| [001](001-system-ownership-boundaries.md) | 哪個系統擁有哪些事實，以及 swallow 因此不得儲存或重建什麼 | — |
| [002](002-server-identity.md) | Server 以 opaque identity 跨站點與重裝維持身分，狀態分為三個獨立軸 | — |
| [003](003-metrics-label-contract.md) | monitoring 拓撲與把 metrics 接回 Server 的標籤契約；swallow 只提供有上限的基本用量圖表 | — |
| [005](005-monorepo-consolidation.md) | 由 submodule superproject 收斂為單一 swallow monorepo | — |
| [006](006-embedded-ansible-execution.md) | swallow 以 pinned ansible-runner 自行執行 Ansible，不使用 AWX | 執行引擎部分被 016 取代 |
| [007](007-cluster-deployment-ownership.md) | Swallow 擁有建立 Kubernetes Platform 的意圖，角色由伺服器端指派，HA 採 k0s 內建負載平衡 | — |
| [008](008-operator-overview-read-model.md) | operator overview 是跨 context 的唯讀聚合視圖 | — |
| [009](009-deployment-template-ownership.md) | Deployment Template 只擁有可重用的 OS 部署意圖，不擁有 image、automation 或執行生命週期 | — |
| [010](010-cluster-lifecycle-actions.md) | Uninstall 改變原部署主機，Delete 只移除 Swallow 紀錄與自有投影 | — |
| [011](011-flexible-k0s-topologies.md) | k0s 支援單機、非 HA 多節點與 HA 三種拓樸，不新增 Node Role | — |
| [012](012-provider-network-configuration.md) | swallow 擁有部署網路意圖，provider adapter 負責翻譯並驗證 NIC 設定 | — |
| [013](013-server-lock-protection.md) | provider 擁有的 Server Lock 是所有 provider 與主機變更的共同防護 | — |
| [014](014-platform-resource-language.md) | Platform 是跨 context 的受管 runtime 聚合，取代 Cluster | — |
| [015](015-platform-term-disambiguation.md) | swallow 指系統本身，capital-P Platform 專指受管 runtime 聚合 | — |
| [016](016-temporal-operation-orchestration.md) | 多步驟 Workflow 由 Temporal 編排：Task DAG、typed Runner、per-resource fencing lease | supersede 006（部分） |
| [017](017-workflow-job-task-runner-model.md) | Workflow／Job／Task／Runner 詞彙、收斂式執行，以及 Workflow 與 playbook 的界線 | refine 016 |
| [018](018-automatic-addressing-provider-auto-assign.md) | 自動定址意圖 `automatic` 由 provider auto-assign 實現，而非 raw DHCP | refine 012 |
| [019](019-slurm-platform-deployment.md) | Slurm 以 per-daemon 角色部署，套件由 image 提供，slurmrestd credential 為選用 | — |
| [020](020-durable-operation-recovery.md) | 失敗或 execution 遺失的 Workflow 以帶 lineage 的新 Workflow 重跑回復、不刪 Platform；遺失只標記不自動重跑 | — |
| [021](021-platform-type-specific-management.md) | Platform 詳情為共用外殼加各 type 專屬視圖，Slurm 另有唯讀即時叢集讀取 | — |
| [022](022-uninstall-release-shortcut.md) | Uninstall 同時 release 主機時直接 release，略過 platform 軟體的 uninstall | — |
| [023](023-slurm-ha-shared-state-provisioning.md) | Slurm HA 的共享 StateSaveLocation 由 Swallow 自動佈署，不由 operator 提供 | — |
| [024](024-slurm-login-and-workload-storage.md) | Slurm 新增 login 角色與選用的 workload 共享檔案系統，與 controller state 分離 | — |
| [025](025-provider-data-overlay.md) | provider 擁有的事實可疊加 swallow-owned overlay，讀取時合併、永不回寫 provider | refine 001、009；被 071 取代 |
| [026](026-slurm-minimum-resource-policy.md) | Slurm 部署採單一選用的全系統 CPU／記憶體／儲存資格門檻，前後端都檢查 | — |
| [027](027-os-image-upload.md) | swallow 可驅動 provisioner 上傳 OS image，但不持有 artifact，custom 與否由 provider 判定 | refine 001、009 |
| [028](028-ephemeral-kubernetes-deployment.md) | Kubernetes 接受 ephemeral（RAM）OS，以開機後主機相容性檢查把關 | — |
| [029](029-infrastructure-zone-pool-ownership.md) | Zone 與 Pool 由 swallow 擁有、以 Site 為範圍，provisioner 有能力時再同步實現 | refine 001、012、025 |
| [030](030-live-ansible-task-streaming.md) | Ansible Task 執行中即時串流 per-task 結果並以計數呈現進度；raw stdout 不進 MongoDB | refine 016、017 |
| [031](031-provider-capability-first-with-swallow-owned-fallback.md) | provisioner 可能擁有的事實採 capability-first，並以同形狀的 swallow-owned fallback 補足 | refine 001；收斂 025、029 |
| [032](032-self-deployed-platform-management.md) | swallow 只管理自己佈署的 Platform，runtime 狀態以 live explorer 讀取、不保存副本 | — |
| [033](033-provider-recovery-policy.md) | provider recovery 由 swallow 擁有的單一政策決定，provisioner 只執行選中的 primitive | — |
| [035](035-os-image-verification-and-deploy-target.md) | 以 Deploy Target 為部署模式詞彙，custom image 須依 target 經 swallow 驗證才能部署 | — |
| [036](036-provisioning-lifecycle-integrity.md) | `allocated` 可回復、驗證採借用歸還、投影以 provider 狀態為準 | refine 033、035 |
| [037](037-operator-cli-component.md) | 新增 `cli/` component，API 服務 binary 改名 `swallow-api` | — |
| [038](038-software-deployment.md) | 單一 Managed Software 是獨立於 Platform 的 deploy target，並以 Job 組入其他 Workflow | — |
| [039](039-ssh-key-management-and-default-user.md) | swallow 擁有 SSH Key 並同步到 provisioner，OS Image 決定登入帳號 | — |
| [040](040-single-vm-production-installation.md) | 單一 VM 安裝：swallow Compose 與 swallowctl 安裝的 MAAS 共置，共用一個 PostgreSQL instance | refine 001（拓樸面） |
| [041](041-deployment-key-only-automation.md) | 所有 automation SSH 登入只用 Deployment Key，移除 Site 私鑰 override | supersede 039（Site override） |
| [042](042-authentication-sessions-and-api-keys.md) | 登入建立 Session（短效 access token 與輪替的 refresh token），User 可建立 API Key | — |
| [043](043-docker-host-management.md) | Docker CE 以 `enableApi` 開放 Engine API，由 api-server 代為即時管理主機 Docker，不保存 Docker 物件 | — |
| [044](044-docker-registry-credentials.md) | 私有 registry 帳密由 swallow 以 installation 範圍加密保存，每次 pull 依 image 解析並隨請求送出 | — |
| [045](045-server-default-user.md) | Server Default User 優先於 OS Image 帳號，設定前以 Deployment Key 驗證 | — |
| [046](046-os-deployment-progress-and-stall.md) | 部署中投影 provider stage，同一 stage 25 分鐘未推進即要求處理 | — |
| [047](047-redfish-boot-media.md) | swallow 自行提供 iPXE ISO，經 BMC Redfish 掛載並導向開機，每次部署前重新套用 | — |
| [048](048-os-provisioning-generic-states.md) | swallow 定義通用的 OS Provisioning State 詞彙，provider 只做對應 | — |
| [049](049-boot-iso-builder.md) | Boot ISO 由 swallow 依 provisioner 以固定範本建置並提供，取代安裝層級的單一 ISO | refine 047 |
| [050](050-compose-only-release-artifacts.md) | Release 只發佈 Compose installation；native bundle 與離線 image archive 延後 | refine 040 |
| [051](051-ghcr-holds-only-swallow-images.md) | GHCR 只放 swallow 建置的 image，第三方 runtime image 以上游 digest 原樣使用 | supersede 040（image package） |
| [052](052-one-command-installation.md) | 每個 release 附 install.sh 一行指令安裝與升級，swallowctl 仍是唯一 lifecycle 工具 | refine 040 |
| [053](053-server-enrollment-and-automatic-inspection.md) | Server Enrollment 引導 network boot 與 existing OS；硬體檢視改為自動啟動的 durable Workflow | — |
| [054](054-provisioner-power-configuration.md) | Power Configuration 歸 provisioner、swallow 即時 write-through；以 power adapter 建模並診斷缺少的 driver | refine 053 |
| [055](055-libvirt-virtual-machine-enrollment.md) | libvirt VM 以 domain 名稱經 Hypervisor enroll；Boot Media 新增 libvirt method | refine 054 |
| [056](056-provisioner-http-proxy.md) | Provisioner HTTP Proxy 由 provisioner 擁有，swallow 以封閉 mode 值集即時讀寫 | 部分被 071 取代 |
| [057](057-uefi-boot-iso-hand-off.md) | Boot ISO 的 UEFI 改用內建 GRUB 走 MAAS 原生路徑，可原地 rebuild，並擋下需重新檢視的磁碟部署 | refine 049 |
| [058](058-deployment-dhcp-and-subnet-dns.md) | `dhcp` 重新成為部署模式，Subnet DNS 由 swallow 透過 provisioner write-through | supersede 018（部分）；部分被 071 取代 |
| [059](059-retry-recovers-failed-deployment.md) | 明確重試失敗的 `provision-os` 會先回復 Server 再重新部署 | refine 033 |
| [060](060-cancel-and-rerun-converge.md) | 取消不改寫已完成的結果並把被中斷的部署帶回 Ready；Rerun 依現況收斂而非沿用歷史結果 | refine 017、020、059 |
| [061](061-recovery-supersedes-blocking-work.md) | Release、Recover 與 Platform uninstall 由後端取消佔用其 Server 的 Workflow，等它們結束後自動執行 | refine 060 |
| [062](062-swallow-managed-gpu-operator.md) | Swallow 以 Platform Job 安裝 AMD GPU Operator，只用 image driver，由 NFD 決定納管節點 | refine 003 |
| [063](063-omnistat-slurm-observability.md) | Slurm 部署一律設定 AMD GPU GRES，Omnistat 為選用功能並組入 Prometheus／Grafana | refine 019、038、062 |
| [064](064-upstream-patches-are-the-exception.md) | 優先選擇可用的上游版本，自行維護 patch 是例外 | refine 063 |
| [065](065-control-plane-ntp-and-post-provision-time-sync.md) | Installation 在主機提供 NTP server，每台 provision 的 Server 都指向它，Platform 只檢查時間 | — |
| [066](066-slurm-job-hooks-and-device-metrics-exporter.md) | Slurm 以單一 Prolog/Epilog dispatcher 執行 hook、預設共用節點，並提供 DME、OCI、PMIx 選項 | — |
| [067](067-platform-claim-on-server.md) | Platform 對 Server 的佔用存為 Platform Claim，被佔用的 Server 不能直接 Release 或刪除 | — |
| [068](068-device-metrics-exporter-managed-software.md) | AMD Device Metrics Exporter 成為可單台安裝的 Managed Software，Slurm 部署改為組入同一 kind | refine 038、066 |
| [069](069-swallow-owns-desired-state.md) | swallow 擁有 desired state 與執行狀態，provider 收斂到它；混合收斂、Temporal 只執行、store 需要 transaction | refine 001、016、017、040；supersede 031 |
| [070](070-core-providers-and-extensions.md) | core 只有領域模型、Workflow 與 provider interface；外部系統都是可抽換的 adapter，平台與 GPU 廠商邏輯放在擴充模組，交付為發行版 | refine 001、006、016、017、038 |
| [071](071-provisioner-realized-desired-state.md) | 由 provisioner 實現的欄位以 spec／status 呈現、由 controller 收斂：非破壞性欄位在定期 resync 時自動修正，第一次觀察即接管，Server 記錄 origin，swallow 擁有所有手動 tag | refine 002、029、069；supersede 025 的一部分 |

`001`–`003` 的章節結構與上方格式不同（Decision / Context / Consequences / Rejected
alternatives）。2026-09-05 已為三者補上 `Status`；但當時未記錄 `Date`，依「不得杜撰歷史理由」
原則保留為 unrecorded 而非捏造。其論述結構刻意維持原樣。`005` 起採用本文件定義的格式。（原 `004`
記錄的 AWX 執行模型已由 `006` 取代並移除。）`034` 從未指派，不代表有遺失或未索引的 ADR。

swallow 另有若干值得記錄的決策（例如 API contract 由 provider component 擁有），但這些理由
目前只存在於結構契約與實作中，尚未經確認。新增這些 ADR 時，請與知道當時脈絡的人確認，
或明確標注「（inferred）」。
