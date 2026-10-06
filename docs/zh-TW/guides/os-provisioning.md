# OS provisioning

[English](../../../docs/en/guides/os-provisioning.md) · [文件首頁](../README.md)

OS provisioning 會透過 durable Workflow 改變 provider-owned machine state。
Deployment request 先記錄 intent，再由 provisioner 與 automation stages 將 Server
converge 到 requested result。

## 準備 image

在 **Provisioning → Images**：

1. 選擇 Site／provisioner catalog。
2. 使用既有 provider image，或 upload supported custom image。
3. 若為 custom image，設定它的 default user（其 cloud-init 建立的帳號；synced 的 Ubuntu、CentOS、
   RHEL image 已有內建值）。swallow 會以此帳號登入已佈署的 Server，見
   [SSH key 與 image 登入帳號](ssh-keys.md)。
4. 佈署前 verify image。
5. 檢查 verification failure 與 target compatibility。

從 Dashboard 上傳 image 仍在開發中，release build 會隱藏 Upload，見
[開發中的功能](dashboard.md#開發中的功能)。

Upload limit、content type 與 provider requirement 定義於
[active provisioning contract](../../../api-server/docs/development/api-contracts/api-server/provisioning.md)。
刪除 image 時會依 contract 操作 owning provider 或 overlay。

## Deployment template

Deployment Template 保存可重用的 operator choice，不是 executable automation。
它可以預選 image 與 settings，但 deployment request 仍會驗證每個 current target。
Template 絕不繞過目前 eligibility。

Dashboard 的 template 功能仍在開發中；release build 會隱藏它，佈署使用 custom configuration。
API 與 CLI 不受影響。

## 佈署 OS

Dashboard wizard 會驗證：

- 選擇的 Servers 屬於目標 Site 且可 deploy。
- Image 可用，且對 target 已完成 verification。
- Requested disk／RAM deploy target 受支援。
- Network settings 與 release options 有效。
- 沒有 lock 或 active conflicting Workflow 阻擋 Server。

送出後會為 targets 建立 durable Workflow。請到 **Workflows** 追蹤；request
成功回傳不代表作業已完成。Server 佈署期間，其詳細頁會顯示 provisioner 目前的安裝階段（例如
*Configuring OS*）。若該階段 25 分鐘沒有推進，Workflow step 會要求處理，而不是等到兩小時上限；
provider 端的佈署仍保持執行，供你檢查、retry 或 release。

## 沒有 provisioner DHCP 的網路：Boot Media

有些 Server 所在網路的 DHCP 由現場提供、而不是 provisioner，因此無法 PXE 開機進入 provisioner。
對這類 Server，BMC 會以 virtual media 掛載 swallow 建置的
[Boot ISO](../../development/glossaries/terms/boot-iso.md) 並優先開機。ISO 先從
現場 DHCP 取得位址，再 chain 到該 Server 的 provisioner。

- **每個 provisioner rack 建置一個：** 開啟 **Provisioning → Boot ISOs → Build
  ISO**。選擇目前 Site 的 provisioner Integration，輸入在該 provisioner 內
  不分大小寫、長度 1–63 字元的唯一名稱，以及 MAAS rack 位址（hostname 或 IPv4，
  可帶 port；預設 `5248`）。Dialog 會建議 `<integration-name>-ipxe`、預填
  Integration endpoint 的 host，並預覽 `http://<rack>:<port>/ipxe.cfg`。在單一 VM
  installation 上，這個 host 就是安裝位址（`install.sh --address`），也就是 rack。請自行
  確認 rack 位址；swallow 不會解析或連線測試。
- **固定開機流程：** swallow 使用已驗證的 template render script，不提供 script
  編輯。它從現場網路取得 DHCP、將 rack 設為 `next-server`，並直接 chain 到
  `ipxe.cfg`，不使用 DHCP boot filename。DHCP 失敗時會 retry，Ctrl-B 可開啟
  iPXE shell；UEFI chain 返回時則交給 firmware 的下一個 boot device。同步建置
  通常只需數秒。
- **開機相容性：** 每個 ISO 內含 iPXE v2.0.0，可在 BIOS 與 UEFI x86_64 開機。
  iPXE 未簽署，因此使用它的 Server 必須關閉 Secure Boot。
- **管理 ISO：** Boot ISOs table 顯示 provisioner／Site、chain URL、大小、使用中
  數量與建置資訊。**View script** 也會顯示 rack、iPXE version、ISO URL 與
  SHA-256；**Download** 使用 BMC 掛載的同一個 URL。只要仍有 enabled Server 使用，
  **Delete** 就會停用或被拒絕。刪除後會移除檔案，原本的 URL 也會停止運作。
- **逐台選擇：** 在 **Server → Summary → Management controller → Boot media**，
  **Enable Boot Media** 必須選擇為該 Server 自己的 provisioner 建置的 Boot ISO。
  若沒有可選項，請依連結前往 **Boot ISOs** 建置。Enable 會執行 Redfish preflight；
  只有 BMC 掛載 ISO 並接受 boot override 後才保存設定。**Boot ISO** 欄會顯示名稱與
  BMC 掛載的 URL。
- **追蹤 preflight：** **Enable Boot Media**、**Change ISO** 與 **Re-apply**
  通常約需五分鐘，其中大部分是剛完成掛載後刻意等待三分鐘，讓 BMC 穩定掛載。
  Dialog 會顯示 progress bar、elapsed time、等待期間的剩餘時間，以及五個步驟：
  檢查 BMC（切換時也會退出上一個 ISO）、以 virtual CD 掛載 ISO、等待 BMC 穩定掛載、
  將接下來的開機導向 virtual CD，以及從 BMC 讀回兩項設定。各步驟會標示已完成、
  進行中或尚未開始；不需要執行的步驟會直接顯示為已完成，例如 BMC 已掛載該 ISO。
- **在背景繼續：** 以 **Continue in background** 關閉 dialog 不會停止 preflight。
  結束前，Boot media block 會顯示 **Applying** 與相同進度，並停用所有 action。
  頁面保持開啟時，成功或失敗結果會以 notification 顯示；重新載入或從其他 browser
  tab 開啟時，也能從 API 恢復進度。
- **操作與佈署：** enabled Server 會提供 **Change ISO**（preflight 會先退出目前
  ISO）、**Re-apply**、**Disable**、**Re-detect Redfish** 與 **Check BMC**。
  Disable 後仍保留選擇的 Boot ISO，供下次啟用。每次 OS deployment 都會先重新套用
  該 Server 的 Boot ISO，並為該次 deployment 凍結這個選擇；BMC 失敗時會停在
  Boot Media Task，修復後再 retry。
- **納管與檢視：** 這類網路上的 machine 還不是 Server，因此第一次 enlist 開機需要
  手動掛載 ISO：在 BMC 以 virtual media 掛上 ISO URL 並開機一次。Server 出現後立即
  啟用 Boot Media。硬體檢視每次執行前都會套用 Boot Media，且在執行當下讀取設定，
  因此啟用後 retry 就會從 ISO 開機（見
  [Server 與 infrastructure](servers-and-infrastructure.md#新增-server)）。

API 與 CLI 使用者可透過 `GET /api/v1/servers/{id}/boot-media` 查看進度；idle
時 `apply` 為 `null`，preflight 期間則包含 ISO ID、目前的 `probing`、`ejecting`、
`mounting`、`settling`、`directing` 或 `verifying` phase，以及 `startedAt` 與
`phaseStartedAt`；`phaseEndsAt` 只會在等待穩定掛載期間有值。
`swallow servers boot-media get <server>` 會在 `enable` 等待期間顯示相同進度。
每台 Server 同時只能執行一個
preflight；期間再次 enable 或 disable 會收到 HTTP 409。

從舊版 installation-supplied ISO 升級後，enabled Server 可能顯示 **Choose a Boot
ISO** 警告。選擇 Boot ISO 前，OS deployment 會以
`boot_media_not_configured` 停止；請透過 **Change ISO** 或 **Re-apply** 選擇。
選擇的 ISO 已刪除或無法提供時，面板會顯示 **The Boot ISO cannot be mounted**
警告，deployment 也會同樣停止。舊的 hand-made
`swallow-ipxe.iso` 與固定 URL 已不再提供。

## 硬體 inspection 與測試

在 Server 的 **Take action → Hardware checks** 中，**Inspect hardware** 會要求
provisioner 重新盤點 machine 硬體；MAAS 將此動作稱為 *Commission*。執行期間
Server 顯示 **Inspecting**；選擇 **Test** 執行 provider hardware test 時則顯示
**Testing**。Summary card 的 **Inspection** 欄位是 provider 上次的 inspection
result，例如 *Passed*。

硬體檢視是 `inspect-hardware` Workflow，只有一個 Job `ensure-inspected`：等待
provider 的 enrollment 結束、套用 Boot Media，再進行最多三次檢視。新納管的 Server
會自動開始。要求處理時，若 Server 的網路不是由 provisioner 的 DHCP 提供，請啟用
Boot Media，再 retry 該 Task 或重新選擇 **Inspect hardware**。細節與如何關閉自動
檢視，請見 [檢查硬體](servers-and-infrastructure.md#檢查硬體)。

## Release 與 recovery

Release 會讓 provider machine 回到 available pool，但不會刪除 Server。它可從
deployed、allocated、failed、broken 或 rescue state 執行，也可包含明確 erase
options。Action 接受後，Servers list 會在 provider 執行期間顯示 **Releasing**，
machine 回到 pool 後顯示 **Ready**；不需離開該頁即可追蹤這段轉換。

Recovery 會依 provider recovery policy 處理 allocated、failed、broken 或 rescue
state。若 memory-backed state 或 active work 可能遺失，必須閱讀 power-off warning。

## Network inspection

Network configuration 讀寫 provider-owned interface 與 link。Automatic addressing
是要求 provider auto-assignment，不是由 swallow 另建 address allocator。

## CLI

```bash
swallow provisioning images list --integration int1
swallow provisioning templates list --site-id site1
swallow provisioning boot-isos list --site-id site1
swallow provisioning boot-isos create --integration int1 --name rack-ipxe --rack 10.0.0.2
swallow provisioning boot-isos get iso1
swallow provisioning boot-isos delete iso1
swallow provisioning deploy --file deploy.yaml
swallow provisioning release --server server1 --erase
swallow servers inspect server1
swallow servers redfish-probe server1
swallow servers boot-media enable server1 --iso iso1
swallow servers boot-media disable server1
swallow servers boot-media get server1 --live
```

Structured payload 請使用 JSON／YAML request file，讓 fields 持續與
provider-owned contract 對齊。
