# Production installation (single VM)

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

This directory is the production installation for one Ubuntu 24.04 amd64 VM: swallow on
Docker Compose plus a co-located MAAS 3.6 region+rack. "Production" names the installation
contract (digest-pinned images, generated secrets, lifecycle tooling), not a multi-host
topology. swallow is in active development; install from a release or candidate manifest.

## Install

The release's `install.sh` downloads and verifies the bundle, extracts it into `/opt/swallow`,
and runs `swallowctl install`, or `swallowctl upgrade` once an earlier release finished
installing there ([installation guide](../../docs/en/installation.md)):

```bash
curl -fsSL https://github.com/maple52046/swallow/releases/download/v<version>/install.sh \
  | sudo bash -s -- --address <address>
```

To inspect the bundle first, download `swallow-compose-<version>.tar.zst` and `SHA256SUMS`
from the release yourself, then:

```bash
sha256sum --ignore-missing -c SHA256SUMS
sudo mkdir -p /opt/swallow
sudo tar --zstd -xf swallow-compose-<version>.tar.zst -C /opt/swallow --no-same-owner
cd /opt/swallow/production
sudo SWALLOW_ADDRESS=<address> ./swallowctl install --release-manifest ../release-manifest.json
```

`install` is idempotent and runs, in order:

1. installs missing host packages (`curl`, `jq`, `openssl`, `snapd`) and Docker Engine with
   the Compose plugin from Docker's apt repository;
2. writes the manifest's version and exact image digests to `.env`, adds the settings below
   that are set in the environment (other settings in `.env` are kept), and generates secrets
   in `secrets/` (mode 0700) with `prepare-secrets.sh`;
3. pulls only missing images, starts MongoDB and PostgreSQL and waits until both are healthy
   (MongoDB runs as the single-node replica set `rs0`), runs `swallow-api migrate`,
   creates the Deployment Key (`swallow-api deployment-key ensure`), and starts the stack;
4. installs the bundled CLI to `/usr/local/bin/swallow` (the API also serves `bin/swallow`
   at `/downloads/swallow` for hosts that enroll with their OS kept);
5. runs [`local-maas.sh`](local-maas.sh): creates the `maas` role and `maasdb` database in
   the Compose PostgreSQL, installs and initializes the MAAS snap, creates the MAAS `admin`,
   stores its API key, gives the MAAS rack an SSH key for `virsh` power (accepting a new
   hypervisor host key on first connection) and stores its public key in
   `secrets/maas-virsh-ssh.pub`, and waits until the official `ubuntu/noble` amd64 image is
   complete;
6. runs [`local-ntp.sh`](local-ntp.sh) `install` and `upstream` ([decision 065](../../docs/decisions/065-control-plane-ntp-and-post-provision-time-sync.md) (control plane ntp and post provision time sync)). The provisioner
   is the co-located MAAS, so there is nothing to install: MAAS's own chronyd is the
   installation's Control-Plane NTP Server on UDP 123 of `SWALLOW_ADDRESS`. `upstream` writes
   `SWALLOW_NTP_UPSTREAM`, or the default `ntp.ubuntu.com` when it is unset, into MAAS's
   `ntp_servers` and keeps `ntp_external_only` off. MAAS's chrony has no `makestep`, so when the
   host clock is more than a second off the upstream the script has chronyd step it once rather
   than slew at +8.3% for minutes;
7. runs [`bootstrap.sh`](bootstrap.sh) through the published API: Site `default`, the MAAS
   provisioner Integration with the rack's `virsh` public key as its `virshSshPublicKey`
   setting (swallow authorizes it on a hypervisor when it enrolls that hypervisor's libvirt
   virtual machines), the Site's Prometheus as its metrics Integration (`site-prometheus`,
   `http://prometheus:9090`, with the setting `labelProxyUrl` `http://prom-label-proxy:8080`; an
   existing metrics Integration is repointed and keeps its other settings, never duplicated),
   Site automation defaults (enabled, Deployment Key, no playbook mappings) when the Site has
   none; then waits for the Integration sync and the `ubuntu/noble` catalog entry;
8. renders the Prometheus exporter discovery for the Site: the `swallow-exporters` job, from
   [`prometheus-exporters.yml.template`](prometheus-exporters.yml.template) with the Site's id,
   into `state/prometheus/swallow-exporters.yml`, restarting Prometheus only when it has not
   loaded that file yet;
9. runs `doctor`, records the installed release in `state/installed`, and prints the
   Dashboard URL and where the passwords are.

It never creates an SSH Access Key: the user adds their own in the Dashboard.

## Topology

| Component | Where | Exposure |
| --- | --- | --- |
| Dashboard (Nginx) | Compose | HTTP `${SWALLOW_HTTP_PORT:-80}`, the only public listener |
| API, worker, Ansible executor | Compose | internal `control` plus `egress` (reach MAAS and Servers) |
| MongoDB (single-node replica set `rs0`), Temporal | Compose | internal only |
| PostgreSQL | Compose | Temporal databases and `maasdb`; `127.0.0.1:${SWALLOW_POSTGRES_HOST_PORT:-5432}` for MAAS |
| Prometheus (the Site's metrics) | Compose | internal `control` plus `egress` (scrape the Servers' exporters); not published, the API reads `http://prometheus:9090` |
| prom-label-proxy (one Platform's view of those metrics) | Compose | internal `control` only; not published, the API reads `http://prom-label-proxy:8080` |
| MAAS 3.6 region+rack | snap on the host | `SWALLOW_MAAS_URL`, which swallow's provisioner Integration uses too |
| Control-Plane NTP Server (MAAS's chronyd) | snap on the host | UDP 123 on every address, including `SWALLOW_ADDRESS` |
| Temporal UI | Compose profile `diagnostics` | `127.0.0.1:${SWALLOW_TEMPORAL_UI_PORT:-8233}`, off by default |

The installation serves plain HTTP. Put a TLS terminator in front of port 80 before exposing
it beyond a trusted management network. Deployed Servers reach the NTP server on UDP 123, so a
host firewall must allow it from the machine networks.

Every Server swallow provisions is pointed at this NTP server once SSH comes up
(`configure-ntp` Task): Compose hands the api-server `SWALLOW_ADDRESS` as `SWALLOW_API_NTP_SERVER`.
Each Platform's install playbook checks every node's clock before installing. On a network
that cannot reach `ntp.ubuntu.com`, give the NTP server a reachable upstream with
`sudo ./swallowctl ntp upstream set 10.0.0.123` (rendered `server 10.0.0.123 iburst`), or set
`SWALLOW_NTP_UPSTREAM` before the first install. The upstream must not
take its own time from this host (directly, or through a gateway whose served time is computed
from this host's clock): this host follows it, so the two would only chase each other. Without a
reachable upstream the server keeps serving its local clock, so Servers still agree with each
other.

The Site's Prometheus collects swallow's own metrics (`swallow-api`) and every exporter swallow
knows is installed on this Site's Servers (`swallow-exporters`, discovered from the API with the
machine token), every 30 seconds. [`prometheus.yml`](prometheus.yml) is shipped; the exporter
job is rendered under `state/`, which a new release does not overwrite. Its metrics are kept for
`SWALLOW_PROMETHEUS_RETENTION` and are never backed up: losing them loses usage history, not
swallow state.

prom-label-proxy sits in front of that Prometheus and confines every query, the label APIs
included, to the series of one Platform: the `platform_id` in the `X-Swallow-Platform-Id` header.
A request without the header is refused. It authenticates nobody, so it is reachable only on the
internal `control` network.

## Settings

Set these in `.env` or the environment before the first `install`. `install` and `upgrade`
write the ones set in the environment into `.env`, so later runs keep them:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SWALLOW_ADDRESS` | the value already in `.env`, else the host the co-located MAAS was first initialized with, else the primary IPv4 | address machines and BMCs use to reach this host (`install.sh --address`) |
| `SWALLOW_HTTP_PORT` | `80` | Dashboard and API port |
| `SWALLOW_POSTGRES_HOST_PORT` | `5432` | loopback port MAAS uses for PostgreSQL |
| `SWALLOW_MAAS_URL` | `http://<SWALLOW_ADDRESS>:5240/MAAS` | address machines use to reach MAAS; fixed at the first install |
| `SWALLOW_BOOT_MEDIA_BASE_URL` | `http://<SWALLOW_ADDRESS>` | address BMCs use to fetch Boot ISOs |
| `SWALLOW_MAAS_IMAGE_TIMEOUT` | `3600` | seconds to wait for `ubuntu/noble` |
| `SWALLOW_SITE_NAME` | `default` | Site created by the bootstrap |
| `SWALLOW_SSH_KNOWN_HOSTS` | scanned per run | static known_hosts for the Site automation |
| `SWALLOW_NTP_UPSTREAM` | `ntp.ubuntu.com` | NTP servers the Control-Plane NTP Server follows, separated by commas or spaces; an address becomes `server <ip> iburst`, a name `pool <name> iburst`. Change it later with `swallowctl ntp upstream` |
| `SWALLOW_NTP_ALLOW` | every client | without a co-located MAAS only: client subnets host chrony answers |
| `SWALLOW_PROMETHEUS_RETENTION` | `30d` | how long the Site's Prometheus keeps metrics (a Prometheus duration); the longest usage history swallow can chart |

## Check

```bash
sudo ./swallowctl doctor
```

`doctor` checks the Dashboard and API, Temporal health, that the worker polls the
`swallow-operations` task queue, that the Ansible executor runs, polls the `swallow-ansible` task
queue, and `ansible-runner` executes the shipped `diagnostic-ping` playbook, the MAAS API and its `ubuntu/noble` boot resource,
the Control-Plane NTP Server (UDP 123 served, MAAS's upstream equals `SWALLOW_NTP_UPSTREAM` or the
default, and whether it follows that upstream), the admin login, the Deployment Key, Site
automation, the metrics Integration pointing at the Site's Prometheus and its prom-label-proxy,
the MAAS Integration sync, and the `ubuntu/noble` entry in the OS Image catalog, that Prometheus is
ready and runs the `swallow-exporters` job of this Site, and that prom-label-proxy confines a query
to the `platform_id` it is given. It also prints how many Workflows have not reached a terminal
status. It deploys nothing.

## Recover

The installation's desired state is the contract `doctor` checks:

1. every declared Compose service runs;
2. the Temporal cluster is healthy;
3. the worker polls the `swallow-operations` task queue with workflow and activity pollers;
4. the Ansible executor process runs, polls the `swallow-ansible` task queue with an activity
   poller, and executes the shipped `diagnostic-ping` playbook;
5. the Dashboard and API answer `/readyz` on the HTTP port;
6. Prometheus is ready and runs the `swallow-exporters` job, and prom-label-proxy confines each
   query to the `platform_id` it is given;
7. the co-located MAAS answers with a complete `ubuntu/noble` and its virsh SSH identity, the
   Control-Plane NTP Server serves UDP 123 with the configured upstream, the installation
   records (admin login, Deployment Key, Site automation, metrics Integration, MAAS Integration
   sync, OS Image catalog) exist, and Prometheus discovers the exporters of the installation Site.

`doctor` only reports drift from this state. After a power loss, a host reboot, or any `FAIL`
line, run one command:

```bash
sudo ./swallowctl ensure
```

`ensure` starts the declared services that are not running, then holds each condition in
dependency order: when one fails, it restarts the services that own it (the MAAS snap for the
MAAS API), runs the idempotent `local-maas.sh` ensure steps, `local-ntp.sh install` and `upstream`,
`bootstrap.sh apply`, or the Prometheus exporter discovery rendering when their checks fail, and
finally runs `doctor`. A condition that still fails is printed with its
services' recent logs. `ensure` is safe to rerun. It never recreates a running container or
applies a changed `.env` (that is `upgrade`), and it never cancels or retries Workflows.

## Operate

- `sudo ./swallowctl upgrade [--release-manifest PATH] [--force]` refuses a release older than
  the installed one and a database whose schema is newer than the release serves (downgrades are
  not supported), and refuses while Workflows are active unless `--force`. It pulls the new
  images, stops the API, worker, and Ansible executor, backs up to
  `backups/pre-upgrade-<installed version>-<time>` (kept until you remove it), migrates, starts
  the stack, ensures the Control-Plane NTP Server and its upstream, the MAAS rack's `virsh` SSH
  key and its record on the Integration, the metrics Integration and the Prometheus exporter
  discovery, and reruns `doctor`. `install.sh` refuses a downgrade before it extracts anything.
- `sudo ./swallowctl ntp upstream [show | set SERVER... | default]` shows, sets, or resets the
  NTP server's upstream. `set` and `default` apply the change first and record it in `.env` only
  when it succeeded. swallowctl owns MAAS's `ntp_servers`: a value set in the MAAS UI is replaced
  on the next install, upgrade, or ensure.
- `sudo ./swallowctl backup` writes MongoDB, the job artifacts, the credential key, the MAAS
  database, and `backup.json` (the release and database schema it was taken from) under
  `backups/`, with checksums relative to the backup. Schedule it daily; retention is seven daily
  and four Sunday weekly recovery points. MAAS boot images re-sync from `images.maas.io`.
  Temporal and the Prometheus metrics (volume `prometheus-data`) are not backed up.
- `sudo ./swallowctl restore BACKUP_DIRECTORY --yes` verifies the checksums, refuses a backup from
  a newer release, stops the API, worker, and Ansible executor, restores MongoDB, the artifacts
  (replacing the current ones), the credential key, and the MAAS database, migrates, terminates
  every swallow run in Temporal, starts the stack, and runs `doctor`. The restored database is the
  truth: each unfinished Workflow is dispatched again from it, so work recorded after the backup
  is lost and Tasks that finished after it run again. Without `--yes` it only says what it would
  replace. SSH key registration attempted while MAAS restarts is retried with backoff and at the
  next controller resync; `swallow ssh-keys sync` (or **Sync to provisioners**) repeats it at once.
- `sudo ./swallowctl uninstall` stops the stack and MAAS and keeps volumes, secrets, and
  backups; `--purge-data` also removes the MAAS snap, the volumes, and the backups.

The `secrets/` directory must never enter source control; back it up with the lifecycle tool.
Installation needs registry access: releases ship no offline image archive, and importing MAAS
images offline is not automated yet.

## Diagnostics

```bash
docker compose logs -f api-server worker ansible-executor
sudo ./local-maas.sh check
sudo ./local-ntp.sh check
docker compose exec prometheus wget -qO- http://127.0.0.1:9090/api/v1/targets   # Prometheus is not published
docker compose exec prometheus wget -qO- --header 'X-Swallow-Platform-Id: <platform id>' \
  'http://prom-label-proxy:8080/api/v1/query?query=up'                          # what one Platform sees
```

The Temporal UI is optional and not part of a release. To use it, set
`SWALLOW_TEMPORAL_UI_IMAGE` in `.env` to an exact digest (`.env.example` suggests one), then
run `docker compose --profile diagnostics up -d temporal-ui`; it listens on loopback only.

The [native path](native/README.md) is an incomplete preview; use this Compose topology.
