# Network Configuration

- Bounded context: OS Provisioning.
- Definition: Swallow's typed view and explicit configuration intent for the
  subnet links and IP addressing of one Server network interface.
- Allowed meaning: A NIC can be configured for DHCP, a static IP, link-only
  subnet attachment, no subnet link, or a provider-managed legacy state. Its
  physical carrier state is observed separately when a provider supplies it.
- Disallowed meaning: VLAN, bond, bridge, DNS, route, or complete network/IPAM
  lifecycle ownership; interpreting a provider's `LINK_UP` mode as proof of a
  physical carrier signal.
- Synonyms: NIC configuration, when the OS Provisioning context is clear.
- Deprecated terms: Network mode when used without distinguishing deployment
  intent from observed NIC configuration.
- Examples: An operator binds a static address to a Ready Server's boot NIC; an
  existing MAAS AUTO link is displayed as provider-managed rather than offered
  as a Swallow deployment choice.
- Related terms: IP Binding, OS Deployment, Server Status.
- Change note: Added when Swallow began owning provider-neutral deployment
  networking rather than requiring operators to configure MAAS out of band.
