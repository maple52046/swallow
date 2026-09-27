# Server 與 infrastructure

[English](../../../docs/en/guides/servers-and-infrastructure.md) · [文件首頁](../README.md)

Server 是 provisioner machine 的 reconciled projection。Operator 可以檢視、組織、
保護與操作 Server，但不會建立 Server record。

## Inventory 與 detail

Servers list 支援 Site scope、pagination、search、status／capability filter、
saved view、live row update 與 bulk selection。Server detail workspace 將 summary、
activity、monitoring、network、storage 與 PCI observations 分開。

Link 與 automation 一律使用 opaque Server ID。不同 Site 擁有相同 hostname 或
address 是合法狀況。

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
swallow servers get server1
swallow provisioning tags edit --server server1 --add amd-gpu
swallow infrastructure zones list --site-id site1
```

精確 flags 請執行 `swallow servers --help`、`swallow provisioning tags --help`
與 `swallow infrastructure --help`。
