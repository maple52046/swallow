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
成功回傳不代表作業已完成。

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
```

Structured payload 請使用 JSON／YAML request file，讓 fields 持續與
provider-owned contract 對齊。
