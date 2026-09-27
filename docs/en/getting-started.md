# Getting started

[繁體中文](../zh-TW/getting-started.md) · [Documentation home](README.md)

This walkthrough starts the development topology, signs in, creates a Site, and
shows the next steps for a real integration. It is an evaluation workflow, not a
production installation.

## 1. Start the stack

Prerequisites:

- Docker Engine with the Compose plugin.
- A user allowed to access the Docker daemon.
- Enough local resources for MongoDB, Temporal, the API, worker, Ansible
  executor, Dashboard, and Prometheus.

```bash
git clone git@github.com:maple52046/swallow.git
cd swallow/deploy/dev
docker compose up -d
docker compose ps
```

Wait until the services are healthy. See the
[development environment reference](../../deploy/dev/README.md) for logs,
configuration overrides, and troubleshooting.

## 2. Sign in

Open <http://localhost:5173> and use `admin` / `admin`. This account and all
secrets bundled by the development Compose file are local-development defaults
and must never be reused in a shared or production environment.

The Overview is initially empty because swallow does not invent infrastructure.

## 3. Create a Site

In **Infrastructure → Sites**, create a Site such as `lab`. A Site is the
scope for integrations, Servers, Platforms, automation policy, and monitoring.

You can do the same through the CLI after building it:

```bash
cd ../../cli
go build -o bin/swallow ./cmd/swallow
printf '%s\n' 'admin' | bin/swallow --endpoint http://127.0.0.1:30051 \
  login -u admin --password-stdin
bin/swallow sites create --file - <<'JSON'
{"name":"lab","description":"Local evaluation site"}
JSON
```

## 4. Register external systems

A useful Site normally has:

- one `provisioner` / `maas` Integration for machine inventory and OS
  lifecycle;
- one `metrics` / `prometheus` Integration for metrics and Alertmanager;
- Site automation settings containing SSH policy, known hosts, playbook
  mappings, and a write-only credential.

Use **Infrastructure → Integrations** and the Site automation controls, or follow
the [initial setup guide](guides/initial-setup.md). Credentials cannot be read
back; `hasCredential` only reports whether one is stored.

## 5. Reconcile and inspect

After a MAAS Integration succeeds, the next reconciliation creates or updates
Server projections. Operators do not create Servers directly.

- Open **Servers** to inspect inventory and staleness.
- Use **Provisioning** to inspect images, verify deployability, and create
  deployment templates.
- Use **Workflows** to observe durable work.
- Use **Monitoring** after registering Prometheus/Alertmanager.

## 6. Choose the next guide

- [Servers and infrastructure](guides/servers-and-infrastructure.md)
- [OS provisioning](guides/os-provisioning.md)
- [Platforms](guides/platforms.md)
- [Managed Software](guides/managed-software.md)
- [CLI reference](reference/cli.md)
- [API integration](reference/api-integration.md)
