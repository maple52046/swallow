# CLI reference

[English](../../../docs/en/reference/cli.md) · [文件首頁](../README.md)

`swallow` binary 是 operator CLI。它與 `swallow-api` 是不同 Go module，
只透過 published HTTP API 溝通。

## 安裝

Production installation 會把 CLI 安裝到 `/usr/local/bin/swallow`。其他 Linux amd64
主機可下載 release asset `swallow-linux-amd64`（列在 release `SHA256SUMS`），或執行
`ghcr.io/<owner>/swallow` 的 `cli-<version>` image：

```bash
sudo install -m 0755 swallow-linux-amd64 /usr/local/bin/swallow
swallow --help
```

也可從 repository 建置 active-development binary：

```bash
cd cli
go build -o bin/swallow ./cmd/swallow
bin/swallow --help
```

## 設定與 authentication

```bash
printf '%s\n' "$PASSWORD" |
  swallow --endpoint http://swallow.example \
  login -u admin --password-stdin
swallow auth me
```

Profile 會以 owner-only permission 儲存在 user config directory。Environment
variable 覆寫 profile，flag 再覆寫 environment variable。以密碼登入會保留一個 session，CLI 會自動
換發；script 與 CI 可建立 API key（`swallow api-keys create`），以 `login --api-key-stdin` 存入或透過
`SWALLOW_API_KEY` 傳入。使用 `logout` 結束 session 並移除已保存的 credential。建立、保存、到期與撤銷
方式請見 [API key](../guides/api-keys.md)。

## Output 與 request body

- `table` 是 interactive default。
- `-o json` 與 `-o yaml` 是 lossless scripting format。
- `servers watch` 將 SSE frame 輸出成 JSON lines。
- Structured mutation 的 `--file` 接受 JSON、YAML，或以 `-` 從 stdin 讀取。

Script 應明確指定 `--site-id`。Configured global Site 適合互動操作，但可能讓
automation scope 不清楚。

## Command groups

以下索引只列出可用 verb，不重複 flags 或 payload examples。請執行
`swallow <group> --help` 查看已安裝 binary 的精確 surface，並從各項連結進入完整
usage manual。

- [`login`、`logout`、`auth me`](../../../cli/docs/usage.zh-TW.md#authsession)：
  建立、檢查、自動換發與結束 password Session，或保存 API key profile。
- [`api-keys`](../../../cli/docs/usage.zh-TW.md#api-keys)：以 `list`、`create` 與
  `delete`（alias `revoke`）管理 non-interactive credential。
- [`overview`](../../../cli/docs/usage.zh-TW.md#overview)：讀取 swallow-wide 或
  Site-scoped operator aggregate。
- [`sites`](../../../cli/docs/usage.zh-TW.md#sites)：以 `list`、`get`、`create`、
  `update`、`delete` 管理 Site；`automation get|set` 與
  `automation credential set` 管理 Site automation。
- [`integrations`](../../../cli/docs/usage.zh-TW.md#integrations)：提供 `list`、
  `get`、`create`、`update`、`delete` 與 `credential set`；
  `enroll-bundle` 產生 existing-OS Server enrollment command。
- [`servers`](../../../cli/docs/usage.zh-TW.md#servers)：提供 `list`、`get`、
  `watch`、`refresh`、`delete`、provider detail、event、power state 與
  provisioning task；lifecycle verbs 涵蓋 power、inspection、test、abort、lock、
  provider recovery state 與 rescue mode。Nested commands 管理
  `power-configuration`、`network`、`placement`、`boot-media`、Redfish probe、
  libvirt `virtual-machines`（`vms`）與 host-side `enroll`。
- [`provisioning`](../../../cli/docs/usage.zh-TW.md#provisioning)：管理 `images`
  與其 metadata、`templates` 與 user data、`boot-isos`、`tags`；執行
  `preflight`、`deploy`、`release`、`recover`、`verify-image`、network
  inspection，以及 task read／retry。
- [`ssh-keys`](../../../cli/docs/usage.zh-TW.md#ssh-keys)：以 `list`、`get`、
  `import`、`generate`、`delete`、`sync` 管理 Access Key，並 show、regenerate
  或 replace installation Deployment Key。
- [`infrastructure`](../../../cli/docs/usage.zh-TW.md#infrastructurezones-與-pools)：
  `zones` 與 `pools` 都提供 `list`、`get`、`create`、`update`、`delete`。
- [`platforms`](../../../cli/docs/usage.zh-TW.md#platforms)：提供 `list`、`get`、
  `deploy`、`update`、`delete`、`uninstall`、`sync`、`sync-all`；讀取 live
  Slurm、管理 `slurm-requirements` 與 `gpu-operator`，並以 nested `kubernetes`
  explorer 管理 node、namespace、application、Pod、related resource、log 與 YAML apply。
- [`workflows`](../../../cli/docs/usage.zh-TW.md#workflows)：提供 `list`、`get`、
  `create`、`cancel`、`rerun`、`timeline`；
  `task retry|logs|stderr|events|artifacts` 可診斷與控制 individual Task。
- [`monitoring`](../../../cli/docs/usage.zh-TW.md#monitoring)：列出與 acknowledge
  alert；`metrics get|range|names` 讀取 current value 與 bounded history。
- [`discovery`](../../../cli/docs/usage.zh-TW.md#discoverymachine-auth)：
  `prometheus` 回傳使用 machine authentication 的 service-discovery target。

Managed Software 目前使用 Dashboard 或 HTTP API。Deprecated aliases 與 Planned
endpoint 刻意不提供。Global flags、request file、exit codes 與 recipes 請讀
[完整 CLI usage manual](../../../cli/docs/usage.zh-TW.md)。
