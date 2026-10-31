# OS Provisioning 設計與規範

本文件是 swallow **OS provisioning** 的現行規格：swallow 如何透過 provisioner（目前是 MAAS）替
Server 部署作業系統，以及在這件事上哪些事實由 provisioner 擁有、哪些由 swallow 擁有。它收斂了
[decision 009](../decisions/009-deployment-template-ownership.md)、
[012](../decisions/012-provider-network-configuration.md)、
[018](../decisions/018-automatic-addressing-provider-auto-assign.md)、
[027](../decisions/027-os-image-upload.md)、
[029](../decisions/029-infrastructure-zone-pool-ownership.md)、
[033](../decisions/033-provider-recovery-policy.md)、
[035](../decisions/035-os-image-verification-and-deploy-target.md)、
[036](../decisions/036-provisioning-lifecycle-integrity.md)、
[046](../decisions/046-os-deployment-progress-and-stall.md)、
[056](../decisions/056-provisioner-http-proxy.md)、
[058](../decisions/058-deployment-dhcp-and-subnet-dns.md) 與
[059](../decisions/059-retry-recovers-failed-deployment.md) 的結果。ADR 只保留當時的脈絡與否決方案；
行為改變時更新本文件，決策本身被推翻時才新增 ADR。

> **本文件的定位**：術語以 root glossary 為準，路由、欄位、status code 與錯誤格式以 `api-server`
> 擁有的 API contract 為準；本文件不重抄欄位表，只描述跨 contract 的行為、規則與不變式。修訂鏈
> （定址 012→018→058、回復 033→036→059）只寫目前有效的規則。實作分層由
> [`api-server`](../../api-server/AGENTS.md) 的文件負責，§12 只提供導航用的程式位置對照。

---

## 1. 目的與範圍

範圍內：

- provisioner 與 swallow 的分工（§2）。
- [Deployment Template](glossaries/terms/deployment-template.md)（§3）。
- [OS Image](glossaries/terms/os-image.md) 的目錄、上傳與刪除（§4），以及驗證與
  [Deploy Target](glossaries/terms/deploy-target.md)（§5）。
- 部署網路意圖（`automatic`／`dhcp`／`static`）與 [Subnet DNS](glossaries/terms/subnet-dns.md)（§6）。
- [Provisioner HTTP Proxy](glossaries/terms/provisioner-http-proxy.md)（§7）。
- [Zone](glossaries/terms/zone.md) 與 [Pool](glossaries/terms/pool.md)（§8）。
- OS 部署的受理與執行（§9），以及部署進度與 provider-stage stall（§10）。
- [Provider Recovery](glossaries/terms/provider-recovery.md)：Release、Recover、Rescue、mark-*、重試失敗的
  部署與生命週期完整性（§11）。

範圍外（由其他文件負責，本文件只在接觸點連過去）：

- Server Enrollment、hardware inspection、Boot Media（含部署開機的補救）、Boot ISO、Power Configuration，以及
  磁碟版面的 inspection-required guard → [server-enrollment-and-boot-media.md](server-enrollment-and-boot-media.md)。
- Workflow 的 Task 重試、`requires_attention` 停等、cancel（含回收被中斷的部署 `return-canceled-deployment`）、
  rerun、`cancelActiveWorkflows` 取代佔用中的工作與 resource lease → [workflow-execution.md](workflow-execution.md)。
- Platform 部署的 `ensure-os` Job 與 [Platform Claim](glossaries/terms/platform-claim.md) →
  [platform-deployment.md](platform-deployment.md)。
- [Deployment Key](glossaries/terms/deployment-key.md)、[SSH Key](glossaries/terms/ssh-key.md) 註冊與
  [Server Default User](glossaries/terms/server-default-user.md) 的規則，以及部署後的時間同步
  （[decision 065](../decisions/065-control-plane-ntp-and-post-provision-time-sync.md)）。

---

## 2. 心智模型：provisioner 擁有什麼、swallow 擁有什麼

OS Provisioning bounded context 的參與者、上下游關係與翻譯方式見
[OS provisioning context map](context-maps/os-provisioning.md)。決定 ownership 的上位規則不在此重述：

- 哪些事實由誰擁有、owned data 與 mirrored fact 的區分：[ADR 001](../decisions/001-system-ownership-boundaries.md)。
- provider-owned fact 上的 swallow overlay：[ADR 025](../decisions/025-provider-data-overlay.md)；
  「provisioner 可能擁有」的事實一律 capability-first with a swallow-owned fallback：
  [ADR 031](../decisions/031-provider-capability-first-with-swallow-owned-fallback.md)（摘要在
  [architecture-spec.md](architecture-spec.md)）。
- Server 身分與三個狀態軸：[ADR 002](../decisions/002-server-identity.md)、glossary
  [Server Status](glossaries/terms/server-status.md)；`provisioning.state` 的值集由 swallow 擁有、adapter 只做
  對應：[ADR 048](../decisions/048-os-provisioning-generic-states.md)、glossary
  [OS Provisioning State](glossaries/terms/os-provisioning-state.md)。
- provider-owned 的 Server Lock 是所有 mutation 的共同閘門：[ADR 013](../decisions/013-server-lock-protection.md)、
  glossary [Server Lock](glossaries/terms/server-lock.md)。

套用到本文件的主題：

| 事實 | Owner | swallow 如何存取 | swallow 儲存 |
| --- | --- | --- | --- |
| Machine inventory、OS 部署的執行與 provider 狀態 | provisioner | reconcile 鏡射到 Server 的 `provisioning` 軸 | mirrored projection |
| OS Image artifact 與 catalog | provisioner | 每次 live read；上傳／刪除由 swallow 驅動（§4） | 不存 |
| OS Image 的顯示名稱、OS、release、tags、default user | swallow | [Provider Data Overlay](glossaries/terms/provider-data-overlay.md)，讀時合併 | `os_image_overlays` |
| OS Image 驗證 | swallow | 每個 Deploy Target 的證據（§5） | `os_image_verifications` |
| Deployment Template | swallow | 完整 CRUD（§3） | owned collection |
| 部署網路意圖 | swallow | 部署時、或修改 Ready Server 的網路時套用（§6） | Template 與 Workflow 的 intent snapshot |
| NIC、subnet、link、IP 分配 | provisioner | live read，每次 mutation 前重新驗證 | 不存；provider id 只當 live reference |
| Subnet DNS、Provisioner HTTP Proxy | provisioner | live read + write-through（§6、§7） | 不存 |
| Zone 與 Pool 的 catalog | swallow | 完整 CRUD，provisioner 有能力時同步實現（§8） | owned collection |
| Server 的 observed zone／pool | provisioner | reconcile 鏡射 | mirrored |
| Server 的手動 tag | swallow | Server 的 spec；Server controller 收斂、resync 修正 drift（[desired-state-model.md](desired-state-model.md) §4.4） | `servers` 的 `spec.tags` |
| Server 的自動 tag（provisioner 自己算的，例如 MAAS 帶 definition 的 tag） | provisioner | reconcile 鏡射，只觀察 | `servers` 的 `observed.automaticTags` |
| Provider recovery policy | swallow | 單一 domain 矩陣（§11） | — |
| Release 後的 static link 清理 | swallow | [Provisioning Task](glossaries/terms/provisioning-task.md)（§6） | owned coordination state |

兩條貫穿全文的不變式：

1. **Provider 詞彙留在 adapter**：MAAS 的 `AUTO`、`LINK_UP`、link id、`get_config`／`set_config`、
   `dns_servers`、`commission` 等都只存在於 MAAS adapter；swallow 的 domain、API 與 dashboard 只用 swallow
   詞彙。adapter 是 anticorruption layer，不是 conformist。
2. **缺少操作型能力就拒絕**：可選的 provider 能力以 `ProviderCapabilities` 宣告、以 optional interface（type
   assertion）取得。provisioner 沒有該能力時，請求以 `400 validation_error` 拒絕，不默默成功，也不保留
   provider 的預設值。

---

## 3. Deployment Template

[Deployment Template](glossaries/terms/deployment-template.md) 是 swallow 擁有、綁定單一 provisioner
Integration 的可重用 OS 部署意圖。API 見
[provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md) 的
Deployment Templates。

**現行行為**

- 內容：名稱、描述、OS Image 參照（provider 接受的 opaque id）、Deploy Target（儲存為 `ephemeral` 布林）、
  網路意圖（mode、subnet、default gateway），以及選用的 cloud-init user data。
- 不包含：provider NIC id、某台 target 專屬的 static IP、套件、script、playbook，也沒有執行狀態。NIC 與
  static IP 在每次部署時依 target 指定。
- cloud-init 加密保存且 write-only：只能整筆取代或清除，回應只帶 `hasUserData`。部署時 user data 可
  `inherit`（沿用 template）、`replace`（只用於該次）或 `omit`。
- 建立 template、修改其 image 或網路、以及用它部署時，都以 provider 的 live catalog 驗證 image。image 從
  catalog 消失時 template 仍在、但無法用來部署；UI 必須顯示這個狀態，而不是把 image metadata 複製進 template。
- 名稱在同一 Integration 內 case-insensitive 唯一；Integration 仍被 template 參照時不能刪除（`409`）。
- 沒有 `network` 的舊紀錄讀為 `automatic`；2026-09-06 前存成 `dhcp` 的 template 現在代表 DHCP（§6）。

**設計理由**：沒有任何外部系統擁有「某個 Integration 上可重用的 image、部署模式與 cloud-init 組合」，所以由
swallow 擁有；可變的 automation 內容屬於 release 內的 playbook，不屬於資料庫（退場的 `ProvisioningProfile`
正是越過這條線）。image 識別與能力只在單一 provisioner Integration 內成立，所以 template 不做成 fleet-wide。
見 [decision 009](../decisions/009-deployment-template-ownership.md)。

---

## 4. OS Image：目錄、上傳與刪除

API 見 [provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md) 的
OS Image Catalog、OS Image Upload、OS Image Overlay。

### 4.1 目錄與 overlay

- 目錄每次依 `integrationId` live read，再合併 swallow 的 overlay（effective 值 = `overlay ?? provider`，回應同時
  帶 `provider*` 原值）。`id` 與 `architecture` 是可部署 artifact 的身分，不可覆寫。
- 目錄只含 provisioner 接受部署的 resource（MAAS 會排除同一 API 回傳的 PXE 與 bootloader artifact）。
  `sizeBytes` 等 artifact metadata 屬於 provider，不進 overlay。
- overlay 的欄位與 default user 規則見 glossary [OS Image](glossaries/terms/os-image.md) 與
  [Provider Data Overlay](glossaries/terms/provider-data-overlay.md)。
- **唯一被鏡射的衍生值**：reconcile 每輪對每個 Integration 讀一次 catalog，把已部署 machine 的
  `osSystem`／`distroSeries` 解析成 image 的 effective 名稱，鏡射到 Server 的
  `provisioning.deployedImageName`（帶 `observedAt`），讓 fleet 列表不必逐筆 fan-out。overlay 改名後另以
  best-effort 立即傳播（[ADR 025](../decisions/025-provider-data-overlay.md)）。catalog 本身不鏡射。

### 4.2 上傳

- 上傳是**可選的 provider 能力**，與刪除對稱（`ImageUpload`／`OSImageUploader` 對應
  `ImageRemoval`／`OSImageRemover`）。provisioner 沒有能力時以 `400 validation_error` 拒絕。
- 上傳的 image 與 synced image 一樣由 provider 擁有。它是否算 *custom* image 由 provider adapter 決定（MAAS 回報
  `osSystem: custom`），呼叫端不送、也不推斷 custom 旗標。
- bytes 的路徑是 browser → api-server → provider：`multipart/form-data` 的檔案部分先 spool 到暫存檔，只為算出
  provider 事先要求的 `size` 與 `sha256`，再分段串流給 provider，完成後刪除暫存檔。swallow 不把整個 artifact
  放進記憶體，也不保留副本。
- 一次上傳是單一同步的 provider 呼叫，不是 Workflow；不受 catalog read 的短 timeout 限制。api-server 的
  request body 上限由 `SWALLOW_API_IMAGE_UPLOAD_MAX_BYTES` 設定（預設 64 GiB）。
- 選用的 `defaultUser` 在 provider 接受上傳後寫入 overlay；寫入失敗時 image 已存在，可之後再以 overlay 設定。
- MAAS adapter：先建立 boot resource，再分段上傳內容；架構展開成 `<arch>/generic`，放進 custom namespace；
  未指定 `filetype` 時用 MAAS 預設（`tgz`）。

### 4.3 刪除

- 只有操作者上傳的 custom image 可刪；synced release 會被 provider 立即重新同步，provider 拒絕並以 `400`
  帶回 provider 原文。刪除同樣是可選能力。
- 刪除成功後一併刪除該 image 的 overlay 與驗證紀錄。刪除 Integration 時不清理驗證紀錄；孤兒紀錄不會再被合併，
  不影響使用者。

**設計理由**：swallow 可以**驅動** provider 擁有的動作，但不因此擁有 artifact（ADR 001）；custom 與否是
provisioner 的性質，所以由後端決定；在 api-server spool 讓 hash 由 swallow 自己計算並可決定性地分段送出，
不依賴瀏覽器計算多 GB 的 hash。見 [decision 027](../decisions/027-os-image-upload.md)。

---

## 5. OS Image 驗證與 Deploy Target

### 5.1 Deploy Target

- [Deploy Target](glossaries/terms/deploy-target.md) `disk`｜`ram` 是 operator 面對的部署模式詞彙。`ram` 就是
  既有的 `ephemeral`，兩者在每個邊界一對一映射（`disk ↔ ephemeral=false`、`ram ↔ ephemeral=true`）。
- 部署請求接受 `deployTarget`（首選）或已 deprecated 的 `ephemeral`；兩者都送時 `deployTarget` 優先，未知值是
  `400`。wire、BSON、durable snapshot、MAAS adapter 與 k0s var 的 `ephemeral` 欄位都不改名。
- `ram` 部署需要 provider 的 `EphemeralDeploy` 能力，否則受理時拒絕。

### 5.2 驗證紀錄

- 驗證是 swallow 擁有的事實（swallow 真的跑過一次部署而且成功），不是 provider 能力。它沿用 overlay 的身分鍵
  `(integrationId, imageId, architecture)`，但存在獨立的 `os_image_verifications` collection：overlay 沒有任何
  覆寫時會刪除自己，放在一起會連帶抹掉驗證。
- 每個 Deploy Target 只記最新一次結果：成功（`verifiedAt`、`operationId`、`serverId`）或失敗（`failedAt`、
  `operationId`、`serverId`，可帶簡短 `reason`），兩者互斥，記下一種就清掉另一種。catalog 以 `verifiedDeployTargets` 與 `failedDeployTargets` 投影。

### 5.3 部署閘門

- *custom* image（`providerOsSystem: custom`）沒有在所要求的 Deploy Target 通過驗證時，部署在受理時以 `409`
  拒絕並帶 swallow 的理由。`failed` 與從未驗證一樣擋；`failedDeployTargets` 只影響顯示。
- synced provider image 視為 provider 已信任，永不受閘門限制。
- 閘門位於共用的 deploy resolve，所以涵蓋 `POST /provisioning/deployments`、`/deployment-operations` 與 Platform
  部署；已 deprecated 的單台 `POST /servers/{id}/deploy` 不套用。

### 5.4 `verify-os-image` Workflow

`POST /provisioning/image-verifications` 以一台 operator 選的 Server 做一次真實部署來證明 image。受理時要求
Server 存在、`ready`、未鎖定、屬於 image 的 Integration、架構相符；證明用的部署走一般的 deploy resolve（image
已完整 staged、`ram` 需要能力），只以 `VerificationRun` 略過 §5.3 的閘門。

| Task ID | Kind | Runner | 何時執行 |
| --- | --- | --- | --- |
| `ensure-boot-media-<serverId>`（僅 Boot Media 啟用的 Server） | `ensure-boot-media` | `internal` | 最先 |
| `provision-<serverId>` | `provision-os` | `provisioner` | 證明用部署 |
| `record-verification` | `record-image-verification` | `internal` | provision 成功 |
| `record-verification-failure` | `record-image-verification-failure` | `internal` | provision 失敗（`ContinueOn: failed`） |
| `return-<serverId>`（`keepServer` 為 `false` 時） | `recover-server` | `provisioner` | 上面兩者之一成功、另一個 skipped（`ContinueOn: succeeded, skipped`） |

規則：

- **借用與歸還**：預設不論證明成功或失敗，Server 都會以 `recover-server` 歸還到 `ready`；`keepServer: true` 時
  省略歸還 Task，成功時留在 deployed、失敗時留在失敗狀態，由 operator 自行 Recover。
- 在 `VerificationRun` 下，證明用部署的可重試失敗或 `requires_attention`（例如 SSH 不通、stage stall）一律改為
  不可重試的 `failed`，讓 Workflow 走到終態並執行歸還，而不是停在 `requires_attention` 佔住 Server；只有從未碰到
  machine 的 `provider_unavailable` 仍可重試。結果是 Workflow 以 `partially_succeeded` 結束，provider 的原因留在
  失敗的 `provision-os` Task 上。
- 取消 `verify-os-image` 時，Workflow 以 best-effort 執行自己的 `recover-server` Task（上限 30 分鐘、忽略錯誤），
  而不是一般部署的 `return-canceled-deployment`。
- 驗證不設定時間（不加 `configure-ntp-<serverId>`），因為 Server 隨即歸還。
- OS Images 列表只把仍在進行（pending／running／waiting）的 verify Workflow 顯示為 verifying；
  `requires_attention` 另外顯示為需要處理，`failed`／`canceled` 不算 verifying。

**設計理由**：provider 不保證上傳的 image 能開機與安裝（Rocky image 曾在 RAM 成功、在 disk 失敗），所以信任必須
按 Deploy Target 分開；驗證是 swallow 的觀察，因此是 owned data。驗證借用的是一台備用機，契約就是要歸還；一般
部署失敗則保留現場給 operator（§11.4）。見 [decision 035](../decisions/035-os-image-verification-and-deploy-target.md)
與 [decision 036](../decisions/036-provisioning-lifecycle-integrity.md)。

---

## 6. 部署網路意圖與 Subnet DNS

swallow 擁有部署網路意圖的詞彙、預設值、驗證與流程；provider adapter 把它翻成自己的 API，並把 provider 自己的
舊狀態只當作觀察值呈現。值集與意義見 glossary [Network Configuration](glossaries/terms/network-configuration.md)；
API 見 [provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md) 的
Network Inspection 與 Multi-Server Deployment。

### 6.1 三種模式

| Mode | 意圖 | MAAS 實現 | 位址與 DNS |
| --- | --- | --- | --- |
| `automatic`（預設） | provisioner 分配並記錄一個穩定位址 | NIC link 為 `AUTO`：從 subnet 的非 dynamic 空間分配 static IP、MAAS 記錄、寫進部署後的 netplan | subnet 需有 static 空間；DNS 用 Subnet DNS，空時用 provisioner 自己的 DNS |
| `dhcp` | 網路自己的 DHCP server 指派位址、gateway、DNS | NIC link 為 `DHCP` | MAAS 只記錄它觀察到的位址；DNS 來自 DHCP server |
| `static` | operator 為每台指定位址 | NIC link 為 `STATIC` | 每台一個 IPv4，須落在所選 subnet、同批內唯一；DNS 同 `automatic` |

規則：

- 空值視為 `automatic`；未知值是 `400`；keep-current 不是有效意圖。`dhcp` 是獨立模式，不是 `automatic` 的別名。
- `defaultGateway` 與每台的 `ipAddress` 只對 `static` 有效。template 可帶 mode、subnet、gateway，但不帶 NIC id
  或 static IP。
- 每台 target 預設用 boot NIC（只有一張 NIC 時用它），可覆寫 `interfaceId` 與 `subnetId`；subnet 依序取：該台指定、
  整批共用、建議值（NIC 唯一已連結的 subnet，或唯一的 managed subnet）。所選 subnet 上有多個 link 時拒絕，請
  operator 先在 Server Network 解除其中一個。
- 在 site DHCP 的網路上用 `dhcp` 部署時，MAAS 收不到該 machine 的 lease：swallow 顯示的是 MAAS 最後觀察到的
  介面位址，部署後以 SSH 進行的 automation 依賴它。

### 6.2 Network inspection 的建議

`POST /provisioning/networks/inspect`（1–100 台，唯讀）回傳每台的 live 網路觀察與一個**非權威**的建議：

1. boot NIC 上恰好一個明確的 Static link → 建議 `static`，沿用其 subnet、IP 與 gateway。多個 Static link 視為
   模糊，需要 operator 輸入。
2. 否則取唯一已連結的 subnet 或唯一 compatible managed subnet；該 subnet 沒有 provisioner DHCP（`provisionerDhcp`
   為 false，MAAS：VLAN 的 DHCP 關閉）時建議 `dhcp`，否則 `automatic`。
3. 有多個 compatible subnet 時回報 `network_selection_required` 並建議 `automatic`，不默默替 operator 選。

觀察到的 NIC 狀態（`dhcp`、`static`、`link_only`、`unconfigured`、`provider_managed`、`unknown`）只供顯示；MAAS 的
`AUTO` 觀察為 `provider_managed`、`LINK_UP` 為 `link_only`，provider 原值只作診斷，不可當作可寫入的意圖送回。

### 6.3 何時套用、adapter 的義務

- swallow 只在明確的 operator 邊界套用網路意圖：送出 OS 部署時，或對一台 present、未鎖定、`ready` 的 Server 編輯
  NIC link（`dhcp`｜`static`｜`link_only`，見
  [server-detail-actions.md](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md)
  的 Structured Network Configuration）。已部署的主機不做持續的網路 reconcile。
- 部署受理時，先對每台 target 檢查 live 網路能力、NIC、subnet、既有 link 與 static 位址，任何問題都在第一次寫入前
  拒絕整批。無法實現意圖的 adapter 必須在寫入前拒絕，不得默默保留 provider 預設值。
- adapter 永不使用 provider 的 force 選項，每次 mutation 後重新讀取 Machine 驗證結果；替換 link 時只解除被指定的
  那一個。
- Release 時若要求 `unbindStaticIPs`，swallow 在送出 Release 前先保存一筆 `release_network_cleanup`
  [Provisioning Task](glossaries/terms/provisioning-task.md)：等 Server 回到 Ready，只移除事前快照中、之後未被改動
  的 static link（DHCP、provider-managed、link-only 與被改過的 link 都保留）。清理失敗可重試，重試不會再次 Release。
  它不是 Ansible Workflow Task。

### 6.4 Subnet DNS

- [Subnet DNS](glossaries/terms/subnet-dns.md) 是 provisioner subnet 的 DNS server 清單（MAAS：subnet 的
  `dns_servers`），由 provisioner 擁有。swallow 以 provider 能力 `SubnetDNS` live read 並 write-through，不儲存，
  也不放進 Integration `settings`。API 見
  [provisioner-subnet-dns.md](../../api-server/docs/development/api-contracts/api-server/provisioner-subnet-dns.md)。
- 清單最多 8 個不重複的 IP（不得帶 zone），`[]` 代表清除。它只影響之後由 provisioner 開始的 `automatic` 與 `static`
  部署；`dhcp` 部署從 DHCP server 取得 DNS；已部署的主機不變。
- 寫入需要 Integration 背後的 MAAS 帳號是 administrator，否則回 `503 provider_unavailable`。
- swallow 仍不建模 DNS zone、record、resolver 或自己的 subnet（ADR 001）；它只透過 provisioner 編輯 provisioner 擁有
  的一個屬性。目前 CLI 沒有對應指令。

**設計理由**：「自動給位址」是 swallow 的意圖，不是協定；MAAS 只在觀察到 live lease 時才知道 DHCP 介面的位址，
而 subnet 的 dynamic range 依慣例是 enlistment／commissioning 用的短期池，所以 `automatic` 改由 `AUTO` 實現，位址
穩定且 provider 一定知道。但在 DHCP 由 site 而非 provisioner 提供的網路上（例如經 Boot ISO 開機的 Server），`AUTO`
不知道 site DHCP 的位址池，兩個分配者可能發出同一個位址，也會忽略 site 的 gateway 與 DNS，因此 `dhcp` 重新成為一個
明確、可被 operator 覆寫的模式，由 network inspection 建議。DNS 跟著 link mode 走，所以 per-subnet 的 DNS 設定沿用
provisioner 自己的設定而不由 swallow 產生 cloud-init。見
[decision 012](../decisions/012-provider-network-configuration.md)、
[decision 018](../decisions/018-automatic-addressing-provider-auto-assign.md)、
[decision 058](../decisions/058-deployment-dhcp-and-subnet-dns.md)。

---

## 7. Provisioner HTTP Proxy

- [Provisioner HTTP Proxy](glossaries/terms/provisioner-http-proxy.md) 是 provisioner 交給它部署的 machine 用來
  抓套件的 proxy（也是 provisioner 自己下載 boot image 用的 proxy），由 provisioner 擁有。swallow 以 provider 能力
  `HTTPProxy`，在 Integration 範圍的獨立 route 上 live read 並 write-through，不儲存。API 見
  [provisioner-http-proxy.md](../../api-server/docs/development/api-contracts/api-server/provisioner-http-proxy.md)。
- 它不是 Integration 的 `settings`：`settings` 描述 swallow 如何連到 provisioner（例如 `timeout`、
  `insecureSkipVerify`）與 swallow 自己的政策（例如 `autoInspect`），MAAS 看不到它們。
- 封閉的 mode 值集 `none`｜`builtin`｜`external`｜`peer`（意義見 glossary）。寫入一律整組取代 mode 與 URL，所以
  寫入後的讀取回報剛寫入的值；讀取以 MAAS 決定 machine 用哪個 proxy 的規則推導 mode，在 swallow 之外改過的設定
  依其效果回報，讀到的值可以原樣寫回。
- URL 必須是 `http`／`https`、只有 host 與選用 port；帶帳密、query、fragment 或路徑都拒絕，因為這個設定可被讀回，
  swallow 不回傳 secret。
- MAAS 以三個 region 設定（`enable_http_proxy`、`use_peer_proxy`、`http_proxy`）實現，逐一寫入、不是原子操作：
  中途失敗會留下新舊混合的狀態，下次讀取如實回報，重送同一次寫入即可收斂。寫入需要 MAAS administrator 帳號；
  不測試 proxy 是否可達；只影響之後開始的部署。
- Dashboard 的入口是 Integration 設定頁的 HTTP proxy 分頁；目前 CLI 沒有對應指令。

**設計理由**：把 proxy 存在 Integration `settings` 再推送，會產生一份在有人直接改 MAAS 時漂移的副本，而且讓 swallow
紀錄的 PATCH 帶上無法回滾的 provider 副作用；直接暴露三個 MAAS 旗標則會讓多種等價組合與 MAAS 詞彙進入 API。見
[decision 056](../decisions/056-provisioner-http-proxy.md)。

---

## 8. Zone 與 Pool

[Zone](glossaries/terms/zone.md) 與 [Pool](glossaries/terms/pool.md) 是 swallow 擁有、Site 範圍的受管分組，由
`infrastructure` feature 提供完整 CRUD 與 Server placement。API 見
[infrastructure.md](../../api-server/docs/development/api-contracts/api-server/infrastructure.md)。

- catalog 由 swallow 儲存，名稱在 Site 內唯一，不依賴 provisioner 保存它們。`providerRealized` 只反映最近一次寫入
  是否傳到具 grouping 能力的 provisioner；`false` 代表紀錄只存在於 swallow，不是錯誤。
- **實現在 provider**（Site 的 provisioner 具 `Grouping` 能力時，MAAS 為 physical zone 與 resource pool）：
  - 建立是 ensure 語意：provider 已有同名 group（MAAS 內建 `default`）視為已滿足。
  - 改名同步到 provider。
  - 寫入順序是先 provider、後 swallow：provider 拒絕時不留下 swallow 紀錄；刪除時 provider 拒絕（例如 group 仍有
    machine）則整個刪除中止。
  - Site 的 provisioner 沒有 grouping 能力時，catalog 寫入只存在 swallow，不算錯誤。
- **Server placement**：`PUT /servers/{id}/placement` 把 Server 指派到 Zone 與／或 Pool。被指派的 Zone／Pool 必須與
  Server 同 Site；指派會寫入該 Server 的 provisioner。因為 Server 一定來自某個 provisioner，provisioner 沒有 grouping
  能力時回 `validation_error`，不默默略過。
- Server 上的 observed zone／pool 是 provider 的鏡射，由 reconcile 更新；它是指派的觀察結果，不是 swallow 的 catalog。
- swallow 不匯入、也不 reconcile provider 的 group 集合，因此直接在 MAAS 建立的 group 或實現失敗會造成漂移；目前
  接受這個代價，指派失敗會回報給 operator。
- `infrastructure` feature 只依賴 `provisioning` 的 domain port（`ProviderFactory`）與自己的 `GroupingRealizer` port，
  MAAS 詞彙只在 provisioning adapter。

**設計理由**：operator 需要在 swallow 管理分組並搬動 Server，而且不論 provisioner 是否支援；原本的 opaque provider
label 無法滿足，所以 Zone／Pool 成為 swallow 的概念，provider 有能力時再實現。它也是
[ADR 031](../decisions/031-provider-capability-first-with-swallow-owned-fallback.md) 規則的 provider-can-write 情形。
見 [decision 029](../decisions/029-infrastructure-zone-pool-ownership.md)。

---

## 9. OS 部署的受理與執行

[OS Deployment](glossaries/terms/os-deployment.md) 的入口（API 見
[provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md)）：

- `POST /provisioning/deployment-operations`：建立 `deploy-os` Workflow，是 dashboard 使用的路徑。
- `POST /provisioning/deployments`（deprecated）：直接分批送出，最多四個並行，回傳受理報告而不是 durable job，
  不回滾。`POST /servers/{id}/deploy`（deprecated）：單台，不套用 §5.3 的驗證閘門。
- Platform 部署的 `provision_os`：在 `ensure-os` Job 中使用同一個 `provision-os` Task，見
  [platform-deployment.md](platform-deployment.md)。
- `POST /provisioning/deployments/preflight`：唯讀的 target 檢查（不檢查 image 與網路）。

### 9.1 受理檢查

所有入口共用同一個 deploy resolve；任何一項失敗都在第一次 provider 寫入前拒絕整個請求：

1. Installation 已有 [Deployment Key](glossaries/terms/deployment-key.md)，否則 `409`。
2. 1–100 台不重複的 target：都存在、present、`ready`、未鎖定（lock 以 live read 確認，讀不到時 `503`、fail
   closed）、屬於同一 Integration，用 template 時也須與 template 的 Integration 相同。
3. **磁碟**部署的 target 若需要重新 inspection（`provisioning.inspectionRequired`），以 `409` 與
   `inspection_required` 拒絕；`ram` 部署允許。見 [server-enrollment-and-boot-media.md](server-enrollment-and-boot-media.md)。
4. image 存在於 live catalog；`ram` 需要 `EphemeralDeploy` 能力；image 已被 provisioner 完整 staged；Server 與
   image 的架構已知不相符時拒絕（Server 尚未回報架構則交給 provider）；§5.3 的驗證閘門。
5. §6 的網路檢查與解析。

durable 路徑另外要求 installation 已設定 NTP server（否則 `409`），且 Site 的 automation 可執行（否則 `503`），因為
每台都會加上 `configure-ntp-<serverId>`（[decision 065](../decisions/065-control-plane-ntp-and-post-provision-time-sync.md)）。
`provision-os` 執行時以凍結的意圖重跑這組檢查：swallow 自己的拒絕（例如 target 不是 ready、image 未 staged 或未驗證、
網路衝突）成為 §9.3 的 `deployment_preflight_rejected`，受理後 Deployment Key 消失則是可重試的 `deployment_key_missing`。
通過檢查後、第一次網路或部署寫入前，swallow 確保 provisioner 持有 swallow 的 SSH Key；失敗時 `provision-os` 以可重試的
`ssh_key_registration_failed` 停下。真正送出部署時才清除 Server 的 Server Default User。

### 9.2 Workflow 的 Task 組成

| Workflow kind | Task ID | Kind | Runner | DependsOn |
| --- | --- | --- | --- | --- |
| `deploy-os` | `ensure-boot-media-<serverId>`（僅 Boot Media 啟用的 Server） | `ensure-boot-media` | `internal` | — |
| `deploy-os` | `provision-<serverId>` | `provision-os` | `provisioner` | 該台的 `ensure-boot-media-<serverId>`（若有） |
| `deploy-os` | `configure-ntp-<serverId>` | `ansible-playbook` | `ansible` | `provision-<serverId>` |
| `release-os` | `release-<serverId>` | `release-os` | `provisioner` | — |
| `recover-server` | `recover-<serverId>` | `recover-server` | `provisioner` | — |
| `verify-os-image` | 見 §5.4 | | | |

Task 的 retry、cancel、rerun 與 lease 語意見 [workflow-execution.md](workflow-execution.md) 與
[workflows.md](../../api-server/docs/development/api-contracts/api-server/workflows.md)。cloud-init 從 intent
snapshot 移除，改存為加密的 opaque secret reference。

### 9.3 `provision-os` 的成功與失敗

- provider 回報 `deployed` 不等於 swallow 完成。成功條件是：provider 以凍結的 image **且相同的 ephemeral 模式**完成
  部署、觀察到 Server 位址、而且 Site 設定的 SSH port（預設 22）在 20 分鐘內可連線。等待 SSH 期間 Server 的
  deployment 為 `verifying`，帶非終態的 `ssh_readiness` 原因。
- Server 的 deployment 結果（`deploying`｜`verifying`｜`succeeded`｜`failed`｜`requires_attention`｜`canceled`）只反映
  `provision-os`，見 glossary [Server](glossaries/terms/server.md)。
- 主要失敗碼：

| Code | 情況 | 可重試 | 明確 Retry 的行為 |
| --- | --- | --- | --- |
| `deployment_failed` | provider 回報 `failed`／`broken`；訊息帶 provider 原因與安裝 log 的錯誤行（[decision 057](../decisions/057-uefi-boot-iso-hand-off.md) 的修訂） | 是 | 先 Recover 再重新部署（§11.3） |
| `deployment_address_unavailable` | 已安裝凍結的 image，但 provider 沒有位址 | 是 | Release、等 Ready、以同一份凍結意圖重新部署 |
| `deployment_ssh_unreachable` | 有位址但 SSH 不通 | 是 | 只觀察，不丟棄已安裝的 OS |
| `deployment_preflight_rejected` | swallow 自己的 preflight 在任何 provider 寫入前拒絕（target 不是 ready、image 未 staged 或未驗證、網路衝突） | 否 | —（Workflow 結束並釋放 Server） |
| `deployment_image_mismatch` | Retry 時 Server 已部署另一個 image | 否 | —（不覆蓋，請先明確 Release） |
| `provider_outcome_unknown` | provider 回應遺失、無法證明結果 | — | `requires_attention`，先觀察，不立即送第二次部署 |

進度相關的失敗碼見 §10。

---

## 10. 部署進度與 provider-stage stall

provider 回報 Deploying 時，`provision-os` 每次觀察都讀 provider 的 machine event stream（每次最多讀最新 20 筆），
讓「慢」與「永遠不會完成」可以分辨。

- **進度投影**：本次 attempt 開始後最新的一筆 event 投影到 Server 的 deployment 軸：`state` 維持 `deploying`、`code`
  留空、`stage` 為 event type 的 snake_case（例如 `configuring_os`）、`statusReason` 形如
  `Provider stage: Configuring OS (since …).`；只有 provider 推進時才改寫。client 把它當進度顯示，不當失敗。
- **stage stall**：25 分鐘沒有更新的 event，Task 變成 `requires_attention`，code 為
  `deployment_provider_stage_stall`，stage 是卡住的 event（本次 attempt 還沒有任何 event 時為 `deployment_progress`），
  訊息指出卡住的階段與起始時間。可重試；swallow 不 abort provider 的部署。
- 進度以 provider event 的時間戳計算，起點是同一 Workflow／Task／attempt 的 deployment `startedAt`，所以 worker 重啟或
  activity retry 不會重置計時；明確的 Task Retry 是新的 attempt，因此有新的計時窗。
- provider 沒有 event stream、或讀取失敗時，永遠不判定 stall。
- 其他 guard 仍然有效：
  - Deploying 滿 10 分鐘且 live power 為 off → `deployment_power_on_timeout`（`requires_attention`，stage
    `deployment_power_on`）；swallow 不自動開機、abort 或重送。stall 窗必須比這個窗長，讓沒開機的 machine 得到更
    明確的診斷。
  - 受理的部署停在 `allocated` 超過 10 分鐘（已保留但沒開始安裝）→ `deployment_reserved_not_started`
    （`requires_attention`）。
  - 整體觀察上限 2 小時 → `provider_observation_timeout`。
- Boot Media Server 的部署開機監看（重新掛載 ISO、經 Redfish 重啟一次）見
  [server-enrollment-and-boot-media.md](server-enrollment-and-boot-media.md)；重啟後 stall 計時從重啟開始。
- 部署前不做 package source 的 preflight：swallow 無法得知每個 image 實際使用的 repository，無法安裝的套件來源仍只在
  安裝時才會發現。

**設計理由**：全域 timeout 分不出慢的 mirror 與死掉的 mirror，而且會耗掉大部分的時間窗；provider event 才能顯示進度。
stall 可能是環境問題、可就地修復，abort 會毀掉 operator 需要的證據，所以只要求處理。見
[decision 046](../decisions/046-os-deployment-progress-and-stall.md)。

---

## 11. Provider Recovery

swallow 擁有單一的 [Provider Recovery](glossaries/terms/provider-recovery.md) policy：由政策（而不是 provisioner 的拒絕
訊息）決定每個正規化的 `provisioning.state` 允許哪個 recovery 意圖、以哪個 provider primitive 實現；provisioner 只執行
選中的 primitive。API 見 [provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md)
的 Durable OS Operations 與
[server-detail-actions.md](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md) 的
Operator State and Provider Recovery。

### 11.1 政策矩陣：Release、Recover、Rescue、mark-*

| 意圖 | 允許的來源狀態 | 目標與實現 |
| --- | --- | --- |
| [Release](glossaries/terms/release.md) | `deployed`、`allocated`、`failed`、`broken`、`rescue` | 收斂到 `ready`。`releasing` 回「已在 releasing」，`ready` 回「不需要」 |
| Recover（UI：Return to Ready） | 同上；`ready` 視為立即成功（no-op） | 依狀態選 primitive，觀察到 `ready` 才算成功（§11.2） |
| Mark fixed | 只有 `broken` | `failed` 時拒絕並指向 Recover／Release |
| Mark broken | 不是 `broken`，且不在 `inspecting`／`deploying`／`releasing`／`testing` | — |
| Rescue（進入） | `deployed`、`broken`、`failed` | 診斷環境，不會到 `ready` |
| Exit rescue | 只有 `rescue` | 回到進入前的 provider 狀態，不會到 `ready` |

規則：

- 矩陣與 Recover 的 primitive 計畫集中在一個 domain policy，durable Release 與 Recover 的受理、Recover 的執行、單一
  operator-state 動作、明確的部署 Retry（§11.3）與 dashboard 的動作閘門都引用它，不得各自重寫。
- 拒絕時一律帶 swallow 撰寫的理由：mark-*／rescue 回 `409 conflict`；Release／Recover 的受理回 `400 validation_error`。
- [Rescue Mode](glossaries/terms/rescue-mode.md) 是診斷環境，不是 recovery 路徑。MAAS 進入／離開 rescue 的過渡狀態
  （包含失敗的變體）都正規化為 `rescue`，讓政策與 UI 把它們當成同一個 rescue 問題。
- Release／Recover Workflow 的受理：1–100 台、單一 Site、不重複；決策前先從 provider live refresh；live lock 檢查；
  [Platform Claim](glossaries/terms/platform-claim.md) 的 Server 一律不能 Release，Recover 只在它 `deployed` 時被拒
  （[decision 067](../decisions/067-platform-claim-on-server.md)）；仍被未完成 Workflow 佔用時回 busy，或以
  `cancelActiveWorkflows` supersede（[workflow-execution.md](workflow-execution.md)）。
- Release 的磁碟清除選項（`erase`、`secureErase`、`quickErase`）與 `unbindStaticIPs`（§6.3）由 API 傳入；swallow 永不
  轉送 MAAS 的 `force`。`release-os` 每次 attempt 都先觀察（已 Ready 即成功、Releasing 則跟到 Ready），見
  [workflow-execution.md](workflow-execution.md)。
- Recover 與 Release 都是 operator 觸發；一般部署失敗後不會自動回復。

### 11.2 Recover 如何收斂到 ready

`recover-server` 每次觀察都重新依當下狀態決定下一步（例如 exit rescue 後變成 `failed` 會再評估），每個 primitive 都有
上限，卡住時停下要求處理而不是無限迴圈；每個 primitive 之前都重新檢查 lock。

- `broken` → Mark fixed（最多 3 次、每次等 60 秒），仍未 Ready → `recover_mark_fixed_unsettled`。
- `rescue` → Exit rescue（最多 5 次、每次等 60 秒；provider 正在進入／離開 rescue 時耐心等待）。provider 回報離開
  rescue 失敗、或在 rescue 子系統超過約 4 分鐘時，升級為 Mark broken → Mark fixed，不清磁碟地回到 Ready；升級後約
  4 分鐘仍未生效 → `recover_rescue_unsettled`。
- `failed`、`deployed`、`allocated` → Release（不清磁碟）。Release 本身失敗、Machine 仍為 failed（典型是無法完成的
  disk erase）時，一次性升級為 Mark broken → Mark fixed；仍未生效 → `recover_release_unsettled`。
- `releasing` → 跟著既有的 Release 到 Ready，沿用同一套 static IP 清理契約。
- 成功條件是觀察到 `ready`，成功後清除 Server 的 deployment 投影；上限 2 小時（`provider_observation_timeout`）。

### 11.3 重試失敗的部署

對失敗的 `provision-os` 做**明確 Retry**（Task attempt 大於 1）就是 operator 要求重新部署那台 Server。Retry 先觀察，再依
provider 狀態行動：

| provider 觀察 | 明確 Retry |
| --- | --- |
| `deployed`，凍結的 image 且 SSH 可連 | 直接成功，不重做 provider 工作 |
| `deployed`，凍結的 image 但沒有位址 | Release、等 Ready、重新套用凍結的網路意圖並重新部署 |
| `deployed`，凍結的 image、有位址但 SSH 不通 | 只觀察 |
| `deployed`，image 不同 | `deployment_image_mismatch`（不可重試），不覆蓋正在執行的 OS |
| `deploying` | 繼續觀察 |
| `ready` | 照一般流程重新送出凍結的部署內容 |
| `failed`、`allocated`、`broken`、`rescue` | 在同一個部署 Workflow 內、以它的 resource lease 執行與 Recover 相同的計畫（不解除 static IP，讓凍結的網路意圖仍然適用），回到 Ready 後重送凍結的部署內容；這段失敗的 stage 以 `deployment_recovery_` 開頭 |

- 凍結的意圖是**磁碟**部署、而 Server 需要重新 inspection 時，Retry 在 Recover 之前就以 `inspection_required`
  （stage `deployment_preflight`，不可重試）結束 Task，避免先清掉 Machine 再被 preflight 拒絕。
- 首次 attempt 永不回復；Temporal 在 worker 交接後對同一 attempt 的 activity retry 也不回復：provider 為
  `failed`／`broken` 時回報 `deployment_failed`，`allocated` 時繼續觀察已送出的部署。
- 這條規則對單台部署與 Platform 部署相同；Platform 部署中其他 Server 與已完成的 Task 保留進度。
- 放棄整個部署（cancel）、rerun，以及取消時被中斷部署的回收，見 [workflow-execution.md](workflow-execution.md)。

**設計理由**：部署停在 `requires_attention` 時仍持有 Server 的 lease，另開的 Recover Operation 會被拒為 busy；兩個
Operation 也不能同時宣告同一台 Server。把 provider 失敗改成不可重試只會讓 operator 多走「Recover、再 repair／rerun」
幾步。preflight 拒絕則是已知結果，重試只會重複同一個拒絕，所以改為不可重試讓 Workflow 結束。見
[decision 033](../decisions/033-provider-recovery-policy.md) 與
[decision 059](../decisions/059-retry-recovers-failed-deployment.md)。

### 11.4 生命週期完整性

- `allocated`（MAAS `RESERVED`／`ALLOCATED`：已保留但沒完成部署，例如失敗或取消的部署、提早結束的驗證借用）是
  Release 與 Recover 的來源，primitive 為 Release。
- 一般部署失敗不會自動 release：失敗的 Machine 是 operator 可能想檢查的證據。只有驗證借用會自動歸還（§5.4），因為
  借用的契約就是歸還。
- **投影誠實化**：
  - reconcile 在 provider 回報 `ready` 或 `allocated` 時，清除已結束（`succeeded`／`failed`／`canceled`）的 deployment
    紀錄；進行中（`deploying`／`verifying`）或等待 operator（`requires_attention`）的紀錄不動。同樣的兩個狀態也清除
    Server Default User（[decision 045](../decisions/045-server-default-user.md)）。
  - `release-os` 與 `recover-server` 成功時清除 Server 的 deployment 投影。
  - client 的 Deployment 欄先讀 provider 軸：`succeeded` 只在 provider 仍回報 `deployed` 時有效，否則依 OS Provisioning
    State 顯示（Releasing、Ready、Allocated…），不讓過期的成功蓋住 provider 的實況。
  - OS Images 只把進行中的 verify Workflow 當作 verifying（§5.4）。

**設計理由**：這三個漏洞同源，都是把 provider 生命週期狀態或過期投影看得比實際更權威；修法是先讀 provider 軸，而不是
把三個由不同系統擁有的狀態軸合併。見 [decision 036](../decisions/036-provisioning-lifecycle-integrity.md)。

---

## 12. 程式位置對照（導航用）

### `api-server`

| 職責 | 路徑 |
| --- | --- |
| Provider port、`ProviderCapabilities` 與 optional interface | `internal/core/provisioning/domain/provider.go` |
| Recovery policy 矩陣與 Recover 計畫 | `internal/core/provisioning/domain/recovery_policy.go` |
| Deploy Target 與架構相容性 | `internal/core/provisioning/domain/deploy_target.go` |
| OS Image 驗證 model 與 repository | `internal/core/provisioning/domain/os_image_verification.go`、`internal/adapters/store/mongodb/provisioning/mongo_os_image_verification_repo.go` |
| Deployment Template | `internal/core/provisioning/domain/deployment_template.go`、`internal/core/provisioning/application/deployment_templates.go` |
| 部署網路模式 | `internal/core/provisioning/domain/network.go` |
| Network inspection 與建議 | `internal/core/provisioning/application/network_configuration.go` |
| Deploy resolve、受理閘門、批次送出 | `internal/core/provisioning/application/deploy_servers.go`、`internal/core/provisioning/application/deployment_targets.go` |
| OS Image 目錄、overlay、上傳、刪除 | `internal/core/provisioning/application/list_os_images.go`、`set_os_image_overlay.go`、`upload_os_image.go`、`delete_os_image.go` |
| Subnet DNS | `internal/core/provisioning/domain/subnet_dns.go`、`internal/core/provisioning/application/subnet_dns.go`、`internal/core/provisioning/delivery/subnet_dns_handler.go` |
| Provisioner HTTP Proxy | `internal/core/provisioning/domain/http_proxy.go`、`internal/core/provisioning/application/http_proxy.go` |
| Operator-state 動作（mark-*、rescue） | `internal/core/provisioning/application/machine_actions.go` |
| Release 與 Provisioning Task | `internal/core/provisioning/application/release_server.go`、`internal/core/provisioning/application/provisioning_tasks.go`、`internal/core/provisioning/domain/provisioning_task.go` |
| Reconcile（stale deployment 清除、deployed image 名稱鏡射） | `internal/core/provisioning/application/reconcile.go` |
| `deploy-os`／`release-os`／`recover-server`／`verify-os-image` 的組裝 | `internal/app/provisioning_operation_adapter.go` |
| `provisioner` Runner（`provision-os`、`release-os`、`recover-server`、Retry 回復） | `internal/app/provider_step_executor.go` |
| 部署進度與 stage stall | `internal/app/deployment_progress.go` |
| 驗證紀錄 Task（`record-image-verification*`） | `internal/app/platform_workflow_step_executor.go` |
| 取消驗證時的歸還 | `internal/adapters/executor/temporalworkflow/workflow.go`（`compensateVerifyCancel`） |
| MAAS adapter（狀態對應、網路、上傳、event、grouping、proxy、subnet DNS） | `internal/adapters/osprovisioning/maas/`（`mapping.go`、`network.go`、`provider.go`、`events.go`、`grouping.go`、`http_proxy.go`、`subnet_dns.go`） |
| Zone／Pool 與 placement | `internal/core/infrastructure/application/service.go`、`internal/app/grouping_realizer.go` |
| 上傳的 request body 上限 | `config/defaults.go`、`config/env.go`（`SWALLOW_API_IMAGE_UPLOAD_MAX_BYTES`） |

### `dashboard`

| 職責 | 路徑 |
| --- | --- |
| Server 列表的 Deployment 欄（先讀 provider 軸） | `src/presentation/pages/servers/serverListPresentation.ts` |
| OS Images 的驗證狀態 | `src/presentation/pages/provisioning/osImageListPresentation.ts`、`src/presentation/pages/provisioning/useOSImageVerificationActivity.ts` |
| Integration 設定頁（HTTP proxy、Subnet DNS 分頁） | `src/presentation/pages/infrastructure/IntegrationSettingsPage.tsx` |

---

## 13. 相關文件

- 決策（本文件收斂的 ADR）：[009](../decisions/009-deployment-template-ownership.md)、
  [012](../decisions/012-provider-network-configuration.md)、
  [018](../decisions/018-automatic-addressing-provider-auto-assign.md)、
  [027](../decisions/027-os-image-upload.md)、
  [029](../decisions/029-infrastructure-zone-pool-ownership.md)、
  [033](../decisions/033-provider-recovery-policy.md)、
  [035](../decisions/035-os-image-verification-and-deploy-target.md)、
  [036](../decisions/036-provisioning-lifecycle-integrity.md)、
  [046](../decisions/046-os-deployment-progress-and-stall.md)、
  [056](../decisions/056-provisioner-http-proxy.md)、
  [058](../decisions/058-deployment-dhcp-and-subnet-dns.md)、
  [059](../decisions/059-retry-recovers-failed-deployment.md)。
- 上位架構決策：[001](../decisions/001-system-ownership-boundaries.md)、
  [002](../decisions/002-server-identity.md)、
  [013](../decisions/013-server-lock-protection.md)、
  [025](../decisions/025-provider-data-overlay.md)、
  [031](../decisions/031-provider-capability-first-with-swallow-owned-fallback.md)、
  [048](../decisions/048-os-provisioning-generic-states.md)。
- 語言：glossary [OS Provisioning Provider](glossaries/terms/os-provisioning-provider.md)、
  [Machine](glossaries/terms/machine.md)、[OS Provisioning State](glossaries/terms/os-provisioning-state.md)、
  [OS Image](glossaries/terms/os-image.md)、[Provider Data Overlay](glossaries/terms/provider-data-overlay.md)、
  [OS Deployment](glossaries/terms/os-deployment.md)、[Deploy Target](glossaries/terms/deploy-target.md)、
  [Deployment Template](glossaries/terms/deployment-template.md)、
  [Network Configuration](glossaries/terms/network-configuration.md)、[IP Binding](glossaries/terms/ip-binding.md)、
  [Provisioning Task](glossaries/terms/provisioning-task.md)、[Subnet DNS](glossaries/terms/subnet-dns.md)、
  [Provisioner HTTP Proxy](glossaries/terms/provisioner-http-proxy.md)、[Zone](glossaries/terms/zone.md)、
  [Pool](glossaries/terms/pool.md)、[Release](glossaries/terms/release.md)、
  [Provider Recovery](glossaries/terms/provider-recovery.md)、[Rescue Mode](glossaries/terms/rescue-mode.md)、
  [Server](glossaries/terms/server.md)、[Server Lock](glossaries/terms/server-lock.md)、
  [Platform Claim](glossaries/terms/platform-claim.md)。
- Context map：[os-provisioning](context-maps/os-provisioning.md)。
- API 契約（`api-server` 擁有）：
  [provisioning](../../api-server/docs/development/api-contracts/api-server/provisioning.md)、
  [infrastructure](../../api-server/docs/development/api-contracts/api-server/infrastructure.md)、
  [provisioner-http-proxy](../../api-server/docs/development/api-contracts/api-server/provisioner-http-proxy.md)、
  [provisioner-subnet-dns](../../api-server/docs/development/api-contracts/api-server/provisioner-subnet-dns.md)、
  [server-detail-actions](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md)、
  [servers-list](../../api-server/docs/development/api-contracts/api-server/servers-list.md)、
  [workflows](../../api-server/docs/development/api-contracts/api-server/workflows.md)。
- 相鄰規格：[platform-deployment.md](platform-deployment.md)、[workflow-execution.md](workflow-execution.md)、
  [server-enrollment-and-boot-media.md](server-enrollment-and-boot-media.md)。
