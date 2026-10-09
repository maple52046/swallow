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
# 1. 登入，將 endpoint 與 session 寫入 profile。
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
token: <access-token>          # 由 `swallow login` 寫入，會自動換發
tokenExpiresAt: 2026-10-02T03:15:00Z
refreshToken: <refresh-token>  # 用來換發 access token；每次換發都會更新
# apiKey: swk_...              # 取代 session：`swallow login --api-key-stdin`
site: site-abc123              # optional default Site scope
machineToken: <machine-token>  # optional，供 discovery endpoint 使用
insecureSkipTls: false         # 只適用有 self-signed certificate 的 lab
```

Profile 含有 credential，因此以 owner-only permission（`0600`）寫入。`login`、
token 換發與 `logout` 只更新 credential 相關 field，保留其他 fields。Profile 只會保存
session 或 API key 其中一種：存入一種時會移除另一種。

### Environment variables

| Variable | 覆寫 field |
| --- | --- |
| `SWALLOW_CONFIG` | profile file path |
| `SWALLOW_ENDPOINT` | `endpoint` |
| `SWALLOW_TOKEN` | `token`（此次 invocation 使用的 access token，不會換發） |
| `SWALLOW_API_KEY` | `apiKey`（優先於已保存的 session） |
| `SWALLOW_SITE` | `site` |
| `SWALLOW_MACHINE_TOKEN` | `machineToken` |
| `SWALLOW_INSECURE` | `insecureSkipTls`（`1`／`true` 啟用） |

## Authentication

CLI 使用以下兩種 credential 之一。

**Session（帳號密碼）。** `login` 會保存短效的 access token 與 refresh token。CLI 會自動換發
access token（到期前，以及收到 `401` 後換發一次），並把新的 token 存回 profile。Session 在 7 天未使用、
登入 30 天後或 `logout` 時結束（server 預設值），之後重新執行 `login`。

```bash
# 建議使用 --password-stdin，避免 secret 留在 shell history。
swallow --endpoint https://swallow.example login -u admin --password-stdin < password.txt
# 也可直接傳入，但較不安全：
swallow login -u admin -p 'REDACTED'
```

**API key（script 與 CI）。** 以密碼登入後建立 API key，之後不需要密碼即可使用。API key 以建立者的身分
運作，在到期或被刪除前都有效。

```bash
swallow api-keys create --name ci --expires-in 90d --secret-out ./ci.key
swallow login --api-key-stdin < ./ci.key          # 存入 profile
SWALLOW_API_KEY="$(cat ./ci.key)" swallow servers list   # 或每次執行時傳入
```

```bash
swallow auth me      # 顯示 identity、role 與 authMethod（session 或 api_key）
swallow logout       # 在 server 結束 session，並移除已保存的 credential
```

- 同時有 API key 與 session 時使用 API key；明確指定 `--token` 或 `--api-key` 時以該次指定為準。
- API key 不能建立其他 API key，需以密碼登入才能建立。
- 多數 endpoints 需要 **admin** credential。
- Discovery endpoints 使用 **machine authentication**：優先使用
  `--machine-token`（或 profile 的 `machineToken`），未設定時才 fallback 到
  session token。

## Global flags

下列 persistent flags 適用於所有 commands：

| Flag | Default | 用途 |
| --- | --- | --- |
| `--config <path>` | 見上節 | profile file path |
| `--endpoint <url>` | profile | api-server base URL，例如 `https://swallow.example` |
| `--token <jwt>` | profile | 此次 invocation 使用的 access token（不會換發） |
| `--api-key <swk_…>` | profile | 此次 invocation 使用的 API key（建議改用 `SWALLOW_API_KEY`） |
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
swallow login --api-key-stdin < ./ci.key
swallow auth me
swallow logout
```

### api-keys

你自己的 API key，供非互動使用。見
[api-keys.md](../../api-server/docs/development/api-contracts/api-server/api-keys.md)。

```bash
swallow api-keys list                                   # 名稱、前綴、建立、到期、最後使用時間
swallow api-keys create --name ci [--expires-in 90d] [--secret-out ./ci.key]
swallow api-keys delete <keyId>                         # alias：revoke
```

`create` 只會回傳一次 secret。指定 `--secret-out` 時會寫入權限 0600 的新檔案（不會覆寫既有檔案）；
否則直接印出。`--expires-in` 接受天數（`90d`）或 duration（`12h`）；省略則永不到期。建立 API key
需要以密碼登入的 session。

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

`sshUser` 為選填：automation 會先以每台 Server 所佈署 OS Image 的 default user 登入，
只有 image 沒有 default user 時才退回 `sshUser`。

`credential.yaml` 只有選填的 `becomePassword`，request 會取代整份 Site credential（`{}`
代表清除）。Automation 一律使用安裝層級的 Deployment Key 登入；帶 `sshPrivateKey` 的 body 會以
`validation_error` 拒絕。要改用另一把 key，請以
`swallow ssh-keys deployment replace --private-key-file <path>` 替換 Deployment Key。
Deployment Key 存在後，`sites automation get` 的 `credentialSource` 欄位會顯示 `deploymentKey`。

```yaml
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

# 保留 OS 主機的 Server Enrollment（會印出 provisioner 的 API key）
swallow integrations enroll-bundle int1
swallow integrations enroll-bundle int1 --swallow-url https://swallow.example.com
```

`enroll-bundle` 會回傳一行要在主機上執行的 `command`。它會從 `--swallow-url` 的位址
（預設為已設定的 endpoint）下載 enrollment script 與 swallow CLI，因此主機必須能以該
位址連到 swallow。

provisioner 的 `settings.autoInspect: "false"` 會關閉新納管 Server 的自動硬體檢視；
未設定代表開啟。

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
swallow servers inspect srv1              # inspect-hardware Workflow，或 retry 等待處理的那一個（MAAS：commission）
swallow servers test srv1
swallow servers abort srv1
swallow servers override-failed-testing srv1
swallow servers lock srv1
swallow servers unlock srv1
swallow servers mark-broken srv1
swallow servers mark-fixed srv1
swallow servers rescue-mode srv1
swallow servers exit-rescue-mode srv1

# Power Configuration：provisioner 的 power driver 與參數，寫入時直接寫回 provisioner。
# 密碼只寫不讀；未帶密碼 flag 時，driver 不變就保留原密碼。
swallow servers power-configuration get srv1
swallow servers power-configuration set srv1 --driver virsh \
  --address qemu+ssh://maas@tainan-ci.lab/system --power-id simple-pig   # libvirt VM
printf '%s' "$BMC_PASSWORD" | swallow servers power-configuration set srv2 \
  --driver ipmi --address 10.0.0.5 --username maas --password-stdin      # BMC
swallow servers power-configuration set srv2 --driver ipmi --address 10.0.0.5 --clear-password
swallow servers power-configuration set srv1 -f power.yaml               # { driver, address, powerId, username, password }

# Network configuration
swallow servers network get srv1
swallow servers network add-link srv1 <interfaceId> -f link.yaml
swallow servers network update-link srv1 <interfaceId> <linkId> -f link.yaml
swallow servers network delete-link srv1 <interfaceId> <linkId>

# Placement（Zone／Pool）— flags 或 --file
swallow servers placement srv1 --zone zone1 --pool pool1
swallow servers placement srv1 --clear-zone --clear-pool
swallow servers placement srv1 -f placement.yaml   # { zoneId, poolId }

# Boot Media：BMC 掛載 Server 所屬 provisioner 的 Boot ISO，並優先從它開機
swallow servers boot-media get srv1 [--live]       # --live 會同時讀取 BMC（需數秒）
swallow servers boot-media enable srv1 --iso iso1  # 在 BMC 上執行 preflight；最多等待 8 分鐘
swallow servers boot-media enable srv1 --iso iso2  # 已啟用的 Server：切換 Boot ISO
swallow servers boot-media disable srv1            # 保留已選的 Boot ISO
swallow servers redfish-probe srv1
```

`link.yaml`：
`{ mode: static, subnetId: "11", ipAddress: "192.0.2.20", defaultGateway: true }`
（`mode` 可為 `dhcp | static | link_only`；只有 `static` 需要 `ipAddress`）。

`servers enroll` 要以 root **在要納管的主機上**執行，不會呼叫 swallow API：它在主機
保留 OS 的情況下把主機註冊到 provisioner，下一次同步後 Server 會以 `deployed` 出現。
通常由 `integrations enroll-bundle` 或 Dashboard **Add servers** 給的 enrollment 指令
下載 CLI 並代為執行。`--endpoint` 與 `--token` 是 provisioner 的，不是 swallow 的。

```bash
sudo swallow servers enroll --provisioner=maas \
  --endpoint http://10.0.0.5:5240/MAAS --token '<MAAS API key>' [--hostname db-03]
# 以 SWALLOW_ENROLL_TOKEN 傳入可避免 key 留在 shell history
```

MAAS 會從 endpoint 下載 `maas-run-scripts`，執行 `register-machine` 與
`report-results`，最後刪除下載的檔案。主機需要 `python3`。

### provisioning

```bash
# OS images
swallow provisioning images list --integration int1
swallow provisioning images upload --integration int1 --name "Ubuntu ROCm" \
  --architecture amd64 --content ./ubuntu-rocm.tgz [--title "..."] [--filetype tgz] [--default-user cloud-user]
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

# Boot ISO：從站點網路的 DHCP 取得位址後，chain 到 provisioner 的 MAAS rack
# （http://<rack>:<port 或 5248>/ipxe.cfg）的 iPXE ISO；建置只需數秒
swallow provisioning boot-isos list --site-id site1            # 也會說明此安裝能否建置
swallow provisioning boot-isos create --integration int1 --name tainan-rack --rack 10.0.0.2
swallow provisioning boot-isos get iso1                        # 含產生的 iPXE script
swallow provisioning boot-isos delete iso1                     # 仍有 Server 的 Boot Media 使用時會被拒絕

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
`{ name: "Golden Ubuntu", osSystem: "Ubuntu LTS", release: "22.04", tags: ["gpu","ml"], defaultUser: "ubuntu" }`

`overlay set` 會取代整份 overlay，因此要保留的值都必須一併帶上。`defaultUser` 是 automation
在以此 image 佈署的 Server 上使用的登入帳號（`images list` 會顯示實際生效的 `defaultUser`，
包含 swallow 為 synced Ubuntu、CentOS、RHEL image 內建的預設值）。

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

### ssh-keys

Deployment Key（系統持有；swallow 以它登入 Server）以及你自己的 Access Keys（只存公鑰）。
swallow 會把每一把 key 註冊到支援 SSH key 的 provisioner（MAAS），之後佈署的 Server 就會授權它。
見 [ssh-keys.md](../../api-server/docs/development/api-contracts/api-server/ssh-keys.md)。

```bash
swallow ssh-keys list                                   # Deployment Key + 你的 Access Keys
swallow ssh-keys get <keyId>                            # 單一 key 及其 provisioner sync 狀態
swallow ssh-keys import --name laptop --public-key-file ~/.ssh/id_ed25519.pub
swallow ssh-keys generate --name jumpbox --private-key-out ~/.ssh/id_ed25519_jumpbox
swallow ssh-keys delete <keyId>
swallow ssh-keys sync                                   # 要求立即同步到 provisioner

swallow ssh-keys deployment show
swallow ssh-keys deployment regenerate
swallow ssh-keys deployment replace --private-key-file ./deploy_key [--name ops-deploy]
```

`generate` 只會回傳一次私鑰。指定 `--private-key-out` 時會寫入一個權限為 0600 的新檔案（絕不覆寫既有
檔案）；未指定則直接印出私鑰。重新產生或替換 Deployment Key 不會改變先前已佈署的 Server：它們只授權
舊的 key。

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

Server 來自 provisioner reconciliation，因此不存在 `servers create`；
`servers enroll` 則是把主機放進 provisioner 的 inventory。Script
建議使用 `-o json`、檢查 process exit code、保留 error request ID、明確傳入
`--site-id`，並依
[provider-owned contract](../../api-server/docs/development/api-contracts/api-server/outline.md)
建立 structured body fields。
