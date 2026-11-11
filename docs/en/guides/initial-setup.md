# Initial setup

[繁體中文](../../zh-TW/guides/initial-setup.md) · [Documentation home](../README.md)

Initial setup establishes the Site boundary, external Integrations, and the
automation policy required before swallow can manage Servers.

The [production installation](../installation.md) already performs steps 1 to 5 for the
Site `default` with its co-located MAAS, its Prometheus, and its local repository, which deployments
do not use yet (step 4 chooses how they use it). Use this guide to review that result, to add more
Sites, or to set up a development stack.

## 1. Create the Site

Create a short user-facing name and an optional description under
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

The production installation runs a Prometheus for its Site and registers it as the
Site's `metrics` / `prometheus` Integration (`site-prometheus`, endpoint
`http://prometheus:9090`); add Alertmanager and Grafana settings to that Integration if
you have them. A Site has at most one metrics Integration. This is what lets swallow draw
usage charts for deployed Servers. swallow queries fixed metric names, bounded usage
history, and current alerts; it does not proxy arbitrary PromQL or store monitoring data.
See [Monitoring](monitoring.md).

The Site's Prometheus discovers the exporters swallow installed in the Site through
swallow's HTTP service-discovery endpoint, so scraped series receive stable `server_id`,
`site`, `platform_id`, and `exporter` labels. In this version other Sites have no
Prometheus.

## 4. Choose where deployments fetch from

This step is optional. Deployments install packages, container images, Python
packages, and files that swallow's modules declare at fixed versions, and each
Site chooses where they come from:

- **Upstream sources**, the default: each Server fetches every artifact from the
  source its module declares. Files are checked against their declared checksums
  and package archives against their declared signing keys, and the Server keeps
  its own OS package sources.
- **Cache**: the Site's local repository, a repository next to swallow (Pulp, in
  the same installation), fetches each artifact from its upstream once and serves
  it to every Server of the Site. Servers fetch what it already serves from it and
  the rest from the upstream sources, so deployments never wait for it to
  synchronize, and Servers must still reach the upstreams.
- **Offline**: Servers fetch every artifact only from the local repository, so
  they never reach the Internet. Deployments are accepted once it serves
  everything they may fetch. The local repository itself still fetches packages
  from the upstreams the first time they are asked for, so swallow's host needs
  Internet access while it fills.

In both local repository modes OS package indexes are signed with the Site's own
key and files are checked against their declared checksums.

The production installation registers the local repository as the Site's
`repository` / `pulp` Integration, adds the Ubuntu archive as the Site's `ubuntu`
source, and leaves the Site on **Upstream sources**. A Site has at most one. To
use it, choose **Cache** or **Offline** as the **Artifact source for
deployments** under **Infrastructure → Local repository** (`mode` in the API):
the local repository starts fetching its content. In cache mode deployments are
accepted at once; in offline mode they are accepted once it is ready, and until
then they are refused with a message that names what to wait for or fix.
Choosing **Upstream sources** again makes later deployments fetch from the
upstream sources; what the local repository already holds is kept. A deployed
Server follows the Site's choice the next time a deployment runs on it.

Everything the local repository holds besides the `ubuntu` source is declared by
swallow's own modules, for example the Docker CE package archive, the k0s binary,
and the container images a Platform runs, so you add a source only for something
swallow does not declare. For a development stack, register it yourself: the
endpoint is where swallow reaches Pulp's API, the `contentOrigin` setting is the
address Servers reach it at, and the credential is the Pulp admin password.

**Infrastructure → Local repository** shows its sources and their sync state and
lets you browse its content by format, with the module that declared each item.
See the [local repository contract](../../../api-server/docs/development/api-contracts/api-server/local-repository.md)
for the API.

## 5. Configure Site automation

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

## 6. Verify readiness

- Integration sync has a recent `lastSucceededAt` and no unresolved error.
- When the Site uses its local repository, **Infrastructure → Local repository**
  says deployments are accepted: every source, file, and declared image is
  **Ready**.
- Servers appear after reconcile; add more with **Servers → Add servers** (see
  [Add servers](servers-and-infrastructure.md#add-servers)).
- Worker and Ansible executor are healthy.
- A diagnostic Workflow reaches a terminal result.
- Prometheus discovery returns targets after exporters exist.

For exact payload fields, use the active
[Sites and Integrations contract](../../../api-server/docs/development/api-contracts/api-server/sites-integrations.md)
and
[Site automation contract](../../../api-server/docs/development/api-contracts/api-server/site-automation.md).
