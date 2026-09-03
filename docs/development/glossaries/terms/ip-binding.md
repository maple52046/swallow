# IP Binding

- Bounded context: OS Provisioning.
- Definition: An explicit static IP address associated with one subnet link on
  one Server network interface through its OS Provisioning Provider.
- Allowed meaning: Creating, replacing, displaying, or removing a static link
  while the Server is Ready, including opt-in cleanup after release.
- Disallowed meaning: A DHCP lease, a provider-managed automatic allocation, a
  fleet-wide reservation system, or ownership of the provider's complete IPAM.
- Synonyms: Static IP binding.
- Deprecated terms: None.
- Examples: `192.168.100.20` is bound to the boot NIC for the selected subnet;
  release cleanup removes that unchanged binding after the Server returns Ready.
- Related terms: Network Configuration, OS Deployment, Provisioning Task.
- Change note: Added to make static addressing and release-time removal explicit
  Swallow actions without claiming broader IPAM ownership.
