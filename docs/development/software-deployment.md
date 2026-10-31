# Software Deployment 設計、開發與規範

本文件是 swallow **software deployment**（Managed Software）的權威設計文件、開發指引與整合
規範。它把 [decision 038](../decisions/038-software-deployment.md) 的決策，以及 root glossary
的 [Managed Software](glossaries/terms/managed-software.md) /
[Software Assignment](glossaries/terms/software-assignment.md) 語言，落成「swallow 如何安裝一個
單一軟體」與「platform 如何組入一個軟體」的具體設計與規範。

它與 [platform-deployment.md](platform-deployment.md) 平行：platform deployment 佈署由多個
component 組成的 runtime；software deployment 佈署單一軟體及其變體。

> **目標結構**：依 [decision 070](../decisions/070-core-providers-and-extensions.md)，每個 Managed Software
> kind 由一個 **Software 模組**提供（見 [core-and-extensions.md](core-and-extensions.md)）；Software Assignment
> 已有 desired 與 last-applied，是最接近 [desired-state-model.md](desired-state-model.md) 的現有設計。
> 模組第一階段實作前，本文件描述的現行做法不變。

## 0. 權威分界：佈署對象，不是 OS 管線

**software 與 platform 的差別是 deploy target 的粒度，不是有沒有碰 OS provisioning。**

- **Software deployment** 的對象是**一個特定軟體，以及它的變體**（roles、版本、server/client、
  發行版安裝路徑）。NFS 的 `server` 與 `client` 是同一軟體的兩個變體；Docker CE 與 Podman 是
  兩個 software kind（互斥），不是同一軟體的兩個變體。
- **Platform deployment** 的對象是**一個 platform：由多個 component 組成的 runtime 整體**
  （k0s：binary + containerd + CNI + 角色；Slurm：slurmctld + slurmd + munge + 選用 login/NFS）。

兩者都是 convergent Workflow、都用 Ansible、都可能碰到 Server provisioning。**與 provisioning
的關係只是能力與預設編排的差異**：software 獨立入口預期目標已 `deployed`（與 `install-exporters`
相同），platform 常組 `ensure-os`。software Workflow **可以**以後組 `ensure-os`，platform **也
可以**只跑 existing OS；因此不能用「有沒有 provision OS」「有沒有 claim」「有沒有 membership」來
判斷這是 software 還是 platform——那些是後果，不是本質。

連動時最清楚：Slurm（platform）**組入** NFS（software）；未來 k8s **組入** Docker CE。Platform 是
組合體，software 是可被組入的單一軟體能力。

## 1. 心智模型：沿用 Workflow → Job → Task → Runner

software deployment 不新增編排引擎，直接沿用
[decision 017](../decisions/017-workflow-job-task-runner-model.md) 的模型與既有 Temporal
執行、lease、retry、`requires_attention`。

一次獨立 software 安裝的 Job 組成：

| Job | Task | Runner | 說明 |
| --- | --- | --- | --- |
| `prepare-hosts` | `wait-for-ssh` | `internal` | 獨立入口要求目標已 `deployed`；**驗證式 SSH 就緒**（以 Deployment Key 實際完成認證，非只探 TCP，見 [platform-deployment.md](platform-deployment.md) §4.6.1） |
| `configure-<kind>` | `ansible-playbook` | `ansible` | 一支 manifest 註冊的 idempotent playbook |
| `record-software` | `record-software-assignment` | `internal` | 成功後把 Software Assignment 標為 `installed` |

uninstall 對稱：`prepare-hosts` → `configure-<kind>`（跑 `uninstall-<kind>` playbook）→
`record-software` 的 `clear-software-assignment`（標 `absent`）。

**失敗語意**：install 的 ansible step 若失敗，`record-software` 不會執行；此時由 step observer 依
ansible step 上攜帶的 assignment 身分把 Software Assignment 標為 `failed`（見 §4.3）。

**SSH 就緒是驗證式、帳號 per-host 解析**：`prepare-hosts` 的 `wait-for-ssh` 會以 Deployment Key
（唯一的 automation 金鑰，decision 041）對每台目標**實際完成 SSH 認證**，不是只探 22 埠；且 SSH 登入帳號為
**per-host 解析**：所佈署 OS Image 有 default user 時只用它，否則以候選清單
（`[站台 sshUser, cloud-user, ubuntu]`，站台值優先）探測，因此可在同一 site 混用 ubuntu（`ubuntu`）與自訂
image（`cloud-user`）的機器。所有候選帳號都登不進去時（例如被以未授權 automation 公鑰的 image 重裝過）
會在此步驟就以清楚的 `ssh_authentication_failed` 失敗，而非拖到 ansible step 才報 `Permission denied
(publickey)`。細節與 fail-fast/grace 語意見 [platform-deployment.md](platform-deployment.md) §4.6.1。

## 2. Managed Software 目錄（第一刀）

| kind | 變體 / roles | spec 欄位 | 互斥 / 前提 |
| --- | --- | --- | --- |
| `docker-ce` | 無 role；選用 `version`；`enableApi` | `version`（選用，pin apt 套件版本）；`enableApi`（boolean，省略即 `true`，見 §4.6） | 與 `podman` 互斥；拒絕已是 Kubernetes member 的 Server |
| `podman` | 無 role；選用 `version` | `version`（選用） | 與 `docker-ce` 互斥；拒絕已是 Kubernetes member 的 Server |
| `nfs` | `server` / `client`（可同時） | server：`exportPath`、`exportOptions`；client：`source`（`host:/path`）、`mountPath`、`mountOptions` | 可與 Platform 共存 |
| `prometheus` | 無 role；`installMethod`；`containerRuntime` | `installMethod`（`container` 預設／`native`）、`containerRuntime`（container method：`docker-ce`／`podman`）、`version`（release 版本 `x.y.z`，預設 `3.15.0`）、`listen`（`loopback` 預設／`management`）、`retention`（Prometheus duration，預設 `15d`）、`scrapeJob`、`scrapeTargets`（`host:port`）、`scrapeServerIds` + `scrapePort`（同 Site 的 Server，每次執行時以 inventory 解析成當下的 primary address） | container method 需 runtime 已 `installed` |
| `grafana` | 無 role；`installMethod`；`containerRuntime` | `installMethod`、`containerRuntime`、`version`（預設 `12.4.12`）、`prometheusUrl`（省略＝同機 Prometheus）、`dashboards`（bundle：`omnistat`、`device-metrics-exporter`） | container method 需 runtime 已 `installed` |
| `device-metrics-exporter` | 無 role；只有 native 一種安裝路徑 | 無（版本固定 `1.5.3`，assignment spec 記錄 `version`） | GPU Inventory 須有 AMD compute GPU；拒絕 Kubernetes member 與帶 Platform Claim 的 Server（`refusedForClaimedServers`，install 與 uninstall 皆拒）；OS 限 Ubuntu 24.04（playbook 檢查） |

擴充新 kind（未來 PostgreSQL、LDAP client）就是新增一筆目錄 + 一支 playbook，前提是仍只依賴
本文件的契約。

**安裝方法**（[decision 063](../decisions/063-omnistat-slurm-observability.md)）：catalog 的
`installMethods` 列出 kind 可用的安裝方法（第一個是預設），`containerRuntimes` 列出 `container` method
可用的 runtime kind。Prometheus／Grafana：

- `container`（預設）：官方 image（`docker.io/prom/prometheus:v<version>`、
  `docker.io/grafana/grafana:<version>`），`docker-ce` 上用 Docker Compose project，`podman` 上用
  Quadlet unit（`/etc/containers/systemd/swallow-<kind>-container.container`，systemd 擁有生命週期，
  需 Podman ≥ 4.4）。獨立 install 時 runtime 必須已在每個 target `installed` 且一致；省略
  `containerRuntime` 即採 target 上已裝的 runtime，不滿足回 409。被 platform 組入時由同一個 Workflow
  較早的 runtime Job 安裝，因此不檢查狀態。
- `native`：Prometheus 為官方 release tarball（以 release 的 `sha256sums.txt` 驗證），放在
  `/opt/swallow/prometheus/<version>`，由 `swallow-prometheus.service`（系統使用者
  `swallow-prometheus`）執行；Grafana 為 Grafana 官方 apt／yum repository 的 pinned `grafana` 套件，
  由套件自己的 `grafana-server.service` 執行，Swallow 只加 drop-in
  （`grafana-server.service.d/10-swallow.conf`，`GF_*` 指向 management address 與 Swallow 擁有的
  provisioning／data 目錄），不改 `/etc/grafana`。uninstall 只移除帶 Swallow drop-in 的套件。

切換方法或 runtime 會先移除前一種的 artifacts（data 目錄保留並改 owner）。三種方法都用 host network，
位址語意一致：Grafana 綁 management address（`ansible_host`）；Prometheus 的 API 沒有驗證，預設只綁
`127.0.0.1`（`listen: management` 才綁 management address），並把位址寫進
`/etc/swallow/prometheus/endpoint`，同機 Grafana 的預設 datasource 讀這個檔。Grafana 是唯讀
（anonymous Viewer、無 login form、無 admin），不持有任何 secret。

**AMD Device Metrics Exporter**（[Device Metrics Exporter](glossaries/terms/device-metrics-exporter.md)）：
standalone 安裝與 Slurm 功能（§5）是同一個 kind、同一支 playbook 與 role（`roles/device_metrics_exporter`），
因此每個 Swallow 管理的 exporter 都有 Software Assignment。它只有一種安裝路徑：AMD 的 pinned native 套件
（`amdgpu-exporter`，SHA-256 驗證；沒有 `container` method）。exporter 綁 management address 的 port `5001`
（`5000` 是 Swallow 抓取的 RDC exporter）、profiler 關閉。請求不接受任何 spec 欄位（帶了即
`validation_error`），assignment 的 spec 記錄 `{ "version": "1.5.3" }`。job attribution 由 trusted var 決定：
standalone 為 `none`（`-enable-slurm-scl=false`），Slurm 組入時為 `slurm`。

- **目標資格**（建立 Workflow 前檢查，不符為 `409 conflict`）：[GPU Inventory](glossaries/terms/gpu-inventory.md)
  須列出 AMD compute GPU；拒絕 Kubernetes member（GPU Operator 自己跑 exporter）；拒絕帶
  [Platform Claim](glossaries/terms/platform-claim.md) 的 Server，install 與 uninstall 皆然（Platform 節點上的
  exporter 由該 Platform 安裝與移除）。
- **OS**：只支援 Ubuntu 24.04。Server projection 對 custom image 只報 `osSystem` `custom`，分辨不出發行版，
  因此由 playbook 在安裝任何東西之前檢查主機，不符即以 `device_metrics_exporter_unsupported_os` 失敗。
- **amdgpu driver**：`gpuagent` 依賴共用的 `swallow-amdgpu-load.service`。standalone install 會套
  `amdgpu_loader`（不加 dependents），uninstall 只在沒有其他 unit 帶 loader 的 ordering drop-in（slurmd、k0s）
  時才移除 loader；Slurm 組入時 loader 由 `slurm_gpu` 管。
- **驗證**：兩種模式都跑 identity check：exporter 沒有 GPU health metrics 即以
  `device_metrics_exporter_no_metrics` 失敗；distinct `gpu_id` 少於 GPU 數（collapsed，例如 MI308X SR-IOV VF）
  只記錄在 Task output，不使安裝失敗。Prometheus 抓取驗證與 result 檔只在帶 observability id（Slurm）時執行。
- Standalone 只提供 exporter，要看圖時另裝 Prometheus（`scrapeServerIds` + `scrapePort: 5001`）與 Grafana
  （`device-metrics-exporter` bundle）。

設計理由（為何是 Managed Software 而不只共用 role、為何沒有 container method、為何 standalone 不一併裝
Prometheus／Grafana、為何不從 projection 判斷 OS）見
[decision 068](../decisions/068-device-metrics-exporter-managed-software.md)。

## 3. Software Assignment（durable record）

Swallow-owned collection `software_assignments`，鍵 `(serverId, kind)`：

| 欄位 | 意義 |
| --- | --- |
| `kind` | 軟體 kind |
| `roles` | NFS：`server` / `client`（可同時）；runtime 無 role |
| `spec` | kind 專屬 desired |
| `state` | `pending` \| `installed` \| `failed` \| `uninstalling` \| `absent` |
| `lastWorkflowId` | 上次寫入的 Workflow |
| `lastAppliedAt` | 上次成功 ensure |

**收斂**：已 `installed` 且 spec 等價仍可再跑（playbook ensure 為 no-op）；spec 不同則再 ensure；
互斥 kind 已 `installed` → 4xx（在建立 Workflow 前擋下）。

**與 Workflow 一起寫入**（decision 069）：install 先在 store transaction 之外規劃 Workflow（lock 檢查與 target
解析），再在一個 transaction 內寫入 Workflow 與每台 target 的 `pending` assignment；uninstall 同樣把 Workflow 與
`uninstalling` 一起寫入。被拒的請求（target busy 或 locked）不留下 Workflow，也不留下 assignment 的變更。

**OS 生命週期**：Server 離開 `deployed`（release / recover 回 `ready` / 重裝）時，其所有 assignment
標為 `absent`（背景 sweep，週期與 membership sync 相同），避免「記錄說有 Docker、磁碟已無」。
Assignment 是 swallow-owned intent + last-applied，**不是**第四條 Server status 軸
（三軸 `provisioning`/`membership`/`health` 不變）。

## 4. 整合契約（published language）

沿用 [platform-deployment.md](platform-deployment.md) §4 的 inventory 與 trusted-vars 契約，
playbook 只依賴契約。

### 4.1 Dynamic inventory

與 platform 相同：`inventory_hostname` = serverId，連線位址 `ansible_host`，`--limit` 為本次
targets。software playbook 需要角色分組時，於 playbook 內以 `group_by` 依 trusted vars 建立
（例如 `nfs_server` / `nfs_client`），不假設 swallow 提供 software 專屬 group。

### 4.2 Trusted variables（client 不可偽造 `swallow_` 前綴）

- 每個 run 皆有 standard vars（`swallow_operation_id`、`swallow_site_id`、`swallow_server_ids`…）。
- software 專屬：
  - `swallow_software_kind`
  - `swallow_software_roles`：`{ serverId: ["server"|"client"|...] }`
  - NFS：`swallow_nfs_server_ids`、`swallow_nfs_client_ids`、`swallow_nfs_export_path`、
    `swallow_nfs_export_options`、`swallow_nfs_client_source`、`swallow_nfs_client_mount_path`、
    `swallow_nfs_client_mount_options`
  - Docker/Podman：`swallow_docker_version` / `swallow_podman_version`（選用）
  - Docker CE Engine API：`swallow_docker_enable_api`（boolean，每次 docker-ce run 都帶）、
    `swallow_docker_api_port`（目前固定 `2375`；由 api-server domain 擁有，explorer 連線用同一值）
  - Prometheus：`swallow_prometheus_install_method`、`swallow_prometheus_container_runtime`
    （native 時為空）、`swallow_prometheus_version`、`swallow_prometheus_listen`（`loopback`／`management`）、
    `swallow_prometheus_retention`、`swallow_prometheus_port`（固定 `9090`，domain 擁有）、
    `swallow_prometheus_scrape_job`、`swallow_prometheus_scrape_targets`、
    `swallow_prometheus_scrape_server_ids` + `swallow_prometheus_scrape_port`（由 playbook 以
    `hostvars[id].ansible_host` 解析；inventory 是該 Site 的執行當下快照）
  - Grafana：`swallow_grafana_install_method`、`swallow_grafana_container_runtime`、
    `swallow_grafana_version`、`swallow_grafana_port`（固定 `3000`）、
    `swallow_grafana_prometheus_url`（空＝同機）、`swallow_grafana_prometheus_port`、`swallow_grafana_dashboards`
  - Device Metrics Exporter：`swallow_device_metrics_exporter_version`、`swallow_device_metrics_exporter_port`
    （固定 `5001`，domain 擁有）、`swallow_device_metrics_exporter_job_attribution`（standalone 為 `none`）。
    Slurm 組入時另疊加 `_job_attribution: slurm`、`_compute_ids`、`_observability_id`、`_cluster_name`、
    `_prometheus_port`、`_grafana_port`、`_record_id`（缺 `_job_attribution` 而有 `_compute_ids` 時，
    playbook 視為 `slurm`，相容 068 之前的 run）

### 4.3 record / failure 契約

- `record-software-assignment`（internal）在 install 成功後把 assignment 標 `installed`、更新
  `lastAppliedAt`。
- `clear-software-assignment`（internal）在 uninstall 成功後把 assignment 標 `absent`。
- install 的 ansible step 攜帶 `softwareKind` 與 `serverIds` 參數；step observer 觀察到該 step 進入
  終態失敗（failed / requires_attention / canceled）時，把對應 assignment 標 `failed`。

### 4.4 Playbook manifest 與命名

- 於 [`api-server/automation/manifest.json`](../../api-server/automation/manifest.json) 註冊：
  `deploy-docker-ce` / `uninstall-docker-ce`、`deploy-podman` / `uninstall-podman`、
  `deploy-nfs` / `uninstall-nfs`、`deploy-prometheus` / `uninstall-prometheus`、
  `deploy-grafana` / `uninstall-grafana`、`deploy-device-metrics-exporter` /
  `uninstall-device-metrics-exporter`（舊名 `configure-device-metrics-exporter` 仍指向同一支
  playbook，供 068 之前受理的 Workflow 重試）。
- playbook 名由 launcher **硬編碼**（比照 Slurm / uninstall-kubernetes），**不需**站台
  `playbookMappings`。
- Python/collection 依賴 pin 於 `requirements.txt` / `requirements.yml`；只用 `ansible.builtin`
  即可者不新增 collection。

### 4.5 撰寫 software playbook 的硬規則

1. 冪等（ensure），可安全重跑。
2. 只依賴 §4.1 inventory 與 §4.2 trusted vars。
3. 以 serverId 為身分（`inventory_hostname`）。
4. 排序（handlers、host loop）留在 playbook。
5. 於 manifest 註冊、依賴鎖定、不逃逸 project root。
6. 可離線為目標（第一刀 lab 可走 distro / 官方 repo，但須 pin 版本；air-gap 為後續）。

### 4.6 Docker CE 的 Engine API 與 Docker Host Explorer

Docker CE 的 `enableApi` 變體讓 api-server 能即時管理主機上的 Docker Engine
（[Docker Host Explorer](glossaries/terms/docker-host-explorer.md)）。設計理由（為何由 api-server 代呼而非瀏覽器直連
或透明 proxy、為何不逐次跑 Workflow、為何不把 Docker 物件鏡射進 Mongo）見
[decision 043](../decisions/043-docker-host-management.md)。

- **Spec 正規化**：install 時 `enableApi` 省略即寫成 `true`，非 boolean 為 `validation_error`；
  因此新紀錄的 assignment `spec` 一定帶明確值。043 之前寫入、沒有此鍵的紀錄視為**未啟用**
  （當時主機上沒有 listener）。
- **Playbook 行為**（`roles/docker_ce/tasks/api.yml`）：啟用時寫 systemd drop-in
  `/etc/systemd/system/docker.service.d/swallow-api.conf`，在原本的 `-H fd://` 之外加
  `-H tcp://0.0.0.0:<port>`（Docker 官方的 remote access 作法；不碰 `daemon.json`，避免與操作者自己的
  `hosts` 設定衝突），daemon-reload 後重啟 dockerd 並等 port 開啟（60 秒內沒開即 install 失敗，不會留下
  `installed` 卻連不上的 assignment）；停用或卸載時移除 drop-in 並確認 port 已關。RHEL family 若 firewalld
  為 active，同步開/關該 port（runtime + permanent，不 reload）；ufw 不處理（MAAS Ubuntu image 預設未啟用）。
- **重啟代價**：drop-in 變更會重啟 dockerd，沒有 restart policy 的 container 會停止；dashboard 在切換處
  明示。
- **切換 = 重跑 ensure**：已安裝主機要開/關 API，就是同 kind 再送一次 install（spec 改變即再 ensure），
  不另設 Job 或 Workflow kind。
- **Explorer 邊界**：api-server 在 `/api/v1/servers/{id}/docker/...`（summary、images、containers、volumes、
  networks）同步呼叫主機的 Engine API，瀏覽器從不直連主機。這是刻意收斂、使用 Docker 原生名詞的路由集合
  （同源、admin 授權、經 provider-owned contract），不是透明的 Docker API proxy；新的 Engine 能力以明確路由加入。
  契約見 [servers-docker](../../api-server/docs/development/api-contracts/api-server/servers-docker.md)。
- **資格只看 swallow-owned 事實**：Server `deployed` 且有 primary address、`docker-ce` assignment
  `installed`、`enableApi: true`。swallow 不探測、也不使用 operator 在 swallow 之外開啟的 Docker API；Podman
  沒有 explorer。不符合時 explorer 回 `409`，consumer 應依 Software Assignment 決定是否提供 explorer。
- **不保存 Docker 物件**：images、containers、volumes、networks 屬於 Docker Engine，每次讀寫都在請求當下呼叫
  Engine，不寫入 Mongo；寫入是同步呼叫，不建立 Workflow。swallow 與 Docker 相關的狀態只有 Software Assignment
  與 §4.7 的 Registry Credential（swallow 自己的 secret，不是 Engine 物件的鏡射）。
- **寫入受 Server Lock 管制**：pull／remove image，以及 create／start／stop／restart／remove container、
  volume、network 在鎖定的 Server 上被拒；讀取照常可用。
- **安全**：listener 是無認證的 plain HTTP、綁所有介面，任何連得到該 port 的人等同取得主機 root，只適用內網。
  dashboard 在 install 選項與 Containers tab 要求啟用處顯示同一份風險提示，建議可從 Internet 連到的 Server
  保持關閉。TLS／mTLS 為後續 hardening（見 §6）。
- **Dashboard**：Server detail 的 "Containers" tab 只在 swallow 裝了 Docker CE 的 Server 上列出；該 tab 也能開關
  API，作法是送出只改了 `enableApi` 的完整 recorded spec 重跑 install。
- **`docker` group**（[decision 045](../decisions/045-server-default-user.md)）：`roles/docker_ce` 把
  `ansible_user`——也就是該 Server 的 effective Server Default User（Server 上設定的值，否則 OS Image 的
  default user）——加入 `docker` group（`root` 除外），讓該帳號不需 sudo 就能用 Docker；新的 group 在該帳號
  下次登入後生效。不另帶 trusted var；043 之前或本變更之前安裝的主機重跑 install 即補上。卸載不移除該帳號
  的 group membership。

### 4.7 Docker CE 的 Registry Credential

Docker Host Explorer 拉 private image 時使用 swallow 保存的
[Registry Credential](glossaries/terms/registry-credential.md)。設計理由（為何存在 swallow 而不是每次輸入、為何不在
主機上 `docker login`、為何是 installation 範圍）見 [decision 044](../decisions/044-docker-registry-credentials.md)。

- **擁有者與範圍**：swallow-owned、installation 級的資料（不分 Site、Server 或 User），存於 collection
  `software_registry_credentials`，以正規化的 registry（host 或 `host:port`）為唯一鍵（unique index），每個
  registry 至多一筆；registry 建立後不能改，要換就刪除再建立。正規化規則見契約。
- **密碼封存**：密碼（或 access token）以 installation credential key 封存，與 Integration credential、Deployment
  Key 相同；API 為 write-only，任何 response 都不回傳密碼，也不寫進 log 或錯誤訊息，只在 pull 當下解封一次。輪替
  credential key 後既有密碼無法解封、必須重新輸入；解封失敗會讓該次 pull 失敗，不會退回匿名 pull。
- **每次 pull 解析**：依 image reference 以 Docker 自己的規則決定 registry（沒有 registry host 的 reference 屬於
  `docker.io`）；有對應的 credential 就以 `X-Registry-Auth` 隨該次請求交給 Engine，否則匿名 pull。credential 不寫入
  主機。pull response 回報解析出的 registry 與是否帶了 credential；pull 失敗時，錯誤訊息附註這次是匿名或以哪個帳號
  登入，因為 registry 的拒絕訊息兩者看起來一樣。
- **主機上的 `docker login` 無效**：它只寫 CLI 的 `config.json`，Engine API pull 只認呼叫端送來的
  `X-Registry-Auth`，因此不使用。
- **管理不接觸外部**：建立、取代、刪除 credential 都不連 registry 也不連主機，也不在儲存時驗證；錯誤的 credential
  在第一次 pull 時以 registry 自己的理由呈現。
- **屬於 Docker CE 的設定**：API 在 `/api/v1/software/docker-ce/registry-credentials`（`/software/` 之下只有 catalog
  與 assignments 是各 kind 共用的）；dashboard 在 Docker CE 自己的 Settings 管理，其他 kind 有設定時各自另立。Pull
  image 對話框提示該 reference 會用哪一筆 credential，實際選擇以 api-server 為準。契約見
  [registry-credentials](../../api-server/docs/development/api-contracts/api-server/registry-credentials.md)。
- **安全**：§4.6 的 listener 是 plain HTTP，credential 以明文經網路送到主機；在該 listener 限定的內網上，讀得到這段
  流量的人本來就能控制主機。listener 的 TLS hardening 會一併涵蓋。

## 5. 與 Platform deployment 的連動（規範）

- Platform launcher 需要某個 software component 時，**把該 software 的 Job 組入 platform 的
  parent Workflow**（cross-Job `DependsOn`），共用同一組 Ansible roles。
- **首個實作**（[decision 063](../decisions/063-omnistat-slurm-observability.md)）：Slurm deploy 在
  `install-platform` 之後依序組入：
  1. container runtime Job（`configure-docker-ce` 或 `configure-podman`）：`containerRuntime` 功能的
     compute＋login node，加上 Omnistat `container` method 的 primary controller（無功能時採該機已
     assign 的 runtime，否則 Docker CE）；已 `installed` 的 Server 跳過，不覆蓋 operator 的 spec；
  2. Omnistat 或 Device Metrics Exporter 開啟時 `configure-prometheus`、`configure-grafana`（目標＝
     primary controller，spec 帶該功能的 `installMethod` 與 runtime）；
  3. Device Metrics Exporter 開啟時 `configure-device-metrics-exporter`（目標＝GPU compute nodes 的
     `device-metrics-exporter`，[decision 068](../decisions/068-device-metrics-exporter-managed-software.md)）。
     其 `install-device-metrics-exporter` Task 同時帶 software 參數（因此安裝失敗時 assignment 隨之標
     `failed`）與 Platform 的 owner／record 參數，Platform 的 DME State、GPU identity、Monitoring Link 照舊由
     step observer 與 result 檔記錄；Slurm 專屬 trusted vars（job attribution、cluster name label、observability
     Server 上的抓取驗證，見 §4.2）疊加在 software vars 上。Slurm uninstall 照舊在每個 target 移除 DME，成功時把
     該 Platform 各 Server 的 `device-metrics-exporter` assignment 標為 `absent`，紀錄不會比套件活得久。

  每個 Job 是「該 kind 的 ansible Task（`install-<kind>`，帶 `softwareKind`／`serverIds`／
  `softwareMode=install`）＋ `record-<kind>` internal record Task」，下一個 Job 依賴上一個的 record
  Task。組入用的 trusted vars 與驗證由 `SoftwareService.PlanComposedInstall` 產生（同樣的
  catalog／role／spec／互斥／k8s 規則，但不檢查 deployed 與 runtime 狀態，因為由 Job 順序保證）；
  launcher 在寫入 Workflow 的同一個 transaction 中呼叫 `RecordComposedInstall` 寫入 pending assignment
  （decision 069），寫入失敗會讓整個 deploy 受理失敗；安裝失敗由既有 step observer 標 `failed`。
- **禁止** `ExecuteChildWorkflow` 一個完整的獨立 software Workflow（會重複 `wait-for-ssh`、搶同一
  per-Server lease、retry/cancel 歸屬混亂、出現兩個 operator-visible Workflow）。
- **禁止**只用 ansible `include_role` 當唯一連動（會抹掉 Job 邊界與 dashboard 分組）。playbook 內部
  仍可 `include_role`，但 Job 邊界由 launcher 決定。

## 6. 第一刀不做（誠實現況）

- **不拆 Slurm 內嵌的 NFS**：Slurm HA state（`slurm_state_server` / `slurm_controller_state`）與
  workload storage 仍留在 `slurm_*` roles。過渡期存在「兩套 NFS 故事」——**generic NFS ≠ Slurm
  managed state NFS**，直到 slice 2 收斂。
- 不做 k8s + Docker runtime、不做 PostgreSQL / LDAP、不把 software 做成 Platform 形狀的聚合、
  不做持續 package 輪詢、不做 air-gap 套件庫。（Slurm launcher 的組入已由 decision 063 實作，見 §5。）
- **Docker Engine API 的 hardening**：listener 的 TLS／mTLS 認證，或改由 api-server 經 SSH tunnel 連 local
  socket；做的時候一併處理 Registry Credential 的明文傳輸。
- **Swarm 與 fleet 級 Docker 管理**：Docker Swarm 或跨 Server 的 Docker 檢視可能需要 swallow 保存 Docker 資料，
  會推翻 §4.6 的「不保存 Docker 物件」，須另立 ADR；依 §0 的分界，跨主機組成的 Swarm 會是 Platform 而非
  Managed Software。
- **Registry Credential 的延伸**：同一 registry 的 Site 或 Server 範圍 credential（需要優先序模型），以及儲存時
  驗證（需要一台主機代為呼叫 Engine 的 `/auth`）。
- **Kubernetes 與 standalone Device Metrics Exporter**：把 Kubernetes 部署到已有 standalone exporter 的 Server，
  目前沒有處理。

## 7. 程式與內容位置對照（導航用）

| 職責 | 路徑 |
| --- | --- |
| Software domain（Assignment、Catalog、repository 介面） | `api-server/internal/core/software/domain/` |
| Software use cases（Install / Uninstall / List / Catalog / Sweep） | `api-server/internal/core/software/application/` |
| Software Mongo repo | `api-server/internal/adapters/store/mongodb/software/` |
| Software HTTP handler | `api-server/internal/core/software/delivery/` |
| Software launcher（Job 組裝） | `api-server/internal/app/software_deployment_adapter.go` |
| record/clear internal step | `api-server/internal/app/platform_workflow_step_executor.go` |
| 失敗標記 observer | `api-server/internal/app/software_assignment_observer.go` |
| Playbooks 與 roles | `api-server/automation/playbooks/deploy-*.yml`、`roles/docker_ce`（Engine API listener：`tasks/api.yml`）、`roles/podman`、`roles/nfs_server`、`roles/nfs_client`、`roles/device_metrics_exporter`（移除：`tasks/uninstall.yml`，`uninstall-slurm` 共用） |
| Docker Host Explorer（view types、use case、Engine client、handler） | `api-server/internal/core/software/domain/docker.go`（port 常數 `DockerEngineAPIPort` 在 `domain/software.go`）、`application/docker_explorer.go`、`delivery/docker_handler.go`、`api-server/modules/software/docker-ce/dockerengine/` |
| Registry Credential（registry 正規化與解析、service、sealed Mongo repo、handler） | `api-server/internal/core/software/domain/registry.go`、`application/registry_credentials.go`、`delivery/registry_credential_handler.go`、`api-server/internal/adapters/store/mongodb/software/mongo_registry_credential_repo.go` |
| Device Metrics Exporter（pinned version、port、job attribution） | `api-server/internal/core/software/domain/device_metrics_exporter.go`；Slurm 組入：`api-server/internal/app/slurm_composition_adapter.go` |
| API 契約 | `api-server/docs/development/api-contracts/api-server/software.md`、`servers-docker.md`、`registry-credentials.md` |

## 8. 相關文件

- 決策：[decision 038 Software deployment](../decisions/038-software-deployment.md)、
  [decision 043 Docker host management](../decisions/043-docker-host-management.md)、
  [decision 044 Docker registry credentials](../decisions/044-docker-registry-credentials.md)、
  [decision 068 Device Metrics Exporter Managed Software](../decisions/068-device-metrics-exporter-managed-software.md)、
  [decision 017 Workflow/Job/Task/Runner](../decisions/017-workflow-job-task-runner-model.md)、
  [decision 019 Slurm platform deployment](../decisions/019-slurm-platform-deployment.md)。
- 語言：glossary [Managed Software](glossaries/terms/managed-software.md)、
  [Software Assignment](glossaries/terms/software-assignment.md)、
  [Docker Host Explorer](glossaries/terms/docker-host-explorer.md)、
  [Registry Credential](glossaries/terms/registry-credential.md)、
  [Device Metrics Exporter](glossaries/terms/device-metrics-exporter.md)、[Platform](glossaries/terms/platform.md)。
- 平行設計：[platform-deployment.md](platform-deployment.md)。
- API 契約（`api-server` 擁有）：
  [software](../../api-server/docs/development/api-contracts/api-server/software.md)、
  [servers-docker](../../api-server/docs/development/api-contracts/api-server/servers-docker.md)、
  [registry-credentials](../../api-server/docs/development/api-contracts/api-server/registry-credentials.md)。
