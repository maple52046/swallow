# Platform

[English](../../../docs/en/guides/platforms.md) · [文件首頁](../README.md)

Platform 是由 swallow 佈署的 Kubernetes 或 Slurm runtime。swallow 不會註冊任意
既有 runtime，也不會把自己稱為 Platform。

![Platform deployment wizard](../../assets/platform-deployment-wizard.png)

## 佈署前

- Target Servers 屬於同一個 Site，且已完成 OS deployment。
- Server lock 與 active conflicting Workflow 已清除。
- Site automation、SSH known hosts、credentials、worker 與 Ansible executor ready。
- 選擇的 topology 符合 minimum resource requirement。
- 選擇的 Slurm topology 所需 shared state／workload storage 已可用。

送出 request 時 wizard 會重新驗證 current eligibility。

開啟 **Deploy platform** 後，先從 runtime catalog 選擇 Kubernetes 或 Slurm，再進入各自的
deployment workspace。兩者只共用 Server discovery 與 OS provisioning controls；steps、
validation、features 與 review 仍依 Platform 類型設計。Optional capability 使用 Switch，
topology 與 implementation mode 使用明確的 radio choice，Slurm node roles 則可各自複選。


## Kubernetes

選擇 supported topology，指派 control-plane 與 worker role。Standalone deployment
仍是一個 Kubernetes Platform。Provider／image combination 支援時，deployment
也可要求 ephemeral OS target。

佈署完成後，Platform detail 會提供 live Kubernetes explorer：

- Namespace 與 application。
- Pod、log 與 related resource。
- YAML apply。
- Node cordon state。

開啟 Kubernetes Platform 後，使用 **Applications**、**Namespaces** 與
**Cluster** 分頁：

- **Applications** 會列出 Deployment、DaemonSet、StatefulSet 與 bare Pod。可依
  namespace 篩選；Deployment 與 StatefulSet 可 scale；Deployment、DaemonSet 與
  StatefulSet 可 restart；也可讀取第一個 Pod 的 bounded log snapshot，或刪除
  workload。**Apply YAML** 可先執行 server-side dry run，再套用一個或多個 object。
- **Namespaces** 可建立與刪除 namespace；system namespace 會標示出來且不能刪除。
- **Cluster** 顯示 version、readiness、schedulable GPU resource，以及每個 node
  對應的 Server。**Cordon** 會阻止新工作排入 node；**Uncordon** 讓它恢復可排程。
  兩者都不會 drain 既有 Pod。

這些 view 會 live query Kubernetes API，不會在 swallow 建立 in-cluster state
的 durable copy。Cluster 頁籤也會顯示每個節點提供的 AMD GPU。

### AMD GPU Operator

Kubernetes workspace 的 **Features** 步驟可以在佈署 Platform 時一併安裝 AMD GPU Operator。
開啟 **AMD GPU Operator** Switch 後才會展開選項：

- **Allocation mode**：**Device Plugin**（Pod 以 `amd.com/gpu` 請求 GPU）或 **DRA**
  （Pod 透過 ResourceClaim 取得 GPU，需要 Kubernetes 1.34 以上）。
- **GPU Operator version**，選 DRA 時另有 **DRA driver version**，都來自 swallow 驗證過的版本。
- Device Plugin 的 **Resource naming**、**Node Labeller** 與 **Device Metrics Exporter**。

不需要挑選節點。operator 附帶的 Node Feature Discovery 會找出所有有支援 AMD GPU 的節點（實體 GPU 或
SR-IOV VF），operator 全部納管；之後加入叢集的節點，會在下一次調整 operator 時納入。

operator 使用 OS image 內建的 GPU driver，請選擇含 `amdgpu` driver 的 image。若 image 開機時不載入
`amdgpu`（部分 Instinct 與 SR-IOV image 會刻意 blacklist），swallow 會在每個納管節點載入它，並在每次重開機、
SSH 起來之後再載入一次，Kubernetes 會等它載入後才啟動。所有納管節點都提供
GPU 之後，佈署才算成功。選取的 workload 節點都沒有 AMD GPU 時，Switch 會停用並顯示
文字原因。

佈署完成後，Platform Overview 的 **AMD GPU Operator** 區塊會顯示狀態與設定，並提供
**Install**、**Configure** 與 **Uninstall**。仍有 Pod 使用 AMD GPU 時，切換 Device Plugin 與 DRA
或卸載都會被拒絕，請先停止那些工作。卸載 operator 不會影響 Platform 本身。swallow 不會
接管手動安裝的 operator。

Device Metrics Exporter 供叢集自己的監控使用；swallow 的 GPU 圖表照舊讀取主機上的 exporter。

## Slurm

Slurm workspace 可獨立指派 controller、compute 與 login roles，並依 topology 與 feature
決定 state-server 與 workload storage responsibility。Features step 分別以 Switch 啟用
shared workload storage、container runtime 與 Omnistat；符合條件的 compute node 會自動
包含 AMD GPU GRES，不提供沒有實際語意的開關。

GPU node 加入前，swallow 會透過 HIP runtime 在每張 GPU 上配置並釋放記憶體來證明 GPU 可用，節點上直接
跑一次、透過 Slurm 再跑一次；失敗的節點會被 drain，部署也會帶著原因失敗。GRES type 預設是 GPU 架構（例如
`gfx942`）。在 **Advanced settings** 可以指定既有 job script 使用的 GRES type（例如 `mi308x_vf`），以及
amdgpu driver module option。swallow 已會套用節點 GPU topology 需要的選項（例如在 ROCm 結束時會 crash 的
情況下加上 `pcie_p2p=0`），所以大多數部署可以留空。這些選項要等 driver 載入後才看得出來，swallow 會原地重新載入
driver、不重開機，所以 RAM（ephemeral）部署也適用；自己指定的選項在第一次載入就生效。option 名稱打錯時，部署會
立刻失敗並提示最接近的正確名稱。

從 **Platforms → Settings** 設定 system-wide Slurm deployment requirement。
啟用 policy 後，設定每個選取的 Slurm node 必須提供的最低 **CPU cores**、
**Memory GiB** 與 **Storage GB**。啟用時三個值都必填；停用則移除這個 floor。
Deploy wizard 會說明無法選擇某個 Server 的原因，並在送出時重新檢查 policy。

Requirement 是 hardware eligibility floor，不會保留 resource、不表示 Slurm
scheduler capacity，也不保證 OS image 能放進 ephemeral root filesystem。

Platform detail 透過 Platform API 讀取 live Slurm state，並把 node 關聯回 Server ID。

## Lifecycle

- **Sync** refresh observed Platform membership／state。
- **Uninstall** 從原始 target 移除 swallow-deployed runtime，並保留 Platform
  record 供診斷。若仍有 workflow 佔著 Platform 的 Server（停在等待處理的失敗部署，或仍在進行的部署），
  對話框會列出它並請你確認取消；uninstall 會在它結束後自動開始。
- **Delete** 只移除 swallow record／projection，不等於 host-side uninstall。
- Bulk action 仍受每個 Platform 的 lifecycle gate 限制。

每次 deployment／uninstall 都會建立 Workflow。失敗時請檢視 Task，在支援的最小
層級 retry，不要直接重建 Platform。

精確 payload 與 lifecycle state 請見 active
[Platforms contract](../../../api-server/docs/development/api-contracts/api-server/platforms.md)
與
[Kubernetes explorer contract](../../../api-server/docs/development/api-contracts/api-server/platforms-kubernetes.md)。
