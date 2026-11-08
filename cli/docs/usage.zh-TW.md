# swallow CLI 使用手冊

[English](usage.md) · [CLI README](../README.zh-TW.md)

本手冊提供 `swallow` CLI（`cli` component）的完整使用方式，適合需要
互動 command，或需要穩定 JSON／YAML output 進行 scripting 的 user 與 external
developer。

CLI 是 `api-server` contract 的 conformist HTTP consumer，涵蓋下列 surface。Managed Software
（含 registry credential 與 Docker explorer）、Server Default User、provisioner HTTP proxy 與 Subnet DNS
目前還沒有 command，請使用 Dashboard 或 HTTP API。Planned endpoint 刻意不提供。本手冊如有語意不清之處，
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

`swallow` 是 client；backend service binary 是 `swallow-api`。兩者由
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

### plans

破壞性變更的 Change Plan。破壞性變更是一份 plan，核准後由該資源的 controller 重新計算並以 Workflow 執行。
寫入變更的指令加上 `--approve` 時，plan 隨寫入一併核准，就像在 dashboard 確認對話框一樣；沒有加時，plan 會在這裡
等 admin 整份核准。見
[change-plans.md](../../api-server/docs/development/api-contracts/api-server/change-plans.md)。

```bash
swallow plans list [--resource-kind platform] [--resource-id <id>] [--state pending_approval,approved]
swallow plans get <planId>        # 動作、預期影響、阻擋事項、狀態，以及為什麼在等
swallow plans approve <planId>
swallow plans reject <planId>
swallow plans policy                               # 加 --approve 送出的變更是否立即開始
swallow plans policy set --approve-on-submit=false # 安全開關：所有 plan 都等人核准
swallow plans policy set --approve-on-submit=true  # 回到預設
```

寫入之後，指令會在 stderr 說明 plan 接下來怎麼走：已核准並執行，或在等待，附上原因（`submitted without
approval`、`the Change Plan policy requires a separate approval`，或阻擋事項）與 `plans approve` 指令。有阻擋事項的
plan 無法核准；plan 預設在計算後 24 小時到期。plan 在執行前 spec 又改變時，會變成 `superseded`，需要新的 plan；
觀察到的狀態改變了已核准 plan 要做的事時，會有一份新的 plan 等人核准。政策只影響設定之後送出的變更，已經提出的 plan
維持原本的狀態。

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
  deploy-kubernetes: deploy-kubernetes
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

`integration.yaml`（kind 可為 `provisioner`、`metrics` 或 `repository`；`platform` 會被拒絕）：

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

`repository` Integration 是 Site 的 Local Repository，Site 打開它之後部署的所有 artifact 都從它取得；
一個 Site 至多一個。`endpoint` 是 swallow 呼叫 provider API 的位址，`settings.contentOrigin`（必填）是
Site 的 Server 讀取內容的位址，credential 是 provider 的 admin 密碼：

```yaml
siteId: site1
kind: repository
providerKind: pulp
name: local-repository
endpoint: http://pulp-web:8080
settings:
  contentOrigin: http://10.170.168.20:8080
credential: "REDACTED-PULP-ADMIN-PASSWORD"
```

CLI 目前還沒有 Local Repository 本身的指令（內容、同步狀態、Site 的來源，以及是否用它部署）；請使用
Dashboard 或 HTTP API。

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
  --address qemu+ssh://maas@hv-01.example/system --power-id simple-pig   # libvirt VM
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
swallow servers placement srv1 --clear-zone --clear-pool   # 回到 provisioner 預設的 zone 與 pool
swallow servers placement srv1 -f placement.yaml   # { zoneId, poolId }

# Boot Media：Server 優先從所屬 provisioner 的 Boot ISO 開機。方式依 Power Configuration 而定：
# redfish（由 BMC 掛載）或 libvirt（由 hypervisor 放到 VM 的 CD-ROM）
swallow servers boot-media get srv1 [--live]       # --live 會同時讀取 BMC 或 hypervisor（需數秒）
swallow servers boot-media enable srv1 --iso iso1  # 執行 preflight；最多等待 8 分鐘
swallow servers boot-media enable srv1 --iso iso2  # 已啟用的 Server：切換 Boot ISO
swallow servers boot-media disable srv1            # 保留已選的 Boot ISO
swallow servers redfish-probe srv1                 # 重新探測方式（BMC Redfish 或 libvirt hypervisor）

# 虛擬機（libvirt）：hypervisor 是已 deploy 的 swallow Server，以 Deployment Key 登入
swallow servers virtual-machines list hv1 [--account ubuntu]   # domain、狀態、MAC、已對應的 Server
swallow servers virtual-machines enroll hv1 lab-vm-1 lab-vm-2 --integration int1 \
  [--boot-iso iso1] [--account ubuntu] [--power-off-running]   # 202 { workflowId }
swallow servers vms enroll hv1 -f vm-enrollment.yaml           # { integrationId, hypervisorServerId, domains, ... }
```

`servers virtual-machines enroll` 以 libvirt domain 名稱指定虛擬機，不需要輸入 MAC：
swallow 會從 hypervisor 讀取。它啟動一個 `enroll-virtual-machines` Workflow，每個
domain 一個 Task 並行執行：domain 必須是關機狀態（或以 `--power-off-running` 讓
swallow 關機）、把 provisioner 的 SSH key 授權到 hypervisor 帳號、`--boot-iso` 會把
Boot ISO 放到 domain 的 CD-ROM 並設為第一開機裝置，最後以 `virsh` power driver 註冊
且**不執行 commissioning**。之後由自動硬體檢查把每台新 Server 帶到 `ready`。以
`swallow workflows get <workflowId>` 追蹤；等待處理的 Task 會說明要修正什麼，修正後以
`swallow workflows task retry <workflowId> <taskId>` 繼續。

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
swallow provisioning images metadata set --integration int1 --image ubuntu/jammy --architecture amd64 -f metadata.yaml
swallow provisioning images metadata clear --integration int1 --image ubuntu/jammy --architecture amd64

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
# （BIOS 以 iPXE 到 ipxe.cfg、UEFI 以 GRUB 到 grub/grub.cfg-<mac>，預設 port 5248）的 ISO；建置只需數秒。
# 原地 rebuild 目前只能在 Dashboard／API 進行（CLI 尚無對應指令）。
swallow provisioning boot-isos list --site-id site1            # 也會說明此安裝能否建置
swallow provisioning boot-isos create --integration int1 --name south-rack --rack 10.0.0.2
swallow provisioning boot-isos get iso1                        # 含產生的 iPXE script
swallow provisioning boot-isos delete iso1                     # 仍有 Server 的 Boot Media 使用時會被拒絕

# Server tags
swallow provisioning tags list --site-id site1
swallow provisioning tags edit --server srv1 --server srv2 --add rack-a --remove decommission
# 編輯寫入 Server 的 spec，provisioner 隨後跟上。`servers get srv1` 會顯示 conditions：MAAS 套上 tag 後
# 為 realized=true。在 MAAS UI 改動的 tag 會在下一次定期 resync（預設 10 分鐘）改回來。
swallow provisioning tags edit -f tags.yaml

# Durable operations（canonical endpoints）
swallow provisioning preflight --server srv1 --server srv2
swallow provisioning deploy -f deploy.yaml
swallow provisioning release --server srv1 --erase --secure-erase --comment "retire"
swallow provisioning recover --server srv1 --unbind-static-ips
swallow provisioning release --server srv1 --cancel-active-workflows   # 先取消佔著 srv1 的 Workflow
swallow provisioning verify-image -f verify.yaml

# Network inspection 與 tasks
swallow provisioning networks inspect --server srv1
swallow provisioning tasks get task1
swallow provisioning tasks retry task1
```

被 Platform claim 的 Server（其 `platformClaim`）不能直接 release，已 deployed 的也不能
recover，加上 `--cancel-active-workflows` 也一樣；`swallow servers delete` 也無法刪除它。CLI 會
指出所屬的 Platform 與下一步：透過 `swallow platforms delete <platformId> --release-servers` 來
release 該 Platform 的 Server。uninstall 成功或刪除 Platform 紀錄
（`swallow platforms delete <platformId> --record-only`）之後，才能刪除該 Server。

`metadata.yaml`：
`{ name: "Golden Ubuntu", osSystem: "Ubuntu LTS", release: "22.04", tags: ["gpu","ml"], defaultUser: "ubuntu" }`

`metadata set` 會取代整份 metadata，因此要保留的值都必須一併帶上。`defaultUser` 是 automation
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
  mode: automatic        # automatic | dhcp | static
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
swallow platforms create -f platform-k8s.yaml --approve   # 立即開始安裝
swallow platforms create -f platform-k8s.yaml        # 安裝會等待：swallow plans approve <planId>
swallow platforms set-spec plat1 -f spec-change.yaml  # { generation, spec }；加入成員會直接收斂
swallow platforms update plat1 -f patch.yaml          # name, gpuStackOwner, monitoringLinks
swallow platforms delete plat1 --approve              # 立即 uninstall（只移除 software）
swallow platforms delete plat1 --approve --release-servers --erase   # uninstall 時一併 release 成員
swallow platforms delete plat1 --cancel-active-workflows   # 一併取消佔著 Server 的失敗或進行中部署
swallow platforms delete plat1 --record-only          # 只刪紀錄，主機保持原樣
swallow platforms sync plat1
swallow platforms sync-all
swallow platforms slurm plat1                         # live Slurm cluster state
swallow platforms slurm-requirements get
swallow platforms slurm-requirements set -f slurm-req.yaml
swallow platforms gpu-operator releases                       # Swallow 提供的 AMD GPU Operator 版本
swallow platforms gpu-operator set plat1 -f gpu-operator.yaml --approve  # 安裝或調整 operator
swallow platforms gpu-operator uninstall plat1 --approve                 # 移除 operator，平台照常運作
swallow platforms hardware-counters enable plat1      # 做得到的每台 compute node 由 Omnistat 收集 VALUBusy
swallow platforms hardware-counters disable plat1     # 每台 compute node 改跑不含 counters 的 Omnistat
```

平台以 spec 描述：成員與它們的角色、該類型的設定，以及成員如何取得作業系統。建立平台與破壞性變更（調整或移除
GPU Operator、刪除平台）會回傳 Change Plan。加上 `--approve` 時 plan 隨寫入核准、變更立即開始，除非 Change Plan
政策要求另外核准（`swallow plans policy`）；沒有加時，要等 `swallow plans approve` 核准才會動手。加入成員、開關 Slurm
平台的 hardware counters 則直接收斂。

`platforms delete` 會 uninstall 任何已開始部署的平台，包括仍在部署中（加上 `--cancel-active-workflows`）或部署失敗的；
只有安裝從未開始的平台會直接刪除。對 uninstall 還在等待的平台再執行一次時，flag 相同就回傳同一份 Change Plan
（加 `--approve` 即核准），flag 不同則提出新的。plan 被拒絕或過期、或 uninstall 失敗之後再執行一次，會重新提出
uninstall；uninstall 執行中則什麼都不變。`platform-k8s.yaml`（見 [platforms.md](../../api-server/docs/development/api-contracts/api-server/platforms.md)）：

```yaml
siteId: site1
name: lab-k0s
type: kubernetes
spec:
  members:
    - { serverId: srva, roles: [control-plane, workloads] }
    - { serverId: srvb, roles: [worker] }
  settings:
    k0sVersion: v1.36.3+k0s.2
```

`spec-change.yaml` 為它加入一個 worker；`generation` 用 `swallow platforms get` 顯示的值：

```yaml
generation: 1
spec:
  members:
    - { serverId: srva, roles: [control-plane, workloads] }
    - { serverId: srvb, roles: [worker] }
    - { serverId: srvc, roles: [worker] }
  settings:
    k0sVersion: v1.36.3+k0s.2
```

要在建立平台時一併安裝 AMD GPU Operator，就在 settings 加上 `gpuOperator`，`gpuStackOwner` 會自動推導，可以不寫。
下面的 `platform-k8s-gpu.yaml` 是一次實機測試用的檔案：三台 Server 在開機網卡上以 Automatic 定址重新安裝 OS
（若 subnet 的 DHCP 不是由 provisioner 提供，改寫 `mode: dhcp` 即由站點的 DHCP server 指派位址），三台都是
control plane 也跑 workload，operator 以 Device Plugin 模式納管 Node Feature Discovery 判定為 AMD GPU 節點的所有節點。`apiVipPrefix`
可以不寫，會採用 control plane 子網的 prefix（這裡是 `/23`），部署也只接受這個值。

```yaml
siteId: site1
name: gpu-run3
type: kubernetes
spec:
  members:
    - { serverId: srva, roles: [control-plane, workloads] }
    - { serverId: srvb, roles: [control-plane, workloads] }
    - { serverId: srvc, roles: [control-plane, workloads] }
  settings:
    k0sVersion: v1.36.3+k0s.2
    podCidr: 172.21.0.0/20
    serviceCidr: 172.21.22.0/24
    apiVip: 10.140.168.13
    apiVipPrefix: 23
    gpuOperator:
      enabled: true
      allocationMode: device-plugin   # 或 dra（Kubernetes 1.34 以上）
  machinePreparation:
    mode: provision_os
    settings:
      imageId: ubuntu-24.04-rocm-10.0.0-gfx942-rdc-clock-index-fix-amdgpu31.50-maasga146-20261015
      deployTarget: disk
    network:
      mode: automatic
      assignments:   # 每台 Server 的開機網卡與子網，可從 `swallow servers get` 取得
        - { serverId: srva, interfaceId: "131", subnetId: "1" }
        - { serverId: srvb, interfaceId: "132", subnetId: "1" }
        - { serverId: srvc, interfaceId: "133", subnetId: "1" }
```

除非 settings 寫了 `monitoringDashboards: false`，Kubernetes 平台也會在叢集內有自己的唯讀 Grafana，以每個 workload
節點的 node port `30300` 開放；它經 swallow 讀 Site 的 Prometheus，只顯示這個平台的 Server，平台會有 Monitoring Link
`Platform Grafana`。`gpuOperator.metricsExporter: true` 時，operator 的 Device Metrics Exporter 在每個 GPU 節點的 host
network 上以 port `5001` 執行，由 Site 的 Prometheus 收集，Grafana 也會加上它的 GPU dashboards。

接著追蹤部署、切換 operator、移除 operator，最後 release Server：

```bash
swallow platforms gpu-operator releases
swallow platforms create -f platform-k8s-gpu.yaml --approve   # 輸出平台與已核准的 plan
swallow platforms get <platformId>                            # status.realizingWorkflowId 是建置的 Workflow
swallow workflows get <workflowId>
swallow platforms gpu-operator set <platformId> -f gpu-operator.yaml --approve   # 調整立即開始
swallow platforms gpu-operator uninstall <platformId> --approve   # 平台仍繼續運作
swallow platforms delete <platformId> --release-servers --approve # 直接 release Server，不先移除平台軟體
```

每個加 `--approve` 的寫入都立即執行變更；uninstall 成功後刪除紀錄。安全開關打開時（`swallow plans policy set
--approve-on-submit=false`），它們都會改為等待，由 `swallow plans approve <planId>` 開始。

`gpu-operator.yaml` 用來調整已部署平台的 operator；沒寫的欄位採用契約的預設值（預設版本、`single` 資源命名、
Node Labeller 開啟、Device Metrics Exporter 關閉）。檔案裡不列節點：operator 會納管 Node Feature Discovery
判定為 AMD GPU 節點的所有節點。仍有 Pod 使用 AMD GPU 時，切換分配方式會被拒絕。之後用 `swallow workflows get <workflowId>` 追蹤，或用 `swallow platforms get` 看 `gpuOperator.state`：

```yaml
enabled: true
allocationMode: dra
draDriverVersion: v1.0.1
```

`slurm-req.yaml`：
`{ minimumResources: { cpuCores: 4, memoryMiB: 24576, storageGB: 80 } }`
（傳送 `{ minimumResources: null }` 可停用）。

Slurm 部署會自動把 compute node 的 AMD GPU 設成 GRES（以 `--gres=gpu:N` 申請）。GRES type 預設是 GPU
架構（例 `gfx942`），可用 `slurm.gpuGresType` 指定名稱。每張 GPU 都必須通過 HIP 配置記憶體的 probe（節點上
直接跑一次、透過 Slurm 再跑一次），否則節點會被 drain、部署失敗。driver module option 會依每台節點的 GPU
topology 推導（例如在 ROCm thunk 會破壞 heap 的情況下加上 `pcie_p2p=0`）；要指定或覆寫時加上
`gpuDriver.moduleOptions`。加上 `containerRuntime` 可在每台 compute 與 login node 安裝 Docker CE 或
Podman，供容器化 workload 使用。除非 settings 寫 `monitoringDashboards: false`，primary controller（第一個
controller）會裝 Platform 自己的唯讀 Grafana：它經 swallow 讀取 Site 的 Prometheus，只顯示這個 platform 的 Server，
platform 也會多一個 Monitoring Link `Platform Grafana`。若要一併安裝 Omnistat，加上 `omnistat`：每台 compute node
裝 exporter，由 Site 的 Prometheus 收集，Grafana 加上 Omnistat 的 dashboards。`installMethod: container`（預設）讓
Grafana 以 container 跑在 primary controller 的 runtime 上（即上面的 `containerRuntime`，否則為 Docker CE）；
`native` 則改由 Grafana 的 apt／yum repository 安裝。`hardwareCounters: true` 另外在做得到的每台 compute node
收集 GPU hardware counter `VALUBusy`（省略時不收集）；做不到的節點在 `omnistat.hardwareCounters.nodes` 回報原因，其他
metrics 照常。已部署的平台以 `swallow platforms hardware-counters enable <platformId>` 與 `disable` 開關它們：
`configure-omnistat` workflow 在每台 compute node 重新設定 Omnistat，平台維持 active，每台 exporter 重啟但不中斷
執行中的工作。以 `swallow workflows get <workflowId>`（平台的 `status.realizingWorkflowId`）追蹤，再以
`swallow platforms get` 讀 `omnistat.hardwareCounters` 的結果。以下 `platform-slurm-omnistat.yaml` 是實跑的檔案：

```yaml
siteId: site1
name: omnistat-slurm-run1
type: slurm
spec:
  members:
    - { serverId: srva, roles: [controller] }
    - { serverId: srvb, roles: [compute] }
    - { serverId: srvc, roles: [compute] }
  settings:
    containerRuntime:
      kind: podman            # 或 docker-ce；省略即不裝 runtime
    omnistat:
      enabled: true
      installMethod: container   # 或 native
      hardwareCounters: true     # 做得到的 compute node 收集 VALUBusy；省略即 false
    gpuDriver:                   # 選用；topology 規則已會在需要時套用 pcie_p2p=0
      moduleOptions: {}
    slurm:
      clusterName: omnistat-run1
      gpuGresType: mi308x_vf     # 選用；省略即用 GPU 架構（gfx942）
  machinePreparation:
    mode: provision_os
    settings:
      imageId: ubuntu-24.04-rocm-10.0.0-gfx942-rdc-amdgpu31.50-slurm-26.05.4-maasga146-20261021
      deployTarget: disk     # or ram
    network:
      mode: automatic
      defaultGateway: false
      assignments: []
```

```bash
swallow platforms create -f platform-slurm-omnistat.yaml --approve
swallow workflows get <workflowId>    # ensure-os、configure-slurm、configure-podman、configure-grafana、configure-omnistat
swallow platforms get <platformId>    # containerRuntime、omnistat.state 與 .hardwareCounters，以及 monitoringLinks 中的 Platform Grafana
```

若不用 Omnistat（兩者不可同時開啟），`deviceMetricsExporter` 會在每台 GPU compute node 安裝 AMD 的 Device
Metrics Exporter，Grafana 與 `installMethod` 與 Omnistat 相同。作業仍可共用節點，每個作業的
GPU 會帶上 job id 標籤。安裝成功後，`deviceMetricsExporter.gpuIdentity` 說明 exporter 是否分得出各張 GPU：
`collapsed`（例如 MI308X virtual function）表示 metrics 照常收集，但至少有一台節點的逐作業 GPU 歸屬是錯的。
`slurm.ociContainers: true` 設定 native OCI container（`srun --container=<bundle>`，bundle 必須在每台節點的相同路徑；
與安裝容器引擎的 `containerRuntime` 互相獨立；部署只提供這些能力，作業怎麼跑由叢集的使用者決定），
`slurm.pmix: true` 安裝 OpenMPI，供 `srun --mpi=pmix_v5` 使用：

```yaml
spec:
  members:
    - { serverId: srva, roles: [controller, login] }
    - { serverId: srvb, roles: [compute] }
  settings:
    deviceMetricsExporter:
      enabled: true
      installMethod: native      # 或 container
    slurm:
      clusterName: dme-run1
      ociContainers: true
      pmix: true
```

```bash
swallow workflows get <workflowId>    # ...、configure-grafana、configure-device-metrics-exporter
swallow platforms get <platformId>    # deviceMetricsExporter.state 與 .gpuIdentity、ociContainers、pmix
```

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
swallow monitoring alerts acknowledge <fingerprint> --site-id site1
# 不帶 -f 時 silence 比對該告警的全部 labels；ack.yaml 可縮小範圍，例如 { matchers: { alertname: GPUHot, server_id: srv1 } }。
swallow monitoring alerts acknowledge <fingerprint> -f ack.yaml --site-id site1
swallow monitoring metrics get --server srv1 --server srv2 --metric cpuUsagePercent
swallow monitoring metrics names

# 用量歷史：scope 擇一（--server、--pool、--zone 或 --platform）。
swallow monitoring metrics range --server srv1 --window 6h
swallow monitoring metrics range --pool pool1 --metric cpuUsagePercent --metric networkReceiveBytesPerSecond --window 24h
swallow monitoring metrics range --platform plat1 --mode perServer --metric memoryUsedPercent
swallow monitoring metrics range --zone zone1 --start 2026-10-10T00:00:00Z --end 2026-10-11T00:00:00Z
```

`ack.yaml`：`{ matchers: [...], duration: "2h", comment: "ack by ops" }`

`range` 預設（`aggregate`）對整個 scope 每個 metric 回傳一條 series；`--mode perServer`
則每台 Server 一條（最多 50 台）。Step 由 server 決定（每條 series 約 180 點）；沒有資料的
metric 不會出現 series。

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
