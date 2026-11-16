# Validation

[English](../../en/guides/validation.md) · [文件首頁](../README.md)

Validation 檢查已部署的 Server 與 Platform 是否真的能用，而不只是已經部署：Server 的時鐘、CPU、記憶體與磁碟；
Server 之間的網路；以 HIP runtime 驗證 AMD GPU；Kubernetes 叢集能不能執行 pod、轉送 Service；Slurm 叢集能不能
交出可用的 GPU、執行 container、啟動 MPI。你選擇一個 **Validation Suite**、它的目標與參數；swallow 以 Workflow
執行，並在 **Validation Run** 記錄每個 check 在每個目標上的 verdict。

## Suite

suite 是一組 check 與它的影響程度：

| 影響 | 意義 |
| --- | --- |
| Observe | 只讀取目標，或探測量小到運作中的 workload 察覺不到（幾個 ping、DNS 查詢、port 檢查）。 |
| Workload | 在一段時間內佔用或吃滿 workload 使用的資源（GPU 運算或記憶體、頻寬、scheduler 的節點）。 |
| Disruptive | 可能中斷服務；每次 run 都要確認，schedule 不能啟動。 |

swallow 的模組提供這些 suite：

| Suite | 驗證對象 | 影響 | Checks |
| --- | --- | --- | --- |
| `slurm-acceptance` | Slurm Platform | Workload | 每台節點都能執行 step、job 能共用節點、rootless OCI container、PMIx 的 MPI、GPU GRES |
| `host-baseline`（實驗性） | Server | Observe | 與安裝設定的 NTP server 時間同步、CPU 數、記憶體、root filesystem 有空間且可寫入 |
| `network-mesh`（實驗性） | Server | Observe | 每台目標都能連到其他目標、能傳送不分段的完整大小封包，並接受 SSH 連線 |
| `amd-gpu-basic`（實驗性） | Server | Workload | amdgpu driver 帶起 PCI 上的每張 AMD GPU，且每張都通過 HIP probe |
| `kubernetes-smoke`（實驗性） | Kubernetes Platform | Observe | 節點 Ready、每台收工作的節點都能執行 pod、cluster DNS、Service 轉送 |

Dashboard 的 **Validation → Suites** 是能力 catalog，依 Platform、Server 與自訂 script 分組。開啟 card 可查看 checks、parameters、impact、budget、最近 Runs 與引用它的 Schedules；直接連到實驗性 suite 仍可閱讀。

實驗性 suite 是預覽：照常執行，但 check 之後可能改變。Dashboard 預設隱藏實驗性 suite，在 Validation → Suites
頁面打開 **Show experimental suites** 才顯示；這個設定存在瀏覽器，與實驗功能的設定分開。實驗性 suite 的 run 與
schedule 一律顯示。同樣地，**Hide other users' suites**（預設關閉，存在瀏覽器）會略過其他使用者撰寫的 script suite；
它從不隱藏 module suite 或你自己的 suite。

沒有 AMD GPU 的 Server、沒有開啟 container 或 MPI 的叢集等情況，不適用的 check 會是 skipped；skipped 不會讓 run 失敗。

### Script suite

每位使用者都能撰寫自己的 suite 並與他人共用：一個在每台目標 Server 上執行的 script，只以 exit code 判定——0 是
passed，其他是 failed，超過 timeout 是 error。輸出的最後 8 KiB 保留為 evidence。module suite 不能修改。

- **誰撰寫。** 建立 script suite 的人就是它的作者，card 與頁面上顯示為 **Created by**。只有作者能編輯它——不是作者的
  admin 也不行。作者或 admin 可以 retire 它；retire 後的 suite 離開 catalog、不能再執行，過去的 run 與 revision 仍可
  閱讀。作者已被刪除的 suite 會顯示原本的 username 並標示 *author deleted*；沒有人能編輯它，admin 可以 retire 它。
- **誰能啟動。** 作者選擇啟動層級：**Pool owners**（預設）只有每個目標所屬 Pool 的 owner 能啟動；**Pool members
  too** 讓每個目標所屬 Pool 的任何 member 都能啟動。admin 能啟動任何 suite。module suite 只有 Pool owner 能啟動。
- **以誰的身分執行。** script suite 在每台目標上以啟動這次 run 的使用者自己的 Linux 帳號執行，擁有該使用者在那台
  Server 上的權限——不會以 root 或 swallow 的 automation user 執行。打開 **Run with sudo** 時，script 透過啟動者自己的
  `sudo -n` 執行，所以只會在 Pool 的 Sudo Policy 給啟動者免密碼 sudo 的目標上執行；沒有打開時，script 仍可在 policy
  允許的地方自己呼叫 `sudo -n`。HOME 是啟動者的 home 目錄（存在且屬於他時），否則是 run 結束後刪除的私有目錄。
- **Revision。** 修改 script、sudo、啟動層級、timeout（最長一小時）或影響程度會產生新的 revision；只改名稱或說明不會。
  每個 revision 都會保留並可閱讀，過去的 run 保留它當時的 revision。儲存只套用在你開啟的那個 revision：如果有人
  （例如你自己在另一個分頁）已經存了更新的 revision，這次儲存會以 conflict 拒絕，不會覆蓋任何內容——重新載入後再編輯
  新的 revision。

每位使用者都讀得到 script 的原文，能讀取 run 的人也都讀得到它的輸出，所以絕對不要在其中放入密碼、金鑰或其他機密。

#### 每台目標需要什麼

因為 script 以你自己的帳號執行，swallow 在接受 run 之前會確認每台目標都能以你的身分執行它，否則拒絕這次 run：

| 拒絕的情況 | 狀態 |
| --- | --- |
| 你的身分不能在某台目標上啟動這個 suite（它的啟動層級） | `403 forbidden` |
| 你不能登入某台目標（由你的 Pool 與 Platform 的設定決定；見 [Zone 與 Pool](servers-and-infrastructure.md#zone-與-pool)） | `403 forbidden` |
| suite 使用 sudo，而 Sudo Policy 沒有給你某台目標上的 sudo | `403 forbidden` |
| suite 使用 sudo，而你在某台目標上的 sudo 需要密碼 | `409 conflict` |
| 沒有設定 Directory、你的帳號沒有 uid，或某台目標沒有 [directory client](managed-software.md#directory-clientsssd)（所有非 Ubuntu 的 Server） | `409 conflict` |

錯誤訊息會指出是哪台目標。主機的實際狀況仍可能與 swallow 看到的不同，例如 sudo 規則還沒到達主機（規則大約一分鐘
生效），或本地帳號與你同名。這時那台目標的結果是 **Error**，summary 以一個代碼開頭：

| 代碼 | 意義 |
| --- | --- |
| `[validation_account_missing]` | 主機上沒有與你的 username 相同的帳號。 |
| `[validation_account_mismatch]` | 主機上與你同名的帳號 uid 與你的不同。 |
| `[validation_become_failed]` | automation user 不能在這台主機上以你的帳號執行指令。 |
| `[validation_sudo_unavailable]` | suite 使用 sudo，而你的 `sudo -n` 在這台主機上失敗；後面附上 sudo 的訊息。 |
| `[validation_script_unplaced]` | script 無法放到這台主機上。 |

## 執行 suite

在 Dashboard 開啟 **Validation → Runs → Run a suite**、Suite detail 的 **Run suite**，或 Server／Platform 的 Validation 區塊。共用流程先選 Suite 與 targets，再檢查 parameters、impact、budget 與最後 review。在 all-sites scope 選擇多台 Server 時，第一台會限制其餘目標必須屬於同一個 Site。使用 CLI：

```bash
swallow validation runs start --suite host-baseline --server srv1,srv2 --wait
swallow validation runs start --suite slurm-acceptance --platform plat1 --wait
```

目標沒有部署、被 lock 或正被其他 Workflow 使用時，swallow 拒絕這次 run；Workload 或 Disruptive 的 suite 也不接受
被 Platform 認領的 Server（請改為驗證該 Platform）。run 不會擋住你的工作：需要正在驗證的 Server 的部署、release
或軟體變更會取消這次 run 並繼續進行。超過 suite 時間預算的 run 會被取消。啟動這次 run 的使用者、每個目標所屬 Pool 的
owner 或 admin，可以在 run 的頁面或以 `swallow validation runs cancel <runId>` 取消它；已記錄的結果會保留。

### 執行改過的 script suite

執行 script suite 之前，Dashboard 會顯示它的作者、revision、script，以及它**以你的帳號執行**、是否使用 sudo。如果
suite 在你上一次執行之後改過，run 對話框會先顯示 **Changed since your last run: rN → rM**：每個改變的設定，以及兩份
script 逐行的 diff（較早的 revision 早於開始保留 revision 時，則顯示完整的新 script）。**Use the new version** 是唯一
能繼續的方式，之後這次 run 固定在那個 revision。對話框開著時作者又存了新的 revision，啟動會以 conflict 拒絕，對話框
接著顯示新的改動讓你檢視。run 絕不會執行比目前 revision 更舊的版本。

使用 CLI 時，讀取 revision 並固定你檢視過的那一個：

```bash
swallow validation runs list --suite <suiteId> --mine --page-size 1   # 你上次執行的 revision
swallow validation suites revision <suiteId> <revision>                # 它的 script 與設定
swallow validation runs start --suite <suiteId> --server srv1 --suite-revision <revision> --wait
```

`--suite-revision` 已經不是目前的 revision 時，啟動會以 `409 conflict` 拒絕。

## 閱讀結果

run 頁面先列出 **Failed** 與 **Error** 問題，包括 target、summary、measurements 與可展開的 evidence。Failed 表示 check 已完成判定但不符合預期；Error 表示無法完成判定。下方仍保留完整 check × target 矩陣供稽核，包含 Passed 與 Skipped；選擇 verdict 會在該 check 旁展開 inspector。run 連到它的 Workflow，那裡有 Task 的 log。

完成的 run，任何 check failed 時 verdict 是 **Failed**；沒有 failed 但有無法判定的 check（主機連不上、缺少 probe）
時是 **Error**；其餘是 **Passed**。run 預設保留 90 天。

`swallow validation runs watch <runId>` 會跟隨 run，只有 verdict 是 passed 時 exit 0，script 可以用它作為閘門。

## 部署會執行驗收 suite

每次 Slurm 部署都在安裝 Slurm 之後執行 `slurm-acceptance` 作為閘門：GPU、container 或 MPI 不能用時，部署停下並
等待處理，失敗的 GPU 節點會被 drain。閘門的 run 與其他 run 列在一起（Started by 為 **Deployment gate**）。修正原因後，
重試部署的驗證 Job 會重跑整個 suite。

## Schedule

schedule 會自己啟動 run：**每次部署後**、**Daily**、**Weekly**，或某個 IANA 時區下的五欄 **Custom cron**。無法識別為 Daily／Weekly 的既有 expression 會原樣保留在 Custom。
run 在時間點到了或部署完成後的一分鐘內啟動。swallow 停機期間錯過的時間點，恢復後補跑一次。前一次 run 尚未結束時，schedule 不會再啟動新的 run；目標被佔用、
被 lock 或沒有部署時略過這一次，並顯示原因。在 Dashboard 使用 **Validation → Schedules**；使用 CLI 時是
`swallow validation schedules create -f schedule.yaml`。

schedule 由 admin 管理，而且只能啟動 module suite：script suite 以啟動它的使用者身分執行，而排程的 run 沒有這樣的
使用者。在這條規則之前建立、指向 script suite 的 schedule 不會啟動 run，並說明原因（**Script suite not
schedulable**）；請把它改成 module suite，或刪除它。

## 相關文件

- [CLI usage：validation](../../../cli/docs/usage.zh-TW.md#validation)
- [Validation API 契約](../../../api-server/docs/development/api-contracts/api-server/validation.md)
- [Workflows](workflows.md)
