# 模組撰寫指南

本文件說明如何在 `api-server/modules/` 新增或修改一個模組的內容：目錄怎麼放、playbook 怎麼登錄、role 怎麼解析與
命名、每支 playbook 都要遵守的規則，以及要加哪些測試。讀者是新增或修改 plugin（Platform、Accelerator、Software
模組）或共用 host automation 內容的開發者與 AI agent。

> **範圍**：模組的宣告（`module.json` 與 root Go package，§1.1）與 host automation 內容（playbook 與 role）。
> 各種 Go hook 的介面隨模組抽出逐一定義（[core-and-extensions.md](core-and-extensions.md) §5.3），每個模組的負責人
> （`.github/CODEOWNERS`）尚未建立，實作時補進本文件。模組是什麼、可以依賴什麼，見
> [core-and-extensions.md](core-and-extensions.md)；Platform
> 與 Managed Software 各自的整合契約（inventory、trusted vars、結果）見
> [platform-deployment.md](platform-deployment.md) 與 [software-deployment.md](software-deployment.md)。

---

## 1. 目錄佈局

```text
api-server/modules/
├── manifest.json                 # release 的 playbook allowlist（§2）
├── <種類>/<id>/                  # 一個模組一個目錄；種類是 platform、accelerator 或 software
│   ├── playbooks/<file>.yml
│   ├── roles/<role>/
│   └── <Go package>/             # 模組的 Go 程式碼（若有）
└── shared/                       # 跨模組共用的內容
    ├── playbooks/<file>.yml
    └── roles/<role>/
```

- 只被一個模組使用的 role 放在該模組的 `roles/`。被兩個以上模組使用，或被 `shared/playbooks/` 的 playbook 使用的
  role 放在 `shared/roles/`。
- 模組 id 用 kebab-case（例如 `device-metrics-exporter`）。
- 模組的 Go 程式碼放在同一個目錄，例如 `modules/software/docker-ce/dockerengine`。`modules/platform/platformapi` 是
  Kubernetes 與 Slurm 暫時共用的過渡位置。image 建置時會去掉 `modules/` 底下的 Go 原始碼
  （`deploy/production/api-server.Dockerfile`），執行時的內容根目錄只有 manifest、playbook 與 role。
- `modules/` 就是 ansible-runner 的 project dir，安裝在 `/opt/swallow/automation/modules`；設定是
  `SWALLOW_API_PLAYBOOK_DIR` 與 `SWALLOW_API_PLAYBOOK_MANIFEST`（預設 `modules` 與 `modules/manifest.json`）。
- 2026-10-31 搬移前的路徑（playbook 與 role 原本在 `api-server` 的 automation 目錄）與現在的對照，見
  [`api-server` architecture spec](../../api-server/docs/development/architecture-spec.md) 的「2026-10-31 搬移前的
  路徑」。

### 1.1 模組的宣告

每個模組以 `modules/<種類>/<id>/module.json` 宣告自己（[decision 075](../decisions/075-module-contract-v1.md)），並在
同一個目錄有一個 root Go package，以 `go:embed` 嵌入它並回傳模組：

```json
{
  "contractVersion": 1,
  "kind": "software",
  "id": "nfs",
  "version": "0.1.0",
  "playbooks": ["deploy-nfs", "uninstall-nfs"],
  "workflowKinds": ["configure-nfs", "uninstall-nfs"],
  "taskKinds": [],
  "completions": [],
  "hooks": [],
  "capabilities": ["host-automation"],
  "routes": []
}
```

- `contractVersion` 目前只有 `1`；core 拒絕載入不支援的版本。`id` 是目錄名稱（kebab-case），`version` 是 SemVer；
  模組規劃的 Workflow 以 `<種類>/<id>@<version>` 為 definition。
- 各清單是模組擁有的名稱：`playbooks`（allowlist 中的名稱，路徑必須在模組的 `playbooks/`）、`workflowKinds`、
  只有這個模組宣告的 `taskKinds`、`completions`、`routes`（`/api/v1/` 底下的路徑前綴）。兩個模組不得擁有同一個名稱；
  不屬於任何模組的 Workflow kind 與 completion 屬於 core。
- `hooks` 列出 Go hook 與「資料表達不了」的理由，root package 實作的 hook 必須剛好是這些；`capabilities` 以
  provider interface 命名（例如 `host-automation`、`os-provisioning`），`spec` 是模組定義資源 spec 時的 JSON Schema。
- root package 的 `Module()` 以 `moduledomain.Load` 讀嵌入的 manifest；在 `internal/app/modules.go` 的
  `compiledModules` 加上它，api、worker 與 ansible-executor 就以同一個 registry 載入，manifest 不合契約時拒絕啟動。
- 模組只 import core、`internal/shared` 與自己的 package；需要另一個模組的能力時，經由 core 的 port 取得。
- Software 模組的 id 就是它的 Managed Software kind。manifest 宣告 Workflow kind `configure-<id>`、`uninstall-<id>`
  與 playbook `deploy-<id>`、`uninstall-<id>`；root package 以 `software` 為名實作 core 的 `Software` port（catalog
  entry、spec 驗證與正規化、trusted vars），composition root 啟動時核對這些名稱並組出 catalog，詳見
  [software-deployment.md](software-deployment.md) §2。
- 提供 HTTP 路由的模組把 use case 與 delivery 分成子 package，delivery package 匯出 `RegisterRoutes`，由
  `internal/app/api.go` 掛到對應的 router；掛上的路徑必須落在 `routes` 宣告的前綴內。use case 依賴的 core 資料以
  模組自己定義的小介面表達，只列它實際呼叫的方法。docker-ce 的 Docker Host Explorer 是例子：`explorer/` 是 use case，
  `explorerhttp/` 是 handler 與路由，`dockerengine/` 是 Engine client。

## 2. 登錄 playbook

[`api-server/modules/manifest.json`](../../api-server/modules/manifest.json) 是 release 的 allowlist：只有登錄的
playbook 可以執行（[decision 006](../decisions/006-embedded-ansible-execution.md)）。

```json
{
  "schemaVersion": 1,
  "playbooks": [
    { "name": "deploy-slurm", "path": "platform/slurm/playbooks/deploy-slurm.yml" }
  ]
}
```

- `path` 相對於 `modules/`，只能是 `<種類>/<id>/playbooks/<file>.yml` 或 `shared/playbooks/<file>.yml`；絕對路徑、
  含 `..` 或其他層級的路徑都會被拒絕，檔案必須存在。
- `name` 必須唯一。同一個 `path` 可以有多個 `name`：改名時保留舊名當 alias，已受理的 Workflow 才能以舊名重試（例如
  `configure-device-metrics-exporter`）。
- api、worker 與 ansible-executor 啟動時以 `LoadManifestCatalog` 驗證 manifest，失敗就拒絕啟動；
  `deploy/release/validate.sh` 另外檢查 `schemaVersion` 與 playbook 不為空。
- 沒有登錄的 playbook 不會被執行；目前每一支 playbook 都已登錄，新增時一併登錄。
- launcher 選擇 playbook 名稱的方式有兩種：寫死在 launcher（Slurm、GPU Operator、Managed Software、
  `uninstall-kubernetes`），或經 Site 的 `playbookMappings`（`deploy-kubernetes`），見
  [platform-deployment.md](platform-deployment.md) §4.5。

## 3. role 的解析與命名

- **每次執行只看得到自己模組與 `shared/` 的 role**：`ANSIBLE_ROLES_PATH` 是 playbook 所屬模組的 `roles/` 加上
  `shared/roles/`；`shared/playbooks/` 的 playbook 只有 `shared/roles/`。使用其他模組的 role 會在執行時找不到。
- **不要把 role 放在 `playbooks/roles/`**：Ansible 會在 playbook 旁邊找到它，但內容測試只在 roles path 內解析，
  會判定為缺少而失敗。
- **role 名稱在所有模組之間唯一**（`shared` 算一個模組），否則一次執行用到哪一份取決於路徑順序。
- **playbook 檔名在所有模組之間唯一**：測試與文件以檔名指稱 playbook。
- 現行的命名慣例（不是測試強制的規則）：playbook 以 kebab-case 的動詞開頭（`deploy-`、`uninstall-`、`configure-`、
  `install-`、`remove-`）；只屬於一個模組的 role 用 snake_case 並帶模組前綴（`k0s_`、`slurm_`）。

## 4. 每支 playbook 都要遵守的規則

Platform 與 Software 共用以下規則；各自特有的規則見 [platform-deployment.md](platform-deployment.md) §5 與
[software-deployment.md](software-deployment.md) §4.5。

1. **冪等**：可以安全重跑；已經達成時不產生變更（ensure 語意）。Workflow 的接續、activity 重試與 restore 都靠這一點。
2. **只依賴契約**：只消費 inventory 與 trusted vars（platform-deployment §4.1、§4.2；software-deployment §4.1、
   §4.2）；不得假設 client 能設定 `swallow_` 前綴的變數，也不得依賴 swallow 的內部實作或非契約的 hostvar 與 group。
3. **以 serverId 為身分**：`inventory_hostname` 是 serverId，連線用 `ansible_host`；需要 OS hostname 時自己
   `gather_facts`。
4. **排序留在 playbook**：`serial`、handler、host loop 屬於 Ansible；只有中途必須插入非 Ansible 的動作時，才拆成兩個
   Task。
5. **依賴鎖定，只用 `ansible.builtin`**：Python 依賴 pin 在
   `api-server/internal/adapters/hostautomation/ansible/requirements.txt`，由 image 安裝。image 不安裝任何 galaxy
   collection 或 role（`requirements.yml` 是空的，也沒有 image 使用它）；需要 collection 時要同時修改 image 建置與
   離線打包，視為架構變更。
6. **不逃出內容根目錄**：不得引用 `modules/` 以外的路徑，也不得使用其他模組的 role（§3）。
7. **結果與 secret 走契約**：需要回傳結果時只寫 `swallow_result_path` 的 `result.json`；secret 只經 sealed secret
   ref 傳入，不得寫入 log 或 history（處理 secret 的 task 加 `no_log: true`）。
8. **可離線**：版本固定，避免執行時抓取未鎖定的內容。
9. **改套件時暫緩 OS 的套件維護**：見 §5。

## 5. 改套件的內容

會安裝、移除或更新套件，或刷新套件索引的 playbook（decision 073，語意見
[core-and-extensions.md](core-and-extensions.md) §5.2）：

- 第一個 play 是 `hosts: all`、`gather_facts: false`，`roles:` 含 `package_maintenance_hold`。
- 最後一個 play 是 `hosts: all`，以 `include_role`（或 `import_role`）`package_maintenance_hold` 加
  `tasks_from: release` 釋放 hold。
- 每個套件 task 都要 `register`、`until: ... is succeeded`、`retries` 與 `delay`，涵蓋 mirror 的暫時性錯誤。
- 「套件 task」包括 `apt`、`dnf`、`yum`、`package`（含 `ansible.builtin.` 前綴）、沒有設 `update_cache: false` 的
  `apt_repository`，以及執行 `apt-get`、`apt`、`dpkg`、`dnf`、`yum` 安裝、移除、升級或更新索引的
  `command`、`shell`、`raw`。

## 6. 測試

內容測試在 `api-server/internal/adapters/hostautomation/ansible/`（package `ansible`），以
`cd api-server && go test ./internal/adapters/hostautomation/ansible/` 執行；部分測試需要 python3、C compiler 與
bash。

**對所有內容自動執行的結構測試**（新增內容時不必改它們，但必須通過）：

| 測試 | 強制的規則 |
| --- | --- |
| `module_roles_test.go` 的 `TestShippedPlaybooksResolveRolesWithinTheirModule` | playbook 直接或間接用到的 role 都在該次執行的 roles path 內 |
| `module_roles_test.go` 的 `TestShippedRoleNamesAreUnique`、`TestShippedPlaybookFileNamesAreUnique` | role 名稱與 playbook 檔名在模組之間唯一 |
| `package_hold_playbook_test.go` | 改套件的 playbook 取得與釋放 hold；每個套件 task 都重試；hold 只改執行期狀態 |
| `playbook_catalog_test.go` | manifest 的路徑符合模組佈局；roles path 只含自己模組與 `shared/` |
| `internal/app/software_deployment_adapter_test.go` 的 `TestEverySoftwareKindHasRegisteredPlaybooks` | 每種 Managed Software 的 install 與 uninstall playbook 都已登錄 |
| `internal/app/modules_test.go` | 每個模組目錄都是編入的模組；manifest 的 playbook 與 allowlist 一致；每個 Workflow kind 與 completion 型別只屬於 core 或一個模組 |

**helper**（`content_test.go`、`module_roles_test.go`）：`moduleDirs`、`roleDir(name)`（找出擁有該 role 的模組）、
`shippedFile(rel)`（`deploy-slurm.yml` 或 `roles/<role>/<file>` 對應到實際檔案）、`readShippedManifest`、
`roleClosure`、`readYAML`；`slurm_job_hooks_playbook_test.go` 的 `slurmPlaybookFile` 以 `shippedFile` 讀檔，其他測試
也在用。

**新內容要加的測試**：為 playbook 依賴的契約寫一個 `<topic>_playbook_test.go`，斷言它引用了必要的 trusted var、play
的順序，以及禁止的操作（例如不得 reboot 記憶體根目錄的主機）；範例是 `k0s_topology_playbook_test.go`。以
`shippedFile` 與 `roleDir` 指稱檔案，不要寫死模組路徑。例外：`internal/core/platform/domain/gpu_operator_test.go` 寫死了
AMD 模組的 chart 路徑，搬移該 role 時要一併修改。

端到端的驗證在 lab 以真機進行（platform-deployment §6.2）：只用 `lab-` 開頭的 Server，不碰 `tainan-` 開頭的機器，
也不刪 MAAS 的資料。

## 7. 搬移或改名內容

- 改 manifest 的 `path`；playbook 的 `name` 已被 Site 的 `playbookMappings` 與已受理的 Workflow 引用，改名時保留
  舊名當 alias（§2）。
- 測試以 helper 找檔案，搬移通常不必改；寫死路徑的例外見 §6。
- 同步文件中的位置表：[platform-deployment.md](platform-deployment.md) §7、
  [software-deployment.md](software-deployment.md) §7、[installation.md](installation.md)（`configure-ntp`）。
- `deploy/production/swallowctl` 的 doctor 執行 `shared/playbooks/diagnostic-ping.yml`，搬移它時一併修改。

## 8. 提交前的檢查清單

- [ ] 內容放在正確的模組目錄；共用的內容放在 `shared/`（§1）。
- [ ] `module.json` 列出模組擁有的 playbook、Workflow kind、completion 與 route，模組已加入 `compiledModules`（§1.1）。
- [ ] 新 playbook 已登錄在 `manifest.json`；改名保留了舊名（§2、§7）。
- [ ] role 只在自己模組或 `shared/` 解析，名稱與檔名唯一（§3）。
- [ ] 遵守 §4 的規則；改套件的 playbook 取得與釋放 hold，套件 task 會重試（§5）。
- [ ] 結構測試通過，並為新內容寫了契約測試（§6）。
- [ ] 位置表與相關規格已更新（§7）。
- [ ] 模組層級的檢查見 [core-and-extensions.md](core-and-extensions.md) §11.2。
