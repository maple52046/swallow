# Dashboard 指南

[English](../../../docs/en/guides/dashboard.md) · [文件首頁](../README.md)

Dashboard 是 swallow active user workflow 的 browser interface。它只使用
published HTTP API，browser 內只保存 UI preference 與目前 session。

## 導覽

側邊欄以分隔線把區域分成四段：Overview；Servers、Platforms 與 Software；
Monitoring 與 Activity；Infrastructure、Pools 與 Users。**Servers** 本身是 inventory 的連結，
也是預設收合的導覽群組；使用旁邊獨立的 chevron 可展開 **OS Images** 與 **Template**，直接進入
任一子頁時則會自動展開。收合成 icon rail 時，Servers icon 會以 flyout 提供相同的三個連結。
側邊欄只列出你能使用的區域：admin 看得到全部，其他 user 看得到 Servers 及其準備資源、Platforms、
Software、Activity、Validation 與 Pools（見 [Session 與權限](#session-與權限)）。用頁面頂端的按鈕可把
側邊欄收合成圖示（這個瀏覽器會記住選擇）；滑鼠停在圖示上或以鍵盤聚焦時會顯示名稱。手機上以選單
按鈕開啟同一份側邊欄的 drawer。

| 區域 | 用途 |
| --- | --- |
| Overview | Site-scoped fleet health、attention items、integration、Platform 與 recent Workflow |
| Servers | Inventory、Add servers（納管引導）、可分享的 filter view、bulk action、tag 與 Manage tags（在整個 Site 改名與刪除）、lock，以及包含 Boot Media 的 Server detail |
| Platforms | Kubernetes／Slurm deployment、lifecycle、settings 與 runtime view |
| Software | 分類呈現 catalog（Docker CE、Podman、libvirt (QEMU/KVM)、NFS、Prometheus、Grafana、AMD Device Metrics Exporter、Node Exporter、VictoriaMetrics，以及這個版本中沒有專屬卡片、列在 Other software 的 software）、各 software deployment 與多 Server 安裝 |
| Monitoring | Fleet health、勾選 Server／Pool／Zone／Platform 的用量圖表、current metrics、選用的 Grafana link，以及（開發中的）alert 與 silence |
| Activity | 兩個分頁。**Workflows**：durable execution、Job、Task、event、log、cancel、rerun 與 retry。**Change Plans**：Platform 破壞性變更的計畫，包括等待核准或拒絕的 plan、已核准 plan 的紀錄，以及安全開關 |
| OS Images | 佈署 Server 所用的 OS image：catalog、每個 image 支援的 deploy mode 與 Deploy。admin 另外可以 upload、編輯、測試與刪除 image；其他 user 看得到每個 image，可以 refresh，並把 image 佈署到自己是 owner 的 Pool 的 Server |
| Deployment Templates（側邊欄顯示 **Template**） | 依 Pool 分組的 OS image 可重用佈署配方 catalog：Disk 或 RAM、網路、最低硬體，以及選用且 write-only 的 cloud-init。member 看得到並能使用所屬 Pool 的 template，owner 能建立與修改，admin 看得到全部並能把 template 移到別的 Pool |
| Infrastructure | Site、Integration、Zone、credential、automation settings、Site 的 local repository（deployment 從哪裡取得 artifact、來源、同步狀態與內容，見[初始設定](initial-setup.md#4-註冊-local-repository)），以及 [Boot ISO](#boot-iso-與-server-boot-media) |
| Pools | 你所屬的 Pool（admin 看得到 installation 的每個 Pool，並在這裡建立、改名與刪除）；每個 Pool 的成員、Pool Role，以及誰在它的 Server 上有 sudo |
| Users（admin） | 帳號、存取設定、Directory 的狀態與最近的存取變更 |
| SSH keys（account menu） | Deployment key、你的 access keys，以及（admin 才看得到的）provisioner sync 狀態，見 [SSH key](ssh-keys.md) |
| API keys（account menu） | 讓 script、CI 與 CLI 不需要密碼即可以你的身分呼叫 swallow 的 key，見 [API key](api-keys.md) |
| Change password（account menu） | 你自己的 swallow 密碼：登入 swallow 用它，Server 上需要密碼的 sudo 規則也問它 |

區域內的分頁保有各自的網址，因此連結與書籤仍然有效：Activity 是 `/workflows` 與 `/change-plans`。
OS Images 是 `/provisioning/images`，Deployment Templates 是 `/provisioning/templates`，兩頁都沒有分頁。`/clusters` 與 `/operations` 下的
route 是 compatibility redirect；`/provisioning/boot-isos` 會轉到 `/infrastructure/boot-isos`，並保留
Site scope。

## 使用 Overview

Overview 是一份唯讀、point-in-time 的摘要，彙整其他 context 擁有的事實。選擇 Site
可限制範圍；清除 Site selection 則查看整個 swallow。**Updated** 時間表示 aggregate
產生的時間。

- **Fleet health**、**Capacity** 與 **Workflows** 摘要 Server reachability、compute GPU 與
  deployment 數量，以及 active／近期失敗工作。Fleet health 把每台 Server 計入一次：Up、Down、
  Unknown（swallow 監控它但沒有答案）或 N/A（swallow 不監控它，包含 absent Server）；見
  [Server health](monitoring.md#server-health)。Development build 啟用 Alerts 時，**Alerts**
  也會摘要選用的 Alertmanager source。沒有 Alertmanager 時顯示 **Not configured**；已設定但
  讀不到時顯示 **Unavailable**。Release build 保留 Alerts card 的位置，但標示為 unavailable。
- **Needs attention** 最多顯示十項 Integration sync failure、unreachable Platform、
  unmatched member、down Server，以及過去 24 小時失敗的 Workflow。啟用 Alerts 時也會包含
  current alert。請從項目進入 owner page 處理；問題解除後，之後的 Overview read 就不再顯示。
- **Recent workflows** 最多連到最近要求的八個 Workflow。**Integrations** 顯示各
  Integration 最近成功 sync 或 failure，並連到管理頁。

Overview 不保存第二份 status，也不執行修復；請用它找到實際擁有該狀態的 Server、
Integration、Platform、Workflow 或 alert view。

## 開發中的功能

以下三項 Dashboard 功能仍在開發中，Dashboard 的 release build 會隱藏它們；API 與 `swallow`
CLI 不受影響。

- **Alerts**：Monitoring 頁面沒有 alert 列表、alert 數量與 Acknowledge，Overview 的 Alerts card
  顯示「Not available in this release」，也不會產生 alert 的 attention item。基礎監控不受影響：
  health、current metrics 與用量圖表在每個 build 都會顯示。請改用 `swallow monitoring alerts`。
- **OS image upload**：OS Images 沒有 Upload。既有 custom image 的 verification 仍可使用。
- **Virtual machines**：Add servers 沒有 libvirt virtual machine 路徑，virtual machine 的
  Summary 也不會顯示經由 hypervisor 的 Boot Media。請改用 `swallow servers virtual-machines` 與
  `swallow servers boot-media`。

Development build 會顯示這些功能，並提供 **Account menu → Experimental features** 逐項關閉。

## Site scope

判讀 inventory 或 monitoring 前先選擇 Site。Site scope 保存在 URL，可直接分享。
尚未選擇 Site，或 Site 沒有 integration 時，empty state 是正常結果。

## Boot ISO 與 Server Boot Media

多數 Site 不需要 Boot ISO。只有 Server 使用 external DHCP、無法走 provisioner
管理的 network boot path 時，才需要 **Infrastructure → Boot ISOs**。頁面會呈現選配的
**Server BMC → Swallow Boot ISO → Site DHCP → MAAS rack** 路徑，以及每個已設定
ISO 的 card。**Build Boot ISO** 會從所選 Integration endpoint 建議 MAAS rack 位址，
自動產生的名稱位於 **Advanced settings**。Card 會分開顯示 served 狀態與 Server
使用情況，固定提供 **Download**，並將 iPXE script、delete、URL、hash 與建置資訊
放在 **More** 和 **Technical details**。

在 **Server → Summary → Management controller → Boot media** 啟用 Boot Media
時，必須選擇為該 Server provisioner 建置的 Boot ISO。沒有可選項時，panel 會連到
**Boot ISOs** 分頁。Enabled Server 可 change 或 re-apply ISO；Disable Boot Media 後仍會
保留原本的選擇。
**Enable Boot Media**、**Change ISO** 與 **Re-apply** 會顯示 BMC preflight
的五步驟進度、elapsed time，以及三分鐘掛載穩定等待的剩餘時間。選擇
**Continue in background** 可關閉 dialog 而不中止作業；block 會維持
**Applying**、停用 action，且重新載入或從其他 tab 開啟時仍會顯示相同進度，
最後以 notification 呈現結果。

## Local repository

**Infrastructure → Local repository** 顯示目前 scope 的 Site 的 local repository，也就是該 Site
自己保存的一份 deployment 所安裝的 package、image 與 file，以及這些 deployment 從哪裡取得它們。
請先選擇 Site；只有一個 Site 的安裝會直接開啟它。

- **Artifact source for deployments** 選擇 Site 的 deployment 怎麼使用這個 repository：**Upstream
  sources**（每個 artifact 從其模組宣告的上游來源取得；Server 保留自己的 OS package 來源）、
  **Cache**（這個 repository 已經提供的從它取得，其餘從上游來源；deployment 不必等它同步），或
  **Offline**（每個 artifact 都只從這個 repository 取得；它提供每一項之後才接受 deployment）。在有人
  變更之前，deployment 使用上游來源。選擇另一個選項時會先請你確認；已接受的 deployment 維持接受時的
  來源，之後也可以隨時切換回來。若在頁面讀取之後有其他人改了 repository，什麼都不會儲存，頁面會顯示
  目前的選擇。正在重新同步的來源會顯示 **Still serving its last sync**，因為這段期間 Server 仍取得它
  上一次的同步內容。
- deployment 使用這個 repository 時，狀態區塊說明從 repository 取得內容的 deployment 是否會被
  接受；不接受時，以 API 的說法列出卡住的原因。swallow 無法使用 Integration 的 credential 或
  content origin 時，會出現一則 alert，連到 Integration settings 中修正它的分頁。deployment 使用
  上游來源時，狀態區塊會說明這一點，以及 repository 不會同步；它已保存的內容會保留。無論哪一種，
  它都顯示 Server 讀取內容的 content origin（附複製按鈕）、Site 的 `repository` Integration 與它
  是否已暫停，以及 swallow 最後一次觀察 repository 的時間。
- **Repository sources** 列出每個來源：swallow 模組宣告的（以模組名稱標示）、你加入的
  （**User**），以及 container registry 與 PyPI 的 pull-through cache。每個來源都顯示狀態
  （**Pending**、**Syncing**、**Ready** 或 **Failed**）與 provider 的訊息、最後一次成功同步的
  時間，以及它提供的項目數。**Files** 列出模組宣告的 file，附可複製完整值的縮短 sha256 與大小。
  **Declared images** 列出模組宣告的 container image，每個都顯示狀態、抓取失敗時 provider 的訊息，
  以及每一層都儲存後的大小。swallow 在 image 的 registry cache ready 之後預先抓取它；在每個 image
  都儲存之前 repository 不是 ready，deployment 會等待；中斷或失敗的抓取會重新開始。其後是宣告的
  Python package。只要還有項目在同步，頁面每 5 秒更新一次。deployment 使用上游來源時，每個來源、
  file 與 image 維持 swallow 最後一次觀察到的狀態：**Ready**、**Not fetched**，或附 provider 訊息的
  **Last sync failed**。
- **Sync now** 請 swallow 立即收斂 repository，而不是等下一次定期檢查；狀態會在 swallow 觀察到
  結果時更新。只有 deployment 使用這個 repository 時才會出現。
- **User sources** 顯示你要求的來源。**Manage user sources** 可加入 Debian 或 Ubuntu archive
  （名稱、URL、suite、component 與選填的 architecture），或移除你的來源，然後儲存整份清單；deployment
  從哪裡取得 artifact 維持不變。模組宣告的來源會標示為不可在此編輯。若在你開啟 dialog 之後有其他人
  改了 repository，什麼都不會儲存：dialog 會顯示目前的清單，請重新做一次你的變更。
- **Content** 依 format（OS package、container image、Python package、file）逐頁瀏覽
  repository 提供的內容，也可以只看一個來源或依名稱搜尋。Format、來源、搜尋與頁碼都保存在 URL，
  可直接分享。

沒有 local repository 的 Site 會說明：它的 deployment 從模組宣告的上游來源取得每個 artifact。
**Register a repository** 會在
Integrations 頁面開啟 Create integration，並預先選好 Repository role 與 Pulp provider：endpoint
是 swallow 呼叫 Pulp API 的位址，**Content origin** 是 Server 讀取內容的位址
（`http://host:port`），credential 則是 Pulp 的 admin 密碼。

## Validation

**Validation → Suites** 列出 module suite 與使用者共用的 script suite（[Validation](validation.md#script-suite)）：

- 每位使用者都有 **New script suite**。編輯器的 **Run with sudo** 開關與 **Who can start** 選項（**Pool owners** 或
  **Pool members too**）都說明它們在目標上的意義，編輯器也提醒 script 與它的輸出會被其他人讀到。因為有人存了更新的
  revision 而被拒絕的儲存，只提供 **Reload the suite**，絕不覆蓋。
- script suite 的 card 與頁面以 **Created by** 顯示作者（作者已被刪除時標示 *author deleted*）。只有作者看得到
  **Edit**；作者與 admin 看得到 **Retire**。
- **Show experimental suites** 旁的 **Hide other users' suites** 會讓 catalog 與 run 對話框略過其他使用者的 script
  suite。預設關閉、存在瀏覽器，而且從不隱藏 module suite 或你自己的 suite。
- 你在任何地方都不能啟動某個 suite 時（例如你不是任何 Pool 的 owner，而它是 module suite 或 **Pool owners** 的
  suite），**Run suite** 與 run 對話框中的 suite 選項仍會顯示，但會停用並以文字說明原因；你的 Pool 角色不夠的目標
  會列出但不能選。
- 對 script suite，run 對話框會顯示它的作者、revision、script，以及 **Runs as** 你自己帳號的 username。使用 sudo 的
  suite 會寫成條件而不是保證：這次 run 需要你在每台目標上有 Pool 的 Sudo Policy 給的免密碼 sudo，沒有的地方會被拒絕。suite 在你上次執行之後改過時，會先顯示 **Changed since your last run: rN → rM**，列出改變的設定與 script
  的 diff；**Use the new version** 是唯一能繼續的方式。對話框開著時 suite 又改了，也會打開同樣的檢視。
- run 頁面顯示 **Runs as**（script suite 是你的帳號，module suite 是 root），並對 run 的啟動者、每個目標所屬 Pool 的
  owner 與 admin 顯示 **Cancel run**。Schedule（只有 admin）只提供 module suite。

## 判讀 status

- Server 沒有單一 combined status；provisioning、membership、health 互相獨立。
- **Servers** 的 **Deployment** 欄位會合併 swallow deployment result 與
  provider-neutral OS provisioning state。**Ready** 是已知、可佈署的 provider
  state，**Unknown** 則不是。Spinner 表示工作正在進行；將滑鼠停在 state 上可查看
  細節。見 [Server 與 infrastructure](servers-and-infrastructure.md#判讀-deployment-欄位)。
- Unknown 保持 unknown，不會被 UI 呈現為 failure。
- Health 顯示為 **Up**、**Down**（收集失敗，以故障呈現）、**Unknown**（警示：swallow 預期監控這台 Server
  但沒有答案）或 **N/A**（中性：swallow 不監控這台 Server）。見 [Server health](monitoring.md#server-health)。
- 信任 cached value 前檢查 integration sync error 與 observation timestamp。
- Destructive action 送出前會顯示 state 與 lock gate。

## Session 與權限

登入會建立一個 session。Dashboard 只在記憶體保存短效的 access token，並透過網頁無法讀取的 refresh
cookie 自動換發，因此使用中不會被踢回登入頁。每個分頁載入時會延續同一個 session。Session 在登出、
7 天未使用或登入 30 天後結束（server 預設值）；此時 Dashboard 回到登入頁並說明 session 已結束，重新
登入後會回到原本的頁面。

Script 與 CLI 可在 **Account menu → API keys** 建立 API key。Secret 只顯示一次；
刪除 key 即撤銷。到期、保存與 CLI 使用方式請見 [API key](api-keys.md)。

Dashboard 透過 `/auth/me` 讀取目前的 caller，不解析 token claims。Role-gated control 只是
presentation guidance；authorization 仍以 API 為準。

誰可以做什麼：

- **admin** 管理 swallow：Site、Integration、OS Image、Zone 與 Pool、Server 的 placement、使用者，以及每台 Server 與
  每個 Platform。admin 可以登入每台 Server 並在上面使用 sudo。
- 其他 **user** 的權限只來自 Pool Membership。Pool 的 **member** 看得到它的 Server、Platform、Workflow、Validation
  Run 與 Deployment Template，以自己的 SSH key 登入它的 Server，並能啟動允許 member 啟動的 script suite；**owner**
  還能操作它們（電源、OS 佈署、軟體、
  Platform、Validation、Pool 的 template），並在
  **Pools** 管理 Pool 的成員與 sudo 預設值。Pool 與它的成員資格涵蓋它在每個 Site 的 Server。不在任何 Pool 的 Server
  只屬於 admin。
- 只有 admin 建立帳號（**Users → Create user**）；owner 以 **Add member**（在 **Pools** 中 Pool 那一列，或點選 Pool 名稱開啟的 Pool 頁面）把
  既有的帳號加入自己的 Pool，對話框會依輸入的 username 片段列出還不是成員的帳號。所屬 Pool 沒有任何 Server 的 user，在
  Server 列表看到 **No servers available**。Pool 的
  **Sudo defaults** 決定 member 有沒有 sudo、sudo 要不要密碼，每個 member 可以個別覆寫。owner 一律有 sudo。Platform
  可以收窄 member 在它的節點上能做的事：在 Slurm 叢集上 member 只能登入 login node，而且沒有 sudo。
- 不能做的動作會停用或隱藏：例如 member 看到 Server 的 **Actions** 按鈕是停用的，並附上原因；它的電源狀態與 tag
  也只顯示、不提供操作。member 看到所屬 Pool 的 Platform、Change Plan、Workflow、Validation Run 與軟體部署時，也沒有
  owner 的操作（uninstall、sync、approve、cancel、retry、執行 module suite、install）——但仍能取消自己啟動的
  Validation Run——Kubernetes Platform 也不顯示即時叢集的
  分頁，那些只有 owner 與 admin 能開。只有 admin 能用的頁面會顯示 **Access denied**。
- 只有 admin 能改的設定不會出現在其他使用者的畫面：Server 的 Power Configuration、Boot Media 與 placement；Change
  Plan 的核准政策；Validation Schedule；軟體的設定（例如 Docker 的 registry credential）；以及
  Deployment Key。owner 佈署 OS 或 Platform 時，透過每台 Server 自己記錄的 provisioner 佈署，不需要選擇。Server 的即時
  硬體資訊（Storage、PCI devices）只顯示給 Pool 的 owner 與 admin。

## Error 與診斷

Action 失敗時會顯示 API error message 與 request ID。要將 UI failure 與 API log
對照時請複製 request ID。Long-running work 應開啟產生的 Workflow，不要停在原 dialog 等待。

完整 route 請見 [dashboard component README](../../../dashboard/README.zh-TW.md)。
