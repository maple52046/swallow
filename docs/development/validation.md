# Validation 設計與規範

本文件是 swallow **Validation** 的現行規格：swallow 如何以 [Validation Suite](glossaries/terms/validation-suite.md)
（測試方案）驗證已部署的 Server 與 Platform、一次 [Validation Run](glossaries/terms/validation-run.md) 怎麼受理、執行與
記錄結果、部署怎麼把 suite 組入成閘門，以及 [Validation Schedule](glossaries/terms/validation-schedule.md) 怎麼自動執行。
Validation 是 swallow 的三項能力之一（Deployment、Monitoring、Validation）。

> **本文件不重述決策**：為什麼由模組宣告 suite、為什麼分成 report 與 gate、為什麼 user script 是資料，見
> [decision 081](../decisions/081-validation-suites-and-runs.md)（validation suites and runs）。Workflow 的共通執行語意（受理、
> 所有權、取消、取代）見 [workflow-execution.md](workflow-execution.md)；模組如何宣告 suite 與撰寫驗證內容見
> [module-authoring.md](module-authoring.md) §7。HTTP 路由、欄位與錯誤以
> [validation 契約](../../api-server/docs/development/api-contracts/api-server/validation.md)為準。

---

## 1. 目的與範圍

- **讀者**：新增或修改 Validation Suite、驗證的受理與結果、Validation Schedule，或把 suite 組入部署的開發者與 AI agent；
  要解釋一次 run 為什麼是這個結果的人。
- **範圍內**：suite 的來源與宣告（§2）、run 的受理與防護（§3）、執行、結果與 verdict（§4）、gate mode（§5）、
  Validation Schedule（§6）、第一批 suite（§7）。
- **範圍外**：部署自己的 postcondition（readiness、node 註冊、membership；各部署規格與
  [workflow-execution.md](workflow-execution.md) §5.6）、OS Image 的驗證（[os-provisioning.md](os-provisioning.md) §5）、
  provisioner 的硬體測試、監控與告警（[monitoring.md](monitoring.md)）。

語言以 glossary 為準：Validation Suite、[Validation Check](glossaries/terms/validation-check.md)、
[Validation Verdict](glossaries/terms/validation-verdict.md)、[Validation Impact](glossaries/terms/validation-impact.md)、
Validation Run、Validation Schedule、[Validation Module](glossaries/terms/validation-module.md)、
[Workflow Template](glossaries/terms/workflow-template.md)。

## 2. Validation Suite

### 2.1 模組宣告的 suite（swallow-defined）

模組在 `module.json` 的 `validations` 宣告 suite，以 `templates` 宣告執行它的 Workflow Template（欄位與寫法見
[module-authoring.md](module-authoring.md) §7）：

| 欄位 | 意義 |
| --- | --- |
| `id` | suite 的 id，kebab-case；所有模組之間唯一（registry 拒絕重複） |
| `title`、`description` | 顯示用 |
| `target` | `servers`（一或多台 deployed Server）或 `platform`（一個 Platform） |
| `platformType` | `target` 為 `platform` 時必填：這個 suite 驗證的 Platform type |
| `impact` | Validation Impact：`observe`、`workload`、`disruptive` |
| `checks[]` | 每個 check 的 `id`、`title`、`description`、`gating`（gate mode 下失敗是否擋下部署） |
| `parameters` | 選用；spec schema 子集（與模組的 `spec` 相同的讀法），驗證 run 的 `parameters` |
| `template` | 執行它的 Workflow Template 名稱，必須在同一個模組的 `templates` |
| `budgetSeconds` | 一次 report run 的整體預算（§4.4） |
| `experimental` | 選用，預設 `false`：預覽中的 suite，check 之後可能改變；照常執行、排程與讀取，dashboard 預設不顯示 |

Platform、Accelerator、Software 模組宣告自己領域的 suite；不屬於任何一個的（主機、網路）放在 Validation Module
（`modules/validation/<id>/`）。core 不認得任何 suite：它讀宣告、編譯範本、受理 run、記錄結果。

### 2.2 user-defined suite

任何已登入的 User 都能經 API 建立 user suite，成為它的作者：名稱、說明、`script`（最大 64 KiB）、`sudo`（預設
`false`）、`starterLevel`（`owner`，預設；或 `member`）、`timeoutSeconds`（預設 600、最大 3600）與 `impact`（預設
`workload`）。所有已登入的 User 都能看到並執行別人的 user suite（受下面的發起層級與 §3 的受理檢查約束）。

- 只有 desired，不需要 provider 實現（與 Deployment Template 相同），所以沒有 controller、generation 與 conditions。
- **作者**：suite 記下建立者的 user id（`createdById`，權限以它判斷）與 username（`createdBy`，顯示用，照建立時的寫法保留）。
  只有作者能修改，admin 也不行；作者或 admin 能 retire。作者被刪除後 suite 仍可讀、可執行，沒有人能修改，admin 能 retire。
  路由的分類見 [identity-and-access.md](identity-and-access.md) §4.2。
- **revision**：每次修改 `script`、`sudo`、`starterLevel`、`timeoutSeconds` 或 `impact`，`revision` 加一（只改名稱或說明不加）。
  每個 revision 是不可變的紀錄（`validation_suite_revisions`：script、`scriptSha256`、`sudo`、`starterLevel`、`timeoutSeconds`、
  `impact`），與 suite 在同一個 transaction 寫入，以 `GET /validation/suites/{suiteId}/revisions/{revision}` 讀取。修改以讀到的
  revision 為條件寫入：同時兩次修改只有一次成功，另一次 `409`，不會產生兩份不同 script 的同號 revision。run 記下它執行的
  revision、script 的 sha256 與 script 本身，之後的修改不改變已受理的 run。這項功能之前建立的 suite 只保存升級時的
  revision（migration 寫入），更早的 revision 讀不到。
- **發起層級**（`starterLevel`）：誰能發起它的 run。`owner` 要求發起者能操作每個目標（Pool 的 owner 或 admin），`member`
  只要求能讀取每個目標（Pool 的 owner、member 或 admin）。module suite 沒有這個欄位，一律等同 `owner`。
- **執行身分**：script 以**發起 run 的 User 自己的 Linux 帳號**在每台目標上執行（§3.1），不是作者的、也不是 automation
  user 或 root。`sudo: true` 時發起者以 `sudo -n` 執行 script，所以只在發起者有免密碼 sudo 的目標上能用；script 本身也能
  呼叫 `sudo -n`，受同一套 Sudo Policy 約束。這項功能之前的 suite 有 `runAs`：讀取時對應成 `sudo`（`root` 是 `true`，其他是
  `false`），store 不改寫。
- 目標只能是 `servers`；它只有一個 check，`script`：exit code 0 是 `passed`，其他是 `failed`，逾時（超過
  `timeoutSeconds`，以 `timeout` 停止）或帳號、sudo、放置的前置條件不成立（§4.3 的代碼）是 `error`。swallow 不比對輸出：
  stdout 與 stderr 的最後 8 KiB 是 evidence（遮蔽後），exit code 是 measurement。script 寫在只有發起者能讀的暫存目錄，script
  結束時就刪除，run 被取消時也一樣（§3.1）；沒有 `#!` 的 script 以 `/bin/sh` 執行。能讀取 run 的人（目標 Pool 的 owner 與 member）都能讀它的 evidence。
- 刪除是 retire：從目錄消失，但過去的 run 與 revision 仍可讀；被 Validation Schedule 引用時拒絕（409）。
- script 是資料，不是 host automation 內容：由 Validation Module `script` 唯一的 playbook `run-validation-script` 執行，
  allowlist 的規則（[core-and-extensions.md](core-and-extensions.md) §5.2）不變。script 不支援 secret，畫面提醒不要
  寫入密碼。
- 別人的 suite 有新 revision 時，dashboard 在發起者上次執行的 revision 之後標示變更、顯示新舊 script 的差異，要選「使用
  新版」才能執行；受理時帶上 `suiteRevision`，revision 不符時 `409`，舊的 revision 不能再執行（§3）。
- 不能組入部署當閘門（§5），也不能被 Validation Schedule 引用（§6）。

### 2.3 目錄

`GET /api/v1/validation/suites` 列出兩種 suite：模組的（`source: module`，帶模組的 kind、id 與版本，不可修改）與
user 的（`source: user`）。同一個 id 只會是其中一種：模組 suite 的 id 是 kebab-case 名稱，user suite 的 id 是 UUID。

## 3. 受理 Validation Run

`POST /api/v1/validation/runs` 以 suite、目標與參數受理一次 report run。依序檢查，任何一項不符就拒絕、不留下紀錄：

1. suite 存在且沒有 retire；`parameters` 符合 suite 的 schema（`validation_error`）。user suite 帶了 `suiteRevision` 而它不是
   目前的 revision 時 409（suite 在發起者看過之後被修改；module suite 不接受 `suiteRevision`，400）。
2. 權限：每個目標都要能讀取（不能讀取時 404）；module suite 與 `starterLevel: owner` 的 user suite 要求能操作每個目標，
   `starterLevel: member` 只要求能讀取（403；[identity-and-access.md](identity-and-access.md) §4.2）。
3. 目標：
   - `servers` suite：至少一台，全部存在、在同一個 Site、`deployed`（409）。
   - `platform` suite：Platform 存在、type 是 suite 的 `platformType`、lifecycle 是 `active`，且模組由 spec 決定的 access
     Server（`PlatformModule.AccessServers`：Slurm 是 login node，沒有時是 controller；Kubernetes 是 control-plane
     member）存在（409）。
4. 防護（lock 與 automation 兩項在規劃 Workflow 時檢查，所以在步驟 5 之後）：
   - 目標 Server 沒有被 lock（409，與其他 Ansible Task 相同的 live 檢查）。
   - impact 是 `workload` 或 `disruptive` 的 `servers` suite，不接受被 Platform Claim 的 Server（409，提示改用該
     Platform 的 suite），因為它會和 Platform 的 workload 搶資源。
   - impact 是 `disruptive` 的 run 要帶 `confirm: true`（400）。
   - Site 的 automation 已啟用、Deployment Key 存在（與其他 Ansible Task 相同的受理檢查）。
5. user suite 的發起者能在每個目標以自己的帳號執行（§3.1；Host Access 與 sudo 以 Directory 投影的同一個函式
   `HostAccessFor` 計算，[identity-and-access.md](identity-and-access.md) §8.3）：
   - 沒有設定 Directory，或發起者沒有 `uidNumber`（不會出現在 Directory）：409。
   - 發起者在某個目標沒有 Host Access：403。
   - `sudo: true`：發起者在某個目標沒有 sudo 是 403；有 sudo 但需要密碼是 409（script 以 `sudo -n` 執行，swallow 沒有
     User 的密碼）。
   - 某個目標沒有安裝 directory client（directory client 那個 kind 的 Software Assignment 不是 `installed`，非 Ubuntu 的
     Server 一律如此）：409，因為主機上沒有發起者的帳號。
6. 目標沒有被未結束的 Workflow 佔用：被佔用時回 409，訊息點名佔用者（busy check，
   [workflow-execution.md](workflow-execution.md) §3）。例外是佔用者本身是 preemptible 的 report run：新的 report run
   不取代另一個 report run，所以同樣回 409。

受理在一個 transaction 內寫入 run（`pending`）與它的 Workflow（kind `run-validation`）。Workflow 的組成：

| Job | Task | Runner | 說明 |
| --- | --- | --- | --- |
| `prepare-hosts` | `wait-for-ssh` | internal | 目標（`platform` suite 是 access Server）以 Deployment Key 可登入 |
| 範本的 Job | 範本的 Task（多半是 `ansible-playbook`） | 依 Task kind | 每個 Ansible Task 宣告 `validation-result` 與 `validation-run-state` |
| `complete-validation` | `complete-validation` | internal | `continueOn: succeeded, failed, skipped, canceled`（check 怎麼結束都執行）；宣告 `validation-run-state`，結束時寫入 status 與 verdict |

- Workflow 宣告 unattended、預算（suite 的 `budgetSeconds`）與 preemptible（impact 不是 `disruptive` 時），
  見 [workflow-execution.md](workflow-execution.md) §4、§5.6、§6、§9。
- 資源：`servers` suite 擁有每台目標的 `server:<id>`；`platform` suite 擁有 `platform:<id>` 與 access Server。
- 範本的 Ansible Task 拿到下列 trusted var（`swallow_validation_*`），再加上一般 Ansible Task 的標準 var 與 artifact var：
  `swallow_validation_run_id`、`swallow_validation_suite`、`swallow_validation_mode`（`report` 或 `gate`）、
  `swallow_validation_checks`（宣告的 check id）、`swallow_validation_parameters`、`swallow_validation_targets`（被驗證的
  Server id）、`swallow_validation_platform_id` 與 `swallow_validation_members`（Platform 的每個成員：`serverId` 與
  `roles`），以及安裝設定的 NTP server `swallow_ntp_servers`（與部署的時鐘檢查相同）。user suite 另有
  `swallow_validation_script`、`swallow_validation_timeout_seconds`、`swallow_validation_sudo` 與
  `swallow_validation_requester`（發起者的 `userId`、`username`、`uidNumber`）。
- run 是動作，沒有自然的 desired state（[desired-state-model.md](desired-state-model.md) §11.1）；它的欄位分類見 §4.1。

### 3.1 user suite 在目標上的執行身分

module suite 不變：automation user 以 Deployment Key 登入，需要時 become root。user suite 的 playbook
`run-validation-script` 一樣以 automation user 登入，只有執行發起者的 sudo 檢查與 script 的兩個 task 切換成發起者：

| 步驟 | 身分 | 說明 |
| --- | --- | --- |
| `getent passwd <username>`，比對 uid 與 `uidNumber`；`stat` 發起者的 home | automation user，不 become | 不存在是 `validation_account_missing`；uid 不同（同名的本機帳號，本機檔案在 nsswitch 中優先）是 `validation_account_mismatch` |
| `sudo -n -- /bin/true`（只在 `sudo: true`） | 發起者 | 失敗是 `validation_sudo_unavailable`（附 sudo 的訊息） |
| 建立 0700 的私有目錄、寫入 script（擁有者是發起者的 uid、0700），最後才把目錄交給發起者 | root | 寫入時目錄仍屬於 root；放不上是 `validation_script_unplaced` |
| `[sudo -n --] /bin/sh -c <wrapper> <私有目錄> timeout --kill-after=10 <timeoutSeconds> [/bin/sh] <script>`，`chdir` 是私有目錄 | 發起者（`sudo: true` 時是 root） | `timeout` 在 `sudo` 之內，可以停止 root 的子行程；外層的 wrapper 等 script 結束後刪除私有目錄，保留 script 的 exit code |
| 刪除私有目錄（`always`） | root | wrapper 通常已經刪除；這一步處理 script 沒有開始的情況 |

- 私有目錄在 script 結束時由等待它的 wrapper 刪除，不靠 play 的 `always`：run 被取消時 play 停止，主機上的 Ansible
  session 結束，`always` 不會執行，但 script 照 §4.4 跑到自己的 timeout，wrapper 也一起留下（它忽略 session 關閉時可能
  收到的 SIGHUP），script 一結束就刪除目錄。wrapper 以執行 script 的身分刪除：`sudo: true` 時是 root，所以 script 留下
  的 root 檔案也一併刪除；不是 sudo 時先把整個目錄樹加上擁有者的寫入權限，script 設成唯讀的子目錄也刪得掉。主機重開機或
  wrapper 被強制結束時，目錄留在暫存目錄裡，由系統的暫存清理處理。

- 切換成發起者的 task 以 `become: true`、`become_user: <username>` 執行，task var 開啟 `ansible_pipelining`（module 經
  stdin 傳送，不需要 ACL；主機沒有 `acl` 套件），設定 `ansible_remote_tmp`（script 用私有目錄；sudo 檢查在私有目錄之前，
  用 `/tmp`），所以不在發起者可寫的 home 留下 `~/.ansible/tmp`；並帶 `ignore_errors: true`：automation user 不能切換成
  發起者時 Ansible 回報 `Missing sudo password` 而且 `failed_when: false` 清不掉。以 `cmd is defined` 判斷指令有沒有執行；
  沒有執行就是 `validation_become_failed`，說明固定為「the automation user cannot run commands as <username>」（Ansible 的
  訊息讀起來像是發起者的密碼問題）。automation user 的 sudo 只允許 root 的 Site 一律如此，沒有替代路徑。
- `HOME`：發起者的 home 存在、是目錄、而且擁有者是發起者的 uid 時用它；否則用私有目錄（同名但已刪除的 User 留下的 home
  屬於舊的 uid，uid 不會重用）。playbook 不建立 home，也不改變主機的其他東西。`sudo: true` 時 script 看到的是 root 的 `HOME`。
- 每個值都是 argv 的一個元素：wrapper 是固定的 shell 文字，私有目錄與指令以位置參數傳入，不被 shell 解析。`become` 寫在每個 task，`validation_report` 不繼承它。

## 4. 執行與結果

### 4.1 Validation Run 的欄位

| 欄位 | 類別 | 寫入者 |
| --- | --- | --- |
| suite（id、source、名稱、模組版本或 revision、sha256、user script）、checks（宣告時的快照）、targets、parameters、mode、trigger、requestedBy、requestedById（手動 run 的發起者 user id）、runAs（user suite：發起者的 username、`uidNumber` 與是否 `sudo`）、workflowId | 動作的請求，受理時寫入一次 | API（手動）、Validation Schedule controller（定時、部署後）、部署的 launcher（gate） |
| results：每個 check 在每個目標上的 verdict、summary、measurements、evidence、observedAt、Task 與 attempt | observed：來源是判定它的 Task | 結果類完成行為 `validation-result`（ansible executor） |
| status、statusReason、verdict、startedAt、finishedAt | derived：由 Workflow 的轉換與 results 推導 | 投影類完成行為 `validation-run-state`（Task 轉換的 transaction 內） |

run 保留 90 天（`validationRunRetention`，以 store 的 TTL 刪除）；它的 Workflow 與 job artifact 依自己的保留期限
（[installation.md](installation.md)）先消失時，run 仍可讀。

### 4.2 結果的形狀

驗證的 playbook 在每台主機把結果放進 fact `swallow_validation_results`，共用 role `validation_report` 在 run 的最後
把它們合併寫到 `swallow_result_path`（[module-authoring.md](module-authoring.md) §7.3）：

```json
{"checks": [{"check": "gpu-gres", "target": {"kind": "server", "id": "<serverId>"}, "verdict": "failed",
  "summary": "Slurm handed out 7 of 8 GPUs", "measurements": [{"name": "visible_gpus", "value": 7, "unit": "count", "expected": "8"}],
  "evidence": "<bounded output>"}]}
```

`validation-result` 只收：run 宣告過的 check、run 的目標（`server` 是 run 的目標 Server 或 Platform 的成員，`platform`
是 run 的 Platform）、Validation Verdict 的四個值；summary 最多 1 KiB、evidence 最多 8 KiB（遮蔽後）、每筆 measurements
最多 20 個。不符合的結果記 log 並略過，不讓 Task 失敗。同一個 check 與目標以最新的 attempt 為準（Task 重試或 Job
重試會覆寫）。

### 4.3 Verdict 與狀態

- 第一個 Ansible Task 開始時 run 進入 `running`。
- `complete-validation` 結束時（成功、失敗或略過）run 進入 `completed`：宣告的每個 check 在它的每個目標上沒有結果時，
  補上 `error`（summary 是判定它的 Task 的錯誤，或「no result was recorded」）。`servers` suite 的目標是每台目標 Server；
  `platform` suite 的 check 若沒有任何結果，以 Platform 為目標補一筆。
- verdict：任何 `failed` 即 `failed`；沒有 `failed` 而有 `error` 即 `error`；其餘 `passed`。
- 取消（user 經 run 的 cancel、Workflow 的 cancel、被取代或預算用完）時 run 進入 `canceled`，沒有 verdict；已記錄的
  results 保留，statusReason 是取消的原因。
- 判定為 `failed` 的 check 不是 Workflow 的失敗：驗證的 playbook 判定失敗也正常結束，所以 `run-validation` Workflow
  多半 `succeeded`；playbook 本身失敗（主機連不上、工具壞掉）才是 Task 失敗，它的 check 成為 `error`。
- check 因為主機上的前置條件不成立而無法判定時，verdict 是 `error`，summary 以 `[code]` 開頭，後面是給 user 看的說明
  （例如 `[validation_account_missing] alice has no account on this host`）。Task 沒有失敗，所以這不是 Failure Class
  （module 的 `failureCodes` 不適用）；`[code]` 讓 dashboard、CLI 與 user 不必解析說明就知道原因。user suite 的代碼：
  `validation_account_missing`、`validation_account_mismatch`、`validation_become_failed`、`validation_sudo_unavailable`、
  `validation_script_unplaced`（§3.1）。受理時能預知的情況在 API 就拒絕（§3），主機上的檢查是 API 與主機之間有差距時
  （例如 sudo 規則的快取）的最後判斷。

### 4.4 預算、取消與取代

- **預算**：`run-validation` Workflow 超過 suite 的 `budgetSeconds` 時被取消（`workflow_budget_exceeded`）；範本的每個
  Task 可宣告自己的 timeout，Ansible Task 逾時以 `task_budget_exceeded` 失敗（[workflow-execution.md](workflow-execution.md)
  §5.6）。
- **unattended**：Task 的失敗（可重試與否）與無法證明的結果都立即落定，Workflow 結束並釋放資源，不停在
  `requires_attention`；`complete-validation` 仍執行並寫入 verdict。所以 report run 沒有 Task 重試或 rerun：要再驗證就
  再受理一次 run。
- **取代**：impact 不是 `disruptive` 的 report run 是 preemptible。之後受理的任何非 preemptible Workflow（部署、Release、
  軟體安裝、Platform 的收斂）要用到它的 Server 時，受理的 transaction 對它寫入取消（`Preempted by Workflow <id>`），
  新 Workflow 等它結束後開始（[workflow-execution.md](workflow-execution.md) §9）。Workflow 結束時，已在主機上執行的
  check 仍會跑到自己的 timeout（user script 是 suite 的 `timeoutSeconds`），所以兩者可能短暫重疊。
- **取消**：`POST /api/v1/validation/runs/{id}/cancel` 對 report run 的 Workflow 寫入取消；gate run 不能單獨取消（409），
  要取消的是它所在的部署。能取消的是 run 的發起者（以 user id 比對，仍要能讀取 run）、能操作每個目標的 Pool owner 與
  admin（[identity-and-access.md](identity-and-access.md) §4.2）。和取代一樣，已在主機上執行的 user script 仍以發起者
  的身分跑到 suite 的 `timeoutSeconds`；它結束時私有目錄與 script 一起刪除（§3.1），主機上不留下任何東西。

## 5. Gate mode：組入部署

Platform 模組可以把自己的 suite 組入部署，作為部署成功的條件。組入的 Jobs 是同一個 Workflow 的一部分，不巢狀啟動另一個
Workflow（[core-and-extensions.md](core-and-extensions.md) §5.4）：

- 部署受理時，launcher 以範本編譯 suite 的 Tasks（mode `gate`，Task id 帶 `gate-` 前綴，access Server 與參數由 launcher
  決定），接在部署的安裝 Task（Slurm 是組入的軟體）之後，再接 `gate-complete-validation`
  （mode `gate`），它屬於 suite 的最後一個 Job，所以重試這個 Job 會連同 check 一起重跑。Job 的顯示名稱與重試粒度取自範本。同一個 transaction 寫入 run
  （trigger `deployment`、`workflowId` 是部署的 Workflow）。部署先佈 OS 時，check 的目標可以是同一個部署才佈好的 Server。
- gate 的 `complete-validation` 讀 run 的 results，任何 gating check 不是 `passed` 或 `skipped` 時以可重試的
  `validation_gate_failed` 失敗，diagnostic 列出失敗的 check、目標與 summary，remediation 指向 run；部署照它原本的
  語意停在 `requires_attention`。修好之後重試這個 Job 會重跑整個 suite（Job 的重試粒度 `job`）。不是 gating 的 check
  失敗只記錄在 run，部署照常成功。
- 部署的 Workflow 沒有宣告 unattended、預算或 preemptible；gate run 的 status 與 verdict 一樣由完成行為寫入。
- Slurm 部署一律組入 `slurm-acceptance`（§7），所以 decision 063、066 的閘門不變：GPU 不能用、container 與 MPI 跑不起來的
  Slurm Platform 不算部署完成，GPU 失敗的節點照樣 drain。它從 primary controller 進入（install 結束時它就能回答整個叢集），
  `gpuServers` 是部署對應 GPU GRES 的 compute node，Job 是 `validate-slurm`；沒有可用的 Validation service 時 Slurm
  部署被拒絕，不會在沒有 gate 的情況下受理。加入成員的部署以參數 `nodes` 只驗變動的成員（`gpuServers` 也只列其中的
  GPU node），既有成員與它們的工作不受打擾；沒有變動成員的部署（例如只改 partition）不組入 gate。GPU node 有 container
  runtime 時參數 `containerImage`、`containerRuntime` 帶入 AMD 官方 ROCm 開發 image（`slurm.GPUTestImage`，固定 tag）與 runtime（[decision 083](../decisions/083-platform-member-scaling.md)
  （Platform member scaling））。

## 6. Validation Schedule

Validation Schedule 是 desired state（[desired-state-model.md](desired-state-model.md) §3）：

| 欄位 | 類別 |
| --- | --- |
| `spec`：`name`、`suiteId`、`serverIds` 或 `platformId`、`parameters`、`trigger`（`kind: cron` 加 `cron` 與 `timezone`，或 `kind: after-deployment`）、`enabled`；`generation` | desired，只由 API 寫入 |
| `status`：`observedGeneration`、`nextRunAt`、`lastRunId`、`lastRunAt`、`lastSkipReason`、`lastDeploymentKey`、`conditions` | derived，只由 controller 寫入 |

- schedule 不接受 impact 是 `disruptive` 的 suite（`validation_error`）：它的每次 run 都要有人帶 `confirm: true`。
- schedule 也不接受 user suite（`validation_error`）：user suite 以發起者的帳號執行，定時的 run 沒有發起者，而建立 schedule
  的 admin 在每台主機都有權限，以那個帳號執行範圍太大。這項規則之前建立、引用 user suite 的 schedule 是
  `realized=false`（reason `user_suite_unsupported`），不受理 run；要讓 schedule 以指定的 Pool owner 執行 user suite 是之後的
  工作。
- `cron` 是五個欄位（分、時、日、月、星期；`*`、清單、範圍與 `/step`），依 `timezone`（IANA，預設 `UTC`）計算。
- worker 的 controller `validation-schedule`（每分鐘 resync，lease 保證單一 instance）：
  - `cron`：到了 `nextRunAt` 就受理一次 run（trigger `schedule`），再把 `nextRunAt` 設成現在之後的下一個時間；停機期間錯過
    的時間點只補跑一次。
  - `after-deployment`：Platform 目標在 Platform 有新的成功實現 spec 的 Workflow（部署、它的 rerun，或 GPU Operator 這類
    變更；run 與 uninstall 不算）時、Server 目標在任一台有新的成功 `deploy-os` Workflow 時受理一次 run；
    `lastDeploymentKey` 記下已處理的部署（Platform 是 Workflow id，Server 是每台最新的 `<serverId>=<workflowId>`），同一次
    部署不會跑兩次。第一次觀察一個 spec（含修改後）時只記下目前的部署，不補跑過去的。
  - 同一個 schedule 已有未結束的 run 時不再受理；受理被拒（目標被其他 Workflow 佔用、Server 被 lock 或不在 `deployed`、
    automation 不能執行、Local Repository 沒有 ready）時略過這一次並記在 `lastSkipReason`，不重試。其他錯誤（例如 store
    暫時失敗）不寫入 status，controller 依退避重試同一次。launcher 以 `ErrRunRefused` 標出受理被拒的錯誤。
  - suite 被 retire 或目標消失時 `realized=false`（reason `suite_missing`、`target_missing`），suite 是 user suite 時
    `realized=false`（reason `user_suite_unsupported`），都不受理 run。
- 停用（`enabled: false`）的 schedule 不受理 run，也不計算 `nextRunAt`。

## 7. 第一批 suite

| suite | 模組 | target | impact | 實驗性 | checks |
| --- | --- | --- | --- | --- | --- |
| `slurm-acceptance` | Platform `slurm` | `platform` | `workload` | 否 | `srun-all-nodes`（不是 gating）、`shared-node`、`oci-container`、`mpi-pmix`、`gpu-gres`、`gpu-container`（每台 GPU node） |
| `kubernetes-smoke` | Platform `kubernetes` | `platform` | `observe` | 是 | `nodes-ready`、`pod-scheduling`（每台 workload node）、`cluster-dns`、`service-routing` |
| `amd-gpu-basic` | Accelerator `amd` | `servers` | `workload` | 是 | `kfd-gpus`、`hip-probe` |
| `host-baseline` | Validation `host` | `servers` | `observe` | 是 | `time-sync`、`cpu-count`、`memory`、`root-filesystem` |
| `network-mesh` | Validation `network` | `servers` | `observe` | 是 | `ping-mesh`、`mtu`、`ssh-port` |
| user suite | Validation `script` | `servers` | 依 suite | 否 | `script` |

- `slurm-acceptance` 是部署原有的驗證，部署以 gate 組入它（§5）。其他四個模組 suite 是部署之外新加的檢查，標為實驗性，
  之後重新設計：被 Platform claim 的 Server 仍不接受 `workload` 的 servers suite，重新設計時改由 Platform 的 suite 提供
  成員上的網路與 GPU 檢查。

- 每個 suite 的 playbook 在目標上只讀取或執行短暫的工作，check 沒有通過是 verdict，不是 Task 失敗；結果經共用 role
  `validation_report` 寫出（§4.2）。Platform suite 只在一台 access Server 執行一次，以成員的 inventory `hostname` 與
  `platform_node_name` 把節點名稱對回 Server；對不回的節點以 Platform 為目標，summary 帶節點名稱。
- `slurm-acceptance` 是原本 `slurm_verify` 的重型檢查（[platform-deployment.md](platform-deployment.md) 的 Slurm）：
  `slurm_verify` 只留 controller 的 ping 與 compute 的註冊。它從叢集本身讀出節點、CPU、GRES 與 partition 的
  `OverSubscribe`，再以每台節點一個 task 讀出啟用的功能（`oci.conf`、OCI probe bundle、MPI probe、HIP probe、container
  runtime），所以單獨執行與組入部署是同一份內容；參數 `nodes` 把每台節點的 check 限縮到那些節點。`gpu-gres` 數的是 job 的
  device cgroup 允許開啟的 render 裝置。有 `containerImage` 與 `containerRuntime` 時 `gpu-container` 在每台 GPU node 先以一個
  job 拉取 image（節點直接向它的 registry 拉，不經 Local Repository）並在 image 內以 `hipcc` 為 `gfx90a`、`gfx942`、`gfx950`
  建置 HIP probe（原始碼是 role 的 `files/gpu_probe.hip`，放在節點的 `/var/tmp`，結束時移除），再以同樣三種 GPU 作業在容器內只掛
  job 的裝置跑 kernel（`--pull=never`）；容器自帶 ROCm，所以 host 的 ROCm 不必涵蓋 GPU 的 gfx 目標。拉不到 image 時
  `gpu-container` 是帶 warning 的 `skipped`（KFD 與 AMD SMI 仍是硬性閘門），拉到了卻建置失敗或 kernel 失敗是 `failed`；功能沒有啟用的 check 是 `skipped`，啟用了卻缺 probe 是 `error`（提示重新部署）。每個 job 都帶
  `--time` 與 `--immediate`，job 名稱是 `swallow-validation-<run>`。單獨執行時只回報，不 drain；只有 gate mode 依
  decision 063 drain 失敗的 GPU node。它與叢集的 workload 搶資源，忙碌的叢集上 check 可能因為等不到節點而失敗。
- `kubernetes-smoke` 在 control-plane member 以 k0s 的 kubectl 執行，在 namespace `swallow-validation`（Pod Security
  `restricted`）以 busybox（Kubernetes 模組宣告的 image）建立短暫的 pod 與 Service，結束前依 run 的 label 刪除；不收工作的節點
  （taint、cordon、NotReady）的 `pod-scheduling` 是 `skipped`，應該是節點卻不在叢集的 worker member 的 `nodes-ready` 是 `failed`。
- `amd-gpu-basic` 比對 PCI 上的 AMD GPU（不含綁定 `vfio-pci`、交給 VM 的）與 KFD topology 的 GPU，再以 Slurm 部署共用的
  HIP probe（共用 role `gpu_probe` 的 `run.yml`，只在執行時複製）驗證；沒有 AMD GPU 的 Server 兩個 check 都是 `skipped`，
  主機沒有 HIP runtime（GPU container 自帶的 Kubernetes node）時 `hip-probe` 是 `skipped`。
- `host-baseline` 的 `cpu-count` 與 `memory` 預設以硬體盤點（inventory 的 `cpu_cores`、`memory_mib` 的 90%）為期望，沒有時
  是 `skipped`；`time-sync` 以部署的 `time_sync_check` 量測與 NTP server 的差距。
- `network-mesh` 從每台目標以 inventory 連線的位址探測其他目標（ICMP、不分段的 MTU 大小封包、SSH port），所以結果描述
  管理網路；只有一台目標時三個 check 都是 `skipped`。
- 需要安裝套件的檢查（iperf3 頻寬、RCCL allreduce、benchmark）與 HA 測試（impact `disruptive`）尚未提供：前者等
  artifact 宣告與 Local Repository 涵蓋它們（[local-repository.md](local-repository.md)）。

## 8. API、dashboard 與 CLI

- API：suite 目錄、revision 與 user suite、run 的受理、列表、讀取與取消、schedule 的 CRUD，見 validation 契約。目錄與
  revision 的讀取開放給所有已登入的 User；user suite 的建立開放給所有 User，修改只限作者，retire 限作者或 admin；run 的權限
  見 §3 與 §4.4；schedule 只限 admin（[identity-and-access.md](identity-and-access.md) §4.2）。run 的列表以
  `requestedBy=me` 找出呼叫者自己發起的 run。
- dashboard：Validation 區（Suites、Runs、Schedules），執行對話框選 suite、目標與參數；user suite 顯示作者
  （Created by），執行對話框顯示作者、revision、script，以及以呼叫者自己的帳號執行、是否 sudo；suite 自呼叫者上次執行後
  有新 revision 時先 review 差異，選「使用新版」才能執行。Suites 頁的「Hide other users' suites」開關（預設關，存在瀏覽器）
  只隱藏別人建立的 user suite，module suite 一直顯示，只在前端過濾。Run 詳情是 check 對目標的矩陣與
  evidence，連到 Workflow 詳情看 log；Server 與 Platform 詳情列出最近的 run。Validation 區不是 dashboard 的實驗功能，
  每個 build 都有。實驗性 suite 由 Validation 區自己的開關（Show experimental suites，存在瀏覽器）決定是否出現在 Suites、執行對話框與 schedule 的
  suite 選單；與實驗功能開關分開，已經存在的 run 與 schedule 照常顯示。
- CLI：`swallow validation suites|runs|schedules`；`runs start --wait` 與 `runs watch` 輪詢到 run 結束。

## 9. 程式位置對照（導航用）

路徑相對於 `api-server/`。

| 職責 | 路徑 |
| --- | --- |
| domain：suite、run、verdict 推導、schedule 與 cron | `internal/core/validation/domain/` |
| 受理、目錄、user suite、結果與狀態、gate、schedule controller | `internal/core/validation/application/` |
| HTTP | `internal/core/validation/delivery/` |
| user suite 發起者的 Host Access 與 sudo（與 Directory 投影共用的 `HostAccessFor`） | `internal/core/access/application/` |
| store | `internal/adapters/store/mongodb/validation/` |
| 範本的格式與 compiler | `internal/core/workflow/domain/template.go` |
| 模組宣告（`validations`、`templates`、`failureCodes`） | `internal/core/module/domain/` |
| launcher（report run 的 Workflow 組成）、完成行為的接線 | `internal/app/validation_launcher.go`、`internal/app/validation_completions.go`、`internal/app/workflow_completions.go` |
| gate 組入部署（Slurm 的 `slurm-acceptance`） | `internal/app/validation_gate.go`、`internal/app/platform_deployment_adapter.go` 的 `prepareSlurm` |
| schedule controller 與它讀的部署 | `internal/core/validation/application/schedule_controller.go`、`internal/app/validation_deployments.go`（worker 在 `internal/app/worker.go` 登錄） |
| 驗證內容 | `modules/validation/{host,network,script}/`、各模組的 `playbooks/validate-*.yml` 與它們的 role（`slurm_acceptance`、`k0s_smoke`、`amd_gpu_census`）、共用 role `modules/shared/roles/validation_report/` 與 `gpu_probe` |

## 10. 設計理由與相關文件

- 模組宣告 suite、report 與 gate 兩種 mode、user script 是資料、任何 User 撰寫並以發起者的帳號執行：decision 081。
- 部署的閘門語意：[decision 063](../decisions/063-omnistat-slurm-observability.md)（omnistat slurm observability）、
  [decision 066](../decisions/066-slurm-job-hooks-and-device-metrics-exporter.md)（slurm job hooks and device metrics exporter）。
- 取代：[decision 061](../decisions/061-recovery-supersedes-blocking-work.md)（recovery supersedes blocking work）；預算與
  unattended：[decision 077](../decisions/077-task-diagnostics-and-wait-policy.md)（Task diagnostics and wait policy）。
- 模組契約：[decision 075](../decisions/075-module-contract-v1.md)（module contract v1）。
