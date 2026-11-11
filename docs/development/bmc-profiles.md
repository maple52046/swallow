# BMC Profiles 設計與規範

本文件是 Redfish adapter 中 [BMC Profile](glossaries/terms/bmc-profile.md) 的現行規格：swallow 怎麼辨識一台 BMC
屬於哪個家族、哪些行為是所有 BMC 共用的 DMTF 標準做法、哪些是某個家族的處理，以及新增或修改一個 profile 的流程。
Boot Media 對 user 可見的行為（probe、enable preflight、`ensure-boot-media`、部署開機的補救）見
[server-enrollment-and-boot-media.md](server-enrollment-and-boot-media.md) §4；決策見
[decision 047](../decisions/047-redfish-boot-media.md)（redfish boot media）。

> **讀者**：修改 Redfish adapter（`api-server/internal/adapters/bmc/redfish`）的開發者與 agent：為一種新的 BMC 修正
> Boot Media、新增或修改 profile、改共同的探索、掛載或開機導向。只透過 API 讀 `redfish.profile` 時讀契約
> [server-detail-actions.md](../../api-server/docs/development/api-contracts/api-server/server-detail-actions.md)。

> **現況**：`default`、`ami`、`supermicro` 三個 profile。`ami` 依 hv-01 的紀錄製作，現在沒有可以實測的 AMI 機器；
> `supermicro` 依 pvt250 的唯讀巡查製作，2026-11-10 在同一台完成 enable 與 RAM 部署（§6.3）。

---

## 1. 為什麼要有 profile

Redfish 只標準化了 schema；各家 BMC 在 virtual media 與開機導向上的行為差很多，而且只能在真機上發現：

- AMI MegaRAC 出廠時關閉 remote media，要用 OEM action 打開；它會改寫掛上的 ISO URL；它的 Aptio BIOS 以固定的
  群組順序開機，DMTF 的 `Cd` override 會讓它卡住。
- Supermicro 讀 BIOS 設定需要付費的 DCMS 授權（HTTP 403），但 AMI 的策略正好需要讀 BIOS。

所有家族的處理寫在同一條路徑時，為 B 家族的修改很容易改到 A 家族的行為，而 A 家族又往往沒有機器可以重測。
profile 把每個家族的處理隔開：改一個 profile 只會影響被辨識成它的 BMC，而共同部分的修改會被每個 profile 的假 BMC
一起測試（§5）。swallow 不可能窮舉所有廠牌、型號與韌體，所以認不出的 BMC 使用 `default` profile：只用 DMTF 標準
做法，可以用就是可以用，不能用時由 enable preflight 以 BMC 自己的錯誤訊息失敗，不猜測任何廠商行為。

profile 屬於 provider adapter（[core-and-extensions.md](core-and-extensions.md) §1：怎麼和某個外部系統溝通）。core 不知道
有哪些 profile，也不依 profile 改變行為；core 只看到 port 的結果與 `RedfishCapability.Profile` 這個不透明的顯示值。

## 2. 共同部分與 profile 的界線

**規則**：共同部分只放 DMTF Redfish schema 定義的行為。廠商名稱、OEM 欄位與韌體怪癖只能寫在
`profile_<vendor>.go`，而且要附上觀察來源：哪台機器、型號、韌體版本與日期。

| 檔案 | 內容 |
| --- | --- |
| `transport.go` | HTTP Basic auth、忙碌與傳輸錯誤的重試、ETag 與 412 重試、錯誤分類（§2.1） |
| `controller.go` | 探索（Systems、host System、Manager、VirtualMedia、BootOptions）、InsertMedia、Task 追蹤、Eject、Reset、settle、讀回驗證 |
| `resources.go` | Redfish 資源的型別、`virtualCDByPath`、CD 是否可插入或退出 |
| `boot.go` | 開機導向的策略介面與 DMTF 策略（§4） |
| `profile.go` | `identity`、`profile`、`defaultProfile`、辨識順序 |
| `profile_ami.go`、`profile_supermicro.go` | 各家族的 profile 與它專用的程式 |

`TestCommonFilesHoldNoVendorTerms` 守住這條界線：共同檔案出現 `AMI`、`Aptio`、`MegaRAC`、`Supermicro`、`ATEN`、`iDRAC`、
`iLO`、`XCC` 等詞就失敗。新的廠商詞彙加進它的清單。

### 2.1 共同的錯誤分類

| BMC 的回應 | 錯誤 | 永久性 | probe 的 `support` |
| --- | --- | --- | --- |
| HTTP 401 | `ErrBMCUnreachable`（帳號被拒） | 永久 | `unreachable` |
| HTTP 403 | `ErrRedfishUnsupported`，detail 帶出請求與 BMC 的訊息（例如 `OemLicenseNotPassed`） | 永久 | `unsupported` |
| 其他 4xx／5xx | `ErrBootMediaRejected`，detail 是 BMC 的訊息 | 依狀態碼 | — |
| 連不上、逾時、忙碌到 context 結束 | `ErrBMCUnreachable` | 暫時 | `unreachable` |

每個錯誤都記下失敗的 operation（`GET /redfish/v1/Systems/1/Bios`、`mount the ISO`）與永久性，port 的方法回傳前再記上
BMC 的 service root（不含帳密，`atEndpoint`）；application 把它們放進 `BootMediaError`，`ensure-boot-media` 失敗時再成為
Task diagnostic（decision 077）。錯誤文字從不包含帳密。

### 2.2 共同的虛擬光碟規則

- 有 `InsertMedia` action 的空閒 CD 才能拿來掛載；已經掛著、只提供 `EjectMedia` 的 CD 仍算虛擬光碟（Supermicro 的
  掛載中 CD 就是這樣）。
- InsertMedia 先帶 `Image`、`TransferProtocolType`、`Inserted: true`、`WriteProtected: true`。DMTF 讓後三個是選用的，
  BMC 不一定支援：回 400 拒絕參數時，去掉訊息 `MessageArgs` 點名的那一個選用參數再送（例如 `PropertyUnknown`、
  `ActionParameterNotSupported`）；`ActionParameter*`／`Property*` 的拒絕沒有點名時，一次去掉 `Inserted` 與
  `WriteProtected`。每次重送至少少一個參數，沒有可去掉的就把錯誤交出去。`Image` 從不去掉；`*ValueFormat*` 是 URL
  本身的格式被拒（例如 AMI 拒絕 `https://`），不重送。
- BIOS 開機選項中的虛擬光碟預設依 `UefiDevicePath` 辨認：同時含 `CDROM(` 與 `USB(`。

## 3. 辨識

探索讀到 service root 時辨識一次，讀到 Manager 後再辨識一次（有些特徵只在 Manager 上）。辨識依據 `identity`：

- service root 的 `Vendor`；
- service root 與 Manager 的 `Oem` 物件的 key；
- host System 與 Manager 的 action 名稱，包含 OEM action。

`recognized` 依序比對：先 `ami`，再 `supermicro`，都不符就用 `default`。特徵可能重疊時（例如別的品牌賣的 AMI BMC），
比較早的 profile 優先，所以新 profile 排在可能和它共用特徵的 profile 後面。

每次呼叫都重新辨識，不儲存、不讓 user 選擇。probe 把辨識結果記在 capability 的 `profile`，只供顯示；dashboard 把
`default` 標成未驗證的 BMC。

## 4. 開機導向的策略積木

profile 依序列出它信任的策略，第一個能導向這台 System 開機的被採用：

```go
type bootStrategy interface {
	// prepare binds the strategy to one discovery, reading what it needs (the Aptio BIOS order);
	// it returns nil when the strategy cannot direct this System's boot.
	prepare(ctx context.Context, s *session, found *discovered) (bootDirection, error)
}

type bootDirection interface {
	direct(ctx context.Context, c *Controller, s *session, found *discovered) (mode string, err error)
	directsBoot(found *discovered) bool // the read-back check
}
```

`prepare` 回傳 error 時探索失敗，不退回下一個策略：讀不到它需要的狀態時，猜測比失敗更危險（例如 Aptio 上退回 `Cd`
會讓主機卡住）。沒有策略可用時，probe 回報 `unsupported`，原因是 profile 的 `noDirection`。

| 策略 | 檔案 | 做法 | `bootOverride` |
| --- | --- | --- | --- |
| `uefiBootOrder` | `boot.go` | BIOS 列出虛擬光碟的 UEFI 開機選項時，移到 `BootOrder` 第一，並設 `UefiBootNext`＋`BootNext` | `Once` |
| `classTarget{"Cd"}` | `boot.go` | DMTF 的 `BootSourceOverrideTarget` 類別目標 | System 允許時 `Continuous`，否則 `Once` |
| `aptioFixedOrder` | `profile_ami.go` | 把 BIOS fixed boot order 的 "UEFI USB Device" 群組排到第一 | `Continuous` |

## 5. 怎麼確保改 B 不會壞 A

- **每個 profile 一台假 BMC**，依真機資料製作：`fake_ami_test.go`（hv-01）、`fake_supermicro_test.go`（pvt250）、
  `fake_dmtf_test.go`（標準 Redfish，`Vendor` 是 Contoso）。
- **port 一致性測試**：`internal/core/server/domain/bmcporttest.Run` 描述 core 對 `RedfishController` port 的期待，與廠商
  無關（可用的 BMC 能 apply 到 ready、401 是 `unreachable`、403 是帶 BMC 訊息的 `unsupported`、沒有虛擬光碟是
  `unsupported`、忙碌會重試、錯誤只包一個 sentinel 且沒有帳密）。`conformance_test.go` 讓每台假 BMC 都跑一遍；慣例
  見 [core-and-extensions.md](core-and-extensions.md) §3.7。
- **profile 測試**（`profile_test.go`）：辨識（給 identity，選到哪個 profile；認不出就是 `default`）、每台假 BMC 的預期
  （profile、策略、`bootOverride`、有沒有讀或寫 BIOS），以及守門測試。

改共同部分時三台假 BMC 都會跑；改一個 profile，只改它那一列的預期。改到 AMI 的程式時，`controller_test.go` 的 AMI
assertion 不得放寬，因為沒有 AMI 機器可以重測。

## 6. 現有的 profile

### 6.1 `default`

- 辨識：沒有任何 profile 相符。
- ISO 比對：BMC 回報的 image 必須和掛上的 URL 完全相同。
- 虛擬光碟開機選項：依 `UefiDevicePath`。
- 策略：`uefiBootOrder`，再 `classTarget{"Cd"}`。不讀 BIOS。
- settle：3 分鐘（已知最慢 BMC 的量測值）。

### 6.2 `ami`

- 觀察來源：hv-01，AMI MegaRAC，韌體 13.06.10，AMI Aptio BIOS，2026-10-03 的 probe 紀錄（decision 047）。
- 辨識：`Vendor` 是 `AMI`；或 service root、Manager 的 `Oem` 有 `Ami`；或有 `#AMIVirtualMedia.*` action。
- 掛載前：退出掛著同一 ISO 路徑但來自別的 host 的 CD（舊的 Boot Media base URL）；remote media 關閉
  （`Oem.Ami.VirtualMedia.RMediaStatus`）時以 `AMIVirtualMedia.EnableRMedia` 打開。
- ISO 比對：忽略 scheme，接受 AMI 改寫的 `//host/dir/file.iso/file.iso` 與 `ImageName`。
- 掛載在掉了 mount 但仍記著 image 的 CD 上之前先退出它（`ejectStaleImage`）。
- 策略：`aptioFixedOrder`、`uefiBootOrder`、`classTarget{"Cd"}`；BIOS 讀取失敗時探索失敗，不退回 `Cd`。
- settle：3 分鐘；InsertMedia 的 Task 完成後 18 或 67 秒就開機時 CD 脫落，三分鐘撐過了 provider 的 power-on。
- 已知限制：fixed boot order 的寫入在 BIOS 套用後沒有保留，見 server-enrollment-and-boot-media.md §4.5。

### 6.3 `supermicro`

- 觀察來源：pvt250（MAAS 名稱 rich-gecko），Supermicro AS-4124GQ-TNMI，主機板 H12DGQ-NT6，BMC 韌體 01.01.24，
  Redfish 1.9.0，2026-11-10 唯讀巡查。
- 辨識：`Vendor` 是 `Supermicro`，或 `Oem` 的 key 是 `Supermicro`（這台的 service root 沒有 `Vendor`，只有 Oem key）。
- 不讀 BIOS：`GET Systems/1/Bios` 在沒有 DCMS 授權時回 403 `SMC.1.0.OemLicenseNotPassed`，而 Boot Media 不需要它。
- 虛擬光碟開機選項：BootOption v1.0.3 沒有 `UefiDevicePath`，所以沒有路徑時以名稱辨認（含 "ATEN Virtual CDROM"，
  例如 `UEFI: ATEN Virtual CDROM YS0J`）。
- 虛擬光碟在 `Managers/1/VirtualMedia/CD1`；掛著時只列出 `EjectMedia`。
- 策略：只有 `uefiBootOrder`（BootOptions、`BootOrder`、`UefiBootNext` 不需要授權）。BIOS 還沒列出虛擬光碟時回報
  `unsupported`，原因是「掛上 ISO 並開機一次後才會列出」，不以類別目標猜測。
- ISO 比對：完全相同；settle 先沿用 3 分鐘。
- 實測結果（2026-11-10，rich-gecko，ISO 先前已手動掛上、`Boot002A` 已列出）：
  - probe 回報 `profile: supermicro`、`support: supported`，`bootOverrideModes` 為 `Once`、`Continuous`。
  - enable preflight 8 秒完成：BMC 回報的 CD1 image 與 Boot ISO URL 完全相同，所以不重新掛載、不 settle；策略是
    `uefiBootOrder`，`bootOverride: Once`，讀回 `overrideTarget: UefiBootNext`、ready。
  - 經 swallow Release 後 RAM 部署（Workflow `f3af8515-d3b6-408e-a2dd-e99e9b0587f6`）：`ensure-boot-media` 6 秒成功。
    MAAS 以 IPMI 開機並強制 PXE 後，主機十分鐘內沒有網路開機進入部署；media check 不需要重掛（mount 一直保持），
    boot check 經 Redfish 重開主機，之後部署約 11 分鐘完成，整個 Workflow 26 分鐘、無人操作。**force-PXE 失敗後
    BIOS 沒有照 `BootOrder` 開進 ISO，這台的部署靠 §4.8 的 boot check**；第一次開機停在哪裡（PXE 等待或磁碟）
    沒有觀察到。
  - 開機後 `UefiBootNext` 已被用掉（`overrideTarget: None`），但虛擬光碟仍在 `BootOrder` 第一，讀回仍是 ready。
- 從空的 CD 掛載（2026-11-11，rich-gecko，使用者先在 BMC web UI 退出 ISO，再從 dashboard 停用與啟用）：
  - InsertMedia 以 400 `PropertyUnknown` 拒絕 `Inserted`（「The property Inserted is not in the list of valid
    properties for the resource.」），所以 §2.2 的重送依 `MessageArgs` 去掉它；`WriteProtected` 會不會也被拒還沒看到，
    `fakeSupermicro` 照較嚴的情況兩個都拒。
  - 退出 ISO、主機沒有 POST 之前，probe 仍是 `supported`：BootOptions 還列著虛擬光碟。
  - 修正後經 swallow 重新啟用（01:35–01:38 UTC）：preflight 3 分 15 秒完成，幾乎都是 settle；**沒有 DCMS 授權也能用
    Redfish InsertMedia 掛 ISO**。讀回 `mediaInserted: true`、image 與 URL 完全相同、`Once` 指向 `UefiBootNext`、
    ready。掛載時 BMC 只對 ISO URL 送了兩次 HEAD，內容在主機讀光碟時才取。
  - BMC web UI 把 URL 拆成 Share Host（`http://10.170.168.20`）與 Path to Image 兩欄，這只是畫面設計：Redfish 的
    `Image` 是一整條 URL，手動掛上的 ISO 讀回的 `Image` 與 Boot ISO URL 完全相同。

## 7. 新增或修改 profile 的流程

1. **唯讀巡查真機**（先讀 `skills/README.md` 與 lab-verification skill）：service root、Systems、Managers、
   VirtualMedia、BootOptions、`Boot` 物件、actions，以及失敗請求的完整錯誤訊息。帳密不得出現在輸出或紀錄中；遇到
   401 就停，避免鎖住 BMC 帳號。
2. **決定是不是新 profile**：差異屬於 DMTF 標準（例如共同程式漏掉了 schema 允許的形狀）就修共同部分；只有某個家族
   這樣做才放進 profile。
3. **做假 BMC**：`fake_<vendor>_test.go`，資源形狀與錯誤照抄巡查結果，並實作 `bmcporttest.Fixture` 的情境。
4. **寫 profile**：`profile_<vendor>.go`，doc comment 寫出觀察來源與每項差異的理由；加進 `recognized`（注意順序）；
   廠商詞彙加進守門測試。
5. **跑測試**：一致性測試與 profile 測試全部通過，既有 profile 的預期不改。
6. **在真機做 preflight**：經 swallow enable Boot Media，再做一次 OS 佈署，觀察 force-PXE 後的開機與部署是否無人
   操作完成。
7. **寫進本文件**：§6 的一節（辨識、各項設定、觀察來源與實測結果），以及 §8 的未驗證項目。

## 8. 已知限制與待驗證

- 沒有 AMI 機器可以重測；AMI 的回歸只靠 `fakeAMI` 與一致性測試。
- Supermicro（實測已確認：image 完全相同；InsertMedia 不需要 DCMS 授權；force-PXE 後要等 boot check 經 Redfish
  重開，部署因此多約十分鐘）：
  - InsertMedia 會不會也拒絕 `WriteProtected`（掛載成功，但重送了幾次沒有紀錄）；
  - 從沒掛過 ISO 的 BIOS，在剛掛上、下一次 POST 前會不會列進 BootOptions（不會的話，第一次 enable 只能回報
    `unsupported`，要考慮加上 `classTarget{"UsbCd"}`）；2026-11-11 退出後 BootOptions 仍列著，回答不了這一項；
  - 主機在沒有 ISO 的情況下 POST 之後，BootOptions 還會不會列著虛擬光碟；
  - settle 需要多久；
  - force-PXE 失敗後第一次開機停在哪裡，以及有沒有不必重開就開進 ISO 的做法。
- 辨識只看 BMC 自己的回報；同一家族不同韌體的差異目前沒有版本條件，遇到時在 profile 內依韌體版本分支，並在本文件
  記下版本界線。

## 9. 程式位置

| 內容 | 位置 |
| --- | --- |
| Redfish adapter 與 profile | `api-server/internal/adapters/bmc/redfish/` |
| port 與 `RedfishCapability.Profile` | `api-server/internal/core/server/domain/boot_media.go` |
| port 一致性測試 | `api-server/internal/core/server/domain/bmcporttest/` |
| `BootMediaError` 的組成 | `api-server/internal/core/server/application/boot_media.go`（`explain`） |
| `ensure-boot-media` 的 Task diagnostic | `api-server/internal/app/boot_media_steps.go`（`bootMediaDiagnostic`） |
| 畫面上的 profile | `dashboard/src/presentation/components/serverSummary/`（`bmcProfileLabel`） |
