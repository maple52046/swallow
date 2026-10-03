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
對這類 Server，swallow 會提供一個 iPXE 開機 ISO，讓 Server 的 BMC 以 virtual media 掛載並優先開機：
ISO 先從現場 DHCP 取得位址，再 chain 到 provisioner。

- **安裝：** 放置 iPXE ISO 檔，並設定 BMC 連到 swallow 的 base URL（見
  [Configuration](../reference/configuration.md#boot-media)）。ISO URL 由安裝固定，不需逐台輸入。
- **偵測：** swallow 會對每台新加入的 Server 偵測其 BMC 是否支援 Redfish virtual media 與 boot
  override（不論 provisioner 使用哪種 power driver），結果顯示在 Server 的
  **Summary → Management controller** 卡片。**Re-detect Redfish** 可重新偵測，例如 BMC 韌體更新後。
- **啟用：** **Enable Boot Media** 會對實際 BMC 執行 preflight：掛載 ISO、把接下來的開機導向它，兩者都成功才
  儲存設定。支援度依硬體與韌體而異，失敗時會顯示 BMC 自己的說明。這個動作不會重開機。
- **佈署：** 啟用 Boot Media 的 Server 每次佈署 OS 時，會先執行 *Ensure Boot Media* 步驟重新套用，因為 BMC
  可能遺失掛載（例如 BMC 重啟後）或開機順序。若 BMC 無法連線，該步驟會要求處理、佈署暫停；BMC 恢復後
  到 **Workflows** retry 即可。若 Server 仍沒開進 ISO（BMC 在 provisioner 開機時丟掉了 ISO，或 BIOS
  直接開了硬碟），swallow 會在兩分鐘時重新掛載 ISO；若十分鐘時 Server 仍未網路開機，就重新套用 Boot Media，
  並透過 Redfish 重開 Server 一次。這種佈署會多花十到十五分鐘。
- **Check BMC** 讀取 BMC 目前的實際狀態。**Disable** 停止重新套用，並要求 BMC 退出 ISO。

## Release 與 recovery

Release 讓 provider machine 回到 ready，並可包含明確 erase options。Recovery
會依 provider recovery policy 處理 failed、broken 或 rescue state。若 memory-backed
state 或 active work 可能遺失，必須閱讀 power-off warning。

## Network inspection

Network configuration 讀寫 provider-owned interface 與 link。Automatic addressing
是要求 provider auto-assignment，不是由 swallow 另建 address allocator。

## CLI

```bash
swallow provisioning images list --integration int1
swallow provisioning templates list --site-id site1
swallow provisioning deploy --file deploy.yaml
swallow provisioning release --server server1 --erase
swallow servers redfish-probe server1
swallow servers boot-media enable server1
swallow servers boot-media get server1 --live
```

Structured payload 請使用 JSON／YAML request file，讓 fields 持續與
provider-owned contract 對齊。
