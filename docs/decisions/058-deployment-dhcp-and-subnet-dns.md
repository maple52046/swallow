# 058. Deployments can take the site's DHCP, and Subnet DNS is written through

- Status: Accepted; Superseded in part by [071](071-provisioner-realized-desired-state.md) (swallow stores Subnet DNS as desired state and corrects drift instead of writing it through)
- Date: 2026-10-13
- 現行規格：[os-provisioning.md](../development/os-provisioning.md) §6 部署網路意圖與 Subnet DNS

## Context

[ADR 018](018-automatic-addressing-provider-auto-assign.md) made `automatic` the only automatic
deployment addressing intent, realized by MAAS `AUTO`, and turned `dhcp` into a deprecated alias
of it. It rejected keeping both as first-class modes because, on a MAAS-managed network, they
mean the same thing and raw DHCP is less reliable.

Servers that boot through a Boot ISO ([ADR 049](049-boot-iso-builder.md),
[ADR 053](053-server-enrollment-and-automatic-inspection.md)) sit on networks whose DHCP is the
site's, not MAAS's. MAAS still models such a subnet and `AUTO` allocates from the space it
believes is free, without knowing the site DHCP server's pool. On `lab-afde-mi308-1` (subnet
`10.170.168.0/23`, MAAS DHCP off on its VLAN) `AUTO` gave `10.170.169.190` while the site DHCP had
leased `.126` and `.127` on the same subnet: the two allocators can hand out the same address,
and the site's DHCP options (gateway, DNS) are ignored.

DNS follows the link mode. For an `AUTO` or static link MAAS writes nameservers from the subnet's
DNS servers (after the VLAN rack's addresses when the subnet allows MAAS DNS) and falls back to
its own DNS when that list is empty; a DHCP link gets none from MAAS and uses what the DHCP
server offers. Setting a subnet's DNS servers meant leaving swallow for the MAAS UI or CLI, and
[ADR 001](001-system-ownership-boundaries.md) lists DNS and subnets as provisioner-owned facts
that swallow does not model.

## Decision

- **`dhcp` is a deployment addressing mode again.** The modes are `automatic` (default),
  `dhcp`, and `static`. `automatic` keeps ADR 018's meaning — the provisioner allocates and
  records a stable address. `dhcp` means the network's own DHCP server assigns the address,
  gateway, and DNS; the MAAS adapter links the NIC in MAAS `DHCP` mode. It is the right choice
  on a subnet whose DHCP the provisioner does not serve, and the wrong one where it does, so the
  two are no longer the same request.
- **Network inspection suggests it.** When the boot NIC has no static binding and the subnet it
  would use has no provisioner DHCP, the suggested mode is `dhcp`. The suggestion stays
  non-authoritative; the operator can pick any mode.
- **The alias ends.** `dhcp` no longer normalizes to `automatic`. Deployment Templates are
  normalized on write since ADR 018, so only a template stored before 2026-09-06 can still say
  `dhcp`; it now means DHCP.
- **Subnet DNS is written through.** A provisioner Integration's subnets and their DNS servers
  are read live and the DNS servers written through, behind a provider capability, the way
  [ADR 056](056-provisioner-http-proxy.md) writes the HTTP proxy. swallow stores nothing. The
  list applies to `automatic` and `static` deployments on that subnet; `dhcp` deployments take
  DNS from the DHCP server.
- **ADR 001 still holds.** swallow models no DNS zones, records, resolvers, or subnets of its
  own; it edits one attribute the provisioner owns, through the provisioner.

This partly supersedes ADR 018 (its `dhcp` alias and its rejection of a separate DHCP mode).

## Alternatives considered

- **Keep `automatic` only and reserve the site DHCP pool in MAAS:** rejected. The operator has to
  copy the site pool into MAAS by hand and keep it in step, and the machine still ends up with an
  address and DNS the site DHCP server knows nothing about.
- **Let `automatic` choose DHCP by itself on subnets the provisioner does not serve DHCP for:**
  rejected by the user in favor of an explicit mode the operator sees and can override.
- **Set DNS per deployment through swallow-generated cloud-init:** rejected by the user in favor
  of the provisioner's own per-subnet setting. swallow composes no cloud-init of its own today,
  and a resolver written beside MAAS's netplan would be a second source of DNS on the host.
- **Store DNS servers in the Integration's `settings`:** rejected for the reason ADR 056 gives —
  a copy that drifts when the provisioner is changed directly.

## Consequences

- The published deployment network mode set gains `dhcp`, and Subnet DNS becomes a new
  provisioner-owned term and contract.
- With site DHCP, MAAS receives no lease for a deployed machine. The address swallow shows is
  the one MAAS last observed for the interface, and SSH-based automation after deployment
  depends on it.
- The MAAS account behind the Integration must be an administrator to edit a subnet.
- A DNS change affects only deployments the provisioner starts afterwards; deployed hosts keep
  their netplan.
