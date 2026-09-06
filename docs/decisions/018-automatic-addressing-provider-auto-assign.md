# 018. Automatic deployment addressing is realized by provider auto-assign

- Status: Accepted
- Date: 2026-09-06

## Context

[ADR 012](012-provider-network-configuration.md) established that swallow owns deployment
network intent and exposes two modes, **DHCP** (default) and **Static**, translating DHCP
directly to the provider's raw DHCP link mode and deliberately never using MAAS `AUTO`
("auto-assign"). That decision conflated a swallow **intent** ("give this machine an address
automatically") with a provider **protocol** ("use DHCP").

In a MAAS-managed lab this produced a recurring, confusing symptom: some deployed VMs showed
**no IP** in swallow even though they were up and reachable. Investigation found the cause is
structural, not a MAAS bug:

- MAAS only knows a DHCP interface's address **while it observes a live lease**. A deployed
  guest that is quiet, slow to renew, or left with a stale netplan simply has no observed
  lease, so MAAS — and swallow, which mirrors MAAS — report no address.
- The subnet's DHCP dynamic range is, by MAAS convention, the **enlistment/commissioning
  pool** (its comment literally said so). Using it as the *deployed* address source misuses a
  short-lived, ephemeral pool.
- MAAS already offers the right primitive for a stable deployed address: **auto-assign
  (`AUTO`)**, which allocates a static IP from the subnet's static space, **records it in
  MAAS**, and bakes it into the deployed netplan. The address is stable, survives reboots and
  DHCP outages, and is always known to the provider regardless of runtime lease state.

From the operator's point of view, DHCP and auto-assign are the same request — "assign an
address for me" — but auto-assign is strictly more reliable and always visible.

## Decision

swallow's automatic-addressing **intent** is named `automatic` and is realized by the
provider's **auto-assign** capability (MAAS `AUTO`), not by raw DHCP. `static` is unchanged.

- The deployment network modes are **`automatic`** (default) and **`static`**. `automatic`
  means "the provisioner assigns and records a stable address"; the anticorruption layer
  chooses the best provider primitive to fulfil it (MAAS `AUTO`).
- The MAAS adapter maps the internal `auto` link mode to MAAS `AUTO`; it still maps `static`
  to `STATIC` and keeps raw `dhcp`/`link_only` only for **manual** per-NIC configuration,
  which is a separate feature and not a deployment intent.
- `dhcp` remains a **deprecated one-release alias** of `automatic` on the deployment API, so
  existing callers keep working and immediately get the more reliable behavior; it normalizes
  to `automatic` and is never stored.

This refines [ADR 012](012-provider-network-configuration.md): swallow still owns the intent
and MAAS vocabulary still stays in the adapter, but the automatic intent is fulfilled with
`AUTO` instead of raw DHCP. ADR 012's rejection of `AUTO` as a *deployment intent* is
superseded; `AUTO` is now an adapter implementation detail of `automatic`, not a swallow
public mode.

## Alternatives considered

- Keep raw DHCP for the automatic mode (ADR 012): rejected — it is the direct cause of the
  intermittent "no IP", because the address is only known while a live lease is observed and
  it misuses the enlistment pool.
- Expose both `automatic` and `dhcp` as first-class deployment modes: rejected — two modes
  with the same operator meaning but different reliability is a leaky abstraction; `dhcp` is
  kept only as a temporary compatibility alias.
- Fix it purely in the dashboard/read path (e.g. surface the guest's self-reported IP):
  rejected — it does not make the address stable or provider-known and would reintroduce the
  provider vocabulary swallow deliberately hides.

## Consequences

- New deployments in `automatic` mode get a stable, MAAS-recorded address, so swallow shows
  the IP immediately and consistently after deployment. The DHCP dynamic range returns to its
  intended enlistment/commissioning role.
- Machines deployed before this change keep their existing netplan until redeployed; the fix
  applies on the next deployment (including via the deprecated `dhcp` alias).
- `automatic` consumes a static IP from the subnet's non-dynamic space; a subnet must have
  static space available (normal for a managed subnet).
- Glossary, the provisioning API contract, and the dashboard use `automatic`; the deprecated
  `dhcp` alias is removed after the one-release window.

## Related

- [ADR 012](012-provider-network-configuration.md) — swallow owns provisioning network intent
  (refined here; its DHCP-vs-AUTO choice is superseded).
- [ADR 001](001-system-ownership-boundaries.md) — integrate, don't reinvent: use the
  provider's auto-assign rather than reimplementing address stability.
