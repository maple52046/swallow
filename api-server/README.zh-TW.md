# swallow api-server

[English](README.md) · [專案文件](../docs/zh-TW/README.md)

`api-server` 是 swallow 擁有 API 的 backend component，建置 `swallow-api`
binary，並擁有 HTTP contracts、intent、policy、stable identity mapping、
reconciliation、durable Workflow activities 與 embedded Ansible content。

## Runtime processes

同一個 binary 提供不同 process：

| Command | 職責 |
| --- | --- |
| `swallow-api api` | HTTP API、authentication、query、reconciliation 與 Workflow start/control |
| `swallow-api worker` | Temporal Workflow／activity worker |
| `swallow-api ansible-executor` | 執行 idempotent Ansible attempt 並發布 task event |
| `swallow-api migrate` | Service start 前的 explicit schema/data migration |

Temporal 是唯一 durable orchestration engine。完整 topology 還需要 Temporal
Server/PostgreSQL 與 MongoDB；不存在 in-process dispatcher fallback。

## External systems 與 ownership

Integration 依 Site 在 runtime 註冊：

| Kind | 目前 provider | 用途 |
| --- | --- | --- |
| `provisioner` | Ubuntu MAAS | Machine inventory、power、OS image、deploy/release、grouping、tag |
| `metrics` | Prometheus/Alertmanager/Grafana | Fixed metric query、alert/silence、link |
| Platform runtime | Kubernetes、Slurm | Self-deployed Platform 的 live membership 與 runtime management |

swallow 儲存 intent 與 identity relationship，不把 hardware fact、live Platform
state、metric 或 alert 複製成競爭的 source of truth。請見
[ownership decision](../docs/decisions/001-system-ownership-boundaries.md)。

## Build 與驗證

`go.mod` 宣告 Go 1.25。

```bash
go build -o bin/swallow-api ./cmd/swallow-api
gofmt -l .
go vet ./...
go test ./...
```

完整 development topology 請使用 [deploy/dev](../deploy/dev/README.zh-TW.md)，
不要只啟動 API。

## 使用明確 dependencies 執行

```bash
export SWALLOW_API_MONGO_URI=mongodb://localhost:27017
export SWALLOW_API_JWT_SECRET='replace-me'
export SWALLOW_API_CREDENTIAL_KEY="$(openssl rand -base64 32)"
export SWALLOW_API_TEMPORAL_ADDRESS=localhost:7233

bin/swallow-api migrate
bin/swallow-api api
```

在不同 process 啟動：

```bash
bin/swallow-api worker
bin/swallow-api ansible-executor
```

Command 需要 automation manifest、playbook directory、Ansible runner environment、
MongoDB 與 Temporal topology。Compose stack 會提供這些細節。

## Configuration

Precedence 由高到低：

1. `SWALLOW_API_*` environment variable。
2. Command flag。
3. `--config` YAML file。
4. Default。

完整 field reference 請見 annotated [config example](docs/config-example.yaml)。
重要群組包括：

- API address、MongoDB、JWT、bootstrap admin、credential encryption 與 machine auth。
- Reconcile 與 attached-inventory interval。
- Temporal address、namespace、task queue、start polling 與 parallelism。
- Ansible runner command、manifest/playbook directory、runtime/artifact storage、
  retention 與 upload limit。

`api.credentialKey` 必填，且必須 decode 成 32 bytes。它會加密 write-only
integration／automation credential，必須與 MongoDB 一起備份；更換後既有
ciphertext 將無法讀取。

`admin` / `admin`、`changeme-in-production` 等 development default
不可用於本機以外環境。

## API contracts

Provider-owned [contract outline](docs/development/api-contracts/api-server/outline.md)
列出 Active、Deprecated、Planned area。只有 Active contract 可直接實作。

Shared behavior：

- Base path `/api/v1`。
- Opaque bearer token 與 role-based authorization。
- 包含 request ID 的統一 JSON error envelope。
- ISO 8601 UTC timestamp。
- 大型 collection 的一致 pagination。

`dashboard` 與 `cli` 是 conformist consumer，不得從 private code 推測 behavior。

## Reconciliation 與 execution

- Provisioner reconciliation 建立／更新 Server projection。
- Inventory sweep refresh expensive attached-hardware observation。
- Platform sync 讀取 runtime-owned membership。
- Temporal Workflow 執行 versioned Job／Task。
- Ansible execution 只允許 [automation/manifest.json](automation/manifest.json)
  內的 playbook。

Expired execution 可能變成 `indeterminate`；因為 external side effect 無法證明，
所以不會自動 retry。

## Development architecture

改動 code 前請讀 [AGENTS.md](AGENTS.md)、component
[architecture specification](docs/development/architecture-spec.md) 與
[coding style](docs/development/coding-style.md)。Domain/application package
必須保持獨立於 Fiber、MongoDB、Temporal、Ansible 與 provider SDK implementation。
