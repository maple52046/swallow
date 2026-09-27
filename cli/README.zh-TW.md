# swallow CLI

[English](README.md) · [完整 usage manual](docs/usage.zh-TW.md) ·
[專案文件](../docs/zh-TW/README.md)

`swallow` 是 swallow 系統的 operator CLI。它與 `api-server` 是不同 Go module，
消費 published HTTP API，並涵蓋下列 operator surface；Managed Software 目前
使用 Dashboard 或 HTTP API。

## Build 與驗證

目前尚無 stable packaged release。

```bash
go build -o bin/swallow ./cmd/swallow
gofmt -l .
go vet ./...
go test ./...
```

## 設定與登入

```bash
printf '%s\n' "$PASSWORD" |
  bin/swallow --endpoint https://swallow.example \
  login -u admin --password-stdin

bin/swallow auth me
```

Profile 預設位於 user configuration directory；`$SWALLOW_CONFIG` 或 `--config`
可選擇其他 path。Resolution order 是 profile、`SWALLOW_*` environment variable、
global flag。

## Output 與 body

- `table` 是 human-readable default。
- `-o json`、`-o yaml` 為 script 保留完整 response。
- `servers watch` 將 Server-Sent Events 輸出為 JSON lines。
- Structured mutation 以 `-f/--file` 接受 JSON、YAML，或 `-` stdin。

Request file 刻意直接跟隨 API contract，不維護 copied CLI struct。

## Command groups

- `auth`、`login`、`logout` — session。
- `overview` — Site-scoped operational summary。
- `sites`、`integrations` — infrastructure identity 與 Site automation。
- `servers` — inventory、stream、detail、protection、provider action。
- `provisioning` — image、verification、template、tag、deploy/release/recovery。
- `infrastructure` — Zone／Pool。
- `platforms` — Kubernetes／Slurm deployment、lifecycle、settings、live view。
- `workflows` — durable Workflow observation／control。
- `monitoring` — alert 與 fixed Server metric。
- `discovery` — 使用 machine auth 的 Prometheus HTTP service discovery。

執行 `swallow <group> --help`，或閱讀
[usage manual](docs/usage.zh-TW.md) 查詢每個 verb、flag、exit code 與 recipe。

## Compatibility 與安全

CLI 只使用 canonical Active route，不暴露 deprecated `/operations`、
`/clusters` 或 legacy single-Server provisioning alias。Token 不會寫入 log，
profile 權限只有 owner 可讀。`--insecure` 只適合受控 lab。

修改 component 前請讀 [AGENTS.md](AGENTS.md)、
[architecture specification](docs/development/architecture-spec.md) 與
[coding style](docs/development/coding-style.md)。
