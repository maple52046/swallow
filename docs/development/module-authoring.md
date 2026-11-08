# 模組撰寫指南

本文件說明如何在 `api-server/modules/` 新增或修改一個模組的內容：目錄怎麼放、playbook 怎麼登錄、role 怎麼解析與
命名、每支 playbook 都要遵守的規則，以及要加哪些測試。讀者是新增或修改 plugin（Platform、Accelerator、Software
模組）或共用 host automation 內容的開發者與 AI agent。

> **範圍**：模組的宣告（`module.json`、root Go package 與它實作的 hook、HTTP 路由，§1.1）、模組在 dashboard 的畫面
> （§1.2）與 host automation 內容（playbook 與 role）。模組是什麼、可以依賴什麼，見
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
- 模組的 Go 程式碼放在同一個目錄，例如 `modules/software/docker-ce/dockerengine`、`modules/platform/kubernetes/kubeapi`。
  兩個模組共用的 Go helper 放 `internal/shared`（例如 `internal/shared/httpjson`），不放在任何一個模組裡。
  image 建置時會去掉 `modules/` 底下的 Go 原始碼
  （`deploy/production/api-server.Dockerfile`），執行時的內容根目錄只有 manifest、playbook 與 role。
- `modules/` 就是 ansible-runner 的 project dir，安裝在 `/opt/swallow/automation/modules`；設定是
  `SWALLOW_API_PLAYBOOK_DIR` 與 `SWALLOW_API_PLAYBOOK_MANIFEST`（預設 `modules` 與 `modules/manifest.json`）。
- 2026-10-31 搬移前的路徑（playbook 與 role 原本在 `api-server` 的 automation 目錄）與現在的對照，見
  [`api-server` architecture spec](../../api-server/docs/development/architecture-spec.md) 的「2026-10-31 搬移前的
  路徑」。

### 1.1 模組的宣告

每個模組以 `modules/<種類>/<id>/module.json` 宣告自己（[decision 075](../decisions/075-module-contract-v1.md)（module contract v1）），並在
同一個目錄有一個 root Go package，以 `go:embed` 嵌入它並回傳模組：

```json
{
  "contractVersion": 1,
  "kind": "software",
  "id": "podman",
  "version": "0.1.0",
  "playbooks": ["deploy-podman", "uninstall-podman"],
  "workflowKinds": ["configure-podman", "uninstall-podman"],
  "taskKinds": [],
  "completions": [],
  "hooks": [
    {
      "name": "software",
      "reason": "Validates the optional package version and turns it into the playbook's trusted variable; the catalog entry travels with it."
    }
  ],
  "capabilities": ["host-automation"],
  "routes": []
}
```

（`api-server/modules/software/podman/module.json`；帶 `spec` schema 的例子見同目錄的 `nfs/module.json`，帶 `artifacts`
的例子見 `platform/kubernetes/module.json` 與 `software/docker-ce/module.json`。）

- `contractVersion` 目前只有 `1`；core 拒絕載入不支援的版本。`id` 是目錄名稱（kebab-case），`version` 是 SemVer；
  模組規劃的 Workflow 以 `<種類>/<id>@<version>` 為 definition。
- 各清單是模組擁有的名稱：`playbooks`（allowlist 中的名稱，路徑必須在模組的 `playbooks/`）、`workflowKinds`、
  只有這個模組宣告的 `taskKinds`、`completions`、`routes`（`/api/v1/` 底下的路徑前綴）。兩個模組不得擁有同一個名稱；
  不屬於任何模組的 Workflow kind 與 completion 屬於 core。
- `hooks` 列出 Go hook 與「資料表達不了」的理由，root package 實作的 hook 必須剛好是這些；`capabilities` 以
  provider interface 命名（例如 `host-automation`、`os-provisioning`）。
- `spec` 是模組定義資源 spec 時的 schema，屬於 core 與 dashboard 讀法相同的 JSON Schema 子集（定義在
  `internal/core/module/domain` 的 `ParseSpecSchema`，契約見
  [`modules.md`](../../api-server/docs/development/api-contracts/api-server/modules.md)）：根是 `"type": "object"`
  加 `"additionalProperties": false`；每個屬性是 `string`、`integer`、`boolean` 或字串 `array`，必須有 `title`，可以有
  `description`、`examples`（表單以第一個作為 placeholder）、字串的 `enum` 與 `pattern`、整數的 `minimum` 與 `maximum`；
  用到其他關鍵字時拒絕啟動。屬性的順序就是通用表單的欄位順序。Software 模組的屬性必須剛好是 catalog entry 的
  `SpecFields`，core 在 hook 的 `ValidateSpec` 之前以 schema 檢查型別與未知欄位，所以 hook 只需要寫跨欄位或依 role 的規則。
  dashboard 對有 schema 而沒有專屬畫面的模組產生通用的表單與詳情（目前是 NFS 與 VictoriaMetrics）。
- `artifacts` 是模組的 host automation 要取得的全部內容。部署時由 Site 選擇的來源提供：Site 的 Local Repository，或這裡
  宣告的上游（[local-repository.md](local-repository.md) §2.1）。型別與檢查在 `internal/core/module/domain` 的 `Artifacts`：
  - `sources`：上游套件來源，Site 使用 Local Repository 時由它鏡像，否則主機直接使用。`name` 是 kebab-case，在 Site 內
    唯一；`format` 是 `deb`、`rpm`、`oci`、`python` 或 `file`（目前模組只宣告 `deb`）；`url`；deb 必須有 `suites` 與
    `components`，其他格式不得有；`architectures` 可省略，只用於 deb 與 rpm。deb 與 rpm 必須有 `signingKey`，其他格式
    不得有：上游簽它的 metadata 的公鑰，`url` 是 https 的下載位址，`fingerprint` 是它的 OpenPGP fingerprint（40 個十六進位
    字，不帶空白；以大寫保存），例如 docker-ce 的
    `{"url": "https://download.docker.com/linux/ubuntu/gpg", "fingerprint": "9DC858229FC7DD38854AE2D88D81803C0EBFCD88"}`。
    從上游取得時，主機下載這把金鑰、確認 fingerprint 之後才以它驗證來源；宣告前先下載金鑰，以 gpg 讀出 fingerprint。
  - `files`：單一檔案。`path` 是相對路徑（不以 `/` 開頭、不含 `.` 或 `..`、只用不需要 URL 跳脫的字元），慣例是
    `<名稱>/<版本>/<檔名>`；`url` 是上游位址；`sha256` 是 64 個小寫十六進位字元。role 以
    `swallow_artifacts.files['<path>']` 取得檔案（Local Repository 或 `url`），並以宣告的 sha256 驗證。
  - `images`：完整的 image reference，必須寫出 registry host，並有 tag、`@sha256:` digest 或兩者，例如
    `docker.io/library/busybox:1.36`，不能寫 `busybox:1.36`。
  - `python`：固定版本的 requirement，`name==version`；同一個 project（名稱依 index 的規則正規化後）只出現一次。
  - `url` 都必須是絕對的 http 或 https URL，不得帶 user info、query 或 fragment。
  - role 只取宣告過的 artifact，而且只從 `swallow_artifacts` 指出的位址取得：它下載的檔案、引用的 image 與設定的套件
    來源都必須出現在 `artifacts`，新增或升級時同時改宣告。image 與 Python 套件在 Local Repository 是 pull-through：
    宣告的 image 會預先抓取，Python 套件則在第一次被要求時才取得（[local-repository.md](local-repository.md) §5.3）；
    container runtime 或 pip 自己解析出的間接依賴不必逐一宣告。
  - OS 套件不在這裡宣告：發行版 archive 的套件來自 Site 的 OS 套件來源（Site 使用 Local Repository 時是 Site 的 user 宣告
    的 `ubuntu` 來源，否則是主機自己的 OS 套件來源），模組只宣告 OS 來源以外的東西。共用 role 的 artifact 由使用它的一個
    模組宣告（例如共用的 `helm` 由 kubernetes 模組宣告）。
  - 兩個模組可以宣告同一個 source 或 file，但內容必須一致：同名的 source 有相同的 `format`、`url` 與 `signingKey`
    （`suites`、`components`、`architectures` 可以不同，取聯集），同一個 `path` 的 file 有相同的 `url` 與 `sha256`；
    不一致時 registry 建立失敗，process 拒絕啟動。
- 每個模組目錄在 `.github/CODEOWNERS` 有一行負責人（目前全部是同一人；後列的規則優先，所以模組的行放在通用的行
  之後），review 依此分派。
- root package 的 `Module()` 以 `moduledomain.Load` 讀嵌入的 manifest；在 `internal/app/modules.go` 的
  `compiledModules` 加上它，api、worker 與 ansible-executor 就以同一個 registry 載入，manifest 不合契約時拒絕啟動。
- 模組只 import core、`internal/shared` 與自己的 package；需要另一個模組的能力時，經由 core 的 port 取得。
- Software 模組的 id 就是它的 Managed Software kind。manifest 宣告 Workflow kind `configure-<id>`、`uninstall-<id>`
  與 playbook `deploy-<id>`、`uninstall-<id>`；root package 以 `software` 為名實作 core 的 `Software` port（catalog
  entry、spec 驗證與正規化、trusted vars），composition root 啟動時核對這些名稱並組出 catalog，詳見
  [software-deployment.md](software-deployment.md) §2。既有的 kind 以 `internal/core/software/domain` 的常數命名（越界，
  core-and-extensions §3.6）；新的 kind 在模組內以 `softwaredomain.Kind("<id>")` 定義自己的值，不在 core 加常數。
- Platform 模組以 `platform` 為名實作 platform domain 的 `PlatformModule`：Platform spec 的 `settings` 是該類型部署 request
  的專屬欄位，`members` 是該類型的 assignment（模組自己的 role 名稱）。
  - `DeployRequest` 與 `SpecFromDeployRequest` 在 spec 與部署 request 之間轉換；controller 接管既有的 Platform 時，也以
    後者讀它最近一次部署的 request。
  - `Changes(applied, desired)` 列出一次 spec 變更的動作，每個依 [desired-state-model.md](desired-state-model.md) §5.2
    標為破壞性或非破壞性；core 依此決定直接收斂，或產生 Change Plan 等核准。`Realization` 指出由哪一種 Workflow 實現
    （部署、卸載、GPU Operator、只重設收集 hardware counters 的監控功能）。`Unsupported` 列出 swallow 還不能執行的動作：破壞性的會擋住 Change Plan，非破壞性的
    讓這次寫入被拒。改不可變欄位是 `ErrImmutableSpecField`。
  - `ReportsMembership` 說明平台是否會回報某個成員；會回報卻沒有回報的成員算 drift。
- Platform 模組以 `platform-reader` 為名實作 platform domain 的 `ReaderBuilder`：core 的 `ReaderFactory` 把 Platform 的
  Integration 解析成 `Connection`（endpoint、token、timeout、CA、設定），再交給 Platform type 對應的 builder。
- Accelerator 模組的 id 就是它的 accelerator id；root package 以 `accelerator` 為名實作 platform domain 的 `Accelerator`
  port（`ID`、`Label`、`IsCompute`）。需要某家 GPU 的 Software 模組在 catalog entry 以 `RequiredAccelerator` 指名
  它的 id，不 import 它。
- 規劃 Workflow 的 launcher 以 Task 的 `Job` 為 Job 命名，Job 的顯示名稱登錄在 `internal/app/workflow_plan.go` 的
  `workflowJobLabels`；Software kind 的 `configure-<kind>` 以 catalog 的 label 命名（`Configure Docker CE`），單獨的
  software Workflow 則依方向標為 `Install NFS`、`Uninstall NFS`。launcher 自己設定的 label 優先；Workflow 服務受理計畫
  時只為沒有 label 的 Job 從 plan catalog 補上，launcher 的測試檢查每個常數 Job 都有 label。
- 提供 HTTP 路由的模組把 use case 與 delivery 分成子 package，delivery package 匯出 `RegisterRoutes`，由
  `internal/app/api.go` 掛到對應的 router；掛上的路徑必須落在 `routes` 宣告的前綴內。use case 依賴的 core 資料以
  模組自己定義的小介面表達，只列它實際呼叫的方法。docker-ce 的 Docker Host Explorer 是例子：`explorer/` 是 use case，
  `explorerhttp/` 是 handler 與路由，`dockerengine/` 是 Engine client。

### 1.2 dashboard 的畫面

模組在 dashboard 的畫面放在 `dashboard/src/modules/<種類>/<id>/`，`index.ts` 匯出一份 manifest，登錄在
`src/modules/registry.ts`（規則見 dashboard 的 [architecture spec](../../dashboard/docs/development/architecture-spec.md)
的 Module UI 一節）：

- Platform 模組（`PlatformModuleUI`）提供 Overview 本體、分頁，以及部署 wizard 的 `deploy.useForm`：類型專屬的欄位、
  角色欄、Features、額外的步驟、review 卡片與部署 request 的欄位。Site、名稱、候選 Server、OS 準備、時鐘檢查與送出
  屬於 wizard 外殼。
- Accelerator 模組（`AcceleratorModuleUI`）提供它帶給某個 Platform 類型的區塊（`platformSections`）與部署功能
  （`deployFeatures`），例如 AMD 模組帶給 Kubernetes 的 GPU Operator。
- 外殼只經由 registry 使用模組；模組不 import 其他模組與 infrastructure，模組專屬的 API adapter 放在模組資料夾，由
  `src/di` 綁定。lint 規則守住這些邊界。
- 沒有專屬畫面的模組，設定的表單與詳情不必寫程式：manifest 的 `spec`（§1.1）經 `GET /api/v1/modules` 提供，dashboard
  以 `ModuleSettingsFields` 與 `ModuleSettingsDetails` 產生（目前是 NFS 與 VictoriaMetrics）。
- Software 模組仍要改 dashboard：`SoftwareKind` 是固定清單，dashboard 會濾掉不認得的 kind。新增 kind 時改：
  - `src/domain/software/types.ts` 的 `SoftwareKind` 與 `SOFTWARE_KINDS`；
  - `softwareCatalogPresentation.ts` 的 `PRESENTATION`（分類、說明、圖示、順序）；分類只有 `SoftwareCategory` 的
    三種（container runtimes、storage and file sharing、observability），放不進去時新增一種；
  - `softwarePresentation.ts` 的 `softwareKindLabel`（catalog 還沒載入時的名稱）；
  - e2e fixture（`dashboard/tests/e2e/fixtures.ts`）的 software catalog 與 `/api/v1/modules` 回應。

## 2. 登錄 playbook

[`api-server/modules/manifest.json`](../../api-server/modules/manifest.json) 是 release 的 allowlist：只有登錄的
playbook 可以執行（[decision 072](../decisions/072-store-driven-workflow-execution.md)（store driven workflow execution））。

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
[software-deployment.md](software-deployment.md) §4.5。Kubernetes 在 Ubuntu 與 RHEL 系列上的差異（套件、firewall、
SELinux、哪些 role 依 OS 分支）見 platform-deployment §2.3。

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
8. **只取宣告的 artifact，從 `swallow_artifacts` 取得**：主機要的套件、檔案、image 與 Python 套件都是模組宣告的固定
   版本，從 trusted var `swallow_artifacts` 指出的位址取得：Site 使用 Local Repository 時是它，否則是宣告的上游。role 不寫
   網際網路的 URL，兩種來源用同一份 role。trusted var 的形狀與受理時的拒絕見 [local-repository.md](local-repository.md)
   §8.1，`artifact_sources` 在兩種來源下怎麼設定主機見同一份的 §8.2。
   - 會安裝套件、下載檔案或拉 image 的 playbook，在 manifest 的 entry 標 `"fetchesArtifacts": true`：swallow 才會注入
     這個 var；Site 使用 Local Repository 而它沒有 ready 時，在受理時就拒絕。別名的 entry 也要標。
   - 這類 playbook 在取得 hold（§5）之後的下一個 play 以 `hosts: all` 執行共用 role `artifact_sources`，之前不得有任何套件、
     下載或拉 image 的 task。它依 `swallow_artifacts.source` 設定主機的 apt、pip、uv 與 container runtime：從 Local Repository
     取得時把它們指向 Local Repository（主機其他的 apt sources，包括 image 帶的，改名為 `.disabled-by-swallow`，可以還原）；
     從上游取得時以宣告的金鑰（確認 fingerprint，主機需要 gpg）加入宣告的 deb 來源，保留主機的 OS 來源，並移除前一種
     模式寫過的設定。playbook 以變數指出它的 run 用哪些 container runtime 拉 image：`artifact_sources_containerd`（k0s 的
     containerd 經 mirror 解析 image，必須在 k0s 啟動之前）、`artifact_sources_docker`（`daemon.json` 的
     `insecure-registries`）、`artifact_sources_podman`。`artifact_sources_root` 是給內容測試的路徑前綴，playbook 不設。
   - role 下載的檔案在 defaults 寫成 `path` 與 `sha256`，以 `{{ swallow_artifacts.files['<宣告的 path>'] }}` 下載、以
     `get_url` 的 `checksum: "sha256:<sha256>"` 驗證；同一個檔案必須是模組 `module.json` 宣告的 artifact（§1.1），path 與
     sha256 相同。
   - 第三方的 deb repository 宣告成模組的 source（帶 `signingKey`），role 只以名稱確認它在 `swallow_artifacts.deb` 裡
     （沒有時以 `[artifact_source_missing]` 失敗，比照 `docker_ce` 與 `grafana`），不自己加 apt key 或 repository。
   - image 在 Kubernetes 上寫原本的名稱（從 Local Repository 取得時 containerd 經 mirror 解析），在 Docker、Podman 上寫成
     `{{ swallow_artifacts.images['<宣告的 reference>'] }}`。
   - role 不指定 Python 的 index：pip 與 uv 讀 `artifact_sources` 寫的節點設定（`/etc/pip.conf`、`/etc/uv/uv.toml`），
     從上游取得時用它們自己的預設 index（PyPI）。只有 `artifact_sources` 讀 `swallow_artifacts.python_index`；其他 role 與
     playbook 不得讀它，也不得設 `PIP_INDEX_URL`、`UV_*INDEX*` 這類變數或 `--index-url` 這類參數
     （`python_toolchain_playbook_test.go` 的 `TestOnlyArtifactSourcesChoosesAPythonIndex`）。
   - 內容測試 `artifact_sources_playbook_test.go` 強制：`uninstall-*` 以外會取得內容的 playbook 都有標記；標記的 playbook
     先執行 `artifact_sources`；role 與 playbook 不寫網際網路的 URL（註解除外），每個 `get_url` 都從
     `swallow_artifacts.files` 下載；role 固定的檔案與模組的宣告一致（§6）。
   - 過渡期的例外列在該測試的 allowlist，並附理由：RHEL 系列的 dnf 分支還用上游的 yum repository（Local Repository
     還沒有 rpm），Prometheus 模組還不在 artifact 宣告的範圍內。
9. **改套件時暫緩 OS 的套件維護**：見 §5。
10. **data 目錄不得交出共用的上層目錄**：軟體的資料放在 `/var/lib/swallow/<名稱>`。`ansible.builtin.file` 會把它建立的
    上層目錄也設成該 task 的 owner 與 mode，所以 role 建立 data 目錄之前先 `include_role` 共用 role
    `shared_data_dir`，讓 `/var/lib/swallow` 維持 `root:root 0755`；否則最先安裝的軟體會擁有它，`0750` 的 data
    目錄（例如 Grafana native）更會讓其他軟體讀不到自己的 data。它也修正先前的安裝留下的錯誤 owner。
11. **Python 工具鏈是需求，不是固定版本的安裝**（[decision 080](../decisions/080-host-toolchains-probe-first.md)（host
    toolchains probe first））：需要以 Python 建置環境的 role 以 `include_role` 執行共用 role `python_toolchain`，取得
    任何 CPython 3.12 與任何能執行的 uv，不自己安裝 `python3-venv` 或編譯工具。
    - 它依序找 `/usr/bin/python3.12`、`/opt/swallow/python/3.12/bin/python3.12`、`PATH` 上的 `python3.12`（必須是
      CPython 3.12），以及 `/usr/local/bin/uv`、`/opt/swallow/bin/uv`、`PATH` 上的 `uv`，用第一個能用的，路徑寫入
      `python_toolchain_python` 與 `python_toolchain_uv`。都找到時不改變主機。
    - 缺 CPython 3.12 時從 OS 套件安裝（`python_toolchain_python_packages`，以 `<distribution>-<major version>` 為 key，
      目前只有 Ubuntu 24.04 的 `python3.12`）；沒有對應套件的 OS 以 `[python312_unavailable]` 失敗。
    - 缺 uv 時下載固定版本的 uv release（file artifact，由 slurm 模組宣告），驗證 sha256 後解開在 `/opt/swallow/uv/`，
      連結到 `/opt/swallow/bin/uv`；沒有宣告該架構時以 `[uv_unavailable]` 失敗。這個固定版本只決定 swallow 提供哪一份，
      主機已有的任何 uv 都接受。
    - 用到它的 playbook 會安裝套件與下載檔案，所以標 `fetchesArtifacts`，並依 §5 取得與釋放 hold。
    - 以 uv 從 wheel 建立環境，安裝的內容以 hash lock 固定（`--require-hashes`）；不用 `python3 -m venv`，也不安裝
      `python3-venv`、`python3-dev`、`build-essential`（`TestNoRoleBuildsPythonWithTheOSToolchain`）。元件先探測、不能用
      才安裝（Omnistat 的做法見 [platform-deployment.md](platform-deployment.md) §6.3）。

## 5. 改套件的內容

會安裝、移除或更新套件，或刷新套件索引的 playbook（[decision 073](../decisions/073-hold-os-package-maintenance-during-automation.md)（hold os package maintenance during automation），語意見
[core-and-extensions.md](core-and-extensions.md) §5.2）：

- 第一個 play 是 `hosts: all`、`gather_facts: false`，`roles:` 含 `package_maintenance_hold`。
- 最後一個 play 是 `hosts: all`，以 `include_role`（或 `import_role`）`package_maintenance_hold` 加
  `tasks_from: release` 釋放 hold。
- 每個套件 task 都要 `register`、`until: ... is succeeded`、`retries` 與 `delay`，涵蓋 mirror 的暫時性錯誤。
- 「套件 task」包括 `apt`、`dnf`、`yum`、`package`（含 `ansible.builtin.` 前綴）、沒有設 `update_cache: false` 的
  `apt_repository`，以及執行 `apt-get`、`apt`、`dpkg`、`dnf`、`yum` 安裝、移除、升級或更新索引的
  `command`、`shell`、`raw`。`yum_repository` 只寫入 `.repo` 檔、不執行 dnf，不算套件 task；之後從新 repository
  安裝的 `dnf` task 才是。
- `artifact_sources` 會刷新套件索引，所以執行它的 playbook 都依本節取得與釋放 hold。
- 需要第三方的 deb repository 時，在模組的 `module.json` 以 `artifacts.sources` 宣告它與它的 `signingKey`（§1.1），由
  `artifact_sources` 加到主機（Local Repository 的鏡像或上游本身），role 只安裝套件（§4 第 8 條）。RHEL 系列在 Local
  Repository 提供 rpm 之前，仍以 `yum_repository` 加上游的 repository，
  開啟 `gpgcheck` 並指定 `gpgkey`（比照 `docker_ce` role），並把它的 URL 列入 §4 第 8 條的 allowlist。

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
| `artifact_sources_playbook_test.go` 的 `TestFetchingPlaybooksAreMarkedFetchesArtifacts`、`TestArtifactPlaybooksPointAtTheirSourcesBeforeFetching` | `uninstall-*` 以外會安裝、下載或拉 image 的 playbook（別名也算）都標 `fetchesArtifacts`；標記的 playbook 在任何取得之前先以 `hosts: all` 執行 `artifact_sources`。還沒標記的 playbook 列在 `fetchesArtifactsNotYet` 並寫出理由 |
| `artifact_sources_playbook_test.go` 的 `TestShippedContentFetchesOnlyDeclaredArtifacts`、`TestPinnedFilesAreDeclaredModuleArtifacts` | role 與 playbook 在註解以外不寫網際網路的 URL（例外列在 `internetURLAllowlist` 並寫出理由，不再相符的項目也會失敗），每個 `get_url` 都從 `swallow_artifacts.files` 下載；role 在 defaults 固定的檔案（path 與 sha256）都是模組宣告的 file artifact，checksum 相同 |
| `artifact_sources_playbook_test.go` 的 `TestArtifactPlaybooksConfigureTheirContainerRuntime` | 拉 image 的 playbook 把自己的 container runtime 交給 `artifact_sources`，`deploy-kubernetes.yml` 在 k0s 啟動之前寫好 mirror，Docker 上的 Grafana image 取自 `swallow_artifacts.images`。playbook 清單寫死在測試中，新增拉 image 的 playbook 時一併加上 |
| `playbook_catalog_artifacts_test.go` | manifest catalog 回報哪些 playbook 標了 `fetchesArtifacts`，並拒絕 schema 沒有的 key（包括退役的舊旗標） |
| `shared_data_dir_playbook_test.go` | defaults 把 `*_data_dir` 放在 `/var/lib/swallow` 底下的 role，在任何建立該目錄的 task 之前 include `shared_data_dir`（§4 第 10 條） |
| `internal/app/software_deployment_adapter_test.go` 的 `TestEverySoftwareKindHasRegisteredPlaybooks` | 每種 Managed Software 的 install 與 uninstall playbook 都已登錄 |
| `internal/app/modules_test.go` | 每個模組目錄都是編入的模組；manifest 的 playbook 與 allowlist 一致；每個 Workflow kind 與 completion 型別只屬於 core 或一個模組 |

**helper**（`content_test.go`、`module_roles_test.go`）：`moduleDirs`、`roleDir(name)`（找出擁有該 role 的模組）、
`shippedFile(rel)`（`deploy-slurm.yml` 或 `roles/<role>/<file>` 對應到實際檔案）、`readShippedManifest`、
`roleClosure`、`readYAML`；`slurm_job_hooks_playbook_test.go` 的 `slurmPlaybookFile` 以 `shippedFile` 讀檔，其他測試
也在用。`artifact_sources_role_test.go` 提供執行 role 的 helper：`newRoleRun(t, role, gatherFacts)` 以該 role 所在模組與
`shared/` 的 roles path 組出一個只跑它的 play（沒有 `ansible-playbook` 時 skip），`run(vars)` 以 extra vars 在 localhost
執行、不 become，回傳輸出與 recap 的 changed、failed；role 的其他 task 檔需要 root 或 systemd 時，
`newRoleTasksRun(t, role, tasksFrom, gatherFacts)` 以 `include_role` 加 `tasks_from` 只跑其中一個 task 檔（仍載入 role 的
defaults）；`writeScratch`、`readScratch` 讀寫暫存目錄的檔案。同一個檔案的
`testLocalRepository()` 是 api-server 在 Local Repository 模式交出的 `swallow_artifacts`，`testUpstreams(keys, fingerprint)`
是上游模式的（docker-ce 與 grafana 的 deb 來源，金鑰在 `keys`）。

**新內容要加的測試**：為 playbook 依賴的契約寫一個 `<topic>_playbook_test.go`，斷言它引用了必要的 trusted var、play
的順序，以及禁止的操作（例如不得 reboot 記憶體根目錄的主機）；範例是 `k0s_topology_playbook_test.go`。以
`shippedFile` 與 `roleDir` 指稱檔案，不要寫死模組路徑。例外：`internal/core/platform/domain/gpu_operator_test.go` 寫死了
AMD 模組的 chart 路徑，搬移該 role 時要一併修改。

role 的行為要實際執行才看得出來時（改寫主機設定、冪等、下載與驗證），以 `newRoleRun` 在 localhost 執行它，並把 role 的路徑
變數指到暫存目錄（`artifact_sources` 是 `artifact_sources_root`）。範例：

- `artifact_sources_role_test.go`，兩種模式都實際執行，只在 Debian 系列的主機執行（上游模式另外需要 gpg）：
  - `TestArtifactSourcesPointsTheHostAtTheLocalRepository`：從 Local Repository 取得時改寫 apt、pip、containerd 與
    Docker 的設定（Docker 保留 `daemon.json` 的其他設定），第二次執行沒有變更；沒有 `swallow_artifacts` 或 `source`
    不認得時在碰主機之前就停下。
  - `TestArtifactSourcesAddsTheDeclaredUpstreams`：從上游取得時以 httptest 提供的金鑰加入宣告的來源，主機自己已設定的
    URI 留給主機的檔案，第二次執行沒有變更；金鑰不是宣告的那一把或 gpg 讀不了時，在 apt 信任它之前停下。
  - `TestArtifactSourcesSwitchesModesBothWays`：切到上游時移除為 Local Repository 寫的設定，切回時主機與第一次執行
    後完全相同，兩種模式都冪等。
- `k0s_binary_role_test.go`：`k0s_binary` 從 httptest server 扮演的來源下載，只把回報固定版本的 binary 當成已安裝，
  所以會取代上次中斷留下的檔案，來源沒有的版本在下載前就失敗。
- `victoriametrics_role_test.go`：以 `newRoleTasksRun` 只跑 `victoriametrics` 的 `release` 與 `preflight`（`service` 需要
  systemd）：release archive 的下載、驗證與解壓，重跑沒有變更，舊版被移除；不接受的 listen 與 retention 在碰主機之前停下。
- `python_toolchain_role_test.go`：`python_toolchain` 依序使用第一個能用的 CPython 3.12 與 uv（跳過不存在的路徑、其他版本
  與不是 uv 的程式），都有時不改變主機；缺 uv 時從 httptest server 下載宣告的 release 並連結，第二次執行沒有變更；
  沒有 CPython 3.12 套件的 OS 以 `[python312_unavailable]` 停下。
- `omnistat_converge_role_test.go`：只執行 `omnistat` 的 `tasks_from: converge`（不需要 root）。已是預期 Omnistat 的環境
  （模擬預先烘進的 image）不帶入工具鏈、不下載，第二次執行沒有變更；沒有環境、版本不同、少一個 patch、以 pip 建的舊環境
  都以假的 uv 重建，斷言 uv 的每個呼叫（`--require-hashes --no-build`、`--no-deps --no-build-isolation`、沒有 index），
  重建後的環境通過探測，第二次執行不再呼叫 uv。

端到端的驗證在 lab 以真機進行，規則（可用哪些機器、事先確認什麼）見 skill
[`skills/lab-verification/SKILL.md`](../../skills/lab-verification/SKILL.md)。

## 7. 搬移或改名內容

- 改 manifest 的 `path`；playbook 的 `name` 已被 Site 的 `playbookMappings` 與已受理的 Workflow 引用，改名時保留
  舊名當 alias（§2）。
- 測試以 helper 找檔案，搬移通常不必改；寫死路徑的例外見 §6。
- 同步文件中的位置表：[platform-deployment.md](platform-deployment.md) §7、
  [software-deployment.md](software-deployment.md) §7、[installation.md](installation.md)（`configure-ntp`）。
- `deploy/production/swallowctl` 的 doctor 執行 `shared/playbooks/diagnostic-ping.yml`，搬移它時一併修改。

## 8. 提交前的檢查清單

每次修改：

- [ ] 內容放在正確的模組目錄；共用的內容放在 `shared/`（§1）。
- [ ] role 只在自己模組或 `shared/` 解析，名稱與檔名唯一（§3）。
- [ ] 遵守 §4 的規則；改套件的 playbook 取得與釋放 hold，套件 task 會重試（§5）。
- [ ] 結構測試通過，並為新內容或改變的行為寫了契約測試（§6）。
- [ ] 行為改變時，描述它的規格已更新（例如 Kubernetes 的 OS 差異在 platform-deployment §2.3）。

新模組，或改變模組的宣告時：

- [ ] `module.json` 列出模組擁有的 playbook、Workflow kind、completion 與 route，模組已加入 `compiledModules`（§1.1）。
- [ ] 模組定義 spec 時，`spec` 屬於 schema 子集，Software 模組的屬性等於 catalog 的 `SpecFields`（§1.1）。
- [ ] 新的 Job 都在 `workflowJobLabels` 有顯示名稱（§1.1；Software 的 `configure-<kind>` 以 catalog 的 label 命名，不必登錄）。
- [ ] dashboard 的畫面已決定：專屬畫面登錄在 `src/modules/registry.ts`，或依 `spec` 產生（§1.2）。
- [ ] 新模組的 `api-server/modules/<種類>/<id>/` 與 dashboard 的模組目錄（有的話；Software 模組沒有）已在 `.github/CODEOWNERS` 登錄負責人；
  `internal/app` 的測試檢查每個編入的模組都有一行。
- [ ] 新 playbook 已登錄在 `manifest.json`；改名保留了舊名（§2、§7）。
- [ ] 搬移或改名後，位置表與相關規格已更新（§7）。
- [ ] 模組層級的檢查見 [core-and-extensions.md](core-and-extensions.md) §11.2。
