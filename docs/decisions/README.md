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
| [`005-monorepo-consolidation.md`](005-monorepo-consolidation.md) | swallow 從 gdcm submodule superproject 收斂為單一 swallow monorepo，並完成品牌改名 |
| [`006-embedded-ansible-execution.md`](006-embedded-ansible-execution.md) | Swallow 如何以 Mongo lease 與 pinned runner 擁有 Ansible operation 執行 |
| [`007-cluster-deployment-ownership.md`](007-cluster-deployment-ownership.md) | Swallow 擁有「建立 Kubernetes Platform」的意圖：k0s HA 拓樸、專職 controller 為何不是 k8s node、部署後如何取得叢集憑證 |
| [`008-operator-overview-read-model.md`](008-operator-overview-read-model.md) | operator overview 是一個唯讀聚合視圖（read model），彙整跨 context 的營運現況 |
| [`009-deployment-template-ownership.md`](009-deployment-template-ownership.md) | Deployment Template 只擁有可重用的 OS deployment intent，不擁有 image、automation content 或 execution lifecycle |
| [`010-cluster-lifecycle-actions.md`](010-cluster-lifecycle-actions.md) | Platform Uninstall changes original k0s targets while Delete removes only Swallow records and owned projections |
| [`011-flexible-k0s-topologies.md`](011-flexible-k0s-topologies.md) | k0s deployment supports standalone, non-HA multi-node, and HA shapes without changing Node Role vocabulary |
| [`012-provider-network-configuration.md`](012-provider-network-configuration.md) | Swallow owns DHCP/static deployment intent while provider adapters translate and verify NIC configuration |
| [`013-server-lock-protection.md`](013-server-lock-protection.md) | Provider-owned Server Lock is the common guard for every provider and host mutation while reads and monitoring remain available |
| [`014-platform-resource-language.md`](014-platform-resource-language.md) | Platform 是跨 context 的 canonical 受管 runtime 聚合，取代 Cluster；cluster 一詞只保留給外部技術 |
| [`015-platform-term-disambiguation.md`](015-platform-term-disambiguation.md) | swallow 是系統本身的唯一稱呼；capital-P Platform 專指受管 runtime 聚合，「平台」不再指 swallow |
| [`016-temporal-operation-orchestration.md`](016-temporal-operation-orchestration.md) | Operation v3 改為 Temporal 編排的多步驟 workflow（DAG、typed executor、per-resource fencing lease），supersede ADR 006 的執行引擎與 site lease |
| [`017-workflow-job-task-runner-model.md`](017-workflow-job-task-runner-model.md) | 定義 Workflow/Job/Task/Runner 詞彙、收斂式（ensure）執行、workflow↔ansible 界線規則、inventory 為 platform playbook 的 published language；refine ADR 016 詞彙 |
| [`018-automatic-addressing-provider-auto-assign.md`](018-automatic-addressing-provider-auto-assign.md) | 部署自動定址意圖 `automatic` 由 provider auto-assign（MAAS `AUTO`）實現而非 raw DHCP，位址穩定且 provider 一定記錄；`dhcp` 降為一 release deprecated alias；refine ADR 012 的模式選擇 |
| [`019-slurm-platform-deployment.md`](019-slurm-platform-deployment.md) | Slurm platform 部署：per-daemon 角色（slurmctld/slurmd）、套件由 image 提供、slurmrestd+JWT 憑證、重用 ensure-os + 新增 configure-slurm Job、單 controller 首版且保留 HA 掛勾、不套 k0s 的 ephemeral 防呆 |
| [`020-durable-operation-recovery.md`](020-durable-operation-recovery.md) | Durable Operation 失敗回復：以新 Operation + `RetryOfOperationId` 重跑（保留成功 Step、複製封存 secrets）不刪 Platform、`requires_attention` 等待期撐過中斷、lost-execution reconciler 只標記不自動修 |
| [`021-platform-type-specific-management.md`](021-platform-type-specific-management.md) | Platform 管理改為共用外殼 + 各 type 專屬視圖（Kubernetes/Slurm）；新增 on-demand Slurm-native cluster read（controllers ping、partitions、node 排程狀態），與 membership sync 及 deployment intent 分離；health 為獨立軸、暫不與 scheduler state 混用 |
| [`022-uninstall-release-shortcut.md`](022-uninstall-release-shortcut.md) | Uninstall 同時 release servers 時直接 release（release 會清 OS,略過多餘的 platform-software uninstall step）,以內部 complete-uninstall finalize step 做投影清理;僅限整平台 uninstall,未來 scale-in 仍走 uninstall |
| [`023-slurm-ha-shared-state-provisioning.md`](023-slurm-ha-shared-state-provisioning.md) | Slurm HA 的 shared `StateSaveLocation` 由 swallow 自動佈署：deploy use case 選 state server（compute-only 優先,退回 primary）,playbook 以 managed NFS export（`slurm_state_server`）+ 每台 controller 掛載與 systemd mount guard（`slurm_controller_state`,fail-closed）供給;`stateSaveLocation` 由必填改為選用覆寫;僅用 `ansible.builtin`;lab 等級單一 storage failure domain,外部/production 儲存為後續 |
| [`024-slurm-login-and-workload-storage.md`](024-slurm-login-and-workload-storage.md) | 新增 Slurm **login** 角色（提交/client,可兼 NFS server;login-only 合法;HA 時 state server 優先選 login）與**選用的 workload 共享檔案系統**（與 controller state 分離,掛所有節點的非重疊路徑,不可 `/home`）：`workloadStorage.mode` 為 `self-hosted`（login node 匯出 NFS）或 `external`（操作者 NFS URL）;roles `slurm_login`/`slurm_workload_storage_server`/`slurm_workload_storage_client`,builtin-only;支援 single-controller 與 login+HA+compute 兩種拓樸 |
| [`025-provider-data-overlay.md`](025-provider-data-overlay.md) | provider-owned 事實可疊加 swallow-owned **overlay**（owned data,以 provider 身分鍵、讀取時 `overlay ?? provider` 合併、永不回寫 provider、entity 刪除時連帶清）;展示型能力缺→overlay 補,操作型能力缺→沿用 `ProviderCapabilities` 標不可用;首個 reference 為 OS Image 名稱;refine ADR 001/009 |
| [`026-slurm-minimum-resource-policy.md`](026-slurm-minimum-resource-policy.md) | Slurm deployment uses one optional system-wide CPU, memory, and storage eligibility floor, enforced in both node selection and backend preflight |
| [`027-os-image-upload.md`](027-os-image-upload.md) | Swallow 可驅動 provisioner 上傳 provider-owned OS image（optional `OSImageUploader` capability，對稱於 delete）：bytes 由 browser→api-server→provider 串流、swallow 不留副本，custom 分類由 provider adapter 判斷而非呼叫端；新增 `POST /provisioning/images`（multipart）；refine ADR 001/009 |
| [`028-ephemeral-kubernetes-deployment.md`](028-ephemeral-kubernetes-deployment.md) | Kubernetes accepts the existing ephemeral OS intent for disposable clusters and gates the booted host with cgroup, module, command, and `k0s sysinfo` checks before cluster state is created |
| [`029-infrastructure-zone-pool-ownership.md`](029-infrastructure-zone-pool-ownership.md) | Zone 與 Pool 改為 swallow-owned、Site-scoped 的受管概念（新 `infrastructure` feature，完整 CRUD 與 Server placement），provider 具備能力時以 optional `GroupingController` capability 實現於 MAAS；Server 的 observed zone/pool 仍為 provider 鏡射；supersede `Machine.Zone/ResourcePool` 的 opaque pass-through 註記，refine ADR 001/012/025 |
| [`030-live-ansible-task-streaming.md`](030-live-ansible-task-streaming.md) | Ansible Task 執行中即時串流 per-task 事件進 MongoDB、run 一開始就投影 `externalExecution`、並以 `Task.Live`（current play/task + ok/changed/failed/unreachable/skipped 計數，非百分比）呈現進度；raw stdout 仍留磁碟；沿用既有輪詢；refine ADR 016/017 |
`001`–`003` 沿用先前的三位數命名，章節結構也與上方格式不同（Decision / Context /
Consequences / Rejected alternatives）。2026-09-05 已為三者補上 `Status`；但當時未記錄
`Date`，依「不得杜撰歷史理由」原則保留為 unrecorded 而非捏造。其論述結構刻意維持原樣。
`005` 起採用本文件定義的格式。（原 `004` 記錄的 AWX 執行模型已由 `006` 取代並移除。）

swallow 另有若干值得記錄的決策（例如 API contract 由 provider component 擁有），但這些理由
目前只存在於結構契約與實作中，尚未經確認。新增這些 ADR 時，請與知道當時脈絡的人確認，
或明確標注「（inferred）」。
