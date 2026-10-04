# Server 與 infrastructure

[English](../../../docs/en/guides/servers-and-infrastructure.md) · [文件首頁](../README.md)

Server 是 provisioner machine 的 reconciled projection。Operator 可以檢視、組織、
保護與操作 Server，但不會建立 Server record。

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
視覺提示。將滑鼠停在 state 上，可查看 provider failure reason 等細節。

Release 接受後可留在 list：provider 執行期間該列會變成 **Releasing**，Server
回到 pool 後再變成 **Ready**。只有 provider 工作時，row action 會顯示
**Monitor server**。Swallow deployment 正在執行或需要檢查時會提供 **View
workflow**；deployment 執行期間，row action 會顯示 **Monitor workflow**。

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

選擇 **Take action → Hardware checks → Inspect hardware**，要求 provisioner
重新盤點 Server 硬體；MAAS 將此動作稱為 *Commission*。執行期間 Deployment
state 是 **Inspecting**。Server detail Summary card 的 **Inspection** 欄位仍會
顯示 provider 上次的結果標籤，例如 *Passed*。

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
swallow provisioning tags edit --server server1 --add amd-gpu
swallow infrastructure zones list --site-id site1
```

精確 flags 請執行 `swallow servers --help`、`swallow provisioning tags --help`
與 `swallow infrastructure --help`。
