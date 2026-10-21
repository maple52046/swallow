# Initial setup

[繁體中文](../../zh-TW/guides/initial-setup.md) · [Documentation home](../README.md)

Initial setup establishes the Site boundary, external Integrations, and the
automation policy required before swallow can manage Servers.

The [production installation](../installation.md) already performs steps 1, 2, and 4 for the
Site `default` with its co-located MAAS. Use this guide to review that result, to add more
Sites, or to set up a development stack.

## 1. Create the Site

Create a short operator-facing name and an optional description under
**Infrastructure → Sites**. A Site is intentionally not a rack hierarchy. Use
Zones and Pools for grouping Servers after inventory appears.

## 2. Register MAAS

Create an Integration with:

- kind `provisioner`;
- provider kind `maas`;
- the MAAS endpoint ending in the MAAS API base;
- a MAAS API credential;
- the target Site.

The credential is encrypted with the API credential key and is never returned.
After creation, check `hasCredential` and the integration sync status. A
successful reconciliation projects MAAS machines into Servers.

In the Dashboard, **Infrastructure → Integrations → Create integration** registers
it and then opens the Integration's settings page. Every Integration's settings
page (its name in the list, or **Edit**) has a tab each for **General** (name,
endpoint, enabled), **Connection** (timeout, TLS, automatic inspection, and
provider-specific settings), and **Credential** (set or replace the key). A MAAS
provisioner also has **HTTP proxy** and **Subnet DNS**, which are applied to MAAS
directly. Each tab has its own URL and its own Save or Apply.

Use the tabs according to who owns the value:

- **General** and **Connection** update the Integration that swallow stores.
  Disabling it stops provider reads; connection settings control how swallow
  reaches it. **Inspect newly enrolled Servers automatically** changes whether
  a newly discovered, never-inspected Server starts an inspection Workflow.
- **Credential** replaces the write-only MAAS API key. Saving another tab does
  not resend or clear it.
- **HTTP proxy** reads and writes MAAS live; it is not an Integration setting.
  **None** gives deployed machines no proxy, **Built-in** uses the MAAS proxy
  directly, **External** gives MAAS and deployed machines the supplied proxy,
  and **Peer** sends them through the MAAS proxy with the supplied proxy as its
  upstream. The proxy URL cannot contain credentials. A change affects later
  deployments, not machines already deployed.
- **Subnet DNS** reads MAAS subnets live and applies up to eight DNS server
  addresses to one subnet at a time. Automatic and Static deployments use this
  list (or MAAS DNS when it is empty); DHCP deployments get DNS from their DHCP
  server. A change affects later deployments only.

Because the last two tabs are live provider settings, a read or write can fail
when MAAS is unavailable, the Integration has no credential, or its MAAS
account is not an administrator.

One Site has one provisioner boundary. Model a MAAS installation spanning
independently operated locations as separate Site integrations.

## 3. Register monitoring

Run a per-Site Prometheus from `deploy/third-party/prometheus`, then create a
`metrics` / `prometheus` Integration with its endpoint and, optionally,
Alertmanager and Grafana settings. This is what lets swallow draw usage charts
for deployed Servers. swallow queries fixed metric names, bounded usage history,
and current alerts; it does not proxy arbitrary PromQL or store monitoring data.
See [Monitoring](monitoring.md).

Prometheus should use swallow's HTTP service-discovery endpoint so scraped
series receive stable `server_id`, `site`, and Platform labels.

## 4. Configure Site automation

Site automation defines:

- the SSH port and an optional fallback SSH user (each OS image's default user
  is tried first);
- mandatory known-host entries;
- the optional write-only become password;
- allowlisted manifest playbook mappings.

Automation always logs in with the installation's deployment key, which is created during
installation (`swallowctl install`) and registered in MAAS automatically; a Site has no
private key of its own. See [SSH keys and image login users](ssh-keys.md).

Only playbooks in the shipped manifest can execute. Automation is not an
external Integration; it is a Site-scoped swallow capability.

## 5. Verify readiness

- Integration sync has a recent `lastSucceededAt` and no unresolved error.
- Servers appear after reconcile; add more with **Servers → Add servers** (see
  [Add servers](servers-and-infrastructure.md#add-servers)).
- Worker and Ansible executor are healthy.
- A diagnostic Workflow reaches a terminal result.
- Prometheus discovery returns targets after exporters exist.

For exact payload fields, use the active
[Sites and Integrations contract](../../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)
and
[Site automation contract](../../../api-server/docs/development/api-contracts/api-server/site-automation.md).
