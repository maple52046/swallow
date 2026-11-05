# Network Configuration

- Bounded context: OS Provisioning.
- Definition: swallow's typed view and explicit configuration intent for the
  subnet links and IP addressing of one Server network interface.
- Allowed meaning: A NIC can be configured for DHCP, a static IP, link-only
  subnet attachment, no subnet link, or a provider-managed legacy state. Its
  physical carrier state is observed separately when a provider supplies it.
  Deployment addressing intent is a distinct, smaller closed vocabulary:

  | Mode (domain value) | UI label | Meaning |
  | --- | --- | --- |
  | `automatic` | Automatic | The provisioner allocates and records a stable address (MAAS auto-assign), with DNS from the subnet's Subnet DNS or the provisioner's own DNS. The default, also on a subnet whose DHCP is not the provisioner's, where the address may conflict with the site DHCP pool. |
  | `dhcp` | DHCP | The network's own DHCP server assigns the address, gateway, and DNS; the provisioner records only what it observes. Chosen by the user for a subnet whose DHCP is not the provisioner's; never suggested. |
  | `static` | Static | A caller-chosen address per Server, with DNS from the subnet's Subnet DNS or the provisioner's own DNS. |

  See [decision 058](../../../decisions/058-deployment-dhcp-and-subnet-dns.md) (deployment dhcp and subnet dns)
  and decision 058.
- Disallowed meaning: VLAN, bond, bridge, DNS, route, or complete network/IPAM
  lifecycle ownership (a subnet's DNS servers are the provisioner's Subnet DNS,
  not part of a NIC's configuration); interpreting a provider's `LINK_UP` mode
  as proof of a physical carrier signal; treating `dhcp` as another name for
  `automatic`.
- Synonyms: NIC configuration, when the OS Provisioning context is clear.
- Deprecated terms: Network mode when used without distinguishing deployment
  intent from observed NIC configuration.
- Examples: A user binds a static address to a Ready Server's boot NIC; a
  deployment with `automatic` addressing produces a provider auto-assigned,
  recorded IP; a user deploying a Server booted through a Boot ISO on a
  site-DHCP network sees a warning about the site DHCP pool, switches from
  `automatic` to `dhcp`, and the Server keeps the address the site DHCP server
  leases it; an
  existing MAAS AUTO link observed on an already-deployed NIC is displayed as
  provider-managed rather than offered as a manual NIC choice.
- Related terms: IP Binding, OS Deployment, Server Status, Subnet DNS.
- Change note: Added when swallow began owning provider-neutral deployment
  networking rather than requiring users to configure MAAS out of band.
  Updated 2026-10-13: `dhcp` is a deployment mode again beside `automatic` and
  `static`, no longer an alias of `automatic`, per
  decision 058. Updated 2026-11-05: `automatic` stays the default and the
  suggestion on a subnet whose DHCP is not the provisioner's, and `dhcp` there is
  the user's choice after a warning, per decision 058.
