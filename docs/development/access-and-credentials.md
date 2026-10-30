# Access and Credentials 設計與規範

本文件是 swallow **存取與憑證（access and credentials）** 的權威現行規格：swallow 自有的 SSH key
（Deployment Key 與 Access Key）、它們如何依 capability-first 落到 provisioner、automation 以哪把金鑰與
哪個帳號登入受管 Server（含 Server Default User）、Site automation credential，以及 API 認證（Session 與
API Key）的概覽。它把 [decision 039](../decisions/039-ssh-key-management-and-default-user.md)、
[decision 041](../decisions/041-deployment-key-only-automation.md)、
[decision 045](../decisions/045-server-default-user.md) 的現行結果寫成一份規格；API 認證的決策見
[decision 042](../decisions/042-authentication-sessions-and-api-keys.md)。

> **本文件的定位**：ADR 只記錄「為什麼」；本檔記錄目前有效的行為與規則。行為改變時更新本檔，決策本身被推翻時
> 才新增 ADR。路由、欄位、狀態碼與錯誤格式以 `api-server` 擁有的 API 契約為準，本檔只連結、不複製欄位表；
> 術語以 root glossary 為準，本檔不重新定義。

---

## 1. 目的與範圍

- **讀者**：修改 SSH key、automation 登入、Site automation credential、Server Default User 或 API 認證相關
  行為的開發者與 AI agent。先讀本檔，再依 [`api-server/AGENTS.md`](../../api-server/AGENTS.md) 進入實作。
- **範圍內**：Deployment Key 的建立、保存、替換與受理前檢查；Access Key；SSH Key 落到 provisioner 的
  realization；automation 的 SSH 金鑰來源與 Site credential；automation 登入帳號解析；Server Default User 的
  設定、驗證與清除；API 認證概覽。
- **範圍外（只連結）**：
  - SSH 就緒（`wait-for-ssh`）的驗證式探測、grace 與逾時、host key 擷取與 inventory 契約屬於 automation 執行
    骨架，寫在 [platform-deployment.md](platform-deployment.md) §4.1、§4.6、§4.6.1。本檔只寫「用哪把金鑰、以
    哪個帳號」。
  - provisioner Integration 自己的 API credential（見
    [sites-integrations](../../api-server/docs/development/api-contracts/api-server/sites-integrations.md) 契約）、
    Platform credential（[platform-deployment.md](platform-deployment.md) §4.4）、
    [Registry Credential](glossaries/terms/registry-credential.md)（[decision 044](../decisions/044-docker-registry-credentials.md)）。
  - Session 與 API Key 的時效、cookie 與 token 細節：只在 §9 概述並連結 decision 042 與 auth 契約。

---

## 2. 心智模型：哪些憑證、誰擁有、存在哪裡

| 憑證 | 擁有者 | 存放位置 | 用途 | 秘密能否讀回 |
| --- | --- | --- | --- | --- |
| [Deployment Key](glossaries/terms/deployment-key.md) | swallow（每個 installation 唯一一把） | MongoDB `ssh_keys`；私鑰以 installation 的 credential key 封存 | 所有 automation SSH 登入；公鑰落到 provisioner | 私鑰不經任何 API 回傳 |
| Access Key（[SSH Key](glossaries/terms/ssh-key.md) purpose `access`） | 一個 User（目前只有 bootstrap admin） | MongoDB `ssh_keys`，只存公鑰 | 人員以自己的私鑰登入受管 Server；公鑰落到 provisioner | swallow 不持有私鑰；產生時回傳一次 |
| Site become password（[Automation Configuration](glossaries/terms/automation-configuration.md) 的 credential） | Site | MongoDB `automation_configurations`，封存 | Ansible 以 sudo 提權且需要密碼時 | write-only，只以 `hasCredential` 表示 |
| Server Default User 的一次性密碼 | 該 OS 帳號的 operator | 不保存 | 一次 SSH 登入，把 Deployment Key 加入該帳號 | 不保存、不記錄、不回傳 |
| [Session](glossaries/terms/session.md)（access token + refresh token） | User | MongoDB（見 decision 042） | API 認證 | 見 §9 |
| [API Key](glossaries/terms/api-key.md) | User | MongoDB，只存 hash 與 display prefix | 非互動 API 認證 | 只在建立時回傳一次 |

兩條貫穿全文的原則：

1. **一把機器金鑰，私鑰只有一份由 swallow 保管**：swallow 只持有 Deployment Key 的私鑰；人員的身分是 Access
   Key 與 API Key，從不與機器身分混用。
2. **swallow 擁有 SSH Key 紀錄**：provisioner 的金鑰清單只是 realization 的目標，不是 source of truth（§5）。

---

## 3. Deployment Key

### 3.1 建立：安裝步驟

- 由一次性指令 `swallow-api deployment-key ensure` 建立 ed25519 key pair（預設名稱與公鑰 comment 為
  `swallow-deployment`）。指令 idempotent：已存在時不變更，只印出 fingerprint；任何情況都不印出私鑰。
- 指令需要與 API 相同的 MongoDB 與 credential key，且 schema 必須已是目前版本（先跑 `migrate`）。以不同的
  credential key 執行會產生 API 無法開啟的金鑰。
- 執行者：Compose 的 `swallowctl install` 與 `upgrade`、native 的 `swallowctl install`（native upgrade 會重新
  安裝）都在 `migrate` 之後執行它；dev 與 testing 的 seed 在安裝後執行它。
- 並行安全：`ssh_keys` 的 partial unique index（`purpose=deployment`）保證最多一把；並行執行時輸掉建立競爭的
  一方視為成功，回報已存在的那把。
- **API 啟動與它無關**：API process 不建立也不需要 Deployment Key 才能啟動。指令本身不接觸 provisioner；公鑰在
  API 的下一次同步時送到 provisioner（§5）。

### 3.2 保存與讀取

- 私鑰以 installation 的 credential key（設定 `credentialKey`）封存在 `ssh_keys` 文件中；所有一般讀取都排除
  它。只有 automation 在即將對主機認證前讀取私鑰，讀出後不得記錄、保存或回傳。
- 任何 API 都不回傳 Deployment Key 的私鑰；公鑰不是秘密，一律回傳。
- 私鑰只存在 MongoDB，還原需要 MongoDB 與 credential key 兩者。Compose `swallowctl` 的備份同時保存兩者。

### 3.3 替換與重產

- **不可刪除**：對 Deployment Key 的 delete 回 `409 conflict`。
- **替換**：以既有、未加密的私鑰替換（OpenSSH、PKCS#1、PKCS#8 或 SEC1 PEM）。有 passphrase 的私鑰會被拒絕，
  因為 swallow 在無人值守下使用它。公鑰由私鑰推導。
- **重產**：產生新的 ed25519 key pair，不回傳私鑰。
- 兩者都保留原 `id`，並以單一文件更新寫入，不會有「沒有可用 Deployment Key」的時刻。尚無 Deployment Key 時
  兩者回 `404`（以 §3.1 的安裝步驟建立）。替換後的同步會從 provisioner 移除舊公鑰、註冊新公鑰（§5.2）。
- **不會重新授權已佈署的 Server**：它們只授權佈署當時的公鑰。要讓 swallow 再次登入，可重新佈署；若必須保留
  現有 OS，以一次性密碼設定 Server Default User，把目前的公鑰安裝到該帳號（§8.2）。
- 可接受的金鑰型別（替換 Deployment Key 與匯入 Access Key 共用）：ed25519、ECDSA（NIST P-256／P-384／P-521）、
  至少 2048 bits 的 RSA；DSA 與 security-key（`sk-*`）型別拒絕。fingerprint（OpenSSH SHA256）在所有 SSH Key
  之間唯一，因此替換成與某把 Access Key 相同的金鑰會回 `409 conflict`。

路由與錯誤碼見 [ssh-keys](../../api-server/docs/development/api-contracts/api-server/ssh-keys.md) 契約。

### 3.4 需要 Deployment Key 的動作

缺少 Deployment Key 時，下列動作在受理時就拒絕（OS 佈署在任何 provider 讀寫之前）：

| 動作 | 缺少時的回應 |
| --- | --- |
| OS 佈署：Deploy OS（durable 與直接的單台、批次）、OS Image 驗證、Platform 的 `provision_os` | `409 conflict`，訊息指出 `swallow-api deployment-key ensure` |
| Platform deploy（任何 `machinePreparation.mode`） | `409 conflict` |
| GPU Operator 的部署後調整（[decision 062](../decisions/062-swallow-managed-gpu-operator.md)） | `409 conflict` |
| libvirt 虛擬機 enrollment 與以 Deployment Key 存取 [Hypervisor](glossaries/terms/hypervisor.md)（[decision 055](../decisions/055-libvirt-virtual-machine-enrollment.md)） | `409 conflict` |
| 設定 Server Default User（§8） | `409 conflict` |
| 任何 Ansible 執行（software deployment、platform playbook 等，§6.3） | 受理時 `503 provider_unavailable`；執行中的 Task 以 retryable 的 `automation_configuration_unavailable` 停下 |

受理之後才消失的 Deployment Key（例如資料被手動移除）會讓 `provision-os` Task 以 retryable 的
`deployment_key_missing` 停在 `requires_attention`；此時尚未寫入 provider，建立金鑰後可重試。

### 3.5 設計理由

- 安裝步驟（而非 API 啟動或 `prepare-secrets.sh`）建立金鑰：durable 的 installation 狀態由安裝建立，各安裝
  路徑共用 API 的設定與封存方式；需要金鑰的是佈署路徑，所以在那裡檢查，缺少時明確指出要執行的指令。
- 只能替換或重產、不能刪除：所有 automation 只有這一把金鑰，刪除會讓每個 Site 失去登入能力（inferred）。
- 完整脈絡與否決方案見 [decision 039](../decisions/039-ssh-key-management-and-default-user.md)。

---

## 4. Access Key

- 屬於一個 User（目前只有 bootstrap admin），只存公鑰。所有端點都要求 `admin`，且以呼叫者為範圍：list 只回
  呼叫者的 Access Key，讀取或刪除別人的 Access Key 與不存在的 key 一樣回 `404 not_found`，無法探測別人的金鑰。
- **匯入**：一行 `authorized_keys`，只能有一把金鑰、不得帶 options（`from=`、`command=` 等），保留 comment；
  型別限制同 §3.3。名稱去除前後空白後為 1–100 字元，在同一擁有者的 Access Key 之間不分大小寫唯一。
- **產生**：swallow 產生 ed25519 key pair，只保存公鑰，私鑰（未加密的 OpenSSH 格式）只在回應中出現一次；
  swallow 不留副本，遺失後無法取回。
- **刪除**：刪除紀錄並觸發同步，從 swallow 註冊過的 provisioner 移除；provider 暫時無法連線時由週期同步重試。
  已佈署的 Server 仍保留該公鑰。
- Access Key 經 provisioner 注入到之後佈署的 Server（MAAS 寫入 image 的 default user）。Server Default User 的
  一次性密碼流程只安裝 Deployment Key，不安裝 Access Key。

**設計理由**：私鑰只由 swallow 保管一份（Deployment Key），人員金鑰只存公鑰，避免 swallow 成為個人金鑰保管庫，
也避免離職者的金鑰成為 automation credential；金鑰以 User 為擁有者，未來的多使用者模型可以直接沿用。見
[decision 039](../decisions/039-ssh-key-management-and-default-user.md) 的 Alternatives considered。

---

## 5. Provisioner realization（capability-first）

依 [architecture-spec.md](architecture-spec.md) 的 Provider ownership 規則
（[decision 031](../decisions/031-provider-capability-first-with-swallow-owned-fallback.md)），SSH Key 是
swallow-owned 的紀錄；provider 有能力持有 SSH key 時，swallow 把每把金鑰的公鑰落到它那裡。

### 5.1 能力判斷

- provider 必須同時宣告 `ProviderCapabilities.SSHKeyRegistration` 並實作 optional interface `SSHKeyRegistrar`。
- MAAS 有此能力：金鑰註冊在擁有該 Integration API key 的 MAAS 使用者底下（`/account/prefs/sshkeys/`），MAAS
  佈署時把它們寫入 image 的 default user。
- 沒有此能力的 provisioner：每把金鑰的同步狀態為 `unsupported`，佈署不因此被擋；該 provisioner 佈署的 Server
  不會經由 provisioner 授權 swallow 的金鑰。

### 5.2 同步規則

- 目標是每個啟用中的 provisioner Integration（跨所有 Site）；暫停的 Integration 不同步，也不列在
  `providerSync`。已刪除的 Integration 的同步紀錄會被丟棄。
- 每次同步先列出 provider 的金鑰，再：
  1. 採用（adopt）provider 已有、金鑰材料相同的金鑰（比對 algorithm 與 base64，忽略 comment）；
  2. 新增缺少的金鑰；
  3. 只移除 swallow 自己註冊或採用、且已不再需要的 provider 金鑰（被刪除的 Access Key、被替換的 Deployment Key
     舊材料），而且只在 provider 仍列出它時移除。
- **operator 直接在 provisioner 加入的金鑰永遠不會被移除**：swallow 只以自己記錄的 provider key id 移除金鑰。
- 每把金鑰在每個 Integration 有一筆同步狀態（`synced`／`pending`／`failed`／`unsupported`，語意見 glossary
  [SSH Key](glossaries/terms/ssh-key.md) 與 ssh-keys 契約）。Deployment Key 替換後，舊材料的紀錄讀為 `pending`，
  直到新材料落地。單一金鑰失敗記為 `failed`，訊息只含可給 operator 看的原因（不含 transport 細節）；provider
  無法建構（例如缺少 credential）也記為 `failed`；一個 provisioner 失敗不影響其他 provisioner。

### 5.3 觸發時機

- API 啟動時一次；每次 SSH Key 變更後；provisioner Integration 寫入後（新註冊的 MAAS 立即取得金鑰）；
  `POST /api/v1/ssh-keys/sync`；以及週期同步（設定 `api.sshKeySyncInterval`，預設 5 分鐘，環境變數
  `SWALLOW_API_SSH_KEY_SYNC_INTERVAL`），用來修補 drift。
- 週期迴圈只在 API process 執行；同一 process 內的同步依序執行，不同 process 的同步可以重疊，以採用對方已新增
  的金鑰化解。
- **佈署前 ensure**：每次 OS 佈署在無副作用的 preflight 之後、任何網路或部署寫入之前，對該 Integration 做一次
  同步，確認 Deployment Key 為 `synced`（或 provisioner 為 `unsupported`）。失敗時不會佈署：
  - Workflow 的 `provision-os` Task 以 retryable 的 `ssh_key_registration_failed` 停在 `requires_attention`，
    provisioner 恢復後重試即可；
  - 同步的直接佈署端點回 `503 provider_unavailable`。

### 5.4 限制

- provisioner 只在 OS 佈署時注入金鑰：重產 Deployment Key、刪除 Access Key 都不會改變已佈署的 Server。
- 把金鑰推送到已佈署主機尚未實作；唯一的例外是以 Server Default User 的一次性密碼安裝 Deployment Key（§8.2）。
- 對沒有能力的 provisioner 以 cloud-init 注入金鑰尚未實作（需要與 operator user-data 合併的設計）。

### 5.5 設計理由

swallow 擁有金鑰紀錄、provisioner 只是落地目標，所以 swallow 會修補 provider 的清單，但不把它當成來源；只移除
自己記錄的 provider 金鑰，operator 的金鑰才安全；佈署前 ensure 保證不會佈出 swallow 無法管理的主機。cloud-init fallback 因
目前唯一的 provisioner（MAAS）已有能力而延後。見
[decision 039](../decisions/039-ssh-key-management-and-default-user.md)。

---

## 6. Automation 的 SSH 金鑰與 Site automation credential

### 6.1 唯一的金鑰來源

- **每一次 automation SSH 登入都使用 Deployment Key**：SSH 就緒（`wait-for-ssh`）、per-host 登入帳號探測、每次
  Ansible 執行、設定 Server Default User 的驗證登入（§8），以及 Hypervisor 存取（decision 055）。
- Site 不能另設私鑰。要在整個 installation 改用另一把金鑰，替換 Deployment Key（§3.3）。
- Ansible 相關的讀取者（execution service、Ansible executor、`wait-for-ssh`、host-user prober）一律經由
  `EffectiveAutomationConfigurations` 取得金鑰與 Site 的 become password，因此不會對「這個 Site 用哪把金鑰」
  有不同答案。

### 6.2 Site credential 只有 become password

- Site automation credential 只保存 write-only 的 become password，以 credential key 封存。寫入會整組取代
  credential；送 `{}` 即清除。`hasCredential` 表示 Site 存有 become password。
- 帶有 `sshPrivateKey`（任何值）的 credential 請求回 `400 validation_error`，並指向替換 Deployment Key 的端點；
  不會默默忽略，讓呼叫端不會以為 override 已生效。
- 先前 release 存下的 Site 私鑰在讀取時被忽略，不會進入任何執行；下一次寫入 credential 時被覆寫並移除舊標記。
- `credentialSource` 於讀取時推導、從不保存，只有兩個值：`deploymentKey`（Deployment Key 存在且被使用）與
  `none`（安裝步驟尚未建立 Deployment Key）。

### 6.3 Ansible 可執行的條件

- Site automation 已啟用（`enabled`）且 Deployment Key 存在，Ansible 就可以為該 Site 執行。這是唯一的規則，受理時
  與 Ansible Task 執行前都用它；不需要 Site 存有任何 credential 紀錄。
- 不符合時：受理回 `503 provider_unavailable`；執行中的 Task 以 retryable 的
  `automation_configuration_unavailable` 停下（在 Workflow 外啟用 automation 或建立金鑰後重試）。
- Site 的 `sshUser` 只是登入帳號探測的候選（§7.3）；`sshPort` 用於所有 automation 登入與 Server Default User 的
  登入（未設定時為 22）；啟用時必須提供 `knownHosts`，host key 驗證不可關閉（見
  [platform-deployment.md](platform-deployment.md) §4.6）。

欄位與錯誤見 [site-automation](../../api-server/docs/development/api-contracts/api-server/site-automation.md) 契約。

### 6.4 設計理由

Site 私鑰 override 只保護兩種主機：ADR 039 之前佈署的 Server（可重新佈署取得 Deployment Key），以及 swallow
管理但未佈署的主機（不在範圍內）。它沒有保護任何人，卻讓 domain 保留第二個金鑰來源，還留下「只靠 Deployment Key
的 Site 仍被要求存有 credential 紀錄」的缺陷。拒絕而非忽略 `sshPrivateKey`，是因為看似成功卻沒有效果的請求會誤導
呼叫端。見 [decision 041](../decisions/041-deployment-key-only-automation.md)。

---

## 7. Automation 登入帳號解析

### 7.1 OS Image default user

- [OS Image](glossaries/terms/os-image.md) 的 [Provider Data Overlay](glossaries/terms/provider-data-overlay.md)
  帶有 swallow-owned 的 `defaultUser`，可在上傳時或之後設定（見
  [provisioning](../../api-server/docs/development/api-contracts/api-server/provisioning.md) 契約）。
- effective 值：overlay 的值；否則依 provider OS family 的內建值（`ubuntu`→`ubuntu`、`centos`→`centos`、
  `rhel`→`cloud-user`）；否則沒有。內建值只依 provider 自己回報的 OS family 推導，不依 overlay 的顯示名稱，改
  image 的顯示 OS 不會改變 automation 的登入帳號。uploaded custom image（OS family `custom`）沒有內建值，需由
  operator 設定。
- effective 值鏡射到每台以該 image 佈署的 Server，成為 `provisioning.deployedImageDefaultUser`（reconcile 時，
  以及 overlay 變更後盡力立即更新）。即使 Server 有自己的 Server Default User，此欄位仍是 image 的值。

### 7.2 Effective Server Default User

每台 Server 的登入帳號只在一處解析（[Server Default User](glossaries/terms/server-default-user.md)）：

1. Server 上 operator 設定的值（projection `defaultUser.source` 為 `server`，§8）；
2. 否則 `deployedImageDefaultUser`（`source` 為 `os_image`）；
3. 否則未知。

Server projection 以 `defaultUser {user, source}` 回報 effective 值（見
[servers-list](../../api-server/docs/development/api-contracts/api-server/servers-list.md) 契約）。設定 Server
Default User 與 OS Image overlay 的帳號名稱共用同一條 POSIX login name 規則。

### 7.3 解析與探測

- effective 值已知時**只用它**，不探測：已知的帳號必須是確定的，錯誤的值以清楚的訊息失敗，而不是默默以另一個
  帳號登入。
- 未知時，以 Deployment Key 依序嘗試候選 `[Site sshUser, cloud-user, ubuntu]`（有設定時 Site 值優先、去重），
  第一個認證成功者即為該台帳號。Ansible 執行時若沒有候選成功，`ansible_user` 退回 Site `sshUser`。
- 探測只送出簽章、不送私鑰，也不驗證 host key；權威的 host key 驗證由 Ansible 執行負責
  （[platform-deployment.md](platform-deployment.md) §4.6）。

### 7.4 套用點

- `wait-for-ssh`（`internal` Runner；platform 的 `ensure-os` 與 software deployment 的 `prepare-hosts` 相同）：
  以 effective 值或候選完成一次實際 SSH 認證。fail-fast、grace 與逾時見
  [platform-deployment.md](platform-deployment.md) §4.6.1。
- Dynamic inventory：hostvar `default_user` 是 effective 值，`image_default_user` 是 OS Image 的值，兩者都只在
  已知時出現（[platform-deployment.md](platform-deployment.md) §4.1）。
- Runner 的 `ansible_user`：取 `default_user`；在 `default_user` 出現之前凍結的 inventory 改取
  `image_default_user`；host 上已存在的 `ansible_user` 不覆寫；都沒有時依 §7.3 探測。playbook 可把
  `ansible_user` 當成該 Server 的 default user（例如 §8.4 的 Docker CE）。
- Hypervisor 存取（decision 055）：未指定帳號時使用 effective Server Default User。

### 7.5 設計理由

不同 OS image family 的預設帳號不同，而一個 Site 只有一個 `sshUser`，所以帳號 per-host 解析；已知 image 帳號時
只用它，讓錯誤明確（[decision 039](../decisions/039-ssh-key-management-and-default-user.md)）。OS 不是由 swallow
佈署的 Server，image 推導的帳號可能不存在，所以 Server 上的值優先於 image 的值
（[decision 045](../decisions/045-server-default-user.md)）。

---

## 8. Server Default User 設定

讓 OS 不是由 swallow 佈署的 Server（例如經 provisioner inventory 或 existing-OS enrollment 進來的主機）可以被
automation 登入；任何 deployed Server 都可以設定，覆寫 image 的值。路由、欄位與錯誤碼見
[server-detail-actions](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md)
契約的 Default User 一節。

### 8.1 前提

- Server 存在、為 `deployed` 且有 primary address、未被 [Server Lock](glossaries/terms/server-lock.md) 鎖住
  （即時檢查，無法確認時 fail closed）；installation 有 Deployment Key。
- 帳號名稱必須是 POSIX login name（允許 `root`）。

### 8.2 設定流程

1. **有一次性密碼時**：api-server 以該帳號與密碼登入一次（password 或 keyboard-interactive 認證），把目前
   Deployment Key 的公鑰 idempotent 地加入該帳號的 `~/.ssh/authorized_keys`：相同 algorithm 與金鑰材料已存在
   （不論 comment）就不再加入；目錄與檔案權限符合 sshd StrictModes；主機有 `restorecon` 時還原 SELinux label。
2. **一律**：以 Deployment Key 登入該帳號並執行 `sudo -n true`，回報 sudo 狀態：`passwordless`、
   `password_required`（automation 使用 Site become password）或 `unavailable`（需要 root 的 automation 會失敗）。
3. **只有 Deployment Key 登入成功才儲存**。金鑰已安裝但後續步驟失敗時，金鑰留在主機上（安裝是 idempotent），
   不儲存任何值。

- 不提供密碼時，operator 必須先自行把 Deployment Key 公鑰加入該帳號（契約提供一行指令），api-server 只做第 2、3 步。
- 連線使用 Server 的 primary address 與 Site 的 SSH port（未設定時 22）；每次登入有固定的逾時上限；不驗證 host key，
  與登入帳號探測相同，權威的 host key 驗證仍由 Ansible 執行負責。

**安全規則**：

- 密碼只用於該次登入，不保存、不記錄、不回傳，也不作為 become password。
- 登入失敗的錯誤訊息只指名 Server 與帳號，從不包含密碼。
- 一次性密碼經 SSH 傳到主機；因為不驗證 host key，冒用該位址的主機可能截取它。此流程只在「所有佈署目標都是內部
  資料中心主機」的前提下可接受（decision 045 由使用者裁定此版本不以此為安全顧慮）；前提改變時必須重新檢討。

### 8.3 清除

- **手動**：delete 移除 Server 上的值，image 的值重新適用。不接觸主機（金鑰仍授權）；沒有值時也成功。
- **自動**：Server 上的值只屬於一次 OS 安裝。
  - swallow 真正送出新的 OS 佈署之前清除：Workflow 的 `provision-os` 在寫入 provider 前清除；只觀察到已收斂 OS 的
    rerun 或 retry 不清除（[decision 060](../decisions/060-cancel-and-rerun-converge.md)）。
  - reconcile 觀察到 Machine 為 `ready` 或 `allocated`（沒有 OS）時清除，不論是經 swallow 或直接在 provisioner
    release；其他狀態（含 `deployed`、`rescue`）保留，因為 OS 仍在主機上。
- reconcile 不以 provider 資料覆寫此值，只在上述情況清除。

### 8.4 Docker group

- [Managed Software](glossaries/terms/managed-software.md) Docker CE 安裝（`present`）時，把 `ansible_user`
  （也就是 effective Server Default User）以 append 方式加入 `docker` group，讓該帳號不需 sudo 即可使用 Docker；
  帳號為空或 `root` 時略過。swallow 佈署的 Server 與 existing Server 套用同一規則；對先前安裝的主機重跑 Docker CE
  安裝即補上。group 成員資格在該帳號下次登入時生效。
- 卸載 Docker CE 不移除 group 成員資格。

### 8.5 尚未實作

- 在 existing-OS enrollment（`swallow servers enroll`，[decision 053](../decisions/053-server-enrollment-and-automatic-inspection.md)）
  時自動帶入 Server Default User 並匯入 Deployment Key，不需在 swallow 輸入密碼：仍延後。實作時應設定同一個 Server
  Default User。
- 把 Access Key 推送到已佈署主機。

### 8.6 設計理由

只靠手動指令會讓每台 existing Server 先需要一個 shell session，所以以一次性密碼為主要路徑、手動指令為備援；不保存
密碼，因為 Site become password 已涵蓋 sudo，且金鑰登入取代密碼；用 api-server 的單次 SSH 登入安裝金鑰，因為
Ansible 本身需要先能登入。見 [decision 045](../decisions/045-server-default-user.md)。

---

## 9. API 認證概覽（Session 與 API Key）

API 認證的決策與時效、cookie、rotation 細節見
[decision 042](../decisions/042-authentication-sessions-and-api-keys.md) 與 auth 契約，本節不重述。

- 兩種代表 User 的 credential：密碼登入建立的 [Session](glossaries/terms/session.md)（短效 access token 加上每次
  使用即換發的 refresh token），以及 User 為非互動 client 建立的 [API Key](glossaries/terms/api-key.md)（前綴
  `swk_`，以 `Authorization: Bearer` 傳送）。
- 單一 authenticator middleware 驗證兩者，產生同一個 principal（User、role、驗證方式），handler 不依 credential
  種類分支。
- 不變式：
  - refresh token 與 API Key 都只存 hash；API Key 的 secret 只在建立時顯示一次。
  - API Key 以擁有者目前的 role 行事（目前只有 admin；scope 延後到多使用者模型）。
  - 以 API Key 驗證的請求不能建立 API Key（`403 forbidden`），外洩的金鑰無法自行繁衍。
  - SSE 的 query-parameter token 只接受 access token，API Key 不出現在 URL。
  - logout 撤銷 Session，但已發出的 access token 在到期前仍有效。
- machine token 是 installation secret，不屬於任何 User，只開放 discovery 與 `/metrics`；它不是 Session 也不是
  API Key。

---

## 10. 程式位置對照（導航用）

`api-server` 內路徑相對於 `api-server/`：

| 職責 | 路徑 |
| --- | --- |
| SSH Key domain（purpose、不變式、同步狀態、ports、錯誤） | `internal/sshkey/domain/` |
| SSH Key use case（匯入、產生、替換、重產、安裝時建立） | `internal/sshkey/application/service.go` |
| Provisioner realization 與佈署前 ensure | `internal/sshkey/application/sync.go` |
| 金鑰材料解析、產生與型別限制 | `internal/sshkey/infra/keymaterial.go` |
| `ssh_keys`（封存私鑰、unique indexes）與同步紀錄 | `internal/sshkey/infra/mongo_key_repo.go`、`internal/sshkey/infra/mongo_sync_repo.go` |
| provisioner 能力判斷 | `internal/sshkey/infra/provisioner_keys.go` |
| `SSHKeyRegistration` capability 與 `SSHKeyRegistrar` interface | `internal/provisioning/domain/provider.go` |
| MAAS `SSHKeyRegistrar` | `internal/provisioning/infra/maas/sshkeys.go` |
| SSH Keys HTTP | `internal/sshkey/delivery/handler.go` |
| 組裝、同步迴圈、Deployment Key adapter | `internal/app/sshkey_adapter.go`、`internal/app/api.go` |
| 安裝步驟 `deployment-key ensure` | `cmd/swallow-api/deployment_key.go`、`internal/app/deployment_key.go` |
| OS 佈署的受理前檢查與佈署前 ensure | `internal/provisioning/application/deploy_servers.go`、`internal/provisioning/application/deploy_server.go` |
| `provision-os` 錯誤碼與送出佈署前清除 Server Default User | `internal/app/provider_step_executor.go` |
| Platform deploy 的 Deployment Key 檢查 | `internal/platform/application/deploy_platform.go` |
| Effective automation credential 與 `credentialSource` | `internal/operation/application/effective_credential.go` |
| `CheckRunnable`、`CredentialSource` | `internal/operation/domain/execution.go` |
| Site credential 儲存（只存 become password） | `internal/operation/infra/mongo_automation_config_repo.go` |
| Site credential HTTP（拒絕 `sshPrivateKey`） | `internal/operation/delivery/execution_handler.go` |
| Ansible Task 的可執行檢查 | `internal/operation/infra/temporalworkflow/ansible_executor.go` |
| 登入帳號候選與探測 | `internal/shared/sshprobe/sshprobe.go` |
| `ansible_user` 解析 | `internal/operation/infra/local_runner.go` |
| `wait-for-ssh` | `internal/app/platform_workflow_step_executor.go` |
| Inventory `default_user`／`image_default_user` | `internal/discovery/application/discovery.go` |
| OS Image default user 的內建值與 effective 值 | `internal/provisioning/domain/os_image_overlay.go` |
| 鏡射到 Server、無 OS 時清除 Server Default User | `internal/provisioning/application/reconcile.go` |
| Server Default User（effective 值、名稱規則、sudo、錯誤） | `internal/server/domain/default_user.go` |
| Server Default User use case | `internal/server/application/default_user.go` |
| 一次性密碼安裝金鑰與 Deployment Key 驗證登入 | `internal/server/infra/ssh_host_access.go` |
| Server Default User HTTP | `internal/server/delivery/default_user_handler.go` |
| Docker CE 的 `docker` group | `automation/playbooks/roles/docker_ce/`（`defaults/main.yml`、`tasks/main.yml`） |
| 認證 middleware（access token 與 API Key） | `internal/shared/middleware/auth.go` |
| Session、API Key | `internal/auth/`、`internal/apikey/` |
| 封存（sealer） | `internal/shared/secret/` |
| 設定（`sshKeySyncInterval`、`credentialKey`） | `config/config.go`、`config/defaults.go`、`config/env.go` |

安裝與 seed（相對於 repository root）：`deploy/production/swallowctl`、`deploy/production/native/swallowctl`、
`deploy/dev/seed.sh`、`deploy/testing/seed.sh`。

---

## 11. 相關文件

- 決策：[decision 039](../decisions/039-ssh-key-management-and-default-user.md)、
  [decision 041](../decisions/041-deployment-key-only-automation.md)、
  [decision 045](../decisions/045-server-default-user.md)、
  [decision 042](../decisions/042-authentication-sessions-and-api-keys.md)；相關的
  [decision 031](../decisions/031-provider-capability-first-with-swallow-owned-fallback.md)（capability-first）、
  [decision 025](../decisions/025-provider-data-overlay.md)（OS Image overlay）、
  [decision 053](../decisions/053-server-enrollment-and-automatic-inspection.md)（existing-OS enrollment）、
  [decision 055](../decisions/055-libvirt-virtual-machine-enrollment.md)（Hypervisor）、
  [decision 060](../decisions/060-cancel-and-rerun-converge.md)（只在真正送出佈署時清除 Server Default User）。
- 規格：[platform-deployment.md](platform-deployment.md) §4.1（inventory）、§4.6（host key）、§4.6.1（SSH 就緒與
  per-host 使用者）；[software-deployment.md](software-deployment.md)；[architecture-spec.md](architecture-spec.md)
  的 Provider ownership。
- Glossary：[SSH Key](glossaries/terms/ssh-key.md)、[Deployment Key](glossaries/terms/deployment-key.md)、
  [Server Default User](glossaries/terms/server-default-user.md)、[OS Image](glossaries/terms/os-image.md)、
  [Automation Configuration](glossaries/terms/automation-configuration.md)、
  [Provider Data Overlay](glossaries/terms/provider-data-overlay.md)、[Session](glossaries/terms/session.md)、
  [API Key](glossaries/terms/api-key.md)、[Integration](glossaries/terms/integration.md)、
  [Hypervisor](glossaries/terms/hypervisor.md)、[Server Lock](glossaries/terms/server-lock.md)、
  [Managed Software](glossaries/terms/managed-software.md)。
- Context map：[automation-execution](context-maps/automation-execution.md)。
- API 契約（`api-server` 擁有）：
  [ssh-keys](../../api-server/docs/development/api-contracts/api-server/ssh-keys.md)、
  [site-automation](../../api-server/docs/development/api-contracts/api-server/site-automation.md)、
  [server-detail-actions](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md)、
  [servers-list](../../api-server/docs/development/api-contracts/api-server/servers-list.md)、
  [provisioning](../../api-server/docs/development/api-contracts/api-server/provisioning.md)、
  [auth-login](../../api-server/docs/development/api-contracts/api-server/auth-login.md)、
  [auth-refresh](../../api-server/docs/development/api-contracts/api-server/auth-refresh.md)、
  [auth-logout](../../api-server/docs/development/api-contracts/api-server/auth-logout.md)、
  [auth-me](../../api-server/docs/development/api-contracts/api-server/auth-me.md)、
  [api-keys](../../api-server/docs/development/api-contracts/api-server/api-keys.md)、
  [conventions](../../api-server/docs/development/api-contracts/api-server/conventions.md)、
  [servers-stream](../../api-server/docs/development/api-contracts/api-server/servers-stream.md)。
