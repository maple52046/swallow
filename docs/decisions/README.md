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
| [`031-provider-capability-first-with-swallow-owned-fallback.md`](031-provider-capability-first-with-swallow-owned-fallback.md) | 為「provisioner 可能擁有的事實」定下單一規則：capability-first with swallow-owned fallback——provider 有能力就驅動 provider（fact 為 mirror）、沒能力就 swallow own（以 ADR 025 overlay 讀時合併），fallback 一律建成同形狀即使目前 provider 皆 capable；把 ADR 025（無 provider 可寫的退化情形）與 ADR 029（provider 可寫情形）收斂為共同規則，refine ADR 001、instance 含 ADR 027；首個套用為 server tag editing |
| [`033-provider-recovery-policy.md`](033-provider-recovery-policy.md) | Swallow 自有的 provider recovery policy 取代 passthrough：Release 放寬到可從 `deployed`／`failed`／`broken`／`rescue` 收斂回 `ready`，新增依狀態編排的 Recover（Return to Ready）Operation，mark-*/rescue 由 Swallow 依 live state 閘控並回自有理由；Rescue 定位為診斷（Exit 不回 Ready）；政策集中於 provisioning domain 的單一矩陣，provisioner 只執行選中的 primitive |
| [`035-os-image-verification-and-deploy-target.md`](035-os-image-verification-and-deploy-target.md) | 導入 Deploy Target（`disk`／`ram`，`ram` 即既有 `ephemeral`，於邊界一對一映射、`deployTarget` 為首選、`ephemeral` 續為 deprecated alias）與 swallow-owned 的 OS Image 驗證：以 ADR 025 身分鍵 `(integrationId, imageId, architecture)` 儲存 per-Deploy-Target 證據（獨立 collection），新增 `verify-os-image` durable workflow（provision-os→record-image-verification→release-os、成功自動 release），並對未驗證的 *custom* image 於部署受理時依 target 閘控（synced image 不閘）；不改任何 `ephemeral` wire/BSON/snapshot/MAAS/k0s 欄位 |
| [`036-provisioning-lifecycle-integrity.md`](036-provisioning-lifecycle-integrity.md) | 修補三個共因的生命週期漏洞：`allocated` 納入 Recover／Release 合法來源（refine ADR 033）；`verify-os-image` 改為借用／歸還契約——provision 無論成敗（含 cancel）都跑 return-to-ready（重用 `recover-server`，經新的 skip-then-run 依賴邊）、僅成功才 record、`VerificationRun` 下 proving 失敗改 non-retryable 以進 terminal（refine ADR 035）；投影誠實化——badge 先讀 provider 軸再退回舊 `deployment.succeeded`、reconcile 於 `ready`／`allocated` 清舊終態 deployment、OS Images 只把真正在跑的 Operation 當 verifying；一般 deploy 失敗仍維持操作者觸發回復 |
| [`037-operator-cli-component.md`](037-operator-cli-component.md) | 新增頂層 `cli/` component（operator 命令列客戶端，`api-server` HTTP 契約的第二個 conformist consumer，獨立 Go module，binary `swallow`）；`api-server` 服務 binary 由 `swallow` 改名 `swallow-api` 以釋放名稱；component 目錄維持 role-based，`swallow`／`swallow-api` 僅為 binary 名 |
| [`039-ssh-key-management-and-default-user.md`](039-ssh-key-management-and-default-user.md) | swallow 自有 SSH Key：單一系統持有的 Deployment Key（安裝步驟 `swallow-api deployment-key ensure` 產生 ed25519、API 啟動不依賴它、OS／Platform 佈署受理前檢查；私鑰加密保管、可重產／替換不可刪）＋ User 的 public-key-only Access Keys；依 capability-first 同步到 provisioner（MAAS sshkeys，含佈署前 ensure）；site 私鑰為選用 override；OS Image overlay 新增 default user，鏡射到 Server 並作為 automation 的唯一登入帳號，未設定才退回候選探測 |
| [`040-single-vm-production-installation.md`](040-single-vm-production-installation.md) | Production installation 是單一 VM：swallow Compose 與 `swallowctl` 自行安裝的 MAAS 3.6 region+rack 共置；MAAS 使用 Compose PostgreSQL instance 內獨立的 `maas` role／`maasdb`（禁用 `maas-test-db`），API／worker／Ansible executor 經 `egress` 網路與 `host.docker.internal` 連 MAAS 與 Server；安裝只確保官方 `ubuntu/noble` amd64，並透過已發佈 API 建立 Site、provisioner Integration 與 automation 預設；Dashboard 改走 HTTP:80；映像統一為單一 GHCR package；ownership 不變（refine ADR 001 的拓樸面） |
| [`041-deployment-key-only-automation.md`](041-deployment-key-only-automation.md) | 所有 automation SSH 登入（wait-for-ssh、登入帳號探測、Ansible）一律使用安裝層級的 Deployment Key；移除 Site 私鑰 override，Site credential 只存 become password，帶 `sshPrivateKey` 的請求回 `400 validation_error`；Ansible 可執行的條件改為「automation 已啟用且 Deployment Key 存在」，取代過時的 Site credential 紀錄檢查；supersede ADR 039 的 override 部分 |
| [`042-authentication-sessions-and-api-keys.md`](042-authentication-sessions-and-api-keys.md) | 登入建立 Session：短效 access JWT（預設 15 分鐘）加上每次換發的 refresh token（瀏覽器用 HttpOnly `SameSite=Strict` cookie、CLI 用 body；30 秒寬限，逾時重用即撤銷 Session；閒置 7 天、最長 30 天）；User 可建立 API key（`swk_`、只存 SHA-256、繼承擁有者角色、可設到期、刪除即撤銷、以 API key 驗證的請求不能再建 key）；單一 middleware 驗證兩者，SSE query token 只接受 access token；machine token 不變 |
| [`043-docker-host-management.md`](043-docker-host-management.md) | Docker CE 新增 `enableApi` 變體（預設開啟，dockerd 另聽 `tcp://0.0.0.0:2375`，無 TLS、僅限內網，關閉或卸載即移除；切換即重跑同一 install ensure）；Server 的 Docker Host Explorer 由 api-server 代呼主機 Engine API（瀏覽器不直連、非透明 proxy），資格只看 swallow-owned 事實（`docker-ce` assignment `installed` 且 `enableApi`）；images／containers／volumes／networks 不寫入 swallow DB，寫入受 Server Lock 管制；預留 Swarm 或 fleet 級 Docker 管理可能需要另立 ADR 保存 Docker 資料 |
| [`044-docker-registry-credentials.md`](044-docker-registry-credentials.md) | Docker Host Explorer 的 private image pull：swallow 保存 installation 級 Registry Credential（以正規化 registry host 為唯一鍵、密碼以 credential key 加密且 write-only），每次 pull 依 image reference 解析 registry（Docker 規則，預設 `docker.io`）並以 `X-Registry-Auth` 傳給 Engine，否則匿名；主機上的 `docker login` 對 Engine API 無效故不採用；Site／Server 範圍與儲存時驗證延後 |
| [`045-server-default-user.md`](045-server-default-user.md) | 每台 deployed Server 可設定 Server Default User（優先於 OS Image default user，重新 deploy 或回到 `ready`／`allocated` 時清除）：設定時可輸入一次性密碼由 api-server 以 SSH 將 Deployment Key 加入該帳號 `authorized_keys`（密碼不保存），一律先以 Deployment Key 登入驗證並回報 sudo 狀態才儲存；wait-for-ssh、inventory `default_user` 與 `ansible_user` 都用 effective 值；Docker CE 把 `ansible_user` 加入 `docker` group；commission 階段自動帶入帳號與匯入 key 為未來方向 |
| [`046-os-deployment-progress-and-stall.md`](046-os-deployment-progress-and-stall.md) | OS 佈署 Deploying 期間把 provider 最新 machine event（例如 Configuring OS）投影為 Server deployment 的非終態 stage，同一 stage 25 分鐘未推進即 `requires_attention`（`deployment_provider_stage_stall`，不 abort provider）；曾實作的部署前 package-origin preflight 與 OS Image check URL 因無法對應每個 image 的實際 repo 而於同日移除（記為否決方案） |
| [`047-redfish-boot-media.md`](047-redfish-boot-media.md) | Boot Media：swallow 以未驗證、支援 Range 的 HTTP 路由提供安裝層級的 iPXE ISO（URL 由安裝固定，不逐台輸入）；每台 Server 只保存 swallow-owned 的啟用設定與 Redfish capability（BMC 帳密每次從 MAAS `power_parameters` 讀取、不落地）；API 每十分鐘偵測新加入或過期的 Server（不以 `power_type` 為閘門）；Server 頁面啟用即 preflight；依韌體選擇開機導向（AMI Aptio 把 UEFI USB 群組排第一、BootOrder+BootNext、或 `Cd`）；每次 OS 佈署前以 `ensure-boot-media` Task 重新套用 |
| [`048-os-provisioning-generic-states.md`](048-os-provisioning-generic-states.md) | swallow 擁有 `provisioning.state` 的通用詞彙（OS Provisioning State），provider adapter 只做對應，自己的字只留在 display-only 的 `providerState`；MAAS 詞 `commissioning` 在 API 改為 `inspecting`、操作 `/commission` 改為 `/inspect`（BREAKING；舊存值讀時視為 `inspecting`，不做 schema migration）；dashboard Deployment 欄顯示通用狀態（Releasing、Failed 等），移除自創的「Not deployed」改稱 Ready，進行中狀態以 spinner 提示 |
| [`049-boot-iso-builder.md`](049-boot-iso-builder.md) | swallow 在 Provisioning 頁依 provisioner 建置 Boot ISO：operator 選 provisioner、填 MAAS rack 位址，swallow 以固定且已驗證的範本產生 iPXE script（site DHCP 後 chain 到 `http://<rack>:5248/ipxe.cfg`），以映像內預先編譯的 iPXE `v2.0.0` 與 `genfsimg -s` 同步打包 BIOS／UEFI ISO（runtime 不需編譯器），存於 Boot Media 目錄並以 `/boot-media/ipxe/<isoId>/swallow-ipxe.iso` 提供；Server 的 Boot Media 改為選擇同一 provisioner 的 Boot ISO，部署時凍結該 ISO URL；安裝層級單一 ISO（`isoPath`）退場（refine ADR 047） |
| [`050-compose-only-release-artifacts.md`](050-compose-only-release-artifacts.md) | Release 只產出 Compose installation：compose bundle、CLI binary、`release-manifest.json`、`offline-media-manifest.json` 與 `SHA256SUMS`；native bundle 待 native Temporal／PostgreSQL packaging 完成才發佈，離線 OCI 封存檔待支援離線安裝（含 MAAS images）才發佈；Temporal UI 不再鏡像，`SWALLOW_TEMPORAL_UI_IMAGE` 改由 operator 自設 digest；發佈機須在 `PATH` 上有 crane，不再以容器代跑；延後而非放棄 ADR 006／016 的 air-gap 目標（refine ADR 040） |

`001`–`003` 沿用先前的三位數命名，章節結構也與上方格式不同（Decision / Context /
Consequences / Rejected alternatives）。2026-09-05 已為三者補上 `Status`；但當時未記錄
`Date`，依「不得杜撰歷史理由」原則保留為 unrecorded 而非捏造。其論述結構刻意維持原樣。
`005` 起採用本文件定義的格式。（原 `004` 記錄的 AWX 執行模型已由 `006` 取代並移除。）

swallow 另有若干值得記錄的決策（例如 API contract 由 provider component 擁有），但這些理由
目前只存在於結構契約與實作中，尚未經確認。新增這些 ADR 時，請與知道當時脈絡的人確認，
或明確標注「（inferred）」。
