# swallow CLI 使用手冊

[English](usage.md) · [CLI README](../README.zh-TW.md)

本手冊提供 `swallow` operator CLI（`cli` component）的完整使用方式，適合需要
互動 command，或需要穩定 JSON／YAML output 進行 scripting 的 operator 與 external
developer。

CLI 是 `api-server` contract 的 conformist HTTP consumer。它涵蓋目前已實作的
operator surface，並刻意不提供 Managed Software、deprecated aliases
（`/operations`、`/clusters`、single-Server `deploy`／`release`）與 Planned
endpoints。Managed Software 請使用 Dashboard 或 HTTP API。本手冊如有語意不清之處，
以
[`../../api-server/docs/development/api-contracts/api-server/`](../../api-server/docs/development/api-contracts/api-server)
下的 provider contract 為準。

## 目錄

- [安裝與 build](#安裝與-build)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Authentication](#authentication)
- [Global flags](#global-flags)
- [Output formats](#output-formats)
- [Request body（`--file`）](#request-body--file)
- [Exit codes 與 errors](#exit-codes-與-errors)
- [Command reference](#command-reference)
- [Recipes](#recipes)

## 安裝與 build

```bash
cd cli
go build -o bin/swallow ./cmd/swallow
# 可選：安裝到 PATH
sudo install -m 0755 bin/swallow /usr/local/bin/swallow
```

`swallow` 是 operator client；backend service binary 是 `swallow-api`。兩者由
不同 component 建置，是不同程式。

## Quick start

```bash
# 1. 登入，將 endpoint 與 token 寫入 profile。
swallow --endpoint https://swallow.example login -u admin --password-stdin < password.txt

# 2. 確認 session。
swallow auth me

# 3. 讀取 operational overview。
swallow overview

# 4. 以 JSON 列出 Servers，供 script 使用。
swallow -o json servers list --page-size 100
```

## Configuration

CLI 從三層解析 connection profile，由低到高依序為；較高層只覆寫有提供的 field：

1. **Profile file**（YAML）。
2. **Environment variables**（`SWALLOW_*`）。
3. **Command-line flags**。

### Profile file

Default path 是 `$SWALLOW_CONFIG`，或
`<user-config-dir>/swallow/config.yaml`（Linux 通常是
`~/.config/swallow/config.yaml`）。每次 invocation 可用 `--config <path>`
指定其他 path。

```yaml
# ~/.config/swallow/config.yaml
endpoint: https://swallow.example
token: <access-token>          # 由 `swallow login` 寫入
site: site-abc123              # optional default Site scope
machineToken: <machine-token>  # optional，供 discovery endpoint 使用
insecureSkipTls: false         # 只適用有 self-signed certificate 的 lab
```

Profile 含有 credential，因此以 owner-only permission（`0600`）寫入。`login`
與 `logout` 只更新 `token` field，保留其他 fields。

### Environment variables

| Variable | 覆寫 field |
| --- | --- |
| `SWALLOW_CONFIG` | profile file path |
| `SWALLOW_ENDPOINT` | `endpoint` |
| `SWALLOW_TOKEN` | `token` |
| `SWALLOW_SITE` | `site` |
| `SWALLOW_MACHINE_TOKEN` | `machineToken` |
| `SWALLOW_INSECURE` | `insecureSkipTls`（`1`／`true` 啟用） |

## Authentication

```bash
# 互動登入。建議使用 --password-stdin，避免 secret 留在 shell history。
swallow --endpoint https://swallow.example login -u admin --password-stdin < password.txt
# 也可直接傳入，但較不安全：
swallow login -u admin -p 'REDACTED'

swallow auth me      # 顯示 caller identity 與 role
swallow logout       # 只清除 local token；JWT 是 stateless，不呼叫 server
```

- 多數 endpoints 需要 **admin** token。
- Discovery endpoints 使用 **machine authentication**：優先使用
  `--machine-token`（或 profile 的 `machineToken`），未設定時才 fallback 到
  session token。

## Global flags

下列 persistent flags 適用於所有 commands：

| Flag | Default | 用途 |
| --- | --- | --- |
| `--config <path>` | 見上節 | profile file path |
| `--endpoint <url>` | profile | api-server base URL，例如 `https://swallow.example` |
| `--token <jwt>` | profile | 此次 invocation 使用的 access token |
| `--site <id>` | profile | 接受 `siteId` command 的 default Site scope |
| `--machine-token <tok>` | profile | discovery machine bearer token |
| `-o, --output <fmt>` | `table` | `table`、`json` 或 `yaml` |
| `--insecure` | `false` | skip TLS certificate verification，只適用 lab |
| `--request-timeout <dur>` | `60s` | per-request timeout；`0` 關閉 client deadline |

接受 Site scope 的 command 都有自己的 `--site-id` flag；省略時會 fallback 到
global `--site`（或 profile 的 `site`）。

## Output formats

- `table`（default）— list 顯示 column table（nested fields 會展平成
  `provisioning.state` 等 dotted columns）；single object 顯示 key/value table；
  paginated response 顯示 `total` summary。過寬 payload 只保留最實用的 columns。
- `json` — indented、lossless；適合 script 與 `jq`。
- `yaml` — lossless；適合人工編輯。

`swallow servers watch` 是 continuous stream，因此無論 `--output` 設定為何，
都會每行輸出一個 JSON object。

## Request body（`--file`）

Read commands 以 flags 接收 path 與 query parameter。Create、update 與 action
commands 若需要 structured payload，會接受 `-f/--file <path>`；`<path>` 可以是
JSON／YAML 文件，或以 `-` 從 stdin 讀取。如此 body 可跟隨 API contract，不必固定
在一組 flags 上。

```bash
# 由 YAML file 讀取：
swallow sites create -f site.yaml
# 由 stdin 讀取：
echo '{"name":"lab","description":"Lab site"}' | swallow sites create -f -
```

許多常見情境也提供 convenience flags，例如
`swallow provisioning release --server s1 --erase`；請選擇較清楚的寫法。

下方範例引用的 body files 應與你的變更一起管理；每個 file 的 field shape
以該 endpoint contract 為準。

## Exit codes 與 errors

- Success exit code 是 `0`；任何 error 都是 `1`。
- Result 寫入 **stdout**，error 寫入 **stderr**，兩者可分開 redirect。
- Server error 由 shared error envelope 輸出，例如：
  `Error: conflict (HTTP 409, request req-9): Unlock the Server before deployment.`
  `requestId` 可與 server log 關聯。

## Command reference

執行 `swallow <group> --help` 與 `swallow <group> <cmd> --help` 取得
authoritative flag list。以下 examples 使用 placeholder IDs。

### auth／session

```bash
swallow login -u admin --password-stdin < password.txt
swallow auth me
swallow logout
```

### overview

```bash
swallow overview                 # swallow-wide
swallow overview --site-id site1 # 限定一個 Site
```

### sites

```bash
swallow sites list
swallow sites get site1
swallow sites create -f site.yaml           # body: { name, description }
swallow sites update site1 -f patch.yaml
swallow sites delete site1

# Site Ansible automation
swallow sites automation get site1
swallow sites automation set site1 -f automation.yaml
swallow sites automation credential set site1 -f credential.yaml
```

`automation.yaml`（見 [site-automation.md](../../api-server/docs/development/api-contracts/api-server/site-automation.md)）：

```yaml
enabled: true
sshUser: ubuntu
sshPort: 22
knownHosts: "host ssh-ed25519 AAAA..."
playbookMappings:
  install-exporters: install-exporters
```

`credential.yaml`：

```yaml
sshPrivateKey: |
  -----BEGIN OPENSSH PRIVATE KEY-----
  ...
becomePassword: REDACTED
```

### integrations

```bash
swallow integrations list --kind provisioner
swallow integrations get int1
swallow integrations create -f integration.yaml
swallow integrations update int1 -f patch.yaml
swallow integrations credential set int1 -f credential.yaml
swallow integrations delete int1
```

`integration.yaml`（kind 可為 `provisioner` 或 `metrics`；`platform` 會被拒絕）：

```yaml
siteId: site1
kind: provisioner
providerKind: maas
name: lab-maas
endpoint: https://maas.lab:5240/MAAS
settings: {}
credential:
  apiKey: "REDACTED-MAAS-API-KEY"
```

### servers

```bash
# List filters 與 pagination
swallow servers list --site-id site1 --provisioning-state deployed --page-size 100
swallow servers list --keyword gpu-42 --include-absent

swallow servers get srv1
swallow servers watch --site-id site1      # SSE；JSON lines；Ctrl-C 結束

swallow servers refresh srv1
swallow servers delete srv1
swallow servers provisioner-detail srv1
swallow servers events srv1 --limit 50
swallow servers power-state srv1
swallow servers provisioning-tasks srv1

# Provider-backed lifecycle actions（無 body）：
swallow servers power-on srv1
swallow servers power-off srv1
swallow servers commission srv1
swallow servers test srv1
swallow servers abort srv1
swallow servers override-failed-testing srv1
swallow servers lock srv1
swallow servers unlock srv1
swallow servers mark-broken srv1
swallow servers mark-fixed srv1
swallow servers rescue-mode srv1
swallow servers exit-rescue-mode srv1

# Network configuration
swallow servers network get srv1
swallow servers network add-link srv1 <interfaceId> -f link.yaml
swallow servers network update-link srv1 <interfaceId> <linkId> -f link.yaml
swallow servers network delete-link srv1 <interfaceId> <linkId>

# Placement（Zone／Pool）— flags 或 --file
swallow servers placement srv1 --zone zone1 --pool pool1
swallow servers placement srv1 --clear-zone --clear-pool
swallow servers placement srv1 -f placement.yaml   # { zoneId, poolId }
```

`link.yaml`：
`{ mode: static, subnetId: "11", ipAddress: "192.0.2.20", defaultGateway: true }`
（`mode` 可為 `dhcp | static | link_only`；只有 `static` 需要 `ipAddress`）。

### provisioning

```bash
# OS images
swallow provisioning images list --integration int1
swallow provisioning images upload --integration int1 --name "Ubuntu ROCm" \
  --architecture amd64 --content ./ubuntu-rocm.tgz [--title "..."] [--filetype tgz]
swallow provisioning images delete --integration int1 --image custom/ubuntu-rocm --architecture amd64
swallow provisioning images overlay set --integration int1 --image ubuntu/jammy --architecture amd64 -f overlay.yaml
swallow provisioning images overlay clear --integration int1 --image ubuntu/jammy --architecture amd64

# Deployment templates
swallow provisioning templates list --site-id site1
swallow provisioning templates get tmpl1
swallow provisioning templates create -f template.yaml
swallow provisioning templates update tmpl1 -f patch.yaml
swallow provisioning templates delete tmpl1
swallow provisioning templates user-data set tmpl1 --from ./cloud-init.yaml   # raw file -> { userData }
swallow provisioning templates user-data set tmpl1 -f userdata.json           # JSON body { userData }
swallow provisioning templates user-data clear tmpl1

# Server tags
swallow provisioning tags list --site-id site1
swallow provisioning tags edit --server srv1 --server srv2 --add rack-a --remove decommission
swallow provisioning tags edit -f tags.yaml

# Durable operations（canonical endpoints）
swallow provisioning preflight --server srv1 --server srv2
swallow provisioning deploy -f deploy.yaml
swallow provisioning release --server srv1 --erase --secure-erase --comment "retire"
swallow provisioning recover --server srv1 --unbind-static-ips
swallow provisioning verify-image -f verify.yaml

# Network inspection 與 tasks
swallow provisioning networks inspect --server srv1
swallow provisioning tasks get task1
swallow provisioning tasks retry task1
```

`overlay.yaml`：
`{ name: "Golden Ubuntu", osSystem: "Ubuntu LTS", release: "22.04", tags: ["gpu","ml"] }`

`deploy.yaml`（見 [provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md)）：

```yaml
serverIds: [srv1, srv2]
templateId: tmpl1        # 或省略並提供 settings.imageId
settings:
  imageId: ubuntu/noble
  deployTarget: disk     # disk | ram
userData:
  mode: inherit          # inherit | replace | omit
network:
  mode: automatic        # automatic | static
```

`verify.yaml`：

```yaml
integrationId: int1
imageId: custom/rocky-10.2
architecture: amd64
deployTarget: ram
serverId: srv1
keepServer: false
```

### infrastructure（Zones 與 Pools）

```bash
swallow infrastructure zones list --site-id site1
swallow infrastructure zones get zone1
swallow infrastructure zones create -f zone.yaml      # { siteId, name, description }
swallow infrastructure zones update zone1 -f patch.yaml
swallow infrastructure zones delete zone1

# pools 使用相同 verbs：
swallow infrastructure pools list --site-id site1
swallow infrastructure pools create -f pool.yaml
```

### platforms

```bash
swallow platforms list --site-id site1
swallow platforms get plat1
swallow platforms deploy -f platform-k8s.yaml
swallow platforms update plat1 -f patch.yaml          # name, gpuStackOwner, exporterOwner
swallow platforms delete plat1
swallow platforms uninstall plat1                     # 只移除 software
swallow platforms uninstall plat1 -f uninstall.yaml   # { releaseServers, releaseOptions }
swallow platforms sync plat1
swallow platforms sync-all
swallow platforms slurm plat1                         # live Slurm cluster state
swallow platforms slurm-requirements get
swallow platforms slurm-requirements set -f slurm-req.yaml
```

`platform-k8s.yaml`（見 [platforms.md](../../api-server/docs/development/api-contracts/api-server/platforms.md)）：

```yaml
siteId: site1
name: lab-k0s
type: kubernetes
gpuStackOwner: provisioning
roleAssignments:
  - { serverId: srva, role: control-plane, runWorkloads: true }
  - { serverId: srvb, role: worker }
```

`slurm-req.yaml`：
`{ minimumResources: { cpuCores: 4, memoryMiB: 24576, storageGB: 80 } }`
（傳送 `{ minimumResources: null }` 可停用）。

#### Kubernetes cluster explorer

```bash
swallow platforms kubernetes summary plat1
swallow platforms kubernetes nodes list plat1
swallow platforms kubernetes nodes cordon plat1 node-1
swallow platforms kubernetes nodes uncordon plat1 node-1

swallow platforms kubernetes namespaces list plat1
swallow platforms kubernetes namespaces create plat1 --name web
swallow platforms kubernetes namespaces delete plat1 web

swallow platforms kubernetes applications list plat1 --namespace web
swallow platforms kubernetes applications get plat1 web Deployment nginx
swallow platforms kubernetes applications scale plat1 web Deployment nginx --replicas 5
swallow platforms kubernetes applications restart plat1 web Deployment nginx
swallow platforms kubernetes applications delete plat1 web Deployment nginx

swallow platforms kubernetes pods list plat1 --namespace web
swallow platforms kubernetes pods logs plat1 web nginx-6d8-abcde --container nginx --tail 200
swallow platforms kubernetes pods delete plat1 web nginx-6d8-abcde

swallow platforms kubernetes services plat1 --namespace web
swallow platforms kubernetes ingresses plat1
swallow platforms kubernetes configmaps plat1
swallow platforms kubernetes secrets plat1        # 只回傳 key names，不回傳 values
swallow platforms kubernetes pvcs plat1

swallow platforms kubernetes apply plat1 -f manifest.yaml
swallow platforms kubernetes apply plat1 -f manifest.yaml --dry-run
```

### workflows

```bash
swallow workflows list --active --status running --page-size 50
swallow workflows get wf1
swallow workflows create -f workflow.yaml
swallow workflows cancel wf1
swallow workflows rerun wf1
swallow workflows timeline wf1

swallow workflows task retry wf1 task1
swallow workflows task logs wf1 task1      # text/plain，verbatim 輸出
swallow workflows task stderr wf1 task1    # text/plain
swallow workflows task events wf1 task1
swallow workflows task artifacts wf1 task1
```

`workflow.yaml`：
`{ kind: custom, intent: "verify SSH", targetServerIds: [srv1], playbookName: diagnostic-ping, extraVars: {} }`

### monitoring

```bash
swallow monitoring alerts list --severity critical --state firing
swallow monitoring alerts acknowledge <fingerprint> -f ack.yaml --site-id site1
swallow monitoring metrics get --server srv1 --server srv2 --metric cpuUsagePercent
swallow monitoring metrics names
```

`ack.yaml`：`{ matchers: [...], duration: "2h", comment: "ack by ops" }`

### discovery（machine auth）

```bash
# 有設定時使用 --machine-token，否則使用 session token。
swallow discovery prometheus --port 9100
swallow discovery prometheus --port 5000 --tag amd-gpu --site-id site1
swallow --machine-token "$TOKEN" discovery prometheus --provisioning-state all
```

## Recipes

```bash
# 用 jq 篩出 Server IDs
swallow -o json servers list --provisioning-state failed \
  | jq -r '.items[].id'

# 監看 fleet，將每個 change compact 輸出
swallow servers watch --site-id site1 | jq -c '{type, id}'

# 動態建立 deploy body
jq -n '{serverIds:["srv1"],settings:{imageId:"ubuntu/noble",deployTarget:"disk"}}' \
  | swallow provisioning deploy -f -
```

Server 來自 provisioner reconciliation，因此不存在 `servers create`。Script
建議使用 `-o json`、檢查 process exit code、保留 error request ID、明確傳入
`--site-id`，並依
[provider-owned contract](../../api-server/docs/development/api-contracts/api-server/outline.md)
建立 structured body fields。
