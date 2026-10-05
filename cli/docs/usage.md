# swallow CLI Usage Manual

This is the usage manual for the `swallow` operator command-line client (the
`cli` component). It is written for operators and external developers who need interactive commands or stable JSON/YAML output for scripts.

The CLI is a conformist HTTP consumer of the `api-server` contract. It covers
its implemented operator surfaces and deliberately omits Managed Software,
deprecated aliases (`/operations`, `/clusters`, single-server
`deploy`/`release`), and Planned endpoints. Use the Dashboard or HTTP API for
Managed Software. When a detail here is ambiguous, the source of truth is the
provider contract under
[`../../api-server/docs/development/api-contracts/api-server/`](../../api-server/docs/development/api-contracts/api-server).

## Contents

- [Install and build](#install-and-build)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Authentication](#authentication)
- [Global flags](#global-flags)
- [Output formats](#output-formats)
- [Request bodies (`--file`)](#request-bodies---file)
- [Exit codes and errors](#exit-codes-and-errors)
- [Command reference](#command-reference)
- [Recipes](#recipes)

## Install and build

```bash
cd cli
go build -o bin/swallow ./cmd/swallow
# optionally put it on PATH
sudo install -m 0755 bin/swallow /usr/local/bin/swallow
```

`swallow` is the operator client; the backend service binary is `swallow-api`.
They are different programs built from different components.

## Quick start

```bash
# 1. Log in (writes the endpoint and a session to the profile).
swallow --endpoint https://swallow.example login -u admin --password-stdin < password.txt

# 2. Confirm the session.
swallow auth me

# 3. Read the operational overview.
swallow overview

# 4. List servers as JSON for scripting.
swallow -o json servers list --page-size 100
```

## Configuration

The CLI resolves its connection profile from three layers, lowest priority
first. A higher layer overrides a lower one for that field only:

1. **Profile file** (YAML).
2. **Environment variables** (`SWALLOW_*`).
3. **Command-line flags**.

### Profile file

Default path: `$SWALLOW_CONFIG`, or `<user-config-dir>/swallow/config.yaml`
(on Linux, `~/.config/swallow/config.yaml`). Override per invocation with
`--config <path>`.

```yaml
# ~/.config/swallow/config.yaml
endpoint: https://swallow.example
token: <access-token>          # written by `swallow login`, renewed automatically
tokenExpiresAt: 2026-10-02T03:15:00Z
refreshToken: <refresh-token>  # renews the access token; rotated on every renewal
# apiKey: swk_...              # instead of a session: `swallow login --api-key-stdin`
site: site-abc123              # optional default Site scope
machineToken: <machine-token>  # optional, for discovery endpoints
insecureSkipTls: false         # lab endpoints with self-signed certs only
```

The file is written with owner-only permissions (`0600`) because it holds
credentials. `login`, token renewal, and `logout` update only the credential
fields; other fields are preserved. A profile holds either a session or an API
key: storing one removes the other.

### Environment variables

| Variable | Overrides |
| --- | --- |
| `SWALLOW_CONFIG` | profile file path |
| `SWALLOW_ENDPOINT` | `endpoint` |
| `SWALLOW_TOKEN` | `token` (an access token for this invocation; it is not renewed) |
| `SWALLOW_API_KEY` | `apiKey` (takes precedence over a stored session) |
| `SWALLOW_SITE` | `site` |
| `SWALLOW_MACHINE_TOKEN` | `machineToken` |
| `SWALLOW_INSECURE` | `insecureSkipTls` (`1`/`true` enables) |

## Authentication

The CLI authenticates with one of two credentials.

**A session (username and password).** `login` stores a short-lived access token
and a refresh token. The CLI renews the access token automatically — shortly
before it expires, and once after a `401` — and saves the renewed tokens to the
profile. The session ends after 7 days without use, 30 days after login, or at
`logout` (server defaults); then run `login` again.

```bash
# Prefer --password-stdin so the secret is not in shell history.
swallow --endpoint https://swallow.example login -u admin --password-stdin < password.txt
# Or pass it directly (less safe):
swallow login -u admin -p 'REDACTED'
```

**An API key (scripts and CI).** Create one while signed in with a password, then
use it without a password. A key acts as the user who created it and works until
it expires or is deleted.

```bash
swallow api-keys create --name ci --expires-in 90d --secret-out ./ci.key
swallow login --api-key-stdin < ./ci.key          # store it in the profile
SWALLOW_API_KEY="$(cat ./ci.key)" swallow servers list   # or pass it per run
```

```bash
swallow auth me      # identity, role, and authMethod (session or api_key)
swallow logout       # end the session on the server and remove stored credentials
```

- With both an API key and a session available, the API key is used. An explicit
  `--token` or `--api-key` flag decides for that invocation.
- An API key cannot create other API keys; sign in with a password for that.
- Most endpoints require an **admin** credential.
- Discovery endpoints use **machine authentication**: they accept the
  `--machine-token` (or `machineToken` profile field) and fall back to the
  session token when no machine token is set.

## Global flags

These persistent flags apply to every command:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--config <path>` | see above | profile file path |
| `--endpoint <url>` | from profile | api-server base URL, e.g. `https://swallow.example` |
| `--token <jwt>` | from profile | access token for this invocation (not renewed) |
| `--api-key <swk_…>` | from profile | API key for this invocation (prefer `SWALLOW_API_KEY`) |
| `--site <id>` | from profile | default Site scope for commands that accept `siteId` |
| `--machine-token <tok>` | from profile | machine bearer token for discovery |
| `-o, --output <fmt>` | `table` | `table`, `json`, or `yaml` |
| `--insecure` | `false` | skip TLS certificate verification (lab only) |
| `--request-timeout <dur>` | `60s` | per-request timeout; `0` disables the client deadline |

Any command that accepts a Site scope has its own `--site-id` flag; when omitted,
it falls back to the global `--site` (or the profile `site`).

## Output formats

- `table` (default) — a column table for lists (nested fields are flattened to
  dotted columns such as `provisioning.state`), a key/value table for a single
  object, and a `total` summary for paginated responses. Wide payloads are capped
  to the most useful columns.
- `json` — indented, lossless; use this for scripting and `jq`.
- `yaml` — lossless; convenient for hand editing.

`swallow servers watch` always emits one JSON object per line regardless of
`--output`, because it is a continuous stream.

## Request bodies (`--file`)

Read commands take path and query parameters as flags. Create, update, and
action commands that carry a structured payload accept `-f/--file <path>`, where
`<path>` is a JSON or YAML document (or `-` to read stdin). This keeps the body
aligned with the API contract instead of a fixed flag set.

```bash
# From a YAML file:
swallow sites create -f site.yaml
# From stdin:
echo '{"name":"lab","description":"Lab site"}' | swallow sites create -f -
```

Many common cases also have convenience flags (for example
`swallow provisioning release --server s1 --erase`); use whichever is clearer.

Example body files referenced below live logically with your change; their field
shapes come from the contract for each endpoint.

## Exit codes and errors

- Exit `0` on success, `1` on any error.
- Results go to **stdout**; errors go to **stderr**, so you can redirect them
  independently.
- Server errors are printed from the shared error envelope, for example:
  `Error: conflict (HTTP 409, request req-9): Unlock the Server before deployment.`
  The `requestId` correlates with the server log.

## Command reference

Run `swallow <group> --help` and `swallow <group> <cmd> --help` for the
authoritative flag list. Examples below use placeholder IDs.

### auth / session

```bash
swallow login -u admin --password-stdin < password.txt
swallow login --api-key-stdin < ./ci.key
swallow auth me
swallow logout
```

### api-keys

Your API keys for non-interactive use. See
[api-keys.md](../../api-server/docs/development/api-contracts/api-server/api-keys.md).

```bash
swallow api-keys list                                   # name, prefix, created, expires, last used
swallow api-keys create --name ci [--expires-in 90d] [--secret-out ./ci.key]
swallow api-keys delete <keyId>                         # alias: revoke
```

`create` returns the secret only once. With `--secret-out` it is written to a new
file with mode 0600 (an existing file is never overwritten); without it the secret is
printed. `--expires-in` accepts days (`90d`) or a duration (`12h`); omit it for a key
that never expires. Creating a key requires a password session.

### overview

```bash
swallow overview                 # swallow-wide
swallow overview --site-id site1 # scoped to one Site
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

`automation.yaml` (see [site-automation.md](../../api-server/docs/development/api-contracts/api-server/site-automation.md)):

```yaml
enabled: true
sshUser: ubuntu
sshPort: 22
knownHosts: "host ssh-ed25519 AAAA..."
playbookMappings:
  install-exporters: install-exporters
```

`sshUser` is optional: automation first logs in as each Server's OS Image default user
and falls back to `sshUser` only when the image has none.

`credential.yaml` holds only the optional `becomePassword` and replaces the Site's whole
credential (`{}` clears it). Automation always logs in with the installation's Deployment
Key; a body with `sshPrivateKey` is refused with `validation_error`. To use another key, replace
the Deployment Key with `swallow ssh-keys deployment replace --private-key-file <path>`. The
`credentialSource` field of
`sites automation get` reports `deploymentKey` once that key exists.

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
```

`integration.yaml` (kind is `provisioner` or `metrics`; `platform` is rejected):

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
# List with filters and pagination
swallow servers list --site-id site1 --provisioning-state deployed --page-size 100
swallow servers list --keyword gpu-42 --include-absent

swallow servers get srv1
swallow servers watch --site-id site1      # SSE; JSON lines; Ctrl-C to stop

swallow servers refresh srv1
swallow servers delete srv1
swallow servers provisioner-detail srv1
swallow servers events srv1 --limit 50
swallow servers power-state srv1
swallow servers provisioning-tasks srv1

# Provider-backed lifecycle actions (no body):
swallow servers power-on srv1
swallow servers power-off srv1
swallow servers inspect srv1              # hardware inspection (MAAS calls it commission)
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

# Placement (Zone/Pool) — flags or --file
swallow servers placement srv1 --zone zone1 --pool pool1
swallow servers placement srv1 --clear-zone --clear-pool
swallow servers placement srv1 -f placement.yaml   # { zoneId, poolId }

# Boot Media: the BMC mounts a Boot ISO of the Server's own provisioner and boots it first
swallow servers boot-media get srv1 [--live]       # --live also reads the BMC (takes seconds)
swallow servers boot-media enable srv1 --iso iso1  # preflight on the BMC; waits up to 8 minutes
swallow servers boot-media enable srv1 --iso iso2  # on an enabled Server: switch Boot ISO
swallow servers boot-media disable srv1            # the chosen Boot ISO is kept
swallow servers redfish-probe srv1
```

`link.yaml`: `{ mode: static, subnetId: "11", ipAddress: "192.0.2.20", defaultGateway: true }`
(`mode` is `dhcp | static | link_only`; `ipAddress` required only for `static`).

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

# Boot ISOs: iPXE ISOs that take a DHCP lease from the site network and chain to a
# provisioner's MAAS rack (http://<rack>:<port or 5248>/ipxe.cfg); built in seconds
swallow provisioning boot-isos list --site-id site1            # also says whether this install can build
swallow provisioning boot-isos create --integration int1 --name tainan-rack --rack 10.0.0.2
swallow provisioning boot-isos get iso1                        # includes the rendered iPXE script
swallow provisioning boot-isos delete iso1                     # refused while a Server's Boot Media uses it

# Server tags
swallow provisioning tags list --site-id site1
swallow provisioning tags edit --server srv1 --server srv2 --add rack-a --remove decommission
swallow provisioning tags edit -f tags.yaml

# Durable operations (canonical endpoints)
swallow provisioning preflight --server srv1 --server srv2
swallow provisioning deploy -f deploy.yaml
swallow provisioning release --server srv1 --erase --secure-erase --comment "retire"
swallow provisioning recover --server srv1 --unbind-static-ips
swallow provisioning verify-image -f verify.yaml

# Network inspection and tasks
swallow provisioning networks inspect --server srv1
swallow provisioning tasks get task1
swallow provisioning tasks retry task1
```

`overlay.yaml`: `{ name: "Golden Ubuntu", osSystem: "Ubuntu LTS", release: "22.04", tags: ["gpu","ml"], defaultUser: "ubuntu" }`

`overlay set` replaces the whole overlay, so include every value you want to keep.
`defaultUser` is the login user automation uses on Servers deployed with the image
(`images list` shows the effective `defaultUser`, including swallow's built-in for
synced Ubuntu, CentOS, and RHEL images).

`deploy.yaml` (see [provisioning.md](../../api-server/docs/development/api-contracts/api-server/provisioning.md)):

```yaml
serverIds: [srv1, srv2]
templateId: tmpl1        # or omit and provide settings.imageId
settings:
  imageId: ubuntu/noble
  deployTarget: disk     # disk | ram
userData:
  mode: inherit          # inherit | replace | omit
network:
  mode: automatic        # automatic | static
```

`verify.yaml`:

```yaml
integrationId: int1
imageId: custom/rocky-10.2
architecture: amd64
deployTarget: ram
serverId: srv1
keepServer: false
```

### ssh-keys

The Deployment Key (system-owned; swallow logs in to Servers with it) and your own
Access Keys (public key only). swallow registers every key in each key-capable
provisioner (MAAS), so Servers deployed afterwards authorize it. See
[ssh-keys.md](../../api-server/docs/development/api-contracts/api-server/ssh-keys.md).

```bash
swallow ssh-keys list                                   # Deployment Key + your Access Keys
swallow ssh-keys get <keyId>                            # one key with its provisioner sync status
swallow ssh-keys import --name laptop --public-key-file ~/.ssh/id_ed25519.pub
swallow ssh-keys generate --name jumpbox --private-key-out ~/.ssh/id_ed25519_jumpbox
swallow ssh-keys delete <keyId>
swallow ssh-keys sync                                   # request an immediate provisioner sync

swallow ssh-keys deployment show
swallow ssh-keys deployment regenerate
swallow ssh-keys deployment replace --private-key-file ./deploy_key [--name ops-deploy]
```

`generate` returns the private key only once. With `--private-key-out` it is written to
a new file with mode 0600 (an existing file is never overwritten); without it the
private key is printed. Regenerating or replacing the Deployment Key does not change
Servers deployed earlier: they authorize only the previous key.

### infrastructure (Zones and Pools)

```bash
swallow infrastructure zones list --site-id site1
swallow infrastructure zones get zone1
swallow infrastructure zones create -f zone.yaml      # { siteId, name, description }
swallow infrastructure zones update zone1 -f patch.yaml
swallow infrastructure zones delete zone1

# pools are identical:
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
swallow platforms uninstall plat1                     # software only
swallow platforms uninstall plat1 -f uninstall.yaml   # { releaseServers, releaseOptions }
swallow platforms sync plat1
swallow platforms sync-all
swallow platforms slurm plat1                         # live Slurm cluster state
swallow platforms slurm-requirements get
swallow platforms slurm-requirements set -f slurm-req.yaml
```

`platform-k8s.yaml` (see [platforms.md](../../api-server/docs/development/api-contracts/api-server/platforms.md)):

```yaml
siteId: site1
name: lab-k0s
type: kubernetes
gpuStackOwner: provisioning
roleAssignments:
  - { serverId: srva, role: control-plane, runWorkloads: true }
  - { serverId: srvb, role: worker }
```

`slurm-req.yaml`: `{ minimumResources: { cpuCores: 4, memoryMiB: 24576, storageGB: 80 } }`
(send `{ minimumResources: null }` to disable).

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
swallow platforms kubernetes secrets plat1        # key names only; values never returned
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
swallow workflows task logs wf1 task1      # text/plain, printed verbatim
swallow workflows task stderr wf1 task1    # text/plain
swallow workflows task events wf1 task1
swallow workflows task artifacts wf1 task1
```

`workflow.yaml`: `{ kind: custom, intent: "verify SSH", targetServerIds: [srv1], playbookName: diagnostic-ping, extraVars: {} }`

### monitoring

```bash
swallow monitoring alerts list --severity critical --state firing
swallow monitoring alerts acknowledge <fingerprint> -f ack.yaml --site-id site1
swallow monitoring metrics get --server srv1 --server srv2 --metric cpuUsagePercent
swallow monitoring metrics names
```

`ack.yaml`: `{ matchers: [...], duration: "2h", comment: "ack by ops" }`

### discovery (machine auth)

```bash
# Uses --machine-token if set, otherwise the session token.
swallow discovery prometheus --port 9100
swallow discovery prometheus --port 5000 --tag amd-gpu --site-id site1
swallow --machine-token "$TOKEN" discovery prometheus --provisioning-state all
```

## Recipes

```bash
# Filter server IDs with jq
swallow -o json servers list --provisioning-state failed \
  | jq -r '.items[].id'

# Watch the fleet and pretty-print each change
swallow servers watch --site-id site1 | jq -c '{type, id}'

# Build a deploy body on the fly
jq -n '{serverIds:["srv1"],settings:{imageId:"ubuntu/noble",deployTarget:"disk"}}' \
  | swallow provisioning deploy -f -
```

Servers come from provisioner reconciliation, so there is no `servers create`.
For scripts, prefer `-o json`, check the process exit code, preserve error
request IDs, pass an explicit `--site-id`, and derive structured body fields
from the
[provider-owned contract](../../api-server/docs/development/api-contracts/api-server/outline.md).
