# 058. swallow owns deployment network intent; the provider adapter realizes its addressing modes

- Status: Accepted
- Date: 2026-09-02
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §6 部署網路意圖與 Subnet DNS

## Context

Deploying an OS needs each target NIC's addressing configured on the provisioner. Leaving that to
the provisioner's defaults, and to the user configuring MAAS directly, makes those defaults product
behavior, leaves released Servers inconsistent, and would make every consumer learn MAAS
vocabulary. The provisioner still owns its subnets, IP allocation, and execution APIs.

What most users ask for is "give this machine an address". On MAAS, raw DHCP serves that badly:
MAAS knows a DHCP interface's address only while it observes a live lease, so quiet deployed
machines showed no IP, and a subnet's dynamic range is by convention the enlistment and
commissioning pool. MAAS auto-assign (`AUTO`) instead allocates a static address from the subnet's
non-dynamic space, records it, and writes it into the deployed netplan.

Servers that boot through a Boot ISO ([decision 047](047-redfish-boot-media.md) (boot media and Boot
ISOs), [decision 053](053-server-enrollment-and-automatic-inspection.md) (Server Enrollment and
automatic inspection)) often sit on networks whose DHCP is the site's, not the provisioner's. There
`AUTO` allocates from space it believes is free without knowing the site DHCP pool, so the two
allocators can hand out the same address, and the site's gateway and DNS options are ignored.

## Decision

- **swallow owns deployment network intent.** The OS Provisioning context owns the addressing
  vocabulary, its default, validation, and flow. The modes are a closed, provider-neutral set:
  - `automatic` (the default): the provisioner allocates and records a stable address. It is
    realized by the provider's auto-assign (MAAS `AUTO`), not by raw DHCP.
  - `dhcp`: the network's own DHCP server assigns the address, gateway, and DNS (MAAS link mode
    `DHCP`). It is the user's explicit choice for subnets whose DHCP the provisioner does not
    serve, and is not another name for `automatic`.
  - `static`: the user chooses each Server's address (MAAS link mode `STATIC`).
- **DNS follows the link mode.** `automatic` and `static` deployments take the subnet's Subnet DNS
  servers, or the provisioner's own DNS when that list is empty; `dhcp` deployments take DNS from
  the DHCP server. Subnet DNS itself is a provisioner subnet attribute that swallow stores as
  desired state and a controller converges, as
  [decision 071](071-provisioner-realized-desired-state.md) (provisioner-realized desired state)
  decides.
- **Network inspection suggests a mode; the user decides.** An explicit static binding on the boot
  NIC suggests `static`; otherwise the suggestion is `automatic`, also on a subnet without
  provisioner DHCP. For such a subnet swallow shows a visible warning instead: `automatic` may
  hand out an address the site DHCP pool also uses, and `dhcp` lets the site DHCP server assign
  the address. The suggestion is never authoritative, and swallow never switches to `dhcp` by
  itself.
- **The provider adapter is an anticorruption layer.** Provider vocabulary (MAAS `AUTO` and
  `LINK_UP`, link ids, operation names) stays in the adapter, as
  [decision 070](070-core-providers-and-extensions.md) (core, provider adapters, and extension
  modules) requires of every adapter; a provider's own NIC states are shown only as observations and
  never accepted back as intent. The adapter never uses provider force options and verifies every
  mutation by reading the machine again. An adapter that cannot honor the intent rejects the
  deployment before its first write; it may not silently keep provider defaults.
- **Intent is applied only at explicit user actions**: an OS deployment, or a NIC link edit on a
  Ready Server. Deployed hosts are not continuously reconciled.
- **swallow keeps only the intent and the coordination it owns.** Subnets, IP allocation, and NIC
  and link identifiers stay the provisioner's, read live and revalidated before each mutation;
  swallow models no subnets, DNS zones, records, or resolvers of its own. Static-link cleanup at
  release, when requested, is a durable Provisioning Task recorded before Release and carried out by
  the release Workflow's `release-os` Task.

## Alternatives considered

- **Conform to the provisioner's defaults** (require an existing subnet link, otherwise send the
  user to MAAS): rejected — behavior would vary by provider, and MAAS `AUTO` is not DHCP.
- **Expose every MAAS link mode as a deployment mode:** rejected — provider vocabulary would become
  swallow's public domain model.
- **Realize `automatic` with raw DHCP** (the first choice for the automatic mode): rejected — the
  address is known only while a live lease is observed, which caused deployed Servers to show no
  IP, and it misuses the enlistment pool.
- **Fix the missing IP in the read path** (show the guest's self-reported address): rejected — the
  address would still be neither stable nor known to the provider.
- **Keep `automatic` only and reserve the site DHCP pool in MAAS:** rejected — the user would copy
  the site pool into MAAS by hand and keep it in step, and the machine would still get an address
  and DNS the site DHCP server knows nothing about.
- **Let `automatic` switch to DHCP by itself on subnets the provisioner does not serve DHCP for:**
  rejected by the user in favor of an explicit mode the user sees and can override.
- **Suggest `dhcp` for subnets without provisioner DHCP** (the earlier suggestion rule, which the
  Deploy OS dialog and the Platform wizard preselected): replaced on 2026-11-05 by the
  user's decision that `automatic` is always the preselected mode. The conflict with the site
  DHCP pool that the `dhcp` suggestion guarded against is now a visible warning, and choosing
  `dhcp` stays with the user.
- **Continuously reconcile every NIC:** rejected — changing a deployed host's network can interrupt
  running workloads without a user action.

## Consequences

- `automatic` gives a stable, provisioner-recorded address that swallow shows right after
  deployment. It consumes a static IP from the subnet's non-dynamic space, so the subnet needs
  static space; the dynamic range stays for enlistment and commissioning.
- On a subnet without provisioner DHCP, a deployment left on `automatic` may receive an address
  the site DHCP pool also hands out. swallow warns but does not prevent it; the user switches to
  `dhcp` when the site DHCP server should assign the address.
- With `dhcp` on a site-DHCP network the provisioner receives no lease. The address swallow shows is
  the one MAAS last observed for the interface, and SSH-based automation after deployment depends
  on it.
- Deployed hosts keep their netplan until they are redeployed; a mode or Subnet DNS change affects
  only deployments started afterwards.
- Every new provider adapter must translate the three modes or reject before writing.

## Revision history

- 2026-09-02: decision 058 made swallow the owner of deployment network intent, with the modes DHCP (default, realized as the provider's raw DHCP) and Static translated by the provider adapter.
- 2026-09-06: decision 058 renamed the default `automatic`, realized it by provider auto-assign (MAAS `AUTO`), and kept `dhcp` only as a deprecated alias.
- 2026-10-13: decision 058 made `dhcp` a separate mode again for networks whose DHCP the provisioner does not serve, and had swallow write Subnet DNS through to the provisioner.
- 2026-11-02: merged decision 058 into this ADR; the body states the current decision.
- 2026-11-05: network inspection no longer suggests `dhcp` for a subnet without provisioner DHCP; `automatic` stays the suggestion and clients warn about the site DHCP pool, per the user's decision that `automatic` is always preselected.
