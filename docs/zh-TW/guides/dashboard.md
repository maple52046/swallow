# Dashboard 指南

[English](../../../docs/en/guides/dashboard.md) · [文件首頁](../README.md)

Dashboard 是 swallow active operator workflow 的 browser interface。它只使用
published HTTP API，browser 內只保存 UI preference 與目前 session。

## 導覽

| 區域 | 用途 |
| --- | --- |
| Overview | Site-scoped fleet health、attention items、integration、Platform 與 recent Workflow |
| Servers | Inventory、filter、saved view、bulk action、tag、lock 與 Server detail |
| Provisioning | OS deployment、template、image、upload 與 verification |
| Platforms | Kubernetes／Slurm deployment、lifecycle、settings 與 runtime view |
| Software | Docker CE、Podman、NFS installation state 與 action |
| Workflows | Durable execution、Job、Task、event、log、cancel、rerun 與 retry |
| Monitoring | Alert、silence、fixed metrics、fleet health 與 Grafana link |
| Infrastructure | Site、Integration、Zone、Pool、credential 與 automation settings |
| SSH keys（account menu） | Deployment key、你的 access keys 及其 provisioner sync 狀態，見 [SSH key](ssh-keys.md) |
| API keys（account menu） | 讓 script、CI 與 CLI 不需要密碼即可以你的身分呼叫 Swallow 的 key |

`/clusters` 與 `/operations` 下的 route 是 compatibility redirect；canonical
navigation 使用 Platforms 與 Workflows。

## 開發中的功能

以下三項 Dashboard 功能仍在開發中，Dashboard 的 release build 會隱藏它們；API 與 `swallow`
CLI 不受影響。

- **Monitoring**：沒有 Monitoring 頁面與 Server Monitoring tab。Overview、Server list 與
  Server detail 上的 health 保留位置，但顯示「Not available in this release」。仍可註冊
  metrics Integration。
- **OS image upload**：OS images 沒有 Upload。既有 custom image 的 verification 仍可使用。
- **Deployment Template**：沒有 Templates workspace、Create template，佈署時也不能選擇或保存
  template；佈署使用 custom configuration。

Development build 會顯示這些功能，並提供 **Account menu → Experimental features** 逐項關閉。

## Site scope

判讀 inventory 或 monitoring 前先選擇 Site。Site scope 保存在 URL，可直接分享。
尚未選擇 Site，或 Site 沒有 integration 時，empty state 是正常結果。

## 判讀 status

- Server 沒有單一 combined status；provisioning、membership、health 互相獨立。
- **Servers** 的 **Deployment** 欄位會合併 swallow deployment result 與
  provider-neutral OS provisioning state。**Ready** 是已知、可佈署的 provider
  state，**Unknown** 則不是。Spinner 表示工作正在進行；將滑鼠停在 state 上可查看
  細節。見 [Server 與 infrastructure](servers-and-infrastructure.md#判讀-deployment-欄位)。
- Unknown 保持 unknown，不會被 UI 呈現為 failure。
- 信任 cached value 前檢查 integration sync error 與 observation timestamp。
- Destructive action 送出前會顯示 state 與 lock gate。

## Session 與權限

登入會建立一個 session。Dashboard 只在記憶體保存短效的 access token，並透過網頁無法讀取的 refresh
cookie 自動換發，因此使用中不會被踢回登入頁。每個分頁載入時會延續同一個 session。Session 在登出、
7 天未使用或登入 30 天後結束（server 預設值）；此時 Dashboard 回到登入頁並說明 session 已結束，重新
登入後會回到原本的頁面。

Script 與 CLI 可在 **Account menu → API keys** 建立 API key。Secret 只顯示一次；刪除 key 即撤銷。

Dashboard 透過 `/auth/me` 讀取目前的 caller，不解析 token claims。Role-gated control 只是
presentation guidance；authorization 仍以 API 為準。

## Error 與診斷

Action 失敗時會顯示 API error message 與 request ID。要將 UI failure 與 API log
對照時請複製 request ID。Long-running work 應開啟產生的 Workflow，不要停在原 dialog 等待。

完整 route 請見 [dashboard component README](../../../dashboard/README.zh-TW.md)。
