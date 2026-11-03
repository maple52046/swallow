# Local development environment

[繁體中文](README.zh-TW.md) · [Getting started](../../docs/en/getting-started.md)

The development Compose project starts the complete swallow topology from source
with hot reload. The host needs Docker Engine and the Compose plugin; Go, Node,
MongoDB, Temporal, Python, and Ansible tooling run in containers.

## Quick start

```bash
cd deploy/dev
docker compose up -d
docker compose ps
```

Open <http://localhost:5173>. The API is on <http://localhost:30051>.
The local-only bootstrap account is `admin` / `admin`.

## Services

| Service | Host port | Purpose |
| --- | ---: | --- |
| `dashboard` | 5173 | Vite dev server and same-origin API proxy |
| `api-server` | 30051 | swallow HTTP API |
| `mongo` | 27017 | swallow-owned state and projections; a single-node replica set `rs0` (connect from the host with `mongodb://localhost:27017/?directConnection=true`) |
| `temporal` | 7233 | durable orchestration |
| `temporal-ui` | 8233 | development diagnostics |
| `temporal-postgresql` | internal | Temporal databases |
| `worker` | internal | Workflow and activity execution |
| `ansible-executor` | internal | Idempotent Ansible attempts and events |
| `prometheus` | 9090 | the Site's Prometheus: swallow's own metrics (`swallow-api`) and the exporters swallow knows are installed (`swallow-exporters`, from swallow discovery) |
| `prom-label-proxy` | 9091 (loopback only) | the Site Prometheus confined to one Platform's series: the `platform_id` in the `X-Swallow-Platform-Id` header; the API reaches it at `http://prom-label-proxy:8080` |

## Common commands

```bash
docker compose logs -f api-server worker ansible-executor
docker compose restart dashboard
docker compose down
docker compose down -v       # destructive: removes local data and caches
docker compose build --no-cache
```

## Check and recover

`swallowctl` checks and converges this stack against the same health contract as a production
installation (minus the co-located MAAS, the installation records, and the installation Site's
exporter discovery, which `prometheus.yml` here replaces with one job for every Site):

```bash
../production/swallowctl doctor --profile dev
../production/swallowctl ensure --profile dev
```

After a host reboot every container restarts at once, and Air can keep a container up whose
process has exited, for example a worker that started before Temporal was ready. `ensure`
starts the services that are not running, restarts those whose check fails, and ends with
`doctor`; a check it cannot fix is printed with the service's recent logs. It needs no root.

## Configuration

The stack starts without a `.env`. Copy `.env.example` only when overriding
defaults:

```bash
cp .env.example .env
```

Important values:

- `DEV_UID` / `DEV_GID` must match the source-tree owner when containers
  write bind mounts.
- `VITE_API_BASE_URL` is empty by default so Vite proxies same-origin
  `/api` requests. Restart Dashboard after changing it.
- `SWALLOW_API_CREDENTIAL_KEY` encrypts write-only credentials. The bundled
  value is local-only; changing it makes existing local credentials unreadable.
- `SWALLOW_API_MACHINE_TOKEN` authenticates Prometheus discovery/metrics and
  other machine endpoints, not the user API.

All bundled passwords, tokens, TLS material, and keys are development-only.

## Hot reload

- API, worker, and Ansible executor use separate Air processes. Each watches
  `api-server/` for `.go`, `.yaml`, and `.yml` changes (tests and docs excluded),
  so such a change rebuilds and restarts all three; restarting the Ansible
  executor interrupts a running playbook. `modules/manifest.json` is not watched:
  restart the three services after changing it.
- Dashboard uses Vite HMR. If `package-lock.json` changes, the entrypoint
  refreshes dependencies.
- Compiled artifacts and caches remain in container/named-volume paths rather
  than polluting tracked source.

Temporal, worker, and Ansible executor must all remain available for Workflows;
there is no API-process execution fallback.

## Access from another machine

Dashboard and API listen on all interfaces for development. Open
`http://<host-ip>:5173`, or tunnel only the Dashboard port:

```bash
ssh -L 5173:localhost:5173 user@host
```

Vite trusts IP/localhost host headers by default. Add a deliberate
`server.allowedHosts` entry before using a development DNS name.

## Boot Media lab

To try Boot Media (Redfish virtual media iPXE boot, [decision 047](../../docs/decisions/047-redfish-boot-media.md) (redfish boot media)) against a real
BMC, set in `.env` the address the BMC network reaches this host at:

```bash
SWALLOW_API_BOOT_MEDIA_BASE_URL=http://10.0.0.5
BOOT_MEDIA_HTTP_PUBLISH=10.0.0.5:80
docker compose up -d api-server
```

Open **Provisioning → Boot ISOs** and build a Boot ISO for the provisioner. There
is no ISO to place in this directory. Built ISOs are stored under the gitignored
`boot-media/<isoId>/` directory and served at
`<SWALLOW_API_BOOT_MEDIA_BASE_URL>/boot-media/ipxe/<isoId>/swallow-ipxe.iso`.
The API is also published on the configured address's port 80 because many BMCs
accept only plain HTTP on port 80.

Editing api-server Go code restarts the worker (Air), which fences a running
deployment's lease; avoid it while a lab deployment is in flight.

## Control-plane NTP

Every OS provisioning points the new Server at this host's NTP server after SSH comes up
(`configure-ntp` Task, [decision 065](../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md) (control plane ntp and post provision time sync)), and each Platform's install playbook checks the nodes' clocks
before installing. `SWALLOW_API_NTP_SERVER` in `.env` is the address Servers reach this host's NTP
server at (not its upstream); the dev stack cannot derive it, so while it is empty every OS
deployment is refused.

The host's NTP server is the Control-Plane NTP Server. With a local MAAS snap it is MAAS's own
chronyd and nothing is installed; otherwise `local-ntp.sh install` installs host chrony. Its
upstream is set separately with `local-ntp.sh upstream` (the production installation uses
`swallowctl ntp upstream`); unset means `ntp.ubuntu.com`. On a network that reaches no public NTP,
point the upstream at a time source it can reach (rendered `server <address> iburst`) and give the
Servers the address they reach this host at. With an upstream at `192.0.2.3` and this host at
`192.0.2.20`:

```bash
sudo SWALLOW_NTP_UPSTREAM=192.0.2.3 ../production/local-ntp.sh upstream
sudo SWALLOW_NTP_UPSTREAM=192.0.2.3 ../production/local-ntp.sh check
echo SWALLOW_API_NTP_SERVER=192.0.2.20 >> .env && docker compose up -d api-server worker
```

Outside an installation `local-ntp.sh` configures MAAS with `SWALLOW_MAAS_API_KEY`, or with the
key of the MAAS user `SWALLOW_MAAS_ADMIN_USER` (default `admin`). Running `local-ntp.sh upstream`
without `SWALLOW_NTP_UPSTREAM` returns MAAS to the default.

The upstream must not take its time from this host. A gateway that serves the clock of the host it
runs on plus a correction (for example an NTP-over-HTTP gateway) must not run here: MAAS's chronyd
would chase a target computed from the clock it disciplines, and the two run away from real time
together. `local-ntp.sh upstream` also
steps the clock once when it is more than a second off the upstream, so set the upstream with the
script rather than in MAAS by hand.

## Platform monitoring addresses

A Slurm Platform's Grafana reads the Site Prometheus through the API's Platform metrics gateway,
and Omnistat admits only the addresses that Prometheus scrapes from. Set both in `.env`:
`SWALLOW_API_PUBLIC_URL` is the URL the Servers reach this API at (publish the API there, for
example with `BOOT_MEDIA_HTTP_PUBLISH`), and `SWALLOW_API_MONITORING_SCRAPE_ADDRESSES` lists this
host's addresses as the Servers see them. While either is empty, a Slurm deployment that needs it
is refused when it is planned. For the lab:

```bash
printf 'SWALLOW_API_PUBLIC_URL=http://10.170.168.20\nSWALLOW_API_MONITORING_SCRAPE_ADDRESSES=10.170.168.20,192.168.100.1\n' >> .env
docker compose up -d api-server worker
```

## Seed a demonstration Site

The idempotent seed script creates the deployment key (`swallow-api deployment-key
ensure`, the installation step; the API does not create it on start), logs in, creates a Site, registers the in-Compose
Prometheus as its metrics Integration with the in-Compose prom-label-proxy as its `labelProxyUrl`
setting (as the production bootstrap does, never a second one), configures site automation, and
optionally registers MAAS when values are present:

```bash
cp .env.example .env
docker compose up -d
bash seed.sh
```

Without MAAS or SSH inputs, those steps are skipped with guidance. Each OS
deployment installs the Node Exporter on the Servers it deploys; install it as
Managed Software on a Server swallow did not deploy. swallow installs nothing on a
locked Server.

To enroll libvirt virtual machines with a local MAAS snap, give its rack the SSH
identity the production installation creates (`local-maas.sh ensure-virsh-identity`
does the same on an installed host), then seed its public key:

```bash
sudo install -d -m 0700 /var/snap/maas/current/root/.ssh
sudo test -s /var/snap/maas/current/root/.ssh/id_ed25519 ||
  sudo ssh-keygen -q -t ed25519 -N '' -f /var/snap/maas/current/root/.ssh/id_ed25519
printf '\n# BEGIN swallow virsh\nHost *\n  StrictHostKeyChecking accept-new\n# END swallow virsh\n' |
  sudo tee -a /var/snap/maas/current/root/.ssh/config >/dev/null
sudo cat /var/snap/maas/current/root/.ssh/id_ed25519.pub >/tmp/maas-virsh.pub
SWALLOW_MAAS_VIRSH_SSH_PUBLIC_KEY_FILE=/tmp/maas-virsh.pub bash seed.sh
```

For manual setup, follow the [initial setup guide](../../docs/en/guides/initial-setup.md).

## Troubleshooting

- **Docker socket permission denied:** log in again after joining the
  `docker` group, or use an explicitly authorized session.
- **Dashboard environment change not visible:** restart `dashboard`; Vite
  reads environment values at process start.
- **API keeps restarting:** inspect `docker compose logs api-server` for Air
  build/config errors.
- **Workflow does not progress:** run `../production/swallowctl ensure --profile dev`; it
  converges Temporal, the worker, and `ansible-executor`, and prints the logs of anything it
  cannot fix.
- **Integration data is old:** read its last success/error, validate the
  external endpoint and credential, then reconcile.
