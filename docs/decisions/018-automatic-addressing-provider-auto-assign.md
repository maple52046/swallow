# 018. Automatic deployment addressing is realized by provider auto-assign

- Status: Accepted (the `dhcp` alias and the rejection of a separate DHCP mode are superseded by [ADR 058](058-deployment-dhcp-and-subnet-dns.md))
- Date: 2026-09-06
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §6 部署網路意圖與 Subnet DNS

## Context

[ADR 012](012-provider-network-configuration.md) established that swallow owns deployment
network intent and exposes two modes, **DHCP** (default) and **Static**, translating DHCP
directly to the provider's raw DHCP link mode and deliberately never using MAAS `AUTO`
("auto-assign"). That decision conflated a swallow **intent** ("give this machine an address
automatically") with a provider **protocol** ("use DHCP").

In a MAAS-managed lab this produced a recurring, confusing symptom: some deployed VMs showed
**no IP** in swallow even though they were up and reachable. The cause is structural, not a
MAAS bug:

- MAAS only knows a DHCP interface's address **while it observes a live lease**. A deployed
  guest that is quiet, slow to renew, or left with a stale netplan has no observed lease, so
  MAAS — and swallow, which mirrors MAAS — report no address.
- The subnet's DHCP dynamic range is, by MAAS convention, the **enlistment/commissioning
  pool**. Using it as the *deployed* address source misuses a short-lived, ephemeral pool.
- MAAS already offers the right primitive for a stable deployed address: **auto-assign
  (`AUTO`)**, which allocates a static IP from the subnet's static space, **records it in
  MAAS**, and bakes it into the deployed netplan. The address is stable, survives reboots and
  DHCP outages, and is always known to the provider regardless of runtime lease state.

From the operator's point of view, DHCP and auto-assign are the same request — "assign an
address for me" — but auto-assign is strictly more reliable and always visible.

## Decision

swallow's automatic-addressing **intent** is named `automatic` and is realized by the
provider's **auto-assign** capability (MAAS `AUTO`), not by raw DHCP. `automatic` is the
default; `static` is unchanged.

- `automatic` means "the provisioner assigns and records a stable address"; the
  anticorruption layer chooses the provider primitive that fulfils it (MAAS `AUTO`).
- `AUTO` is an adapter implementation detail of `automatic`, not a swallow public mode. The
  raw `dhcp` and `link_only` link modes remain available for manual per-NIC configuration.
- This refines [ADR 012](012-provider-network-configuration.md): swallow still owns the intent
  and MAAS vocabulary still stays in the adapter, but ADR 012's rejection of `AUTO` is
  superseded. It follows [ADR 001](001-system-ownership-boundaries.md): use the provider's
  auto-assign rather than reimplementing address stability.

Revision history: this ADR also kept `dhcp` as a deprecated one-release alias of `automatic`
on the deployment API and rejected a separate DHCP mode. On 2026-10-13
[ADR 058](058-deployment-dhcp-and-subnet-dns.md) ended the alias and made `dhcp` a mode again,
for networks whose DHCP the provisioner does not serve; `automatic` keeps the meaning and MAAS
`AUTO` realization decided here (original text in git history).

## Alternatives considered

- Keep raw DHCP for the automatic mode (ADR 012): rejected — it is the direct cause of the
  intermittent "no IP", because the address is only known while a live lease is observed and
  it misuses the enlistment pool.
- Expose both `automatic` and `dhcp` as first-class deployment modes: rejected — two modes
  with the same operator meaning but different reliability is a leaky abstraction; `dhcp` was
  kept only as a temporary compatibility alias. (Reversed by ADR 058 once site-DHCP networks
  made the two requests different.)
- Fix it purely in the dashboard/read path (e.g. surface the guest's self-reported IP):
  rejected — it does not make the address stable or provider-known and would reintroduce the
  provider vocabulary swallow deliberately hides.

## Consequences

- Deployments in `automatic` mode get a stable, MAAS-recorded address, so swallow shows the IP
  immediately and consistently after deployment. The DHCP dynamic range returns to its
  intended enlistment/commissioning role.
- Machines deployed before this change keep their existing netplan until redeployed.
- `automatic` consumes a static IP from the subnet's non-dynamic space; a subnet must have
  static space available (normal for a managed subnet).
