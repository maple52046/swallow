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

One Site has one provisioner boundary. Model a MAAS installation spanning
independently operated locations as separate Site integrations.

## 3. Register monitoring

Create a `metrics` / `prometheus` Integration with the Prometheus endpoint
and, where required by the contract, Alertmanager and Grafana settings. swallow
queries fixed metric names and current alerts; it does not proxy arbitrary
PromQL or store monitoring data.

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
