# Production installation (single VM)

[繁體中文](README.zh-TW.md) · [Installation choices](../../docs/en/installation.md)

This directory is the production installation for one Ubuntu 24.04 amd64 VM: swallow on
Docker Compose plus a co-located MAAS 3.6 region+rack. "Production" names the installation
contract (digest-pinned images, generated secrets, lifecycle tooling), not a multi-host
topology. swallow is in active development; install from a release or candidate manifest.

## Install

```bash
sudo ./swallowctl install --profile production --release-manifest ../release-manifest.json
```

`install` is idempotent and runs, in order:

1. installs missing host packages (`curl`, `jq`, `openssl`, `snapd`) and Docker Engine with
   the Compose plugin from Docker's apt repository;
2. writes the manifest's version and exact image digests to `.env` (other settings in `.env`
   are kept) and generates secrets in `secrets/` (mode 0700) with `prepare-secrets.sh`;
3. pulls only missing images, starts MongoDB and PostgreSQL, runs `swallow-api migrate`,
   creates the Deployment Key (`swallow-api deployment-key ensure`), and starts the stack;
4. installs the bundled CLI to `/usr/local/bin/swallow`;
5. runs [`local-maas.sh`](local-maas.sh): creates the `maas` role and `maasdb` database in
   the Compose PostgreSQL, installs and initializes the MAAS snap, creates the MAAS `admin`,
   stores its API key, and waits until the official `ubuntu/noble` amd64 image is complete;
6. runs [`bootstrap.sh`](bootstrap.sh) through the published API: Site `default`, the MAAS
   provisioner Integration, and Site automation defaults (enabled, Deployment Key, no
   playbook mappings) when the Site has none; then waits for the Integration sync and the
   `ubuntu/noble` catalog entry;
7. runs `doctor` and prints the Dashboard URL and where the passwords are.

It never creates an SSH Access Key: the operator adds their own in the Dashboard.

## Topology

| Component | Where | Exposure |
| --- | --- | --- |
| Dashboard (Nginx) | Compose | HTTP `${SWALLOW_HTTP_PORT:-80}`, the only public listener |
| API, worker, Ansible executor | Compose | internal `control` plus `egress` (reach MAAS and Servers) |
| MongoDB, Temporal | Compose | internal only |
| PostgreSQL | Compose | Temporal databases and `maasdb`; `127.0.0.1:${SWALLOW_POSTGRES_HOST_PORT:-5432}` for MAAS |
| MAAS 3.6 region+rack | snap on the host | `http://<primary IPv4>:5240/MAAS`; swallow reaches it at `http://host.docker.internal:5240/MAAS` |
| Temporal UI | Compose profile `diagnostics` | `127.0.0.1:${SWALLOW_TEMPORAL_UI_PORT:-8233}`, off by default |

The installation serves plain HTTP. Put a TLS terminator in front of port 80 before exposing
it beyond a trusted management network.

## Settings

Set these in `.env` or the environment before the first `install`:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SWALLOW_HTTP_PORT` | `80` | Dashboard and API port |
| `SWALLOW_POSTGRES_HOST_PORT` | `5432` | loopback port MAAS uses for PostgreSQL |
| `SWALLOW_MAAS_URL` | `http://<primary IPv4>:5240/MAAS` | address machines use to reach MAAS |
| `SWALLOW_MAAS_IMAGE_TIMEOUT` | `3600` | seconds to wait for `ubuntu/noble` |
| `SWALLOW_SITE_NAME` | `default` | Site created by the bootstrap |
| `SWALLOW_SSH_KNOWN_HOSTS` | scanned per run | static known_hosts for the Site automation |

## Check

```bash
sudo ./swallowctl doctor
```

`doctor` checks the Dashboard and API, Temporal health, that the worker polls the
`swallow-operations` task queue, that the Ansible executor runs and `ansible-runner` executes
the shipped `diagnostic-ping` playbook, the MAAS API and its `ubuntu/noble` boot resource,
the admin login, the Deployment Key, Site automation, the MAAS Integration sync, and the
`ubuntu/noble` entry in the OS Image catalog. It deploys nothing.

## Operate

- `sudo ./swallowctl upgrade [--release-manifest PATH] [--force]` backs up first, refuses
  while Workflows are active unless `--force`, migrates, and reruns `doctor`.
- `sudo ./swallowctl backup` writes MongoDB, the job artifacts, the credential key, and the
  MAAS database under `backups/`. Schedule it daily; retention is seven daily and four Sunday
  weekly recovery points. MAAS boot images re-sync from `images.maas.io`.
- `sudo ./swallowctl restore BACKUP_DIRECTORY` verifies checksums, restores MongoDB, the
  artifacts, the credential key, and the MAAS database, then recreates the API and Dashboard.
  SSH key sync attempted while MAAS restarts reports `failed` until the next sync interval;
  `swallow ssh-keys sync` (or **Sync to provisioners**) repeats it at once.
- `sudo ./swallowctl uninstall` stops the stack and MAAS and keeps volumes, secrets, and
  backups; `--purge-data` also removes the MAAS snap, the volumes, and the backups.

The `secrets/` directory must never enter source control; back it up with the lifecycle tool.
For an air-gapped host, `docker load` the release OCI archive first: `install` only pulls
images that are missing. Importing MAAS images offline is not automated yet.

## Diagnostics

```bash
docker compose --profile diagnostics up -d temporal-ui   # needs SWALLOW_TEMPORAL_UI_IMAGE
docker compose logs -f api-server worker ansible-executor
sudo ./local-maas.sh check
```

The [native path](native/README.md) is an incomplete preview; use this Compose topology.
