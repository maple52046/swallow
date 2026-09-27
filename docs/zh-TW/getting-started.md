# 快速入門

[English](../en/getting-started.md) · [文件首頁](README.md)

本教學會啟動 development topology、登入、建立 Site，並說明接上真實 integration
的下一步。這是 evaluation 流程，不是 production installation。

## 1. 啟動環境

需求：

- Docker Engine 與 Compose plugin。
- 目前使用者可以存取 Docker daemon。
- 足以執行 MongoDB、Temporal、API、worker、Ansible executor、Dashboard 與
  Prometheus 的本機資源。

```bash
git clone git@github.com:maple52046/swallow.git
cd swallow/deploy/dev
docker compose up -d
docker compose ps
```

等待 services 變成 healthy。Log、設定覆寫與 troubleshooting 請見
[development environment reference](../../deploy/dev/README.zh-TW.md)。

## 2. 登入

開啟 <http://localhost:5173>，使用 `admin` / `admin`。這個帳號和 development
Compose 內的 secrets 都只適用本機開發，不得在共享或正式環境沿用。

Overview 一開始是空的，因為 swallow 不會自行捏造 infrastructure。

## 3. 建立 Site

在 **Infrastructure → Sites** 建立例如 `lab` 的 Site。Site 是 integration、
Server、Platform、automation policy 與 monitoring 的 scope。

建置 CLI 後也可完成相同操作：

```bash
cd ../../cli
go build -o bin/swallow ./cmd/swallow
printf '%s\n' 'admin' | bin/swallow --endpoint http://127.0.0.1:30051 \
  login -u admin --password-stdin
bin/swallow sites create --file - <<'JSON'
{"name":"lab","description":"Local evaluation site"}
JSON
```

## 4. 註冊外部系統

實用的 Site 通常需要：

- 一個 `provisioner` / `maas` Integration，提供 machine inventory 與 OS lifecycle。
- 一個 `metrics` / `prometheus` Integration，提供 metrics 與 Alertmanager。
- Site automation settings，包含 SSH policy、known hosts、playbook mappings
  與 write-only credential。

請使用 **Infrastructure → Integrations** 與 Site automation controls，或依照
[初始設定指南](guides/initial-setup.md)。Credential 無法讀回；
`hasCredential` 只表示是否已儲存。

## 5. Reconcile 與檢視

MAAS Integration 成功後，下一輪 reconciliation 會建立或更新 Server projection。
Operator 不會直接建立 Server。

- 開啟 **Servers** 檢視 inventory 與 staleness。
- 使用 **Provisioning** 檢視 image、驗證 deployability 並建立 deployment template。
- 使用 **Workflows** 觀察 durable work。
- 註冊 Prometheus／Alertmanager 後使用 **Monitoring**。

## 6. 選擇下一份指南

- [Server 與 infrastructure](guides/servers-and-infrastructure.md)
- [OS provisioning](guides/os-provisioning.md)
- [Platform](guides/platforms.md)
- [Managed Software](guides/managed-software.md)
- [CLI reference](reference/cli.md)
- [API integration](reference/api-integration.md)
