# Server Enrollment 與 Boot Media 設計規範

本文件是 swallow **Server Enrollment**、**hardware inspection**、provisioner **Power Configuration**、
**Boot Media** 與 **Boot ISO** 的權威設計文件與開發規範。它描述「機器如何進入 provisioner 的
inventory、如何被檢視到 `ready`，以及在 provisioner 不提供 DHCP 的網路上如何讓機器開機到
provisioner」目前生效的規則。

## 目的與範圍

- **範圍內**：三條 Server Enrollment 路徑（network boot、existing OS、libvirt virtual machine）；
  自動與手動 hardware inspection（`inspect-hardware` Workflow 與它的 Task）；Power Configuration 與
  power adapter（`bmc`／`virsh` family）；Boot Media（`redfish` 與 `libvirt` method、capability probe、
  enable preflight、`ensure-boot-media`、部署開機的補救）；Boot ISO（建置、BIOS iPXE 與 UEFI GRUB
  的 hand-off、原地 rebuild）；以及 UEFI 儲存配置需要重新檢視時的部署防護。
- **範圍外**：OS 佈署本身、定址、部署失敗的 Retry／Recover 與 stage-stall 偵測，見
  [os-provisioning.md](os-provisioning.md)；Workflow 的 Task 重試、取消與 rerun 語意，見
  [workflow-execution.md](workflow-execution.md)；platform 的 `ensure-os` Job，見
  [platform-deployment.md](platform-deployment.md)。

本文件**不重述** glossary 與 API contract：術語以 root glossary 為準，路由、欄位、狀態碼與錯誤碼以
`api-server` 擁有的 contract 為準（見 §8）。決策理由以 ADR 為準，各節的「設計理由」只摘要並連結。

---

## 心智模型

一台機器從「不存在」到「可佈署」要經過兩件事，swallow 把它們分開：

1. **Enrollment**（[Server Enrollment](glossaries/terms/server-enrollment.md)）只把機器放進
   provisioner 的 inventory。Server 與它的 `serverId` 永遠由 reconcile 從 provisioner 的 Machine
   投影而來（[decision 002](../decisions/002-server-identity.md)），enrollment 從不建立 Server 紀錄。
2. **Hardware inspection** 是 durable 的 `inspect-hardware` Workflow，把 `new` 的 Server 帶到 `ready`
   （MAAS 的 commission）。

provisioner 不提供 DHCP 的網路上，機器要靠 **Boot Media** 開機到 provisioner：Server 的 Boot Media
setting 選一個 **Boot ISO**，swallow 把它掛到 BMC 的 virtual media 或 VM 的 CD-ROM 並讓它先開機。

事實的所有權（[decision 001](../decisions/001-system-ownership-boundaries.md)）：

| 事實 | 擁有者 | swallow 的做法 |
| --- | --- | --- |
| [Power Configuration](glossaries/terms/power-configuration.md)（MAAS `power_type`／`power_parameters`，含 BMC 帳密） | provisioner | 每次即時讀取、write-through，不落地；密碼 write-only |
| Machine 的狀態、enlistment、commissioning 結果 | provisioner | 讀取並投影 |
| [Boot Media](glossaries/terms/boot-media.md) setting（是否啟用、選哪個 Boot ISO、上次套用結果） | swallow | 存在 Server 上，reconcile 不覆寫 |
| [BMC](glossaries/terms/bmc.md) 的 Redfish capability、VM 的 libvirt capability | swallow（帶 `probedAt` 的 probe 結果） | 定期與按需重新 probe，不回寫 provisioner |
| [Boot ISO](glossaries/terms/boot-iso.md)（紀錄與檔案） | swallow | 由 api-server 建置、儲存與提供 |
| BMC／Hypervisor 的即時狀態（ISO 是否掛著、開機導向） | BMC／Hypervisor | 只讀取，從不假設 |

---

## 1. Server Enrollment

Server 列表的 **Add servers** 提供引導。三條路徑的結果都只是 provisioner inventory 裡多了一個
Machine；之後的 Server 一律來自 reconcile。

### 1.1 Network boot

- operator 讓機器 power cycle 並從網路開機，provisioner 的 enlistment 把它建成 New，swallow 投影為
  `new`，接著由自動 hardware inspection（§2.4）帶到 `ready`。
- **External DHCP 仍是 PXE**，只是經由 iPXE Boot ISO：provisioner 還沒 enlist 這台機器，swallow 不知道
  它的 BMC，因此 Add servers 引導 operator 選（或先建）該 provisioner 的 Boot ISO，顯示 ISO URL，讓
  operator 從 BMC web console 或以畫面給出的 Redfish 指令掛載後開機。enlist 之後，同一台 Server 的 Boot
  Media（§4）才由 swallow 自動套用。

### 1.2 Existing OS（保留作業系統、不重開機）

- 主機上執行一行指令：從 installation 下載 `/downloads/swallow-enroll.sh`，script 再從 installation
  下載 `/downloads/swallow`（linux-amd64 CLI），執行 `swallow servers enroll --provisioner=maas …`，
  結束後刪除 CLI。兩個下載都**不需認證**、都不含 secret；script 的 CLI 下載位址取自抓取它的那個
  request，因此主機用哪個位址連 swallow 都可以。
- `swallow servers enroll` 對 MAAS 包裝 `maas-run-scripts register-machine`（Machine 建成 Deployed）與
  `report-results`（從執行中的 OS 記錄硬體），完成後刪除下載的一切。provider 邏輯只存在於 CLI，不在
  shell script 裡重寫。
- 指令中的 provisioner endpoint 與 API key 由 admin-only 的 enroll-bundle 路由交出（`Cache-Control:
  no-store`、不記 log）。這是「credential write-only」的唯一例外：MAAS 的 `register-machine` 需要 MAAS
  API key，沒有更窄的 credential。執行過指令的主機可以使用該 key；不信任主機時應輪替。
- Server 投影為 `deployed` 並保留 OS，**永遠不會被自動 inspection**（inspection 會重開機）。
  enrollment 不帶入 [Server Default User](glossaries/terms/server-default-user.md)。
- CLI 檔案來自設定 `api.cliBinary`（`SWALLOW_API_CLI_BINARY`，production 為 bundle 的 `bin/swallow`），
  每次 request 檢查檔案；未設定或檔案不存在時回 `404` 與純文字原因。

### 1.3 libvirt virtual machine

operator 指定 [Hypervisor](glossaries/terms/hypervisor.md) 與 domain 名稱，swallow 把每個 domain 註冊進
provisioner。

- **Hypervisor 是 Server 的角色，不是新紀錄或新 credential**：present、`deployed`、有 primary address 的
  Server。swallow 以 installation 的 [Deployment Key](glossaries/terms/deployment-key.md)、Site 的 SSH port
  登入，帳號為 operator 指定的 `account`，否則為該 Server 的 effective Server Default User，執行
  `virsh -c qemu:///system`。帳號必須能使用 system libvirt daemon（Ubuntu 的 `libvirt` group）。
- **durable Workflow**：kind `enroll-virtual-machines`（definition `virtual-machine-enrollment` v1），target 是
  Hypervisor Server（兩個 enrollment 不會同時改同一台 Hypervisor），每個 domain 一個獨立、無依賴的
  `enroll-virtual-machine` Task（`provisioner` Runner），在 Workflow parallelism 內平行執行，單一失敗可單獨
  retry。受理時檢查：Integration 是 provisioner 且具備 machine registration 與 Power Configuration、與
  Hypervisor 同 Site、Boot ISO 屬於同一 provisioner、Deployment Key 存在、Hypervisor 未被 lock。
- 每個 Task 依序 ensure（每步都可安全重跑，retry 沿用已註冊的 Machine）：
  1. domain 存在（否則 `domain_not_found`，不可 retry）。
  2. domain 已關機；執行中則 `requires_attention`（`domain_running`），除非請求帶 `powerOffRunning`，此時
     swallow 以 libvirt 強制關機。這是 swallow 唯一會改 VM 電源的地方，而且只在 operator 明確要求時。
  3. Integration 有 `settings.virshSshPublicKey` 時，把該公鑰授權給 Hypervisor 上的登入帳號，讓
     provisioner 能經 `qemu+ssh` 連到 libvirt（§3.4）。
  4. 帶 `bootIsoId` 時，Boot ISO 放上 domain 的 CD-ROM 並設為第一開機（§4.6）。
  5. provisioner 有對應的 Machine：已有同一 MAC 的 Machine 就沿用並取代其 Power Configuration；否則以
     domain 的架構（`x86_64`→`amd64/generic`、`aarch64`→`arm64/generic`，其他為 `architecture_unsupported`）、
     全部 MAC、由 domain 名推導的 hostname，以及 `virsh` Power Configuration（address
     `qemu+ssh://<account>@<hypervisor address>/system`、power ID 為 domain 名）註冊，**不 commission**
     （MAAS `commission=false`）。
  6. provisioner 能經 `virsh` driver 讀到電源，否則 `provisioner_cannot_reach_hypervisor`。
  7. reconcile 投影出該 Server（最多等五分鐘，否則 `server_not_projected`）。
  8. 帶 `bootIsoId` 時，把該 Server 的 Boot Media 存為啟用並套用該 Boot ISO，之後每次 inspection 與 OS
     佈署都會重新套用。
- 結果是 `new`、已關機、有 automatic power driver 的 Server，自動 inspection 的 enrollment wait 在連續兩次
  讀到關機後即 settle，再由 provisioner 的 commission 開機（§2）。Integration 關閉 `autoInspect` 時改為手動
  Inspect。
- swallow 對 Hypervisor 的 libvirt 只做 provisioner 做不到的事：讀 domain、改 CD-ROM 與開機順序。除了上面
  operator 明確要求的 `powerOffRunning`，開關機都由 provisioner 的 `virsh` driver 執行。改 domain 定義是對
  Hypervisor 的 mutation，因此被 lock 的 Hypervisor 拒絕 enrollment 與 `libvirt` Boot Media 的 enable／disable。
- Dashboard 的入口是 experimental feature `virtualMachines`；API 與 CLI（`swallow servers virtual-machines
  list|enroll`）為正式功能，因為 experimental 開關只改 Dashboard 的呈現。

### 設計理由

- 由 reconciler 直接 commission 會在讀取路徑寫入、與 enlistment 開機競速，也沒有 durable 的 retry 與
  attention；從 api-server 註冊既有主機則不可能，硬體事實必須在主機上讀
  （[decision 053](../decisions/053-server-enrollment-and-automatic-inspection.md)）。
- 主機很少裝有 CLI、常常不能上網，而 installation 本來就附帶 CLI，所以由 installation 提供下載
  （decision 053）。
- VM 以名稱 enroll，而不是 MAAS `add-chassis` 或 VM host：前者以名稱前綴選 VM、把 domain 改成先網路後磁碟
  開機（破壞 external DHCP 的 iPXE ISO）且非同步；後者把 VM 的生命週期與開機順序交給 MAAS
  （[decision 055](../decisions/055-libvirt-virtual-machine-enrollment.md)）。

---

## 2. Hardware inspection：`inspect-hardware` Workflow

### 2.1 Workflow 組成

kind `inspect-hardware`、definition `hardware-inspection` v1，一個 target Server，一個 Job
`ensure-inspected`，三個依序的 ensure Task：

| Task ID | Kind | Runner | 確保 |
| --- | --- | --- | --- |
| `wait-enrollment-<serverId>` | `wait-enrollment-settled` | `provisioner` | provider 自己的 enrollment 已結束 |
| `ensure-boot-media-<serverId>` | `ensure-boot-media` | `internal` | Server 的 Boot Media（若啟用）已套用，執行時即時解析（§4.7） |
| `inspect-<serverId>` | `inspect` | `provisioner` | provider 已檢視硬體，Server 為 `ready` |

Workflow 的 intent snapshot 記錄 `origin`：`automatic`（自動 sweep）或 `requested`（operator 的 Inspect）。
origin 決定 enrollment wait 的行為（§2.2）與已經 `ready` 時是否重檢（§2.3）。這些 kind 與 Task ID 前綴是
published vocabulary，改名會破壞已持久化的 Workflow。

**不變式**：swallow 從不自行設定 Server 的 provisioning state，也不把 Server 標成 failed；attention 留在
Workflow 上。檢視失敗的修正（Power Configuration、Boot Media）不需要把 Server 重設回 New。

### 2.2 `wait-enrollment-settled`：等待 provider 的 enrollment 結束

MAAS 在 enlistment 環境仍在主機上執行時就建立 New Machine，enlistment 結束時才關機；在關機前
commission 會與 enlistment 開機競速。

- 經由 optional provider capability（`EnrollmentSettler`）讀取 observation：`settled`、`enrolling`，或
  power uncontrollable。沒有此 capability 的 provisioner 直接成功。
- MAAS 的判斷：Machine 已離開 New 即 settled；仍是 New 時，automatic driver 的電源即時讀取（讀取失敗則
  退回 MAAS 記錄的狀態），關機即 settled。**power uncontrollable** 只在 enlistment 的 commissioning script
  set 已結束（不再是 pending、installing、running、applying netconf）、driver 不是 automatic、且電源未記錄為
  off 時才回報——MAAS 是在那組 script 內設定實體機的 BMC driver，script 還在跑時缺 driver 只是還沒設好。
- 每 15 秒讀一次，需**連續兩次** settled 才成功（避免 enlistment 重開機時 BMC 一瞬間回報 off）；連續兩次
  power uncontrollable 即停在 `requires_attention`（`power_configuration_required`，指名 Power Configuration
  為修正方式）；20 分鐘內沒有 settled 則 `requires_attention`（`enrollment_not_settled`）。讀取失敗會重設
  兩個計數而不是失敗。Machine 被 provisioner 刪除時為不可 retry 的 `machine_not_found`。這些停止點都還沒有
  commission 任何東西。
- **requested** 的 Workflow 不等待（operator 已斷言機器可以開機）：只讀一次，只有 Machine 完全沒有 power
  driver（control `none`）才停在 `power_configuration_required`，因為 provisioner 無法開機檢視；讀取失敗或
  有任何 driver（含 `manual`）都直接成功。
- 是否等待由 Workflow 的 origin 決定，與是否 retry 無關：automatic Workflow 的每次 retry 都重新完整等待，
  所以修好 Power Configuration 後 retry 不會 commission 一台還在 enlist 的 Machine；requested Workflow 的
  retry 仍只讀一次。

### 2.3 `inspect`：有界的檢視嘗試

- 最多三次 attempt。每次先做即時的 [Server Lock](glossaries/terms/server-lock.md) 檢查，再對 provider
  發出 Inspect（MAAS commission），然後觀察。第一次 attempt 若 Server 已在 `inspecting`／`testing`（worker
  交接後的 activity retry，或 provider 自己開始了），就跟隨它而不再發第二次。automatic Workflow 的 Server 若
  已經 `ready` 直接成功；requested 的則重新檢視。
- 觀察：`ready` 為成功；`testing` 視為進展（MAAS 的 commissioning tests）；`failed`／`broken` 為該次失敗
  （發出後有兩分鐘寬限，避免讀到競態的舊投影）。
- **Stall**：`inspecting` 期間 15 分鐘沒有任何 provider event，即中止（abort）該次 attempt；MAAS 的 abort
  會把從未檢視過的 Machine 送回 New。每次 attempt 另有 45 分鐘上限（provider 沒有 event stream 時這是唯一的
  stall 判斷），到時同樣 abort。abort 後三分鐘內仍未離開 `inspecting`，停在 `requires_attention`
  （`inspect_abort_unsettled`）而不再 commission。
- Server 不是因 swallow 的 abort 而離開 `inspecting`（例如 operator 在 provisioner 中止）時，停在
  `requires_attention`（`inspect_interrupted`）；swallow 不與 operator 對抗。
- 三次用盡後停在可 retry 的 `requires_attention`：最後一次 stall 為 `inspect_pxe_unreached`，provider 回報
  失敗為 `inspect_failed`。stall 的訊息依 Server 的 power adapter 給出網路開機的修正：有 BMC 的 Server 指向
  Boot Media；沒有 BMC 的（VM）指向它自己在 hypervisor 上的開機順序（NIC 或 iPXE 開機媒體優先）。stall 的
  Server 回到 `new`，MAAS 標成 Failed commissioning 的則保持 `failed`（可再 Inspect）。
- 取消 Workflow 會 abort 進行中的檢視。
- 各 attention code 的修正方式見
  [server-enrollment.md](../../api-server/docs/development/api-contracts/api-server/server-enrollment.md)
  的 recovery 表。

### 2.4 自動檢視

API process 內、與 reconciler 分開的 sweep（reconcile interval）在以下條件全部成立時啟動
`inspect-hardware` Workflow（requested by `system`，origin `automatic`）：

- provisioner Integration 已啟用，且 `settings.autoInspect` 不是 `"false"`（每次 sweep 都重讀；未設定即開啟，
  見 [sites-integrations.md](../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)）；
- Server present、未被 lock、provisioning state 為 `new`；
- swallow 在 24 小時內首次觀察到該 Server；
- 該 Server 從未有過任何狀態的 `inspect-hardware` Workflow（一次性，失敗或取消的自動檢視不會被迴圈重試）；
- 沒有其他 Workflow 佔用該 Server。

reconciler 維持唯讀投影；被拒、busy、locked 的 Server 只記 debug log、下次再看。超過 24 小時的 `new`
Server、existing-OS enrollment 的 `deployed` Server，以及自動檢視失敗或取消的 Server，只能手動 Inspect。

### 2.5 手動 Inspect 與 retry

- `POST /servers/{id}/inspect` 使用同一個 Workflow（origin `requested`），先即時 refresh Server 再檢查：
  Server 必須 present、未被 lock，provisioning state 為 `new`、`ready`、`failed`、`broken` 或 `unknown`；
  `deployed`、`allocated` 與進行中的狀態回 `409`（inspection 會重開機，`deployed`／`allocated` 須先
  Release）。
- 該 Server 的 `inspect-hardware` Workflow 停在 `requires_attention` 時，此請求改為 retry 那個 Workflow
  （`resumed: true`，優先 retry `inspect` Task），讓期間設定的 Power Configuration 或 Boot Media 用在下一次
  commission；仍在執行，或有其他 Workflow 佔用時回 `409`。
- 對任一 Task 的 retry 都會重跑整個 `ensure-inspected` Job：enrollment wait 依 §2.2 重跑、Boot Media 重新
  讀取並套用、`inspect` 重新得到三次 attempt。一般的 Task retry 語意見
  [workflow-execution.md](workflow-execution.md)。

### 設計理由

- 用盡次數後標 failed 再要求重設回 New 不可行：Recover 只回到 Ready，MAAS 也沒有把 Failed commissioning
  送回 New 的 primitive；只依賴 MAAS 自己的 commission timeout 則會在半小時後留下 Failed 且沒有機會說明
  Boot Media（[decision 053](../decisions/053-server-enrollment-and-automatic-inspection.md)）。
- 24 小時視窗避免升級或重新啟用 Integration 時開機 operator 刻意留在 New 的 Machine（decision 053）。
- enrollment wait 以 enlistment script set 的狀態而不是固定時間判斷「缺 power driver」：實體機的 IPMI driver
  在 enlistment 後段才設定；縮短 timeout 只會讓同樣無法診斷的 attention 提早出現；VM 跳過 wait 仍會找不到
  driver 並與 enlistment 競速；把每次 retry 都當成略過 wait 會 commission 仍在 enlist 的 Machine
  （[decision 054](../decisions/054-provisioner-power-configuration.md)）。

---

## 3. Power Configuration 與 power adapters

### 3.1 所有權與 write-through

- Server 的 Power Configuration 是 provisioner 用來開關與讀取電源的 driver 及其參數（MAAS `power_type` 與
  `power_parameters`），由 provisioner 擁有。swallow 從 API（`GET`／`PUT /servers/{id}/power-configuration`）、
  CLI（`swallow servers power-configuration get|set`）與 Dashboard 即時讀取並 write-through，**從不儲存**，
  也從不回傳或記錄密碼（回應 `Cache-Control: no-store`）。需要 provider capability `powerConfiguration`。
- 密碼 write-only：省略時，driver 不變就保留、driver 改變就清除（前一個 driver 的 secret 不得帶到新 driver）；
  `""` 清除；其他值取代。
- swallow 先依 family 驗證，provisioner 再驗證並可能拒絕。寫入只改 Power Configuration：不開關電源、不測試
  provisioner 能否連線（之後讀 `GET /servers/{id}/power-state`）、也不會 resume 停在 attention 的
  `inspect-hardware` Workflow（需另外 retry 或 Inspect）。
- 屬於 provisioner VM host（MAAS pod）的 VM，電源由 VM host 管理，在 swallow 中唯讀（`editable: false`）。
- MAAS 的 Integration 帳號必須是 administrator 才能讀寫 power parameters（Boot Media 讀 BMC 帳密也依賴這點）。
- 欄位與錯誤碼見
  [server-detail-actions.md](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md#power-configuration)。

### 3.2 Power adapter 與 driver family

程式以 **power adapter** 建模（"power adapter" 是實作詞彙，不是 operator 語言）：

- 每個 driver family 一個 adapter：`bmc`（driver `ipmi`、`redfish`；參數 address、username、password）與
  `virsh`（libvirt VM；參數 hypervisor URI、power ID、選用 password）。registry 依序登錄 `bmc`、`virsh`，
  把 driver 解析到 adapter，由 adapter 驗證並正規化設定。
- 只有部分 family 具備的功能是 adapter 的 optional extension，以 type assertion 取得。目前唯一的 extension
  是 `bmc` family 的 out-of-band BMC access，`redfish` Boot Media 用它連 BMC。呼叫端向 adapter 要 extension，
  不比對 driver 名稱或 VM host 成員資格；VM host 成員即使 VM host 的 driver 在 `bmc` family 也沒有 BMC。
- registry 不認得的 driver 照原樣回報、沒有 family、沒有 extension，也不能被寫入（不在可選 `drivers` 內）。
- **Redfish 是 BMC 的功能，不是 power type**：provisioner 的 `redfish` driver 只是 `bmc` family 開關電源的
  一種方式；Redfish Boot Media 是對 BMC 的 probe，與 driver 無關（§4.3）。
- provisioner detail 只有 `bmc` family 顯示 `BMC` 區塊；其他 driver（例如 `virsh`）顯示不含密碼的 `Power`
  區塊，不把 libvirt URI 稱為 BMC。

### 3.3 Control 分類

| control | 條件 | 意義 |
| --- | --- | --- |
| `none` | 沒有 driver | provisioner 無法開關也無法讀取電源，不能 inspect 或 deploy |
| `manual` | MAAS `manual` | 由人開關電源，狀態讀不到 |
| `automatic` | 其他所有 driver，含 swallow 不會寫的 driver（例如 VM host 自己的 driver） | provisioner 自行開關並讀取電源 |

enrollment wait 與 inspection 只需要知道 control（§2.2）。

### 3.4 `virsh` address 與 provisioner 的 SSH 存取

- swallow 只接受乾淨的 `qemu+ssh://[user@]host[:port]/system`：不得含密碼、query（例如 `?keyfile=`）、
  fragment、其他 scheme 或 path；power ID 必填，為 domain 名稱或 UUID；不接受 `username`（SSH 帳號放在
  address）。理由是 MAAS 拒絕額外的 URI 參數，而 session URI 或本機 socket 會在執行動作的 rack 上解析。
- 連線的是 provisioner 而不是 swallow：MAAS rack controller 需要能以 SSH 登入 hypervisor 上的帳號（金鑰在
  MAAS snap 的 `/var/snap/maas/current/root/.ssh`）。swallow 不經 SSH 驅動 libvirt 的電源，也不持有
  hypervisor 的 secret。
- 共置 MAAS 的 installation 讓 rack 接受新的 hypervisor host key，並把 rack 的公鑰記成 provisioner
  Integration setting `virshSshPublicKey`（公鑰，不是 secret）；§1.3 的 VM enrollment 把它授權給 Hypervisor
  的登入帳號，因此 installation 之後不需要離開 swallow 的步驟。手動設定的 `virsh` Power Configuration，
  provisioner 的 SSH 存取仍是前置條件。

### 設計理由

- Redfish 不設為第三種 power type：參考 BMC 以 IPMI 驅動但仍提供 Redfish
  （[decision 047](../decisions/047-redfish-boot-media.md) 已否決以 `power_type == redfish` 為閘門）。
- swallow 不經 SSH 驅動 libvirt 電源、不儲存 power credential：前者重複 provisioner 的 power driver 並把
  hypervisor SSH key 帶進 swallow，後者違反 provisioner 擁有 credential 的規則
  （[decision 054](../decisions/054-provisioner-power-configuration.md)）。VM enrollment 只碰 provisioner
  做不到的部分（讀 domain、Boot Media），不改變這一點（[decision 055](../decisions/055-libvirt-virtual-machine-enrollment.md)）。

---

## 4. Boot Media

### 4.1 Setting 與所有權

- 每台 Server 的 swallow-owned 欄位：Boot Media setting（`enabled`、`isoId`、`updatedAt`、
  `lastAppliedAt`／`lastAppliedBy`（`preflight` 或 `ensure`）、`bootOverride`、`lastError`／`lastErrorAt`）、
  Redfish capability、libvirt capability，以及進行中 preflight 的 apply 紀錄。reconcile 不覆寫它們。
- setting 只記意圖與歷史，從不代表即時狀態；ISO 是否掛著、開機導向是否生效，一律從 BMC 或 Hypervisor 讀取
  （Read 的 `?live=true`）。
- operator 從不輸入 ISO URL：setting 指名一個 **同一 provisioner Integration** 的 Boot ISO，URL 由它推導
  （§5.3）。套用失敗只記錄 `lastError`，從不改變 `enabled` 或選定的 Boot ISO；成功的套用清除 `lastError`。
- 路由與欄位見
  [server-detail-actions.md](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md#boot-media)；
  CLI 為 `swallow servers boot-media get|enable|disable|redfish-probe`。

### 4.2 Method

Boot Media 的 method 由 Server 的 Power Configuration 即時決定（provisioner 必須具備 Power Configuration
capability，否則沒有 method）：

- **`redfish`**：driver 解析到具有 BMC extension 的 adapter（`bmc` family、非 VM host 成員）且 provisioner
  持有 BMC address。BMC 的 address、帳號與密碼每次呼叫時從 provisioner（MAAS `power_parameters`）讀取，只在
  記憶體中使用，不儲存、不記錄、不從這些路由回傳。provisioner 不肯透露（帳號不是 administrator）時回報為
  無法取得 BMC 連線。
- **`libvirt`**：driver 為 `virsh`、非 VM host 成員、有 power ID，且 address 的 host 以 address、hostname 或
  FQDN 對應到同一 Site 的另一台 present Server（即 Hypervisor）。登入帳號取 address 中的 user，沒有時取
  Hypervisor 的 effective Server Default User；domain 為 power ID。
- 其他情況沒有 method，不能啟用 Boot Media。

Read 回應的 `method` 由已儲存的 probe 推導：libvirt probe 找到 Hypervisor 時為 `libvirt`，否則 Redfish probe
找到 BMC 時為 `redfish`。

### 4.3 Capability probe

- API process 的 sweep 每 `api.redfishProbeInterval`（預設 10 分鐘，`SWALLOW_API_REDFISH_PROBE_INTERVAL`）
  執行一次，probe 所有 present、已有 provisioning 投影，且 Redfish capability 不存在（新 enroll 的 Server）或
  超過 `api.redfishProbeMaxAge`（預設 24 小時，`SWALLOW_API_REDFISH_PROBE_MAX_AGE`；BMC 韌體更新可能改變它
  支援的功能）的 Server，同時最多四台。也可按需 `POST /servers/{id}/redfish/probe`。
- 是否 probe **不看 power driver**：以 IPMI 驅動的 BMC 也可能提供 Redfish。
- Redfish 結果 `support`：`supported`（Redfish 有回應，且 host System 有 virtual CD 與可導向 CD 的 boot
  override）、`unsupported`、`unreachable`（沒有 Redfish service 或拒絕 provisioner 的 BMC 帳號）、`no_bmc`。
  libvirt VM 的 Redfish 結果記為 `no_bmc`，另做 libvirt probe（`supported`／`unsupported`（沒有此 domain）／
  `unreachable`／`no_hypervisor`）。
- probe 無法連上 provisioner 時不寫入任何結果，以免留下錯誤的舊資料；BMC 或 Hypervisor 本身不可達則是一個
  成功的 probe，結果為 `unreachable`。

### 4.4 Enable preflight 與 disable

- **Enable 就是 preflight**（`PUT /servers/{id}/boot-media`，`enabled: true` 與 `isoId`）：Boot ISO 必須屬於
  Server 的 provisioner Integration 且可用（`redfish` 需要 Boot Media base URL 與檔案；`libvirt` 只需要檔案），
  Server 必須未被 lock（`libvirt` 另要求 Hypervisor 未被 lock）。swallow 重新 probe、掛上 Boot ISO、把開機
  導向它、讀回兩者，**全部成功後**才把 `enabled: true` 與該 `isoId` 存起來。支援程度因硬體型號甚至 BMC
  韌體版本而異，所以必須在該台 Server 上實際試過。
- 對已啟用的 Server 再 enable 會重新套用；換 `isoId` 以同樣方式切換，先退出前一個 Boot ISO。
- 每台 Server 同時只跑一個 preflight（apply 紀錄；超過十分鐘視為已遺棄），進度依序為 `probing`、
  `ejecting`、`mounting`、`settling`、`directing`、`verifying`，不需要的階段略過。request 持續到 preflight
  結束，client 以 Read 的 `apply` 追蹤進度。
- **Disable** 同樣要求 Server 未被 lock，且不能與進行中的 preflight 同時執行。它先存 setting（operator 的意圖
  不得依賴 BMC），再盡力退出 ISO 並清除開機導向，回報 `reverted`／`revertError`；保留最後的 Boot ISO 作為歷史。
  停用後該 Server 的 OS 佈署不再碰它的 BMC。

### 4.5 `redfish` method：掛載與開機導向

BMC 廠商行為集中在單一 adapter（`internal/adapters/bmc/redfish`）；新的 BMC 家族可能需要新策略，由它的
preflight 以 BMC 自己的錯誤訊息失敗而發現。

- **探索**：每次呼叫都重新探索 BMC 資源（host System、virtual CD、actions），不信任儲存的路徑。host System 依序
  以 provisioner 的 System hint（MAAS `node_id`）、Server 的 hardware UUID 辨識，否則為「唯一有 `Boot` 的
  System」；GPU baseboard 等非 host System 不會被選中。讀不到的 System 會略過，只要仍能辨識 host（host 關機時
  GPU baseboard 會回 HTTP 500，而這正是佈署開始時）。忙碌的 BMC（HTTP 503、連線重設）會重試到 context 結束。
- **掛載**：需要時先啟用廠商的 remote media（AMI 的 OEM action `AMIVirtualMedia.EnableRMedia`）。InsertMedia
  不帶 `UserName`／`Password`（參考 BMC 對 HTTP 拒絕它們），transfer protocol 跟隨 URL scheme；許多 BMC 只接受
  port 80 的 `http://`（§5.3）。若某個 virtual CD 掛著同一路徑但來自其他 host（base URL 改過），先退出。
- **Mount settle**：剛掛上的 ISO 要等三分鐘、確認仍然 inserted 才算完成，期間不讓 host 開機；BMC 若掉了這個
  mount，就再掛一次（仍記著 image 名稱但 `Inserted: false` 的裝置視為空閒）。ISO 已經掛著的 Server 不付這段
  等待。preflight 整體上限六分鐘。
- **開機導向**，依韌體能力選擇：
  1. **AMI Aptio fixed boot order**：把 "UEFI USB Device" 群組（BMC virtual CD 所在）排到 BIOS fixed boot
     order 第一，persistent（`Continuous`）。它撐得過 MAAS 的 force-PXE：PXE 失敗時 BIOS 退回群組順序；USB
     群組沒有 ISO 時則落到磁碟。
  2. BIOS 把 virtual CD 列為 UEFI boot option 時：放到 `BootOrder` 第一，並設 `BootNext`（`Once`）。
  3. 其他：DMTF `Cd` override，允許時用 `Continuous`。
  只要可行，都另外加上一次性的 `BootNext` 到 virtual CD。最後讀回：ISO inserted 且 override 指向 virtual CD
  才算成功，否則以 BMC 回報的狀態拒絕。
- 採 Aptio 策略時，Server 只要 ISO 還掛著就一直先開 ISO，這是刻意的常態：MAAS 對已 deployed 的 Machine 回答
  從本機開機（UEFI 路徑見 §5.2）。
- **已知限制**：在參考 Server 上，fixed boot order 的寫入在 BIOS 套用後沒有保留（下次 POST 又回報 "UEFI
  Hard Disk" 第一），因此每次佈署前都重寫，而那些佈署只能靠 §4.8 的 boot check 開到 ISO。在 Aptio 上改以
  `BootOrder` + `BootNext` 優先能否避免，尚未確認。

### 4.6 `libvirt` method

- swallow 以 Deployment Key 登入 Hypervisor（§1.3），把 Boot ISO 上傳為 Hypervisor `default` storage pool 中的
  volume `swallow-ipxe-<isoId>.iso`（每個 Boot ISO 一份）。沒有 `default` pool 時比照 `virt-install` 定義為
  `/var/lib/libvirt/images` 的 directory pool，inactive 時啟動它。volume 內容以 SHA-256 比對，不同就取代，
  所以中斷的上傳或 rebuild 後的 ISO 會在下一次套用時更新。
- 把 volume 放上 domain 的 CD-ROM（沒有 CD-ROM 時新增），並在 domain 的 **persistent definition** 把 CD-ROM
  設為第一開機；執行中的 domain 從下次啟動起使用。`bootOverride` 為 `Continuous`，直到停用。
- 沒有 settle 等待，也沒有部署期間的補救：provisioner 的 `virsh` driver 以 `virsh start`／`destroy` 開關
  domain，從不改開機順序，沒有東西會把它還原。
- Disable 退出 CD-ROM 並移除它的開機順序，其他開機裝置維持原順序。
- 改 domain 定義是對 Hypervisor 的 mutation，因此 enable 與 disable 除了 VM 本身，也要求 Hypervisor 未被 lock。

### 4.7 `ensure-boot-media` Task

因為 BMC 可能之後失去 mount 或 override（BMC 重啟會卸載 ISO、BIOS 會重排開機順序、provisioner 自己的
power-on 會覆寫一次性設定），每次 hardware inspection 與 OS 佈署前都有一個 `internal` Runner 的
`ensure-boot-media` Task 重新套用。

- **OS 佈署**（Deploy OS、OS image verification、Platform 的 `ensure-os` Job）：建立 Workflow 時，對**受理當下**
  Boot Media 已啟用的每台 Server，在同一個 Job 內加入 `ensure-boot-media-<serverId>`，該 Server 的
  `provision-os` 依賴它；Task 參數凍結 Boot ISO 的 `isoId` 與 `isoUrl`，`provision-os` 另帶相同 URL 的
  `bootMediaIsoUrl`（§4.8）。執行時重新讀 setting（期間已停用就不碰），然後**無條件**重新套用 mount 與開機導向，
  不相信「已經 ready」能撐到 host 開機。它不重開機：provisioner 的佈署本來就會開機。沒有 Boot Media 的 Server
  的 Workflow 與 Boot Media 存在之前完全相同。
- **Hardware inspection**：Task 帶 `resolveLive`、不凍結 ISO，執行時才讀 Server 的 Boot Media，讓 inspection
  停在 attention 後才啟用的 Boot Media 也能被 retry 用上；停用時直接成功、不碰 BMC。
- 失敗：BMC 或 Hypervisor 不可達、拒絕，為可 retry 的 `boot_media_ensure_failed`，依賴它的 `provision-os` 或
  `inspect` 不會執行。沒有可用的 Boot ISO（沒選、檔案不存在、`redfish` 沒有 base URL）為
  `boot_media_not_configured`：OS 佈署凍結的 ISO 不會自己變好，所以**不可 retry**（須選好 Boot ISO 再重新
  佈署）；inspection 的即時解析則**可 retry**。
- 成功時記錄 `lastAppliedBy: ensure`。只有 inspection 與 OS 佈署會 re-ensure；provisioner 的其他 power-on
  （例如 rescue、hardware test）不會。

### 4.8 部署開機的補救（僅 `redfish`）

啟用 Boot Media 的 Server，其 `provision-os` 的 observer 以第一次讀到 Deploying（provisioner 開機的時間點）
為起點監看部署開機：

- **Media check（兩分鐘）**：BMC 已不再掛著 ISO 時立刻掛回——不 settle、不重開機——讓 boot check 有 mount
  可依靠（某些 BMC 會在 provider power-on 時丟失剛掛上的 mount）。
- **Boot check（十分鐘，超過觀察到最慢的 POST 8.7 分鐘）**：provider 最新的 event 仍是佈署開始（host 從未網路
  開機進入佈署，例如 BIOS 開了磁碟），就重新套用 Boot Media（mount 含 settle、開機順序、一次性開到 virtual
  CD），並**經 Redfish**（`ForceRestart`，關機時為 `On`）重開 host；45 秒後若 restart 掉了 mount 就再掛回。
  provisioner 仍保持 Deploying，重開的 host 接上同一次佈署；stage-stall 的計時從重開時重新開始。
- 每次觀察最多重開一次，只有 boot check 會重開；補救不改 Task 狀態，失敗只記 log；仍然沒有進展的佈署由
  stage-stall 偵測回報（見 [os-provisioning.md](os-provisioning.md)）。
- boot check 需要 provider 的 event stream；provider 沒有 event 時只做 media check。用到 boot check 的佈署會
  多花十到十五分鐘。
- 重開經由 Redfish 而不經 provisioner，因為 provisioner 的 IPMI power-on 正是讓剛掛上的 mount 脫落、並重寫
  force-PXE 旗標的事件。

### 設計理由

- 只有 preflight 不夠（BMC 重啟卸載 ISO、BIOS 重排開機順序、MAAS 在 power-on 時覆寫一次性設定）；只有
  Workflow step 也不夠（operator 必須在依賴它之前知道這台硬體或韌體做不到）
  （[decision 047](../decisions/047-redfish-boot-media.md)）。
- 不到處用 `Cd` override：它讓 AMI Aptio 主機卡住並破壞 BIOS 的群組順序；只加長 provider power-on 前的
  settle 也不夠：三分鐘仍曾在 MAAS 的 IPMI power-on 時掉 mount，沒有任何等待被證明足夠（decision 047）。
- `libvirt` method 上傳 volume 而不是讓 QEMU 以 HTTP CD-ROM 讀 URL：後者依賴 QEMU 的 curl block driver，且需要
  hypervisor 連得到 swallow 的 Boot Media URL（[decision 055](../decisions/055-libvirt-virtual-machine-enrollment.md)）。

---

## 5. Boot ISO

### 5.1 建置

- operator 在 Provisioning 頁選 provisioner Integration、給 MAAS rack 位址（Dashboard 以 Integration endpoint
  的 host 預填；MAAS region 與 rack 同機時即為 rack）與名稱，`POST /provisioning/boot-isos` **同步**建置
  （數秒，不需要 durable Workflow）。名稱在同一 Integration 內唯一（不分大小寫）、1–63 字元；rack 位址為 IPv4
  或 hostname，可帶 `:port`（預設 5248），swallow 不解析也不連線。
- script 一律由 swallow 的**固定範本**產生，沒有 free-form script：iPXE script（BIOS）與 GRUB 設定（UEFI）
  都只填入 rack 位址（§5.2）。
- **runtime 不需要 compiler**：api-server image 內含從固定 tag（iPXE `v2.0.0`，並核對 commit）編譯、不內嵌
  script 的 `ipxe.lkrn`，iPXE 的 `genfsimg`，以及 `grub-common`（`grub-mkstandalone`）、`grub-efi-amd64-bin`、
  mtools、xorriso、ISOLINUX／syslinux。每個 ISO 以 `genfsimg -s autoexec.ipxe` 打包：BIOS 的 `ipxe.lkrn` 以該
  script 為 initrd；UEFI 的 EFI binary 是為這個 ISO 以 `grub-mkstandalone` 建的 GRUB（設定內嵌在 memdisk）。
  image 仍會建 `ipxe.efi`，但不打包。
- 建置在私有目錄完成後 rename 到位，URL 永遠不會提供寫到一半的 ISO。
- installation 缺少任何前提（Boot Media base URL、可寫的 Boot Media 目錄、iPXE asset、GRUB、打包工具）時，
  列表回報 `builder.available: false` 與原因，建置回 `409`。
- 每個紀錄保存 `script`、`grubConfig`、`chainUrl`、`uefiChainUrl`、`ipxeVersion`、大小與 SHA-256、`inUseBy`
  與建置／rebuild 的時間與操作者。欄位見
  [boot-isos.md](../../api-server/docs/development/api-contracts/api-server/boot-isos.md)。

### 5.2 BIOS 與 UEFI 的 hand-off

韌體決定走哪條路徑；Boot ISO 不能用來選擇 Server 的韌體模式。

- **BIOS（ISOLINUX → iPXE）**：從站點 DHCP 取得租約，`next-server` 設為 rack（MAAS 後續 script 以它組 URL），
  chain `http://<rack>:<port>/ipxe.cfg`，不使用 DHCP 的 boot filename，所以站點的 PXE server 不會接手；DHCP
  失敗或 chain 返回就重試（Ctrl-B 進 iPXE shell）。
- **UEFI（x86_64，ISO 自己的 GRUB）**：載入 `efinet`、`http` 與分割表模組 `part_gpt`、`part_msdos`；以
  `net_bootp` 取得站點 DHCP 租約（每十秒重試，直到 `efinet0`…`efinet15` 之一有租約），取第一張有租約的網卡，
  `net_default_interface` 設為 `<card>_dhcp`（GRUB 自己的變數命名，讓 MAAS 的 `BOOTIF` 能解出
  `net_default_mac`）、`net_default_server` 設為 rack，兩者 export 給 MAAS 的設定，再經 HTTP 載入 rack 的
  `grub/grub.cfg-<mac>`。這是 MAAS 原生的 UEFI 路徑：MAAS 記錄 `bios_boot_method=uefi`、以該 MAC 辨識
  Machine（或 enlist 未知機器），下次 commission 給它 EFI system partition。MAAS 沒有開機時 GRUB 退回韌體的
  下一個開機裝置。
- **已 deployed 的 Machine 走同一路徑**：Boot ISO 排第一時，MAAS 回應本機開機設定，搜尋每顆磁碟的 EFI system
  partition 並 chainload 已安裝的 loader；GRUB 會自動載入檔案系統模組但不會載入分割表模組，所以必須事先載入
  `part_gpt` 與 `part_msdos`，否則搜尋不到任何分割區。
- UEFI 路徑不依賴站點 DHCP 指向任何地方：GRUB 接受任何租約，以設定內的位址連 rack。rack 必須在 boot port 對
  租約所在網路提供 HTTP。
- iPXE 與 swallow 的 GRUB 都**沒有簽章**：開 Boot ISO 的 Server 必須關閉 Secure Boot。

### 5.3 提供 ISO 檔案

- `GET`／`HEAD /boot-media/ipxe/{isoId}/swallow-ipxe.iso` **不需認證**（BMC 無法送 token，而且每次開機都
  依需要串流讀取 ISO），支援 HTTP Range（BMC HTTP virtual media 需要），未知或格式錯誤的 id（必須是 UUID）
  回 `404`。ISO 不含 secret。
- 掛載 URL 為 `<api.bootMedia.baseURL>/boot-media/ipxe/<isoId>/swallow-ipxe.iso`，由 installation 固定。許多
  BMC 只接受 port 80 的 `http://`，且 image 不能位於 server 根目錄（AMI MegaRAC），所以 installation 必須在 BMC
  網路連得到的位址的 port 80 發佈 `/boot-media/`（production 由 reverse proxy 轉送）。
- 設定：`api.bootMedia.baseURL`（`SWALLOW_API_BOOT_MEDIA_BASE_URL`；未設定時不能建置或以 `redfish` 掛載）、
  `api.bootMedia.dir`（`SWALLOW_API_BOOT_MEDIA_DIR`，持久且 API process 可寫；worker 以唯讀方式讀取，供
  `libvirt` 上傳與 inspection 的即時解析）、`api.bootMedia.ipxeDir`（`SWALLOW_API_IPXE_DIR`）。

### 5.4 原地 rebuild

- `POST /provisioning/boot-isos/{id}/rebuild` 以**目前的範本**與 ISO 已存的 rack 位址重新產生兩條路徑，同步
  建置後原子地取代同一 id 的檔案；id 與 URL 不變，因此所有使用它的 Boot Media setting 與已受理的 Workflow
  （含已受理的佈署與 inspection）都繼續有效，從下次開機起使用新 ISO。rebuild 失敗時保留舊檔與舊紀錄；同一
  process 內 rebuild 一次一個。
- `redfish` Boot Media 串流 ISO URL，下次開機即使用新檔；`libvirt` 在 Hypervisor 上保有一份 volume，下一次
  `ensure-boot-media` 因 SHA-256 不符而取代它（§4.6）。
- 在 UEFI hand-off 之前建的 ISO 沒有 GRUB（`grubConfig` 與 `uefiChainUrl` 為空），UEFI 韌體仍走 iPXE；Boot
  ISOs 頁面標示 "UEFI needs rebuild" 並提供 Rebuild。BIOS 不受影響。rebuild 後，經舊路徑 commission 過的 UEFI
  Server 仍需重新 inspect（§6）。
- 換 iPXE 版本要 rebuild 才會用上；每個 ISO 記錄自己的 `ipxeVersion`。
- CLI 目前提供 `swallow provisioning boot-isos list|get|create|delete`，尚無 rebuild 指令。

### 5.5 刪除與已退場的 installation ISO

- 有任何 Server 的已啟用 Boot Media 使用某個 Boot ISO 時，刪除回 `409`；須先停用那些 Server 的 Boot Media 或
  換成其他 Boot ISO。刪除會一併移除檔案。
- installation 提供的單一 ISO（`api.bootMedia.isoPath`／`SWALLOW_API_BOOT_MEDIA_ISO_PATH`）與固定路由
  `/boot-media/ipxe/swallow-ipxe.iso` 已移除；設定值只會在啟動時被警告並忽略。舊路徑的 URL 只保留來退出 BMC 上
  先前掛著的 ISO。
- 在 Boot ISO 存在之前啟用的 setting 沒有 `isoId`：Server 頁面要求選一個（帶 `isoId` 重新 enable），在那之前它
  的 OS 佈署的 ensure Task 以 `boot_media_not_configured` 失敗。

### 設計理由

- 不為每個 ISO 以 `EMBED=` 編譯 iPXE：那需要 runtime image 有 C toolchain、每次要數分鐘，而 `genfsimg -s` 以預先
  編譯的 binary 數秒就得到相同結果；不提供 free-form script：已驗證的範本涵蓋 external DHCP，打錯字只會表現為
  一台永遠開不起來的主機；不保留 installation ISO 作為預設：同一件事兩個來源會讓手工路徑繼續存在
  （[decision 049](../decisions/049-boot-iso-builder.md)）。
- UEFI 不用 iPXE chain MAAS 的 `grubx64.efi`：實作並在 lab 測試後否決，GRUB 拿不到 iPXE 的網路狀態而停在
  prompt；讓 MAAS 記成 `uefi` 是因為 MAAS 依最後記錄的 boot method 決定 layout，經 `ipxe.cfg` 時記成 `ipxe`
  並視同 BIOS，UEFI 部署就會在 curtin 的 bootloader 步驟失敗
  （[decision 057](../decisions/057-uefi-boot-iso-hand-off.md)）。
- 原地 rebuild 而不是建新 ISO 再逐台切換：後者讓舊 ISO 對仍使用它的人保持錯誤，且每台 Server 都要改一次 Boot
  Media（decision 057）。

---

## 6. 需要重新檢視的儲存配置（inspection-required guard）

經舊路徑（UEFI 韌體經 iPXE 開機）commission 的 Machine，MAAS 給的是 BIOS layout；之後改以 UEFI 開機時，每次
**磁碟**部署都會在 curtin 失敗。swallow 偵測這種狀態並在佈署開始前拒絕：

- **偵測由 provider adapter 負責，且以證據為準**：adapter 標記「layout 在重新檢視之前無法進行磁碟部署」的
  Machine，並附 swallow 撰寫的原因。MAAS 的條件：Machine 以 UEFI 開機（`bios_boot_method=uefi`）、開機碟有分割
  區，且沒有 EFI system partition（掛在 `/boot/efi` 的 FAT）。沒有 layout 或資料缺漏的 Machine 從不被標記。
  結果投影為 `provisioning.inspectionRequired`／`inspectionRequiredReason`（見
  [servers-list.md](../../api-server/docs/development/api-contracts/api-server/servers-list.md)），重新 inspection
  重建 layout 後清除。
- **磁碟部署在開始前被拒**：受理、platform validation 與 Step 執行共用的部署 preflight，在 lock 檢查的同一次即時
  讀取中讀這個旗標，以 target issue `inspection_required`（`409`）拒絕 disk 部署；RAM 部署不分割磁碟，不受影響
  （[Deploy Target](glossaries/terms/deploy-target.md)）。明確 Retry 失敗的磁碟部署時，在 Recover 之前先檢查，以
  不可 retry 的 `inspection_required` 結束 Task，讓 Workflow 釋放 Server 去 Inspect，而不是先清空 Machine 再被拒
  （Retry 與 Recover 的完整規則見 [os-provisioning.md](os-provisioning.md)）。
- **失敗的部署指出原因**：`deployment_failed` 的訊息帶 provisioner 的錯誤描述，以及該次 attempt 安裝 log 中最
  具體的一行（optional capability `InstallationFailureReader`；MAAS 讀 Step attempt 開始後最新的失敗安裝結果，只
  下載它的 `install.log`，該行最多 300 字元、移除 URL 帳密，提到 password、token 或 secret 的行一律不用）。讀不到
  時省略，且讀 log 從不改變 Step 結果。訊息格式見
  [provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md)。

### 設計理由

- 不在部署前把 EFI system partition 補進 MAAS layout：那是修補症狀，每台新的 UEFI Server 都需要，而且 MAAS 會繼續
  認為 Machine 以 BIOS 開機；只警告也不行：這種 Server 的每次磁碟部署都會在數分鐘後失敗，接受它沒有任何好處
  （[decision 057](../decisions/057-uefi-boot-iso-hand-off.md)）。

---

## 7. 程式位置對照（導航用）

### Go（`api-server`）

| 職責 | 路徑 |
| --- | --- |
| `inspect-hardware` Workflow 組裝、手動 Inspect 的 gate 與 resume | `internal/app/hardware_inspection_adapter.go` |
| `wait-enrollment-settled` 與 `inspect` Task | `internal/app/inspection_steps.go` |
| 自動檢視 sweep、`autoInspect` setting、24 小時視窗 | `internal/core/provisioning/application/hardware_inspection.go` |
| 可檢視的 state（inspection policy） | `internal/core/provisioning/domain/inspection_policy.go` |
| MAAS enrollment observation 與 existing-host enrollment | `internal/adapters/osprovisioning/maas/enrollment.go` |
| enroll-bundle 與 enrollment script | `internal/core/provisioning/application/host_enrollment.go` |
| 不需認證的下載（script、CLI） | `internal/app/enrollment_downloads.go` |
| `enroll-virtual-machines` Workflow 與 `enroll-virtual-machine` Task | `internal/app/virtual_machine_enrollment.go`、`internal/core/provisioning/application/virtual_machine_enrollment.go` |
| MAAS machine registration（`commission=false`） | `internal/adapters/osprovisioning/maas/machine_registration.go` |
| Hypervisor 角色與登入 | `internal/core/server/domain/hypervisor.go` |
| libvirt 存取（domain、pool、volume、CD-ROM） | `internal/adapters/hypervisor/libvirt/host.go`、`internal/adapters/hypervisor/libvirt/domainxml.go` |
| Power Configuration model、control、power adapter 與 registry | `internal/core/provisioning/domain/power.go`、`internal/core/provisioning/domain/power_adapters.go` |
| Power Configuration use case 與 MAAS 實作 | `internal/core/provisioning/application/power_configuration.go`、`internal/adapters/osprovisioning/maas/power_configuration.go` |
| Boot Media model（setting、capability、method、phase） | `internal/core/server/domain/boot_media.go` |
| Boot Media use case（probe、preflight、ensure、部署開機補救、capability sweep） | `internal/core/server/application/boot_media.go` |
| method 解析、Boot ISO 檔案路由、capability sweep 迴圈 | `internal/app/boot_media_adapter.go` |
| `ensure-boot-media` Task、OS 佈署的 Task 插入、部署開機監看 | `internal/app/boot_media_steps.go` |
| Redfish adapter（探索、掛載、開機導向策略） | `internal/adapters/bmc/redfish/controller.go`、`internal/adapters/bmc/redfish/boot.go` |
| Boot ISO model 與 iPXE／GRUB 範本 | `internal/core/provisioning/domain/boot_iso.go` |
| Boot ISO 建置、rebuild、刪除 | `internal/core/provisioning/application/boot_isos.go` |
| `genfsimg`／`grub-mkstandalone` 打包與檔案 | `internal/adapters/bootiso/genfsimg/boot_iso_builder.go` |
| MAAS UEFI layout 偵測（`inspectionRequired`） | `internal/adapters/osprovisioning/maas/mapping.go` |
| 部署 preflight 的 `inspection_required` | `internal/core/provisioning/application/deployment_targets.go` |
| MAAS 安裝 log 的失敗行 | `internal/adapters/osprovisioning/maas/installation.go` |
| 設定（`bootMedia`、`cliBinary`、`redfishProbe*`） | `config/config.go`、`config/env.go`、`config/defaults.go` |

### 其他

| 內容 | 路徑 |
| --- | --- |
| iPXE 固定版本與 GRUB／打包工具 | `deploy/production/api-server.Dockerfile` |
| CLI：Boot ISO、Boot Media、Power Configuration、VM enrollment、enroll-bundle | `cli/internal/command/bootisos.go`、`bootmedia.go`、`powerconfig.go`、`virtualmachines.go`、`integrations.go` |
| CLI：主機端 `swallow servers enroll`（MAAS） | `cli/internal/hostenroll/hostenroll.go` |

---

## 8. 相關文件

- 決策：[decision 047 Redfish Boot Media](../decisions/047-redfish-boot-media.md)、
  [decision 049 Boot ISO builder](../decisions/049-boot-iso-builder.md)、
  [decision 053 Server Enrollment 與自動檢視](../decisions/053-server-enrollment-and-automatic-inspection.md)、
  [decision 054 Power Configuration](../decisions/054-provisioner-power-configuration.md)、
  [decision 055 libvirt VM enrollment](../decisions/055-libvirt-virtual-machine-enrollment.md)、
  [decision 057 UEFI Boot ISO hand-off](../decisions/057-uefi-boot-iso-hand-off.md)。
- 語言：glossary [Server Enrollment](glossaries/terms/server-enrollment.md)、
  [Power Configuration](glossaries/terms/power-configuration.md)、[BMC](glossaries/terms/bmc.md)、
  [Boot Media](glossaries/terms/boot-media.md)、[Boot ISO](glossaries/terms/boot-iso.md)、
  [Hypervisor](glossaries/terms/hypervisor.md)、[Machine](glossaries/terms/machine.md)、
  [OS Provisioning State](glossaries/terms/os-provisioning-state.md)、
  [Server Lock](glossaries/terms/server-lock.md)、[Deployment Key](glossaries/terms/deployment-key.md)、
  [Server Default User](glossaries/terms/server-default-user.md)、[Workflow](glossaries/terms/workflow.md)、
  [Task](glossaries/terms/task.md)。
- API 契約（`api-server` 擁有）：
  [server-enrollment](../../api-server/docs/development/api-contracts/api-server/server-enrollment.md)、
  [server-detail-actions](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md)、
  [boot-isos](../../api-server/docs/development/api-contracts/api-server/boot-isos.md)、
  [provisioning](../../api-server/docs/development/api-contracts/api-server/provisioning.md)、
  [sites-integrations](../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)、
  [servers-list](../../api-server/docs/development/api-contracts/api-server/servers-list.md)。
- 相關規格：[os-provisioning.md](os-provisioning.md)（OS 佈署、定址、回復）、
  [workflow-execution.md](workflow-execution.md)（Task 重試、取消、rerun）、
  [platform-deployment.md](platform-deployment.md)（`ensure-os` Job）。
