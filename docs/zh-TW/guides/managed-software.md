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
- **Prometheus** 與 **Grafana：** 每台 Server 安裝一個 pinned instance，可用 Server 上的
  Docker CE 或 Podman 以 container 執行，或從原廠的官方 release／套件以 native 安裝。
- **AMD Device Metrics Exporter：** 在 AMD GPU Server 上以 native 方式安裝 AMD 的
  per-GPU metrics exporter；見下文。
- **Node Exporter：** 從 OS 套件安裝 Prometheus 的 host exporter。swallow 會自動安裝，並讓它
  自動化的每台已佈署 Server 一直保有它；見下文。
- **VictoriaMetrics：** 以 native 方式安裝一個 pinned 的 single-node VictoriaMetrics：
  一個讓 Prometheus 相容 client 寫入、讓 Grafana 讀取的 metrics store；見下文。
- **libvirt (QEMU/KVM)：** 讓 Server 成為 KVM hypervisor，swallow 可以列出並 enroll 它上面的
  virtual machine；見下文。
- **Directory client（SSSD）：** 讓 Server 從 installation 的 Directory 讀取 swallow 的使用者、他們的 SSH key 與 sudo
  規則，讓人以自己的帳號登入。swallow 會自動在它自動化的每台已佈署 Server 上安裝；見下文。

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

**Software** 是 swallow 可管理 software 的 catalog。開啟 software card 後可查看能力與目前
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

## AMD Device Metrics Exporter

AMD Device Metrics Exporter 在 Server 位址的 port 5001 提供 per-GPU 的 Prometheus
metrics。目標須有 AMD compute GPU 且執行 Ubuntu 24.04，沒有可設定的選項。若 Server 還沒有
swallow 的 amdgpu driver loader，安裝時會一併裝上。

它只安裝 exporter。安裝完成後 swallow 的 Site Prometheus 就會收集它；要看圖時，另外安裝帶 Device Metrics
Exporter dashboard、datasource 讀那套 Prometheus 的 **Grafana**。

以 Device Metrics Exporter 部署的 Slurm Platform 會在其 GPU node 安裝同一個 software，
並開啟 Slurm job attribution。Server 屬於某個 Platform 期間，其上的 exporter 由該 Platform
管理：Software 頁面不會在那台 Server 上安裝或解除安裝它，uninstall 該 Platform 時會一併移除。
Kubernetes Platform 的 GPU Operator 有自己的 exporter，因此 Kubernetes node 不提供此 software。

Slurm Platform 自己的 Grafana 就是它 primary controller 上的 **Grafana**，和其他安裝一樣列在這裡，連同它使用的
container runtime。uninstall 該 Platform 時兩者都會保留，直到在這裡解除安裝。在 Site 的 Prometheus 收集它的
exporter 之前部署的 Slurm Platform，那台主機上還有一個 **Prometheus**；swallow 已不再使用它，也不會移除它，
不需要它的歷史資料時請在這裡解除安裝。

## Node Exporter

Node Exporter 在 port 9100 提供 host metrics（CPU、memory、disk、network），swallow 的
Prometheus discovery 在每台 Server 上 scrape 這個 port。它安裝 OS 發行版的套件——Debian 與
Ubuntu 為 `prometheus-node-exporter`，RHEL 家族為 `node_exporter`（來自 EPEL）——不需要
container runtime，也沒有可設定的選項。

每次 OS 佈署都會在佈署的 Server 上安裝它，而且 swallow 會讓每個啟用 automation 的 Site 中每台已佈署、
未 lock 的 Server 一直保有它，包括不是由 swallow 佈署的 Server：缺少時安裝、失敗時重試，Site 的
Prometheus 收集不到時再安裝一次（見 [Monitoring](monitoring.md#node-exporter-會一直保持安裝)）。所以很少
需要在這裡安裝；在某台 Server 上 uninstall 它，swallow 就不再在那台安裝，直到有人再安裝（lock Server 也一樣）。任何 Platform 的成員都與其他已佈署的 Server 一樣可以安裝。
安裝前會先移除主機上仍在執行的 swallow 舊版 containerised node-exporter
（`swallow-node-exporter.service`）；Docker 與 RDC exporter container 不受影響。

## Directory client（SSSD）

Directory client 讓 Server 經 LDAPS 從 installation 的 Directory 讀取帳號、群組、SSH key 與 sudo 規則，並以
installation 的 CA 驗證。之後人以自己的 swallow username 與 SSH key 登入所屬 Pool 允許的 Server，得到 Pool 給他們的
sudo；Server 的 `/etc/passwd`、`authorized_keys` 與 sudoers 檔案都不會被寫入，本地帳號照常可用。成員資格、key 與 sudo
的變更大約一分鐘內到達 Server。

swallow 像 Node Exporter 一樣自動安裝它：在啟用 automation 的 Site 中，每台已佈署、未 lock 的 Server，在 Directory
已經有使用者之後。它只支援 Ubuntu。Server 連不到 Directory 的 port 636，或本地帳號與 swallow 的使用者同名或同 id 時，
安裝會停下，而且不改變任何人的登入方式。它沒有設定。在某台 Server 上 uninstall 它，swallow 就不再在那台安裝，直到有人
再安裝。


VictoriaMetrics 安裝 VictoriaMetrics 1.155.0（single-node）的官方 release binary，swallow 從 Site
選擇的來源取得並驗證。它不需要 container runtime，只支援 amd64 的 Server，目前只在 Ubuntu 24.04 驗證過，
RHEL 家族尚未驗證。它在 port 8428 提供
VictoriaMetrics 與 Prometheus 相容的寫入與查詢 API。它有兩個設定：

- **Answers on：** `loopback`（預設）只回應同一台 Server 上的程式，例如那裡的 Grafana；
  `management` 在 Server 的 management address 上回應。VictoriaMetrics 沒有登入，連得到這個
  address 的人都能讀寫所有 series，只在可信任的網路上選 `management`。
- **Retention：** 樣本保留多久，寫成整數加 `d`（天）、`w`（週）、`M`（月）或 `y`（年），例如
  `90d` 或 `1y`，最多 100 年。留空即用 VictoriaMetrics 自己的預設 `1M`。沒有單位的數字、`m`
  或小時都不接受，因為 VictoriaMetrics 對它們的解讀不同，或不接受少於一天。

要看圖時，安裝 **Grafana** 並把 **Prometheus URL** 設成 VictoriaMetrics 的位址，例如同一台 Server
上的 `http://127.0.0.1:8428`。swallow 的 Site Prometheus 不收集它，也沒有 Platform 會安裝它。
以其他位址或 retention 重新設定會重啟它並保留資料；解除安裝會移除 service、release 與**它的全部
資料**。

## libvirt (QEMU/KVM)

libvirt 從 OS 套件安裝 QEMU、libvirt 與 UEFI firmware，讓 libvirt daemon 保持執行，並把 Server 的
default user（swallow 登入用的帳號）加入 `libvirt` 與 `kvm` group。之後這台 Server 就能當 hypervisor：
它上面的 virtual machine 可以依名稱列出並 enroll（見
[libvirt virtual machine](servers-and-infrastructure.md#libvirt-virtual-machine)）。它不會建立 virtual
machine，也沒有可設定的選項。

- 需要 Ubuntu 24.04、x86_64 CPU，而且 BIOS 已開啟硬體虛擬化（Intel VT-x 或 AMD-V）。本身是 virtual
  machine 的 Server 還需要它的 hypervisor 開啟 nested virtualization。缺少任何一項時，安裝會在安裝任何
  東西之前停下並說明原因。
- 在 RAM 部署的 Server 上，會另外安裝 KVM 需要、但 RAM image 沒有附的 kernel 模組。RAM 部署的 Server
  重開機後，它安裝的一切都會消失，swallow 卻仍顯示 libvirt 已安裝；這時請重新安裝。
- 在已經跑著 virtual machine 的主機上安裝也是安全的：不會重啟 libvirt daemon，也不會改動 libvirt 的
  network、主機的網路設定或既有的 virtual machine。
- 安裝的 Workflow 會回報主機有幾個 IOMMU group。0 表示 IOMMU 沒開：要把 PCI 裝置交給 virtual machine
  之前，請先在 BIOS 開啟它。
- 主機上還有 virtual machine 時會拒絕解除安裝。解除安裝會移除套件，但保留 `/var/lib/libvirt` 下的
  disk image。

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

User 請使用 Dashboard 的 **Software** 頁面。`swallow` CLI 尚未提供
`software` command group；automation client 應依 active
[Managed Software contract](../../../api-server/docs/development/api-contracts/api-server/software.md)
處理 payload fields、authentication 與 errors；Containers tab 的 API 見
[Docker Host Explorer contract](../../../api-server/docs/development/api-contracts/api-server/servers-docker.md)。
