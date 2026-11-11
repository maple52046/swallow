# Software Deployment 設計、開發與規範

本文件是 swallow **software deployment**（Managed Software）的權威設計文件、開發指引與整合
規範。它把 [decision 038](../decisions/038-software-deployment.md)（software deployment） 的決策，以及 root glossary
的 [Managed Software](glossaries/terms/managed-software.md) /
[Software Assignment](glossaries/terms/software-assignment.md) 語言，落成「swallow 如何安裝一個
單一軟體」與「platform 如何組入一個軟體」的具體設計與規範。

它與 [platform-deployment.md](platform-deployment.md) 平行：platform deployment 佈署由多個
component 組成的 runtime；software deployment 佈署單一軟體及其變體。

> **讀者**：新增 Managed Software kind（§2）、修改 install／uninstall 的 Workflow 與 Software Assignment 的狀態
> （§1、§4）、或把 kind 組入 platform 部署（§5）的開發者與 agent。只透過 API 列出或顯示 Software Assignment
> 時不需要本檔，讀 software 契約即可。本檔很長：先看標題，再讀任務需要的章節。

> **模組**：每個 Managed Software kind 由一個 **Software 模組**提供（[decision 070](../decisions/070-core-providers-and-extensions.md)（core providers and extensions）、
> [decision 075](../decisions/075-module-contract-v1.md)（module contract v1），見 [core-and-extensions.md](core-and-extensions.md)）。core 不認得任何
> kind：catalog 由各模組的 `software` hook 組成（§2）。kind 專屬的產品常數（版本、port）仍在 core，之後隨 Platform
> 模組一起搬出。Managed Software 中只有 Node Exporter 已遷移到 [desired-state-model.md](desired-state-model.md)：
> 它的 desired 由政策推導，由 worker 的 Node Exporter controller 收斂（§2 的 Node Exporter）。其他 kind 的 install 與
> uninstall 仍是由 user 或計畫觸發的動作，Software Assignment 記錄最後一次安裝的 spec 與結果，主機上被外部移除或
> 停止不會被發現；新行為依該文件 §1.1 先設計成 desired 與收斂。

## 0. 權威分界：佈署對象，不是 OS 管線

**software 與 platform 的差別是 deploy target 的粒度，不是有沒有碰 OS provisioning。**

- **Software deployment** 的對象是**一個特定軟體，以及它的變體**（roles、版本、server/client、
  發行版安裝路徑）。NFS 的 `server` 與 `client` 是同一軟體的兩個變體；Docker CE 與 Podman 是
  兩個 software kind（互斥），不是同一軟體的兩個變體。
- **Platform deployment** 的對象是**一個 platform：由多個 component 組成的 runtime 整體**
  （k0s：binary + containerd + CNI + 角色；Slurm：slurmctld + slurmd + munge + 選用 login/NFS）。

兩者都是 convergent Workflow、都用 Ansible、都可能碰到 Server provisioning。**與 provisioning
的關係只是能力與預設編排的差異**：software 獨立入口預期目標已 `deployed`，platform 常組
`ensure-os`，OS deployment 則在部署好的 OS 上組入 Node Exporter（§5）。software Workflow
**可以**以後組 `ensure-os`，platform **也可以**只跑 existing OS；因此不能用「有沒有 provision OS」「有沒有 claim」「有沒有 membership」來
判斷這是 software 還是 platform——那些是後果，不是本質。

連動時最清楚：Slurm（platform）**組入** NFS（software）；未來 k8s **組入** Docker CE。Platform 是
組合體，software 是可被組入的單一軟體能力。

## 1. 心智模型：沿用 Workflow → Job → Task → Runner

software deployment 不新增編排引擎，直接沿用
[decision 072](../decisions/072-store-driven-workflow-execution.md)（store driven workflow execution） 的模型；資源所有權、重試、`requires_attention`
與完成行為照 [workflow-execution.md](workflow-execution.md)。

一次獨立 software 安裝的 Job 組成：

| Job | Task | Runner | 說明 |
| --- | --- | --- | --- |
| `prepare-hosts` | `wait-for-ssh` | `internal` | 獨立入口要求目標已 `deployed`；**驗證式 SSH 就緒**（以 Deployment Key 實際完成認證，非只探 TCP，見 [platform-deployment.md](platform-deployment.md) §4.6.1） |
| `configure-<kind>` | `ansible-playbook` | `ansible` | 一支 manifest 註冊的 idempotent playbook |
| `record-software` | `record-software-assignment` | `internal` | 成功後把 Software Assignment 標為 `installed` |

uninstall 對稱：`prepare-hosts` → `configure-<kind>`（跑 `uninstall-<kind>` playbook）→
`record-software` 的 `clear-software-assignment`（標 `absent`）。

**失敗語意**：install 的 ansible Task 若失敗，`record-software` 不會執行；此時由該 Task 宣告的完成行為依
它攜帶的 assignment 身分把 Software Assignment 標為 `failed`。record Task 宣告同一個完成行為，所以 record 被取消、
失敗或略過時 assignment 也是 `failed`，不會停在 `pending`（見 §4.3）。

**SSH 就緒是驗證式、帳號 per-host 解析**：`prepare-hosts` 的 `wait-for-ssh` 會以 Deployment Key
（唯一的 automation 金鑰，[decision 039](../decisions/039-ssh-key-management-and-default-user.md)（ssh key management and default user））對每台目標**實際完成 SSH 認證**，不是只探 22 埠；且 SSH 登入帳號為
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
| `grafana` | 無 role；`installMethod`；`containerRuntime` | `installMethod`、`containerRuntime`、`version`（預設 `12.4.12`）、`prometheusUrl`（省略＝同機 Prometheus）、`dashboards`（bundle：`node-exporter`、`omnistat`、`device-metrics-exporter`） | container method 需 runtime 已 `installed` |
| `device-metrics-exporter` | 無 role；只有 native 一種安裝路徑 | 無（版本固定 `1.5.3`，assignment spec 記錄 `version`） | GPU Inventory 須有 AMD compute GPU；拒絕 Kubernetes member 與帶 Platform Claim 的 Server（`refusedForClaimedServers`，install 與 uninstall 皆拒）；OS 限 Ubuntu 24.04（playbook 檢查） |
| `node-exporter` | 無 role；只有 OS 套件一種安裝路徑 | 無（版本由 OS 套件決定） | 可與 Platform 共存；OS 限 Debian／Ubuntu 與 RHEL 家族（playbook 檢查） |
| `victoriametrics` | 無 role；只有 native 一種安裝路徑 | `listen`（`loopback` 預設／`management`）、`retention`（整數加 `d`／`w`／`M`／`y`，預設 `1M`，至多 100 年）；版本固定 `1.155.0`，assignment spec 記錄 `version` | 可與 Platform 共存；只宣告 amd64 的 release；只在 Ubuntu 24.04 驗證過，RHEL 家族未驗證 |

catalog 是 build 編進的 Software 模組，API 依 `kind` 排序回傳。擴充新 kind（未來 PostgreSQL、LDAP client）
就是新增一個 Software 模組，前提是仍只依賴本文件的契約：

- `modules/software/<kind>/module.json`：模組 id 就是 kind；宣告 Workflow kind `configure-<kind>`、
  `uninstall-<kind>`，playbook `deploy-<kind>`、`uninstall-<kind>`，以及 `software` hook 與它的理由。
- root Go package 實作 `software` hook（core 的 port `Software`）：`Entry` 回傳 catalog entry，`ValidateSpec`、
  `NormalizeSpec`、`InstallVars` 處理 spec 與 trusted vars，都不做 I/O。spec 指名其他 Server 時另實作
  `ServerReferences`，core 會檢查它們與 target 在同一個 Site。
- 模組可在 manifest 以 `spec` 宣告 spec schema（JSON Schema 子集），屬性必須等於 catalog entry 的 `SpecFields`。
  core 在 `ValidateSpec` 之前以 schema 拒絕未知欄位與型別錯誤，hook 只寫跨欄位或依 role 的規則；dashboard 對沒有
  專屬設定畫面的 kind 依 schema 產生安裝表單與「View settings」詳情。目前 NFS 與 VictoriaMetrics 宣告了 schema。
- composition root 啟動時核對上述名稱並組出 catalog，這些 Workflow kind 要求 target 為 `deployed`。
- 撰寫步驟見[模組撰寫指南](module-authoring.md) §1.1、§1.2。
- **沒有參數的 kind**：catalog entry 的 `SpecFields` 為空，spec 可以省略或為 `{}`，`ValidateSpec` 對帶任何欄位的
  spec 回 `ErrSpecInvalid`（API 為 `400 validation_error`），比照 Device Metrics Exporter。只有選用欄位的 kind
  （例如 Podman 的 `version`）照常宣告欄位。

模組目錄之外還要改的地方，漏改時多數沒有測試會失敗：

- `internal/app/modules.go` 的 `compiledModules`；`.github/CODEOWNERS` 登錄模組目錄（有測試檢查）。
- `modules/manifest.json` 登錄 `deploy-<kind>` 與 `uninstall-<kind>`；
  `internal/adapters/hostautomation/ansible/software_playbook_test.go` 寫死了這份清單，一併加上。
- OpenAPI `areas/workflows.yaml` 的 `WorkflowKind` enum 加上 `configure-<kind>`、`uninstall-<kind>`，接著重新產生
  `openapi.yaml` 與 dashboard 的型別（[api-contracts README](../../api-server/docs/development/api-contracts/README.md)
  的 OpenAPI 一節）。`SoftwareKind` 在契約中是 pattern，不必改。
- dashboard 的專屬呈現（選用：沒有時 kind 以通用的呈現列在 Other software，[模組撰寫指南](module-authoring.md) §1.2）。
- 本文件 §2 的 kind 表與 §7 的位置表加上新 kind。
- software 契約：新 kind 有專屬的欄位或規則時補一節（比照 Device Metrics Exporter）。
- 列出 kind 的公開頁面：Managed Software 指南、concepts、dashboard 指南，以及它們的 zh-TW 對應頁。

**安裝方法**（[decision 063](../decisions/063-omnistat-slurm-observability.md)（omnistat slurm observability））：catalog 的
`installMethods` 列出 kind 可用的安裝方法（第一個是預設），`containerRuntimes` 列出 `container` method
可用的 runtime kind。Prometheus／Grafana：

- `container`（預設）：官方 image（`docker.io/prom/prometheus:v<version>`、
  `docker.io/grafana/grafana:<version>`；Grafana 的 image 宣告在模組的 `artifacts.images`，以
  `swallow_artifacts.images` 對應的 reference 拉取：Site 使用 Local Repository 時是
  `<registry>/docker.io/grafana/grafana:<version>`，否則是原本的 reference），`docker-ce` 上用 Docker Compose project，`podman` 上用
  Quadlet unit（`/etc/containers/systemd/swallow-<kind>-container.container`，systemd 擁有生命週期，
  需 Podman ≥ 4.4）。獨立 install 時 runtime 必須已在每個 target `installed` 且一致；省略
  `containerRuntime` 即採 target 上已裝的 runtime，不滿足回 409。被 platform 組入時由同一個 Workflow
  較早的 runtime Job 安裝，因此不檢查狀態。
- `native`：Prometheus 為官方 release tarball（以 release 的 `sha256sums.txt` 驗證），放在
  `/opt/swallow/prometheus/<version>`，由 `swallow-prometheus.service`（系統使用者
  `swallow-prometheus`）執行；Grafana 為 Grafana 官方 apt／yum repository 的 pinned `grafana` 套件（apt 經 Local
  Repository 鏡像的模組來源 `grafana` 取得），
  由套件自己的 `grafana-server.service` 執行，swallow 只加 drop-in
  （`grafana-server.service.d/10-swallow.conf`，`GF_*` 指向 management address 與 swallow 擁有的
  provisioning／data 目錄），不改 `/etc/grafana`。uninstall 只移除帶 swallow drop-in 的套件，連同 purge 之後套件
  仍留下的 `grafana` 使用者與 group、`/etc/grafana`、`/var/lib/grafana`（Grafana 啟動時解開的 plugin）與
  `/var/log/grafana`；drop-in 最後才移除，中途失敗的 uninstall 重跑時仍會完成。

切換方法或 runtime 會先移除前一種的 artifacts（data 目錄保留並改 owner）。三種方法都用 host network，
位址語意一致：Grafana 綁 management address（`ansible_host`）；Prometheus 的 API 沒有驗證，預設只綁
`127.0.0.1`（`listen: management` 才綁 management address），並把位址寫進
`/etc/swallow/prometheus/endpoint`，同機 Grafana 的預設 datasource 讀這個檔。Grafana 是唯讀
（anonymous Viewer、無 login form、無 admin）。組入 Platform 時，datasource 可以帶 Platform 的 monitoring token
（secret var `swallow_grafana_datasource_token`，[monitoring.md](monitoring.md) §7）：token 只寫在 root 才能讀的
environment file，由 systemd、podman 或 Docker Compose 以 root 讀取後交給 Grafana，datasource 檔只引用環境變數，
所以 Grafana 自己的檔案不持有它。只在一台 Server 上執行的 install 在所有 dashboard 都能讀到之後，把
`{grafanaUrl}`（`http://<management address>:3000` 加上首頁 dashboard 的 `/d/<uid>/<slug>`，見下方的 dashboard bundles；
沒有 bundle 時就是這個位址）寫進 run 的 result 檔；Platform 組入的 Grafana 以它記錄 Monitoring Link。

**dashboard bundles**（共用 role `grafana_provisioning` 的 `files/dashboards/<bundle>/`，全部以 datasource UID `prometheus` 讀取）：
datasource、dashboard provider 與 bundles 是每個 swallow 安裝的 Grafana 共用的一份：grafana role 以 `tasks_from: files`
把它們寫成 provisioning 檔，Kubernetes 的 Platform Grafana 以 `tasks_from: kubernetes` 取得 Helm values 與每個 bundle 的
ConfigMap（[platform-deployment.md](platform-deployment.md) §2.3.3）。模組之間不讀彼此的檔案，所以兩者都不自帶副本。

| bundle | 內容 | 來源 |
| --- | --- | --- |
| `node-exporter` | Servers：CPU、memory、root filesystem 使用率、network 收送 bytes/s、load average；以 `server_id` 變數（`label_values(node_uname_info, server_id)`）選 Server，圖例以 `nodename` 標示 | swallow 自己撰寫 |
| `omnistat` | Omnistat 的 RMS global、node、job | 上游 Omnistat，固定版本 |
| `device-metrics-exporter` | DME 的 Slurm overview、GPU、node、job | 上游 DME，固定版本 |

**首頁 dashboard**：swallow 記錄的 `Platform Grafana` 連結直接開啟 Grafana 的首頁 dashboard，網址是該 dashboard
自己的 `/d/<uid>/<slug>`。每個 bundle 的首頁以 JSON 裡固定的 uid 與 Grafana 由它的標題算出的 slug 登錄在
`grafana_provisioning` 的 `grafana_provisioning_landing_dashboards`（`grafana_role_test.go` 對照 JSON 檢查兩者），所以網址
跨版本、跨重啟不變：`device-metrics-exporter` 是 Overview（`/d/fe0ayyeeoxvy8f/overview`）、`omnistat` 是 Global - RMS
（`/d/edqnz7pce7togc-rms/global-rms`）、`node-exporter` 是 Servers（`/d/swallow-node-exporter/servers`）。Grafana 有多個
bundle 時取 `grafana_provisioning_landing_order` 的第一個（`device-metrics-exporter`、`omnistat`、`node-exporter`）。共用
role 的 `grafana_provisioning_landing_path` 由 `grafana_provisioning_bundles` 算出 `/d/<uid>/<slug>`，沒有 bundle 時為空；
grafana role 與 Kubernetes 的 Platform Grafana 只在回報的 task 把它接在 Grafana 位址之後，health 與 dashboard 的等待仍打
Grafana 位址。include 的 vars 在 include 結束後失效，所以回報的 task 以自己 role 的 bundle 清單重設 `grafana_provisioning_bundles`。

新增 bundle 時，同時在 `internal/core/software/domain/observability.go` 的 bundle 清單與 `grafana_provisioning` 的
`grafana_provisioning_folders` 登錄（`grafana_role_test.go` 檢查兩邊一致）；它要能成為首頁時，也登錄首頁的 uid 與 slug 並決定在
`grafana_provisioning_landing_order` 的位置。

**AMD Device Metrics Exporter**（[Device Metrics Exporter](glossaries/terms/device-metrics-exporter.md)）：
standalone 安裝與 Slurm 功能（§5）是同一個 kind、同一支 playbook 與 role（`roles/device_metrics_exporter`），
因此每個 swallow 管理的 exporter 都有 Software Assignment。它只有一種安裝路徑：AMD 的 pinned native 套件
（`amdgpu-exporter`，SHA-256 驗證；沒有 `container` method）。exporter 綁 management address 的 port `5001`
（`5000` 是 RDC exporter 的 port，仍保留）、profiler 關閉。請求不接受任何 spec 欄位（帶了即
`validation_error`），assignment 的 spec 記錄 `{ "version": "1.5.3" }`。job attribution 由 trusted var 決定：
standalone 為 `none`（`-enable-slurm-scl=false`），Slurm 組入時為 `slurm`。

- **目標資格**（建立 Workflow 前檢查，不符為 `409 conflict`）：[GPU Inventory](glossaries/terms/gpu-inventory.md)
  須列出 AMD compute GPU；拒絕 Kubernetes member（GPU Operator 自己跑 exporter，也在 host 的 port 5001）；拒絕帶
  [Platform Claim](glossaries/terms/platform-claim.md) 的 Server，install 與 uninstall 皆然（Platform 節點上的
  exporter 由該 Platform 安裝與移除）。
- **OS**：只支援 Ubuntu 24.04。Server projection 對 custom image 只報 `osSystem` `custom`，分辨不出發行版，
  因此由 playbook 在安裝任何東西之前檢查主機，不符即以 `device_metrics_exporter_unsupported_os` 失敗。
- **amdgpu driver**：`gpuagent` 依賴共用的 `swallow-amdgpu-load.service`。standalone install 會套
  `amdgpu_loader`（不加 dependents），uninstall 只在沒有其他 unit 帶 loader 的 ordering drop-in（slurmd、k0s）
  時才移除 loader；Slurm 組入時 loader 由 `slurm_gpu` 管。
- **驗證**：兩種模式都跑 identity check：exporter 沒有 GPU health metrics 即以
  `device_metrics_exporter_no_metrics` 失敗；distinct `gpu_id` 少於 GPU 數（collapsed，例如 MI308X SR-IOV VF）
  只記錄在 Task output，不使安裝失敗。每台 exporter host 先確認自己的 exporter 在 bind address 回應；Site 的
  Prometheus 依 Software Assignment 收集它（[monitoring.md](monitoring.md) §5.2），所以不檢查任何 Prometheus。result 檔
  `{gpuIdentity, nodes}` 只在 Slurm 組入（帶 compute ids）時寫。
- Standalone 只提供 exporter；Site 的 Prometheus 已經收集它，要看圖時另裝讀那套 Prometheus 的 Grafana
  （`device-metrics-exporter` bundle）。

設計理由（為何是 Managed Software 而不只共用 role、為何沒有 container method、為何 standalone 不一併裝
Prometheus／Grafana、為何不從 projection 判斷 OS）見
[decision 068](../decisions/068-device-metrics-exporter-managed-software.md) (device metrics exporter managed software)。

**Node Exporter**：Prometheus 的 host exporter，安裝 OS 發行版的套件（Debian／Ubuntu 以 apt 安裝
`prometheus-node-exporter`；RHEL 家族以 dnf 安裝 `node_exporter`，來自 EPEL），由套件自己的 systemd service 執行，
port `9100`。主機不安裝也不需要 Docker 或任何 container。

- **組入 OS 部署**：每個 OS 部署都在它 provision 的每台 Server 上安裝 Node Exporter（§5），讓 Server 一部署好就有
  監控。之後的變化，以及 swallow 沒有部署的 Server（existing-OS enrollment 或匯入、直接在 MAAS 部署、OS image
  verification 留下的 Server），由下面的 desired state 與 controller 負責。
- **目標資格**：沒有這個 kind 自己的規則。OS provisioning 在每台部署的 Server 上帶入 Node Exporter，主機上永遠
  只有一個 node exporter，所以任何 Platform 的成員都與其他 deployed Server 一樣接受；locked 的 Server 由每個 install
  共同的 lock 檢查拒絕（`409 conflict`）。請求不接受任何 spec 欄位。
- **OS**：playbook 依 OS 家族選套件；其他 OS 家族以 `node_exporter_unsupported_os` 失敗，repository 沒有該套件
  （例如 RHEL 家族未啟用 EPEL）以 `node_exporter_package_unavailable` 失敗。
- **舊的 container unit**：安裝前先停止並移除 swallow 舊的 containerised node-exporter（systemd unit
  `swallow-node-exporter.service` 與它的 container，佔用 port 9100）；Docker 本身與主機上已有的 RDC container 不動。
- uninstall 停止 service 並移除套件。

Node Exporter 是 desired state，由 worker 的 Node Exporter controller 收斂：

- **desired（由政策推導）**：每台存在（不是 absent）、provisioning 為 `deployed`（disk 與 RAM 部署都算）、沒有 lock，
  而且所屬 Site 的 automation 已啟用（Site 的 Automation Configuration）的 Server，都應該有 Node Exporter。這是推導出的
  desired（[desired-state-model.md](desired-state-model.md) §3.2）：沒有 spec 欄位、沒有寫入者，也沒有 `generation` 與
  conditions；政策本身是本規格的一部分。不符合條件的 Server 不受管：controller 不安裝，也不卸載，因為卸載是破壞性的
  （desired-state-model.md §5.2）。
- **observed**：該 Server 的 `node-exporter` Software Assignment 的 state，以及它的 Site Prometheus 對該 Server
  node-exporter target 的 `up`（[monitoring.md](monitoring.md) §5.2；讀取時查詢，不保存）。Site 沒有 Prometheus、target
  在這段時間沒有被收集，或讀取失敗時，結果是 unknown：controller 只依 assignment 動作，從不依 unknown 的觀察動作。
- **收斂**（worker 的 `node-exporter` controller，desired-state-model.md §7）：以 Server 為 key，由 `servers` 與
  `software_assignments` 的 change feed 喚醒，定期 resync（`controllerResyncInterval`，預設 10 分鐘）時看每台 `deployed`
  的 Server。對受政策涵蓋、沒有未結束的 Workflow 佔用的 Server：
  - 沒有 assignment 或 assignment 為 `absent`：安裝。
  - `failed`：resync 時依退避重試，從 assignment 轉為 `failed` 起算 15 分鐘、1 小時、6 小時，之後每 24 小時，依
    controller 自己連續未成功的安裝次數決定等多久。
  - `installed`：resync 時讀 Site Prometheus。過去 10 分鐘（寬限時間）的每一次 scrape 都失敗、assignment 已
    `installed` 超過寬限時間，而且 provisioner 沒有回報主機關機時，再安裝一次（role 是 ensure：套件存在、service
    enabled 並啟動）。再安裝之後 exporter 仍一次都沒有被成功收集時，同樣依上面的退避等待下一次。
  - `pending`、`uninstalling`，或有任何未結束的 Workflow 佔用 Server（[workflow-execution.md](workflow-execution.md)
    §4）：現在不動作。
  - 「連續未成功的安裝」是 controller 自己啟動的最近幾次 install 中，失敗、被取消，或成功之後 exporter 一次都沒有被
    成功收集的那些；遇到別人在這台 Server 上的任何 Workflow，或 48 小時以前結束的安裝，就停止計算，退避重新開始。
    所以 OS 不支援、repository 沒有套件或 port 收集不到的 Server 最多每天一次 install，不會形成迴圈。
  - change feed 觸發時只依 assignment 的 state 動作（安裝缺少的、取消自己停住的 install）；讀 Prometheus 與重試失敗
    的安裝只在 resync，因為 reconcile 每一輪都會改寫每台 Server。安裝在受理時被拒（例如 Site 的 automation 還無法
    登入）的 Server，10 分鐘內不會因 change 再試，下一次 resync 照常再試。
- **安裝的路徑**：與 user 的 install 相同（`SoftwareService.Install`：同樣的檢查，在一個 transaction 中寫入 Workflow 與
  `pending` assignment）。Workflow 是一般的 `configure-node-exporter`，`requestedBy` 為
  `swallow:node-exporter-controller`，所以 assignment、Workflow 與 timeline 都和任何 install 一樣。
- **不留下停住的 install**：install 的 Ansible Task 失敗時 Workflow 停在 `requires_attention`，仍佔著 Server。controller
  取消自己啟動、已經停住（或 install Task 已失敗、還沒停住）的 install，原因為
  `The Node Exporter install failed; swallow retries it later.`；被取消的 install 與 record Task 經
  `software-assignment-failure` 把 assignment 標為 `failed`（§4.3），Server 被釋放，之後依退避重試。它只取消
  `requestedBy` 是自己的 `configure-node-exporter` Workflow，從不取消 user、OS 部署、Platform 或其他 controller 的
  Workflow。
- **auto-correct**（desired-state-model.md §6.2）：Node Exporter 被外部移除、service 停止或開機後起不來，是 controller
  自動修正的 drift。理由：監控涵蓋率是 swallow 自己的事（Server 的 health 與基礎監控都依賴它，monitoring.md §8），
  而安裝是冪等、非破壞性的 ensure，只確保套件存在、service enabled 並啟動，不動 workload，也不重開機。修正在 resync
  時發生（§6.2 的時機），每次修正都是一個看得到的 Workflow。
- **組入照舊**：OS 部署、Platform 部署的 `ensure-os` 與保留主機的 Platform uninstall 仍組入安裝（§5），讓 Server 一部署
  好、uninstall 一結束就有 Node Exporter；controller 負責之後的變化。
- **user 的 uninstall**：受政策涵蓋的 Server 被 uninstall 後 assignment 為 `absent`，controller 會再安裝。要讓某台
  Server 不裝 Node Exporter，lock 它，或關閉該 Site 的 automation。
- **設定**：`SWALLOW_WORKER_NODE_EXPORTER_ENSURE`（config `nodeExporterEnsure`，預設 `true`，值必須是布林，否則 process
  不啟動）。`false` 時 worker 不註冊這個 controller，只剩組入的安裝；開發環境預設關閉，因為它與 lab 共用機器。
- **涵蓋也決定 health**：這個政策（程式中的 `EnsureCoverage`）同時是 Server health 的「受涵蓋」判斷
  （monitoring.md §8）：受涵蓋、但 assignment 不是 `installed` 或 `uninstalling` 的 Server，health 是 `unknown`；不受
  涵蓋、也不是 target 的 Server 是 `not_applicable`。controller 關閉時這個判斷不變。
- **已知限制**：assignment 為 `pending` 或 `failed` 時 Site Prometheus 不收集它（monitoring.md §5.2）。主機當機、provisioner 卻仍
  回報開機時，再安裝的 `wait-for-ssh` 失敗，controller 取消它，assignment 轉為 `failed`，這台 Server 的 health 就從
  `down` 變成 `unknown`，直到某次重試成功。

**VictoriaMetrics**：一台 Server 上一個固定版本（`1.155.0`）的 single-node VictoriaMetrics，在 port `8428` 提供
Prometheus 相容的寫入與查詢 API。依 §0 的分界，長期運作在一台主機上的 VictoriaMetrics 是 Managed Software；Slurm job
為自己起停的 VictoriaMetrics（Omnistat user mode）是那個 job 的 runtime，不是這個 kind。

- **安裝路徑**：只有 native。模組以 file artifact 宣告官方 release archive
  （`victoria-metrics-linux-amd64-v1.155.0.tar.gz`），從 Site 選擇的來源取得、以宣告的 sha256 驗證，解到
  `/opt/swallow/victoriametrics/<version>/`，由 `swallow-victoriametrics.service`（系統使用者 `swallow-victoriametrics`，
  只能寫 data 目錄 `/var/lib/swallow/victoriametrics`）執行。沒有 `container` method：binary 是一個靜態檔案，不需要
  runtime。只宣告 amd64：Local Repository 要等每個宣告的檔案都到齊才 `ready`，所以不宣告用不到的架構；其他架構以
  `victoriametrics_release_unavailable` 失敗。
- **OS**：只在 Ubuntu 24.04 驗證過（lab 經 swallow 的 install、reconfigure、uninstall）。RHEL 家族未驗證：playbook
  不拒絕它，但沒有在 RHEL 主機上跑過，不得宣稱支援。
- **已安裝的判斷**：以 binary 回報的版本判斷，不以檔案存在判斷，所以中斷留下的 binary 會被取代；release 或 unit 改變時
  重啟 service，舊的 release 目錄被移除。解壓時不保留 archive 記錄的 owner：上游記錄的是 uid 1000，多數 cloud image 的
  預設使用者，保留它會讓那個帳號能替換 service 執行的 binary。
- **位址**：API 沒有認證，讀寫都開放，所以預設只綁 `127.0.0.1`；`listen: management` 才綁 management address
  （`ansible_host`），比照 Prometheus。安裝在 `/health` 回 200 之後才成功。
- **retention**：整數加 `d`、`w`、`M`、`y`，預設 `1M`（VictoriaMetrics 自己的預設）。VictoriaMetrics 把沒有單位的數字讀成
  月、把 `m` 讀成分鐘，一天以下則啟動即失敗，所以都不接受；超過 100 年（`36500d`、`5214w`、`1200M`、`100y`）也拒絕。
  格式由模組的 spec schema 檢查，上限由 hook 檢查，都在建立 Workflow 之前（`validation_error`）。
- **讀取**：Grafana 以既有的 `prometheusUrl` 讀它（同機為 `http://127.0.0.1:8428`）。它不宣告 `Exporter`，Site 的
  Prometheus 不收集它自己的 metrics；目前沒有 Platform 組入它。
- **uninstall**：移除 unit、release、data 與系統使用者（比照 Prometheus，資料一併刪除）。
- **仍是動作**：與其他尚未遷移的 kind 相同（[desired-state-model.md](desired-state-model.md) §1.1），install 與
  uninstall 是 user 觸發的 Workflow，assignment 記錄最後一次套用的 spec；主機上被外部移除、停止或改寫不會被發現。
  遷移後由 desired 與 controller 取代，role 已是 ensure，不必改內容。

## 3. Software Assignment（durable record）

swallow-owned collection `software_assignments`，鍵 `(serverId, kind)`：

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

**與 Workflow 一起寫入**（[decision 069](../decisions/069-swallow-owns-desired-state.md)（swallow owns desired state））：install 先在 store transaction 之外規劃 Workflow（lock 檢查與 target
解析），再在一個 transaction 內寫入 Workflow 與每台 target 的 `pending` assignment；uninstall 同樣把 Workflow 與
`uninstalling` 一起寫入。被拒的請求（target busy 或 locked）不留下 Workflow，也不留下 assignment 的變更。

**OS 生命週期**：拿走 Server OS 的 Workflow——release-os（包括 Platform uninstall 的 `release-servers` Job）、
recover-server，以及 OS image verification 歸還 Server 的 recover Task——成功時，該 Task 的
`server-deployment-cleared` 完成行為在 Task 的 transaction 內清除 deployment 記錄，並把該 Server 所有尚未
`absent` 的 assignment 標為 `absent`（保留各記錄的 `lastWorkflowId`）。OS 在 swallow 之外被移除時由 reconcile
處理：它觀察到機器為 `ready` 或 `allocated`（沒有 OS）時，把該 Server 的 assignment 標為 `absent`，與清除過時的
deployment 記錄、Server Default User 一起。`rescue` 或 `broken` 的 Server 保留 assignment（OS 仍在磁碟上）。
這避免「記錄說有 Docker、磁碟已無」。
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
    `swallow_docker_api_port`（目前固定 `2375`；由 docker-ce 模組擁有，explorer 連線用同一值）
  - Prometheus：`swallow_prometheus_install_method`、`swallow_prometheus_container_runtime`
    （native 時為空）、`swallow_prometheus_version`、`swallow_prometheus_listen`（`loopback`／`management`）、
    `swallow_prometheus_retention`、`swallow_prometheus_port`（固定 `9090`，domain 擁有）、
    `swallow_prometheus_scrape_job`、`swallow_prometheus_scrape_targets`、
    `swallow_prometheus_scrape_server_ids` + `swallow_prometheus_scrape_port`（由 playbook 以
    `hostvars[id].ansible_host` 解析；inventory 是該 Site 的執行當下快照）
  - Grafana：`swallow_grafana_install_method`、`swallow_grafana_container_runtime`、
    `swallow_grafana_version`、`swallow_grafana_port`（固定 `3000`）、
    `swallow_grafana_prometheus_url`（空＝同機）、`swallow_grafana_prometheus_port`、`swallow_grafana_dashboards`；
    secret var `swallow_grafana_datasource_token`（選用）
  - Device Metrics Exporter：`swallow_device_metrics_exporter_version`、`swallow_device_metrics_exporter_port`
    （固定 `5001`，domain 擁有）、`swallow_device_metrics_exporter_job_attribution`（standalone 為 `none`）。
    Slurm 組入時另疊加 `_job_attribution: slurm`、`_compute_ids`、`_cluster_name`（缺 `_job_attribution` 而有
    `_compute_ids` 時，playbook 視為 `slurm`，相容 068 之前的 run）
  - VictoriaMetrics：`swallow_victoriametrics_version`（固定 `1.155.0`）、`swallow_victoriametrics_listen`
    （`loopback`／`management`）、`swallow_victoriametrics_retention`、`swallow_victoriametrics_port`（固定 `8428`，模組擁有）

### 4.3 record / failure 契約

- `record-software-assignment`（internal）在 install 成功後把 assignment 標 `installed`、更新
  `lastAppliedAt`。
- `clear-software-assignment`（internal）在 uninstall 成功後把 assignment 標 `absent`。
- install 的 ansible Task 與 record Task 都宣告投影類完成行為 `software-assignment-failure`（參數 `softwareKind`、
  `serverIds`；完成行為見 [workflow-execution.md](workflow-execution.md) §12）：record 成功之前，install 都還沒
  完成。任一個 Task 轉為 `failed`、`requires_attention`、`canceled`，或因前面的 Task 失敗而 `skipped` 時，handler
  把對應 assignment 標 `failed`。軟體已裝好但 record 沒有完成時也是 `failed`；重新 install 會在主機上收斂（playbook
  是 ensure），再標 `installed`。uninstall 不宣告這個完成行為：失敗時 assignment 維持 `uninstalling`，等重試。

### 4.4 Playbook manifest 與命名

- 每個 kind 的 `deploy-<kind>` 與 `uninstall-<kind>` 註冊於
  [`api-server/modules/manifest.json`](../../api-server/modules/manifest.json)（舊名
  `configure-device-metrics-exporter` 仍指向 DME 同一支 playbook，供 decision 068 之前受理的 Workflow 重試；
  `install-exporters`、`uninstall-exporters` 指向 Node Exporter 的 `deploy-node-exporter`、`uninstall-node-exporter`，
  供以舊名受理的 Task 重試）。
  每個 kind 是一個模組，playbook 在
  `api-server/modules/software/<kind>/playbooks/`、專屬 role 在同一模組的 `roles/`；DME 使用 `shared/roles/` 的
  `device_metrics_exporter` 與 `amdgpu_loader`（Slurm 也使用它們）。
- playbook 名與 Workflow kind 由 kind 推導（`deploy-<kind>`／`uninstall-<kind>` 與
  `configure-<kind>`／`uninstall-<kind>`），模組的 manifest 宣告同一組名稱，**不需**站台 `playbookMappings`。
- Python 依賴 pin 於 `requirements.txt`；image 不安裝任何 collection，所以只用 `ansible.builtin`
  （[模組撰寫指南](module-authoring.md) §4）。

### 4.5 撰寫 software playbook 的硬規則

先遵守[模組撰寫指南](module-authoring.md)的共通規則（§4：冪等、只依賴契約、以 serverId 為身分、排序留在
playbook、依賴鎖定、不逃出內容根目錄、結果與 secret 走契約、只取宣告的 artifact；§5：改套件的 playbook 取得與釋放
`package_maintenance_hold`，套件 task 要重試）、登錄方式（§2）與 role 的解析（§3）。software 另外要滿足：

1. 契約是 §4.1 的 inventory 與 §4.2 的 trusted vars；每個 kind 的 install 與 uninstall playbook 都要登錄
   （`TestEverySoftwareKindHasRegisteredPlaybooks`）。
2. 套件與 image 都從 Site 選擇的來源取得（[local-repository.md](local-repository.md) §8）：Site 使用 Local Repository
   時，發行版的套件來自 Site 的 `ubuntu` 來源，第三方的 deb repository（例如 Docker CE 的 `docker-ce`）由模組宣告、由
   Local Repository 鏡像；否則發行版的套件來自主機自己的 OS 來源，第三方的 deb repository 以模組宣告的 URL 與
   `signingKey` 加到主機。spec 指定 `version` 時 pin 該版本，未指定時安裝該來源目前的版本（Docker CE、Podman）。
   Prometheus 模組還不在 artifact 宣告的範圍內，RHEL 系列的 dnf 分支在 Local Repository 提供 rpm 之前仍用上游的
   repository（[模組撰寫指南](module-authoring.md) §4 第 8 條的例外）；沒有網際網路的 Site 以離線匯入取得內容是後續
   （local-repository.md §9）。

### 4.6 Docker CE 的 Engine API 與 Docker Host Explorer

Docker CE 的 `enableApi` 變體讓 api-server 能即時管理主機上的 Docker Engine
（[Docker Host Explorer](glossaries/terms/docker-host-explorer.md)）。設計理由（為何由 api-server 代呼而非瀏覽器直連
或透明 proxy、為何不逐次跑 Workflow、為何不把 Docker 物件鏡射進 Mongo）見
[decision 043](../decisions/043-docker-host-management.md) (docker host management)。

- **Spec 正規化**：install 時 `enableApi` 省略即寫成 `true`，非 boolean 為 `validation_error`；
  因此新紀錄的 assignment `spec` 一定帶明確值。043 之前寫入、沒有此鍵的紀錄視為**未啟用**
  （當時主機上沒有 listener）。
- **Playbook 行為**（`roles/docker_ce/tasks/api.yml`）：啟用時寫 systemd drop-in
  `/etc/systemd/system/docker.service.d/swallow-api.conf`，在原本的 `-H fd://` 之外加
  `-H tcp://0.0.0.0:<port>`（Docker 官方的 remote access 作法；不碰 `daemon.json`，避免與使用者自己的
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
  `installed`、`enableApi: true`。swallow 不探測、也不使用 user 在 swallow 之外開啟的 Docker API；Podman
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
- **`docker` group**（decision 039）：`roles/docker_ce` 把
  `ansible_user`——也就是該 Server 的 effective Server Default User（Server 上設定的值，否則 OS Image 的
  default user）——加入 `docker` group（`root` 除外），讓該帳號不需 sudo 就能用 Docker；新的 group 在該帳號
  下次登入後生效。不另帶 trusted var；043 之前或本變更之前安裝的主機重跑 install 即補上。卸載不移除該帳號
  的 group membership。

### 4.7 Docker CE 的 Registry Credential

Docker Host Explorer 拉 private image 時使用 swallow 保存的
[Registry Credential](glossaries/terms/registry-credential.md)。設計理由（為何存在 swallow 而不是每次輸入、為何不在
主機上 `docker login`、為何是 installation 範圍）見 [decision 044](../decisions/044-docker-registry-credentials.md)（docker registry credentials）。

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
     compute＋login node，加上 Platform Grafana 以 `container` method 安裝時的 primary controller（採該機
     已 assign 的 runtime，否則 Docker CE）；已 `installed` 的 Server 跳過，不覆蓋 user 的 spec；
  2. Platform 的 `monitoringDashboards` 開啟時（預設，CPU-only 與沒有監控功能的 Platform 也一樣）
     `configure-grafana`：目標＝primary controller，spec 帶監控功能的 `installMethod`（沒有功能時 `native`）與
     runtime、`prometheusUrl`＝Platform metrics 閘道、dashboards `node-exporter` 加上功能的 bundle。Platform 的
     monitoring token 在 Workflow 受理時發出，只以 sealed secret var `swallow_grafana_datasource_token` 交給
     `install-grafana`，assignment 的 spec 不含它；`install-grafana` 的結果類完成行為 `platform-grafana-result` 寫入
     Monitoring Link `Platform Grafana`（[platform-deployment.md](platform-deployment.md) §6.3）；
  3. Device Metrics Exporter 開啟時 `configure-device-metrics-exporter`（目標＝GPU compute nodes 的
     `device-metrics-exporter`，[decision 068](../decisions/068-device-metrics-exporter-managed-software.md)）。
     其 `install-device-metrics-exporter` Task 同時帶 software 參數（因此安裝失敗時 assignment 隨之標
     `failed`）與 Platform 的 owner／record 參數，Platform 的 DME State 與 GPU identity 照舊由該 Task 宣告的投影類與
     結果類完成行為記錄；Slurm 專屬 trusted vars（job attribution、cluster name label，見 §4.2）疊加在 software vars
     上。Slurm uninstall 照舊在每個 target 移除 DME，成功時把該 Platform 各 Server 的 `device-metrics-exporter`
     assignment 標為 `absent`，紀錄不會比套件活得久。

  不組入 Prometheus：Site 的 Prometheus 收集每個 exporter（[monitoring.md](monitoring.md) §5）。在這之前部署、當時
  組入了 Prometheus 的 Platform 保留那筆 `prometheus` Software Assignment，沒有任何東西會移除它，要移除時經
  software API uninstall。

  每個 Job 是「該 kind 的 ansible Task（`install-<kind>`，帶 `softwareKind`／`serverIds`／
  `softwareMode=install`）＋ `record-<kind>` internal record Task」，下一個 Job 依賴上一個的 record
  Task。組入用的 trusted vars 與驗證由 `SoftwareService.PlanComposedInstall` 產生（同樣的
  catalog／role／spec／互斥／k8s 規則，但不檢查 deployed 與 runtime 狀態，因為由 Job 順序保證）；
  launcher 在寫入 Workflow 的同一個 transaction 中呼叫 `RecordComposedInstall` 寫入 pending assignment
  （decision 069），寫入失敗會讓整個 deploy 受理失敗；安裝失敗由 install 與 record Task 宣告的 `software-assignment-failure` 標 `failed`。
- **OS 部署組入 Node Exporter**：deploy-os Workflow 在每台 provision 的 Server 的 `configure-ntp-<serverId>` 之後
  加上 `install-node-exporter-<serverId>`（ansible，跑 `deploy-node-exporter`）與 `record-node-exporter-<serverId>`
  （internal `record-software-assignment`）；Workflow 受理時寫入 pending assignment，安裝失敗由
  `software-assignment-failure` 標 `failed`。這兩個 Task 是葉節點，沒有 Task 依賴它們，因此 exporter 失敗不會擋住
  OS，但 Workflow 會停在 `requires_attention` 直到 user 重試。Platform 部署的 `ensure-os` 對它 provision 的 Server
  做同樣的事（獨立的 `configure-node-exporter` Job，`install-platform` 不等它），不分 Platform 類型。Platform
  uninstall 不移除 Node Exporter；保留主機的 uninstall 都在 `uninstall-platform` 之後為保留的 Server 組入同一個 Job，
  確保它們最後都有 Node Exporter（[platform-deployment.md](platform-deployment.md) §2.1、§2.4）。`ensure-os` 重用或
  已有 OS 的 Server 不重新安裝。組入之外的安裝由 Node Exporter controller 負責（§2 的 Node Exporter）。
- **禁止** `ExecuteChildWorkflow` 一個完整的獨立 software Workflow（會重複 `wait-for-ssh`、搶同一
  Server 的資源所有權、retry/cancel 歸屬混亂、出現兩個 user-visible Workflow）。
- **禁止**只用 ansible `include_role` 當唯一連動（會抹掉 Job 邊界與 dashboard 分組）。playbook 內部
  仍可 `include_role`，但 Job 邊界由 launcher 決定。

## 6. 第一刀不做（誠實現況）

- **不拆 Slurm 內嵌的 NFS**：Slurm HA state（`slurm_state_server` / `slurm_controller_state`）與
  workload storage 仍留在 `slurm_*` roles。過渡期存在「兩套 NFS 故事」——**generic NFS ≠ Slurm
  managed state NFS**，直到 slice 2 收斂。
- 不做 k8s + Docker runtime、不做 PostgreSQL / LDAP、不把 software 做成 Platform 形狀的聚合、
  不做持續 package 輪詢。（Slurm launcher 的組入已由 decision 063 實作，見 §5。套件從 Site 選擇的來源取得，見
  §4.5；Local Repository 的離線匯入見 [local-repository.md](local-repository.md) §9。）
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
| Software domain（Assignment、`Catalog` 與 `Software` hook 的 port、spec helper、repository 介面） | `api-server/internal/core/software/domain/` |
| 各 kind 的 catalog entry、spec 驗證與正規化、trusted vars（`software` hook） | `api-server/modules/software/<kind>/module.go` |
| 由 registry 組出 catalog 與 Workflow kind | `api-server/internal/app/modules.go`（`composeModules`） |
| Software use cases（Install / Uninstall / List / Catalog） | `api-server/internal/core/software/application/` |
| Software Mongo repo | `api-server/internal/adapters/store/mongodb/software/` |
| Software HTTP handler | `api-server/internal/core/software/delivery/` |
| Software launcher（Job 組裝；playbook 由 kind 推導，definition 記成模組的 `software/<kind>@<version>`） | `api-server/internal/app/software_deployment_adapter.go` |
| record/clear internal step | `api-server/internal/app/platform_workflow_step_executor.go` |
| 失敗標記（`software-assignment-failure` 完成行為） | `api-server/internal/app/workflow_completions.go`、`api-server/internal/app/software_assignment_failure.go` |
| OS 被拿走時標 `absent`（`server-deployment-cleared` 完成行為；reconcile） | `api-server/internal/app/workflow_completions.go`（`clearSoftwareAssignments`）；`api-server/internal/core/provisioning/application/reconcile.go`（`clearStaleOSState`） |
| Node Exporter（catalog entry、組入 OS 部署） | `api-server/modules/software/node-exporter/module.go`；組入：`api-server/internal/app/node_exporter_planner.go` |
| Node Exporter controller（政策、退避、取消自己停住的 install；與 kind 無關的 ensure controller）與它在 worker 的組裝、Site Prometheus 的讀取 | `api-server/internal/core/software/application/ensure_controller.go`；`api-server/internal/app/node_exporter_controller.go`、`api-server/internal/app/worker.go` |
| VictoriaMetrics（kind、固定版本、port、retention 的規則） | `api-server/modules/software/victoriametrics/module.go`；spec schema 與 release 的宣告在同目錄的 `module.json` |
| Playbooks 與 roles | `api-server/modules/software/<kind>/`：`playbooks/deploy-*.yml`、`playbooks/uninstall-*.yml`；roles `docker_ce`（Engine API listener：`tasks/api.yml`）、`podman`、`nfs_server`、`nfs_client`、`prometheus`、`grafana`、`node_exporter`、`victoriametrics`；共用的 `api-server/modules/shared/roles/device_metrics_exporter`（移除：`tasks/uninstall.yml`，`uninstall-slurm` 共用）、`amdgpu_loader`、`grafana_provisioning`（Grafana 的 datasource 與 dashboard bundles）與 `shared_data_dir`（Prometheus、Grafana、VictoriaMetrics 建立 data 目錄前讓 `/var/lib/swallow` 維持 root 擁有）；撰寫規則見[模組撰寫指南](module-authoring.md) |
| Docker Host Explorer（view types、use case、Engine client、handler 與路由） | 全部在 docker-ce 模組：`api-server/modules/software/docker-ce/explorer/`（view types、`DockerExplorerUseCase`）、`explorerhttp/`（handler 與 `RegisterRoutes`）、`dockerengine/`（Engine client）；`EngineAPIPort`、`SpecEnableAPI` 與 `APIEnabled` 在 root package `api-server/modules/software/docker-ce/module.go` |
| Registry Credential（registry 正規化與解析、service、sealed Mongo repo、handler） | `api-server/internal/core/software/domain/registry.go`、`application/registry_credentials.go`、`delivery/registry_credential_handler.go`、`api-server/internal/adapters/store/mongodb/software/mongo_registry_credential_repo.go` |
| Device Metrics Exporter（pinned version、port、job attribution） | `api-server/internal/core/software/domain/device_metrics_exporter.go`；Slurm 組入：`api-server/internal/app/slurm_composition_adapter.go` |
| API 契約 | `api-server/docs/development/api-contracts/api-server/software.md`、`servers-docker.md`、`registry-credentials.md` |

## 8. 相關文件

- 決策：[decision 038 Software deployment](../decisions/038-software-deployment.md)、
  [decision 043 Docker host management](../decisions/043-docker-host-management.md)、
  [decision 044 Docker registry credentials](../decisions/044-docker-registry-credentials.md)、
  [decision 068 Device Metrics Exporter Managed Software](../decisions/068-device-metrics-exporter-managed-software.md)、
  [decision 072 Workflow/Job/Task/Runner](../decisions/072-store-driven-workflow-execution.md)、
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
