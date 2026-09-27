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

## Kubernetes

選擇 supported topology，指派 control-plane 與 worker role。Standalone deployment
仍是一個 Kubernetes Platform。Provider／image combination 支援時，deployment
也可要求 ephemeral OS target。

佈署完成後，Platform detail 會提供 live Kubernetes explorer：

- Namespace 與 application。
- Pod、log 與 related resource。
- YAML apply。
- Node cordon state。

這些 view 會 live query Kubernetes API，不會在 swallow 建立 in-cluster state
的 durable copy。

## Slurm

Slurm deployment 會依 topology 指派 controller、compute、login、state-server
與 workload storage responsibility。送出前先設定並檢查 Site 的 Slurm deployment
requirement。Resource minimum 是 policy，不是建議；target set 不符合時 API 會拒絕。

Platform detail 透過 Platform API 讀取 live Slurm state，並把 node 關聯回 Server ID。

## Lifecycle

- **Sync** refresh observed Platform membership／state。
- **Uninstall** 從原始 target 移除 swallow-deployed runtime，並保留 Platform
  record 供診斷。
- **Delete** 只移除 swallow record／projection，不等於 host-side uninstall。
- Bulk action 仍受每個 Platform 的 lifecycle gate 限制。

每次 deployment／uninstall 都會建立 Workflow。失敗時請檢視 Task，在支援的最小
層級 retry，不要直接重建 Platform。

精確 payload 與 lifecycle state 請見 active
[Platforms contract](../../../api-server/docs/development/api-contracts/api-server/platforms.md)
與
[Kubernetes explorer contract](../../../api-server/docs/development/api-contracts/api-server/platforms-kubernetes.md)。
