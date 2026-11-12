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

實驗性 suite 是預覽：照常執行，但 check 之後可能改變。Dashboard 預設隱藏實驗性 suite，在 Validation → Suites
頁面打開 **Show experimental suites** 才顯示；這個設定存在瀏覽器，與實驗功能的設定分開。實驗性 suite 的 run 與
schedule 一律顯示。

沒有 AMD GPU 的 Server、沒有開啟 container 或 MPI 的叢集等情況，不適用的 check 會是 skipped；skipped 不會讓 run 失敗。

### Script suite

你可以撰寫自己的 suite：一個在每台目標 Server 上執行的 script，只以 exit code 判定——0 是 passed，其他是
failed，超過 timeout 是 error。輸出的最後 8 KiB 保留為 evidence。可以選擇執行帳號（automation user 或 root）、
timeout（最長一小時）與影響程度。修改 script 或這些設定會產生新的 revision；過去的 run 保留它當時的 revision。

script 依原樣儲存與顯示，並以所選帳號的權限在每台目標上執行，所以絕對不要在其中放入密碼或金鑰。

## 執行 suite

在 Dashboard 開啟 **Validation → Runs → Run a suite**，或在 Server 的 Summary、Platform 的 Overview 的 Validation
區塊按 **Run a suite**。使用 CLI：

```bash
swallow validation runs start --suite host-baseline --server srv1,srv2 --wait
swallow validation runs start --suite slurm-acceptance --platform plat1 --wait
```

目標沒有部署、被 lock 或正被其他 Workflow 使用時，swallow 拒絕這次 run；Workload 或 Disruptive 的 suite 也不接受
被 Platform 認領的 Server（請改為驗證該 Platform）。run 不會擋住你的工作：需要正在驗證的 Server 的部署、release
或軟體變更會取消這次 run 並繼續進行。超過 suite 時間預算的 run 會被取消。也可以在 run 的頁面或以
`swallow validation runs cancel <runId>` 自行取消；已記錄的結果會保留。

## 閱讀結果

run 的頁面顯示每個 check 在每個目標上的 verdict。選擇一個 verdict 可以看到該 check 的發現：摘要、量測值與預期值，
以及主機印出的 evidence。run 連到它的 Workflow，那裡有 Task 的 log。

完成的 run，任何 check failed 時 verdict 是 **Failed**；沒有 failed 但有無法判定的 check（主機連不上、缺少 probe）
時是 **Error**；其餘是 **Passed**。run 預設保留 90 天。

`swallow validation runs watch <runId>` 會跟隨 run，只有 verdict 是 passed 時 exit 0，script 可以用它作為閘門。

## 部署會執行驗收 suite

每次 Slurm 部署都在安裝 Slurm 之後執行 `slurm-acceptance` 作為閘門：GPU、container 或 MPI 不能用時，部署停下並
等待處理，失敗的 GPU 節點會被 drain。閘門的 run 與其他 run 列在一起（Started by 為 **Deployment gate**）。修正原因後，
重試部署的驗證 Job 會重跑整個 suite。

## Schedule

schedule 會自己啟動 run：依某個時區的 cron expression（例如 Asia/Taipei 每晚 02:00），或在目標每次部署之後。
run 在時間點到了或部署完成後的一分鐘內啟動。swallow 停機期間錯過的時間點，恢復後補跑一次。前一次 run 尚未結束時，schedule 不會再啟動新的 run；目標被佔用、
被 lock 或沒有部署時略過這一次，並顯示原因。在 Dashboard 使用 **Validation → Schedules**；使用 CLI 時是
`swallow validation schedules create -f schedule.yaml`。

## 相關文件

- [CLI usage：validation](../../../cli/docs/usage.zh-TW.md#validation)
- [Validation API 契約](../../../api-server/docs/development/api-contracts/api-server/validation.md)
- [Workflows](workflows.md)
