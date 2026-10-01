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

`/clusters` 與 `/operations` 下的 route 是 compatibility redirect；canonical
navigation 使用 Platforms 與 Workflows。

## Site scope

判讀 inventory 或 monitoring 前先選擇 Site。Site scope 保存在 URL，可直接分享。
尚未選擇 Site，或 Site 沒有 integration 時，empty state 是正常結果。

## 判讀 status

- Server 沒有單一 combined status；provisioning、membership、health 互相獨立。
- Unknown 保持 unknown，不會被 UI 呈現為 failure。
- 信任 cached value 前檢查 integration sync error 與 observation timestamp。
- Destructive action 送出前會顯示 state 與 lock gate。

## Session 與權限

Dashboard 從 login endpoint 取得 opaque bearer token，再透過 `/auth/me` 讀取 caller。
它不解析 JWT claims。Session 過期或 user 已刪除時會回到 login。Role-gated control
只是 presentation guidance；authorization 仍以 API 為準。

## Error 與診斷

Action 失敗時會顯示 API error message 與 request ID。要將 UI failure 與 API log
對照時請複製 request ID。Long-running work 應開啟產生的 Workflow，不要停在原 dialog 等待。

完整 route 請見 [dashboard component README](../../../dashboard/README.zh-TW.md)。
