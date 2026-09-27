# CLI reference

[English](../../../docs/en/reference/cli.md) · [文件首頁](../README.md)

`swallow` binary 是 operator CLI。它與 `swallow-api` 是不同 Go module，
只透過 published HTTP API 溝通。

## 從 source 安裝

目前沒有穩定 packaged release。請從 repository 建置 active-development binary：

```bash
cd cli
go build -o bin/swallow ./cmd/swallow
bin/swallow --help
```

## 設定與 authentication

```bash
printf '%s\n' "$PASSWORD" |
  swallow --endpoint https://swallow.example \
  login -u admin --password-stdin
swallow auth me
```

Profile 會以 owner-only permission 儲存在 user config directory。Environment
variable 覆寫 profile，flag 再覆寫 environment variable。使用 `logout` 清除 stored
access token。

## Output 與 request body

- `table` 是 interactive default。
- `-o json` 與 `-o yaml` 是 lossless scripting format。
- `servers watch` 將 SSE frame 輸出成 JSON lines。
- Structured mutation 的 `--file` 接受 JSON、YAML，或以 `-` 從 stdin 讀取。

Script 應明確指定 `--site-id`。Configured global Site 適合互動操作，但可能讓
automation scope 不清楚。

## Command groups

`auth`、`login`、`logout`、`overview`、`sites`、`integrations`、
`servers`、`provisioning`、`infrastructure`、`platforms`、`workflows`、
`monitoring`、`discovery` 是 CLI 已實作的 operator surface。Managed Software
目前使用 Dashboard 或 HTTP API。Deprecated aliases 與 Planned endpoint 刻意不提供。

Flags、examples、exit codes 與 recipes 請讀完整
[CLI usage manual](../../../cli/docs/usage.zh-TW.md)。
