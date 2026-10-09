# Server 與 infrastructure

[English](../../../docs/en/guides/servers-and-infrastructure.md) · [文件首頁](../README.md)

Server 是 provisioner machine 的 reconciled projection。Operator 可以檢視、組織、
保護與操作 Server，但不會建立 Server record。

## 新增 Server

Machine 進入 provisioner 的 inventory 後就會成為 Server。**Servers → Add servers**
（空的 Server 列表也會提供）一次問一個問題，最後只顯示要做的那一步，接著等待並列出
陸續出現的 Server。Site 還沒有 provisioner 時，改為顯示 **Connect a provisioner**。

| 回答 | 要做的事 | 結果 |
| --- | --- | --- |
| 從 PXE 開機，provisioner DHCP | 讓 server 從 PXE 開機（重開機或開機由你決定）。 | 先是 **New**，自動檢視後變成 **Ready**。 |
| 從 PXE 開機，external DHCP | PXE 經由 iPXE Boot ISO：在 BMC console 掛上畫面上的 Boot ISO URL（Virtual Media，從虛擬光碟開機一次），或執行產生的 Redfish 指令；Server 出現後啟用它的 [Boot Media](os-provisioning.md#沒有-provisioner-dhcp-的網路boot-media)。 | 同上。 |
| 保留 OS | 在該主機執行畫面上的指令。 | **Deployed**，OS 不變、不重開機。 |

不需要在 MAAS 按 *Commission*：swallow 會等 enlistment 結束並關機後自行檢視（見
[檢查硬體](#檢查硬體)）。

實驗用的 virtual machine 也用同樣方式 enlist（從網路或 iPXE 開機媒體開機），但
enlistment 無法替它設定 power driver，MAAS 因此看不到它關機。它的檢視會停下並提示
**Set power configuration**；替它設定 `virsh` 的 [Power configuration](#power-configuration) 後再 retry。

Redfish 分頁填入 BMC 位址與帳號後，會產生 `curl` 指令：插入 virtual media、設定下次
從它開機一次，並開機；`curl` 會詢問 BMC 密碼。指令假設 system 為 `1`、virtual media
為 `CD1`，並附上列出實際 ID 的方式。

保留 OS 的指令不需要事先安裝任何東西：

```bash
curl -fsSL 'https://swallow.example.com/downloads/swallow-enroll.sh' \
  | sudo sh -s -- --provisioner=maas --endpoint '<MAAS URL>' --token '<MAAS API key>'
```

Script 會從你的 installation 下載 swallow CLI，再執行 `swallow servers enroll`，它包裝
MAAS `maas-run-scripts register-machine` 與 `report-results`。主機需要 `curl`、
`python3`，並能以 HTTP 連到 swallow 與 MAAS（Ubuntu、x86_64）。之後不會自動檢視（檢視
會重新開機）。指令含有 provisioner 的 API key，這是 MAAS 所需；只在可信任的主機執行，
否則請在 MAAS 更換該 key。

## Inventory 與 detail

Servers list 支援 Site scope、pagination、search、status／capability filter、
saved view、live row update 與 bulk selection。Server detail workspace 將 summary、
activity、monitoring、network、storage 與 PCI observations 分開。swallow 安裝過 Docker CE
的 Server 另有 **Containers** tab，管理該 host 的 Docker images、containers、volumes 與
networks（見 [Managed Software](managed-software.md#docker-engine-api-與-containers-tab)）。

Link 與 automation 一律使用 opaque Server ID。不同 Site 擁有相同 hostname 或
address 是合法狀況。

## 判讀 Deployment 欄位

**Deployment** 欄位顯示 Server 的 OS deployment state；mobile Server card 與
Server detail header 也使用相同呈現。它會依下列順序合併最新的 swallow deployment
result 與 provider-neutral
[`provisioning.state`](../../development/glossaries/terms/os-provisioning-state.md)：

1. 正在執行、但不是 OS 安裝的 provider 工作：**Releasing**、**Inspecting**
   或 **Testing**。
2. 正在執行或未成功的 swallow deployment：**Deploying**、**Verifying**、
   **Failed**、**Attention** 或 **Canceled**。
3. 已安裝 OS 時，以純文字顯示 image 名稱。
4. 其他情況依 OS provisioning state 本身的名稱顯示。

Server detail header 不顯示 image 名稱，而是顯示結果：swallow 佈署並驗證過的 OS
顯示 **Deployed**；OS 已安裝但沒有 swallow deployment result 時顯示 **Unknown**。
已安裝的 image 請見 Summary card。

| 顯示 | Operator 應如何判讀 |
| --- | --- |
| Inspecting / Testing | Provider 正在盤點硬體，或執行硬體測試。 |
| Releasing | Provider 正在讓 Server 回到 available pool，包括執行要求的 disk erase。 |
| Deploying / Verifying | Provider 正在安裝 OS，或 swallow 正在驗證安裝結果。 |
| Ready | Server 位於 provider 的 available pool，可以接受 OS deployment；此狀態取代舊的「Not deployed」標籤。 |
| Allocated / New / Retired | Server 已保留但尚未佈署、尚未完成硬體 inspection，或已退出服務。 |
| Failed / Broken / Rescue | 上一個 lifecycle action 失敗、provider 將 machine 標為不可用，或 Server 位於診斷環境。 |
| Unknown | 目前沒有 provider observation，或 adapter 無法辨識 provider state；不代表 Ready。 |

進行中的狀態會在標籤後顯示小型 spinner。文字本身才是 state，spinner 只提供
視覺提示。整列或 mobile card 也會呈現淡藍色底色，並有柔和光帶掃過。將滑鼠
停在 state 上，可查看 provider failure reason 等細節。

起始時間已知時，list row 與 mobile card 會在 state 下方顯示 timer icon 與持續
更新的執行時間；Server detail header 則顯示在 state 旁。Swallow OS deployment
從 deployment 開始時起算，從 **Deploying** 進入 **Verifying** 時不會重設。
Provider 工作（**Releasing**、**Inspecting**、**Testing**，或不是由 swallow
發起的 **Deploying**）則從 swallow 第一次觀察到該 state 時起算。將滑鼠停在
執行時間上可查看起算依據。

Provider state 的起始時間是 swallow 的 observation，不是 provider 確切的
transition time；其精確度約等於 inventory reconcile 或 targeted Server refresh
的頻率。Release 等 operator action 後，list 會持續追蹤該 Server，因此時間會接近
即時。起始時間未知時不會顯示執行時間。

Release 接受後可留在 list：provider 執行期間該列會變成 **Releasing**，Server
回到 pool 後再變成 **Ready**。

### 下一步圖示與 Actions 選單

狀態需要下一步時，Deployment 欄的狀態後方會出現一個小圖示。圖示沒有文字：將滑鼠
停在圖示上或以鍵盤 focus，即可看到它的用途；連到 Activity 的圖示也會標出會開啟
Server **Activity** 分頁的哪個區塊。由上往下第一個符合的條件生效。

| 圖示 | 條件 | 開啟 |
| --- | --- | --- |
| 警示 — **Review activity** | Server 已不在 provider inventory 中（absent）。 | **Activity → Provider events** |
| 火箭 — **Deploy OS** | Server 未 lock 且為 **Ready**。 | 不離頁的 Deploy OS dialog；固定該 Server，並在 OS image、Installation 與 Networking steps 前檢查 live readiness 與 networking |
| 箭頭 — **View workflow** | Swallow deployment 正在執行。 | 該 deployment 的 Workflow |
| 眼睛 — **View activity** | 只有 provider 工作正在執行。 | Releasing 開啟 **Activity → Provisioning tasks**，Inspecting 開啟 **Related Operations**，其他開啟 **Provider events** |
| 箭頭 — **View workflow** | 上一次 swallow deployment 失敗或需要處理。 | 該 deployment 的 Workflow |
| 警示 — **Review activity** | Provider 回報 **Failed**、**Broken** 或 **Rescue**。 | **Activity → Provider events** |

閒置的 Server 沒有圖示，請從名稱開啟。Provider 操作（電源、硬體檢查、lock、
recovery、release 與 delete）都在該列的 **Actions** 選單；mobile card 上也是同一個按鈕。

Bulk **Deploy OS** 會在相同 dialog 中固定目前 selection。單台或 bulk deployment
成功後，list／detail 仍留在原頁並持續追蹤各 target 的 projection；bulk selection
會清除，成功通知則提供 **View workflow**。

## Quick view 與 fleet signal

- **All** 顯示目前 inventory。
- **Deployable** 顯示目前存在、未 lock 且為 `ready` 的 Server。
- **Active deployments** 包含 `inspecting`、`deploying`、`releasing`、
  `testing` 的 provider 工作，以及正在 deploying 或 verifying 的 swallow
  deployment。
- **Needs attention** 包含 provider 的 `failed`、`broken`、`rescue`，以及
  failed 或 requires attention 的 swallow deployment。

Fleet overview 的 **OS deployment → Active / Attention** count、預設 operational
sort 與 row highlight 都使用相同規則。正在執行的工作會排在最前面，其次是需要
處理的狀況。

## 檢查硬體

硬體檢視以 `inspect-hardware` Workflow 執行。network boot 剛納管的 Server 會自動
開始，也可以從 **Take action → Hardware checks → Inspect hardware** 手動要求；MAAS
將此動作稱為 *Commission*。執行期間 Deployment state 是 **Inspecting**。Server detail
Summary card 的 **Inspection** 欄位仍會顯示 provider 上次的結果標籤，例如 *Passed*。

Workflow 先等 enrollment 結束（MAAS 會把 machine 關機），若 Server 已啟用 Boot Media
就先套用，再進行最多三次檢視。單次檢視若 15 分鐘沒有 provider 進度就會 abort，
從未檢視過的 MAAS machine 會因此回到 **New**。次數用完後 Workflow 會要求處理：
Server 列表與 Server detail 會顯示 **Hardware inspection needs attention**，並連到記錄原因的 Workflow。
swallow 不會把 Server 標成 failed。

處理方式依停下的原因而定：

| 原因 | 先修正，再 retry |
| --- | --- |
| Enrollment 已結束，但 Server 沒有 MAAS 讀得到的 power driver（VM 的常見情況） | 設定它的 [Power configuration](#power-configuration)；確認 **Power state** 讀得到；若仍開機就關機。 |
| Enrollment 20 分鐘內沒有結束 | 確認主機已完成 enlistment，且 MAAS 讀得到它的電源。 |
| Server 從未以網路開機進入檢視 | 若其網路不是由 provisioner 的 DHCP 提供，請啟用 Boot Media；VM 則把 NIC 或 iPXE 開機媒體排在 hypervisor 開機順序的第一位。 |
| Provider 回報檢視失敗 | 到 MAAS 查看檢視結果。 |

Retry 該 Workflow 的 Task，或重新選擇 **Inspect hardware**；兩者都會重跑整個檢視。
Retry 會再次等待 enrollment 結束，因此不會 commission 一台仍在 enlist 的 machine；
MAAS 讀到 machine 已關機後，大約 30 秒內就會通過等待。Server 沒有等待中的 Workflow
時，**Inspect hardware** 會建立一個略過等待的 Workflow，因為你已確認 Server 現在可以開機。

自動檢視只套用在 swallow 於最近 24 小時內第一次看到、且從未被 swallow 檢視過的
Server。可在 Integration 對話框以 **Inspect newly enrolled Servers automatically**
逐一關閉；手動 Inspect hardware 仍可使用。Server 為 deployed、allocated、rescue，
或正在進行其他 provider 工作或其他 Workflow 時，Inspect 會被拒絕。

## Power configuration

Server 的 power configuration 是 MAAS 用來切換與讀取它電源的 power driver 及其設定。
它由 MAAS 擁有；swallow 即時讀取，並把你的修改寫回 MAAS。設定位於 Server detail 的
**Summary**：有 BMC 的 Server 在 **Management controller** 卡片，沒有 BMC 的 Server 在
**Power control** 卡片。選擇 **Edit power configuration**（或 **Set power configuration**）。

| Driver | 適用 | 設定 |
| --- | --- | --- |
| IPMI、Redfish | 實體 Server 的 BMC | BMC 位址、帳號、密碼 |
| virsh | libvirt virtual machine | Hypervisor URI `qemu+ssh://user@host/system`、domain 名稱或 UUID、密碼（選填） |

只有 BMC driver 會讓 Server 有 Boot Media；不論 MAAS 用這兩種 driver 的哪一種，
Redfish Boot Media 都是對 BMC 探測。密碼只寫不讀：留空即保留原密碼（更換 driver
時，除非輸入新密碼，否則會移除）。

使用 `virsh` driver 時，連到 hypervisor 的是 MAAS 而不是 swallow，因此 MAAS rack
controller 必須先能以 SSH 連到 hypervisor 帳號，driver 才會生效。MAAS 以 snap 安裝時，
把金鑰、`known_hosts` 與 SSH 選項放在 `/var/snap/maas/current/root/.ssh`，並在 snap 內確認：
`sudo snap run --shell maas -c 'virsh -c qemu+ssh://user@host/system list --all'`。
URI 保持乾淨：MAAS 不接受 `?keyfile=` 之類的 query 參數。同一台 hypervisor 上的多台
VM 共用同一個 URI，以 domain 區分。

儲存不會切換電源，也不會恢復檢視。先讀 **Power state** 確認 MAAS 連得到 driver，
再 retry 檢視。屬於 MAAS VM host 的 virtual machine 由 VM host 提供電源，在這裡為唯讀。

## Provider 與 observation failure

Provider error 不會刪除最後一次 projection。UI 會將 provider failure、
observation age 與 health 分開呈現。單台 Server 需要立即更新時使用 refresh；
reconcile 仍是 fleet mechanism。

## Tag

Tag 可編輯單台 Server，也可用 tri-state bulk change。MAAS 支援 tag 時由 MAAS
authoritative，reconciliation 會 mirror 結果；不支援時由 swallow 儲存同形狀
fallback。Consumer 只會看到一個 effective tag set。

## Zone 與 Pool

Zone／Pool 是在 **Infrastructure** 管理的 swallow-owned grouping resource。
透過 Server placement action 指派。Provisioner 有能力時會 realize grouping；
否則 swallow-owned value 仍是 effective placement。

## Lock 與 destructive action

Lock 會排除 Server 的自動或 destructive management。解除前先確認沒有 Workflow
或 external operator 依賴這項保護。Power、release、delete、rescue/recovery 與
placement action 也受 provider capability 與 current state 限制。

刪除 Server 是 provider-backed action：先刪除 backing machine，再刪除 projection，
避免下一輪 reconcile 立刻重建。

## CLI 範例

```bash
swallow servers list --site-id site1 --provisioning-state deployed
swallow servers list --site-id site1 --provisioning-state inspecting
swallow servers get server1
swallow servers inspect server1
swallow servers power-configuration get server1
swallow servers power-configuration set server1 --driver virsh \
  --address qemu+ssh://maas@hypervisor.lab/system --power-id vm-01
swallow servers power-state server1
swallow integrations enroll-bundle int1          # existing OS 指令（含 MAAS API key）
swallow provisioning tags edit --server server1 --add amd-gpu
swallow infrastructure zones list --site-id site1
```

精確 flags 請執行 `swallow servers --help`、`swallow provisioning tags --help`
與 `swallow infrastructure --help`。
