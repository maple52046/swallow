# SSH key 與 image 登入帳號

[English](../../../docs/en/guides/ssh-keys.md) · [文件首頁](../README.md)

swallow 會登入它所佈署的 Server，以確認 SSH 就緒並執行 Ansible。它自己管理這些用途的 key，
也知道每個 OS image 該用哪個帳號登入。

## 兩種 key

從右上角的 account menu 開啟 **SSH keys**。

- **Deployment key**：整個安裝只有一組 key pair，由安裝流程產生（ed25519）：`swallowctl install` 與
  `swallowctl upgrade` 會在資料庫 migration 之後執行 `swallow-api deployment-key ensure`。私鑰加密保存，
  且不會顯示。它不存在時，OS 與 Platform 佈署會被拒絕。除非某個 Site 設定了自己的 automation key，swallow 在所有 Site 都用它
  進行 SSH 就緒檢查與 Ansible。
- **Access keys**：你自己登入 Server 用的 key。swallow 只保存公鑰。你可以匯入既有公鑰，或產生新的
  key pair；產生的私鑰只會顯示一次，關閉對話框前請先下載或複製。

## Key 如何進到 Server

swallow 會把每一把 key 註冊到能保存 SSH key 的 provisioner（MAAS：擁有該 Integration API key 的帳號），
MAAS 佈署時再把這些 key 寫入 image 的 default user。頁面會依 provisioner 顯示每把 key 的狀態：
**Synced**、**Pending**、**Failed**（附原因）或 **Not supported**。

- 每次 key 變更後、每隔幾分鐘，以及每次 OS 佈署前都會同步；**Sync to provisioners** 可立即要求同步。
- 若佈署前 swallow 無法把 deployment key 註冊到 MAAS，該佈署會以 `ssh_key_registration_failed` 停下；
  MAAS 恢復後重試即可。
- 你自己在 MAAS 加入的 key，swallow 絕不會移除。

Key 只在 Server 佈署時注入。重新產生或替換 deployment key、刪除 access key，都**不會**改變已經佈署的
Server：它們保留佈署當時的 key。若有需要，請重新佈署（或更新它們的 `authorized_keys`）。

## Image default user

每個 OS image 都有一個 **default user**：它的 cloud-init 建立的帳號，也是 swallow 在以此 image 佈署的
Server 上登入所用的帳號。

- Synced 的 Ubuntu、CentOS、RHEL image 有內建預設值（`ubuntu`、`centos`、`cloud-user`）。
- 上傳的 custom image 可在上傳對話框設定，或之後在 OS Images 頁面用 **Edit** 設定（例如 `cloud-user`）。
- **Default user** 欄顯示實際生效的帳號名稱。已佈署 Server 的摘要頁有 **Connection** 卡片，
  顯示該登入帳號、位址，以及可直接複製的 `ssh <user>@<address>` 指令。

Image 沒有 default user 時，swallow 會退回 Site 的 SSH user，再依序嘗試 `cloud-user` 與 `ubuntu`。

## Site override

Site 的 automation credential 仍可帶自己的私鑰；有設定時，該 Site 會用它取代 deployment key。
留空即使用 deployment key。Site 的 SSH user 為選填，只作為上述的退回選項。

## 備份

Deployment key 的私鑰存於 MongoDB，並以 API credential key 加密。兩者必須一起備份與還原，否則 swallow
無法使用還原後的 key。

## CLI

```bash
swallow ssh-keys list
swallow ssh-keys generate --name laptop --private-key-out ~/.ssh/id_ed25519_laptop
swallow ssh-keys deployment show
swallow provisioning images upload --integration int1 --name rocky-10 \
  --architecture amd64 --content ./rocky.tgz --default-user cloud-user
```

精確欄位定義於
[SSH Keys contract](../../../api-server/docs/development/api-contracts/api-server/ssh-keys.md)
與
[provisioning contract](../../../api-server/docs/development/api-contracts/api-server/provisioning.md)。
