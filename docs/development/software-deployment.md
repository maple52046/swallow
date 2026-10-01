# Software Deployment 設計、開發與規範

本文件是 swallow **software deployment**（Managed Software）的權威設計文件、開發指引與整合
規範。它把 [decision 038](../decisions/038-software-deployment.md) 的決策，以及 root glossary
的 [Managed Software](glossaries/terms/managed-software.md) /
[Software Assignment](glossaries/terms/software-assignment.md) 語言，落成「swallow 如何安裝一個
單一軟體」與「platform 如何組入一個軟體」的具體設計與規範。

它與 [platform-deployment.md](platform-deployment.md) 平行：platform deployment 佈署由多個
component 組成的 runtime；software deployment 佈署單一軟體及其變體。

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
| `prepare-hosts` | `wait-for-ssh` | `internal` | 獨立入口要求目標已 `deployed`；**驗證式 SSH 就緒**（以 automation 金鑰——站台 override，否則 Deployment Key——實際完成認證，非只探 TCP，見 [platform-deployment.md](platform-deployment.md) §4.6.1） |
| `configure-<kind>` | `ansible-playbook` | `ansible` | 一支 manifest 註冊的 idempotent playbook |
| `record-software` | `record-software-assignment` | `internal` | 成功後把 Software Assignment 標為 `installed` |

uninstall 對稱：`prepare-hosts` → `configure-<kind>`（跑 `uninstall-<kind>` playbook）→
`record-software` 的 `clear-software-assignment`（標 `absent`）。

**失敗語意**：install 的 ansible step 若失敗，`record-software` 不會執行；此時由 step observer 依
ansible step 上攜帶的 assignment 身分把 Software Assignment 標為 `failed`（見 §4.3）。

**SSH 就緒是驗證式、帳號 per-host 解析**：`prepare-hosts` 的 `wait-for-ssh` 會以 automation 金鑰
（站台 override，否則 Deployment Key）對每台目標**實際完成 SSH 認證**，不是只探 22 埠；且 SSH 登入帳號為
**per-host 解析**：所佈署 OS Image 有 default user 時只用它，否則以候選清單
（`[站台 sshUser, cloud-user, ubuntu]`，站台值優先）探測，因此可在同一 site 混用 ubuntu（`ubuntu`）與自訂
image（`cloud-user`）的機器。所有候選帳號都登不進去時（例如被以未授權 automation 公鑰的 image 重裝過）
會在此步驟就以清楚的 `ssh_authentication_failed` 失敗，而非拖到 ansible step 才報 `Permission denied
(publickey)`。細節與 fail-fast/grace 語意見 [platform-deployment.md](platform-deployment.md) §4.6.1。

## 2. Managed Software 目錄（第一刀）

| kind | 變體 / roles | spec 欄位 | 互斥 / 前提 |
| --- | --- | --- | --- |
| `docker-ce` | 無 role；選用 `version` | `version`（選用，pin apt 套件版本） | 與 `podman` 互斥；拒絕已是 Kubernetes member 的 Server |
| `podman` | 無 role；選用 `version` | `version`（選用） | 與 `docker-ce` 互斥；拒絕已是 Kubernetes member 的 Server |
| `nfs` | `server` / `client`（可同時） | server：`exportPath`、`exportOptions`；client：`source`（`host:/path`）、`mountPath`、`mountOptions` | 可與 Platform 共存 |

擴充新 kind（未來 PostgreSQL、LDAP client）就是新增一筆目錄 + 一支 playbook，前提是仍只依賴
本文件的契約。

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

### 4.3 record / failure 契約

- `record-software-assignment`（internal）在 install 成功後把 assignment 標 `installed`、更新
  `lastAppliedAt`。
- `clear-software-assignment`（internal）在 uninstall 成功後把 assignment 標 `absent`。
- install 的 ansible step 攜帶 `softwareKind` 與 `serverIds` 參數；step observer 觀察到該 step 進入
  終態失敗（failed / requires_attention / canceled）時，把對應 assignment 標 `failed`。

### 4.4 Playbook manifest 與命名

- 於 [`api-server/automation/manifest.json`](../../api-server/automation/manifest.json) 註冊：
  `deploy-docker-ce` / `uninstall-docker-ce`、`deploy-podman` / `uninstall-podman`、
  `deploy-nfs` / `uninstall-nfs`。
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

## 5. 與 Platform deployment 的連動（規範；第一刀只定契約）

- Platform launcher 需要某個 software component 時，**把該 software 的 Job 組入 platform 的
  parent Workflow**（cross-Job `DependsOn`），共用同一組 Ansible roles。
- **禁止** `ExecuteChildWorkflow` 一個完整的獨立 software Workflow（會重複 `wait-for-ssh`、搶同一
  per-Server lease、retry/cancel 歸屬混亂、出現兩個 operator-visible Workflow）。
- **禁止**只用 ansible `include_role` 當唯一連動（會抹掉 Job 邊界與 dashboard 分組）。playbook 內部
  仍可 `include_role`，但 Job 邊界由 launcher 決定。

## 6. 第一刀不做（誠實現況）

- **不拆 Slurm 內嵌的 NFS**：Slurm HA state（`slurm_state_server` / `slurm_controller_state`）與
  workload storage 仍留在 `slurm_*` roles。過渡期存在「兩套 NFS 故事」——**generic NFS ≠ Slurm
  managed state NFS**，直到 slice 2 收斂。
- **不改 Slurm launcher**、不做 k8s + Docker runtime、不做 PostgreSQL / LDAP、不把 software 做成
  Platform 形狀的聚合、不做持續 package 輪詢、不做 air-gap 套件庫。

## 7. 程式與內容位置對照（導航用）

| 職責 | 路徑 |
| --- | --- |
| Software domain（Assignment、Catalog、repository 介面） | `api-server/internal/software/domain/` |
| Software use cases（Install / Uninstall / List / Catalog / Sweep） | `api-server/internal/software/application/` |
| Software Mongo repo | `api-server/internal/software/infra/` |
| Software HTTP handler | `api-server/internal/software/delivery/` |
| Software launcher（Job 組裝） | `api-server/internal/app/software_deployment_adapter.go` |
| record/clear internal step | `api-server/internal/app/platform_workflow_step_executor.go` |
| 失敗標記 observer | `api-server/internal/app/software_assignment_observer.go` |
| Playbooks 與 roles | `api-server/automation/playbooks/deploy-*.yml`、`roles/docker_ce`、`roles/podman`、`roles/nfs_server`、`roles/nfs_client` |
| API 契約 | `api-server/docs/development/api-contracts/api-server/software.md` |

## 8. 相關文件

- 決策：[decision 038 Software deployment](../decisions/038-software-deployment.md)、
  [decision 017 Workflow/Job/Task/Runner](../decisions/017-workflow-job-task-runner-model.md)、
  [decision 019 Slurm platform deployment](../decisions/019-slurm-platform-deployment.md)。
- 語言：glossary [Managed Software](glossaries/terms/managed-software.md)、
  [Software Assignment](glossaries/terms/software-assignment.md)、[Platform](glossaries/terms/platform.md)。
- 平行設計：[platform-deployment.md](platform-deployment.md)。
- API 契約（`api-server` 擁有）：
  [software](../../api-server/docs/development/api-contracts/api-server/software.md)。
