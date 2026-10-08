# Managed Software

[English](../../../docs/en/guides/managed-software.md) · [文件首頁](../README.md)

Managed Software 會在已 deployed 的 Server 安裝或移除一個 host-level software，
刻意與 Platform deployment 分開。

## Supported software

- **Docker CE：** 從 configured package source 安裝 Docker runtime。
  **Enable the Docker Engine API** 選項（預設開啟）讓 swallow 能從 Server 的
  **Containers** tab 管理該 host 的 Docker；見下文。
- **Podman：** 安裝 request 選擇的 Podman variant。
- **NFS：** 依 required storage settings 安裝 server 或 client role。

Shipped automation manifest 是 allowlist。Software kind／variant 沒有出現在 active
API contract 與 manifest 時，就不是 supported capability。

## Eligibility

安裝前：

- 每個 target Server 都是 deployed、manageable state——dashboard 只列出已安裝 OS、
  且沒有進行中、失敗或待處理的 swallow OS deployment 的 Server（OS 在 swallow 外部安裝的
  Server 也可以）。
- Server 未 lock。
- 沒有 conflicting active Workflow 擁有 target。
- Site automation 能為每台 host resolve SSH user 與 credential。
- Required software-specific settings 已提供。

一個 request 可包含多台 eligible Servers。送出時 API 會再次套用同一組 gates。

## Software Assignment

Software Assignment 以 Server 與 software kind 為 key，記錄 desired state、
last-applied state、related Workflow 與 failure information。它不代表每個 external
package fact 都即時；結果不清楚時請看 associated Workflow 與 host diagnostics。

## Install 與 uninstall

**Software** 是 Swallow 可管理 software 的 catalog。開啟 software card 後可查看能力與目前
deployment，再使用 **Install software** 選擇一台或多台 eligible Servers，接著設定該
software。已經存在 assignment 的 Server 則從同一個 detail 頁進行 **Reconfigure**、
**Retry install** 或 **Uninstall**。

Server 的 **Take action** → **Install software** 是 fixed-Server 入口：第一步從分類 catalog
選擇 software，第二步設定該 software。已安裝或失敗的 assignment 會預填完整設定，供
reconfigure 或 retry；進行中的 assignment 則直接提供 Workflow link。Server 無法安裝時，
action 會說明原因。送出後會透過 registered playbook mapping 建立 Workflow。Uninstall 是
explicit request，會產生另一個 Workflow；刪除 Server／Platform 不會默認 uninstall 所有
software。

變更 NFS server/client role 可能影響 mounted storage 與 workload availability，
需要額外檢查。

## docker group

安裝 Docker CE 時，會把該 Server 的 default user（swallow 登入用的帳號，顯示在 Server 的
**Connection** 卡片）加入 `docker` group，讓它在下次登入後不需 sudo 就能使用 Docker。OS 不是由
swallow 安裝的 Server，請先設定它的 default user（見 SSH key 與 image 登入帳號）。對先前已安裝的
Server 重新套用 Docker CE 即可補上。

## Docker Engine API 與 Containers tab

swallow 在 Server 上安裝 Docker CE 後，Server detail 頁面會出現 **Containers** tab，
包含四個區塊：**Containers**（create、start、stop、restart、logs、remove；預設顯示）、
**Images**（pull、remove）、**Volumes** 與 **Networks**。所有內容都經由 swallow API 即時
讀取 host 的 Docker Engine；swallow 不保存這些物件。

管理功能需要 Docker Engine API，Docker CE 安裝預設會啟用：

- Docker daemon 會另外在所有介面的 TCP `2375` port 監聽，**沒有認證、沒有 TLS**。
  能連到這個 port 的人等同擁有該 host 的 root 權限。只在內部網路使用；Server 若能被
  Internet 存取，請關閉此選項。
- 沒有 API 時，tab 會說明原因並提供 **Enable Docker API**；已啟用 API 的 Server 可用
  **Disable Docker API** 關閉。兩者都是以變更後的設定重跑同一個 Docker CE 安裝，
  沒有額外的 job。
- 變更設定會重啟 Docker daemon，沒有 restart policy 的 container 會停止。
- 此選項出現前安裝的 Docker CE 視為未啟用 API；自行開啟的 Docker API 不會被使用。
- Server 被 lock 時 tab 仍可讀取，但所有變更都會停用，直到 unlock。
- 一次 pull 最長可執行 55 分鐘，足以下載數十 GB 的 GPU image。Pull 進行中可以關閉
  **Pull image** 對話框：Images 區塊會列出仍在進行的 pull，每個完成時都會通知。逾時的
  pull 會保留已下載的 layer，再 pull 一次會從那裡接續。

### Private registry

要 pull private image，請在 **Software › Docker CE › Settings** 新增 **Registry credential**。
只有具備專屬設定的 software 才會顯示這個 action。請輸入 registry host
（例如 `harbor.example.com`，Docker Hub 用 `docker.io`——輸入 `hub.docker.com` 也會存成
`docker.io`）、username，以及 password 或 access token。對話框會顯示憑證實際存成哪個
registry。Pull 時會使用 image reference 中那個 registry 的憑證——`nginx`、`team/app`
代表 Docker Hub——**Pull image** 對話框會顯示這次會用哪一個；沒有憑證時就是匿名 pull。

憑證由所有 Server 共用、加密儲存，密碼之後不會再顯示（要變更請用 Replace）。在 host 上執行
`docker login` 沒有用：Docker Engine API 不會讀取 host 上保存的憑證。憑證會經由與其他 Docker
請求相同、未加密的 API listener 傳到 host。

## Dashboard 與 API

Operator 請使用 Dashboard 的 **Software** 頁面。`swallow` CLI 尚未提供
`software` command group；automation client 應依 active
[Managed Software contract](../../../api-server/docs/development/api-contracts/api-server/software.md)
處理 payload fields、authentication 與 errors；Containers tab 的 API 見
[Docker Host Explorer contract](../../../api-server/docs/development/api-contracts/api-server/servers-docker.md)。
