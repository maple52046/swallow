# Provider Interface

- Bounded context: swallow-wide (core and extensions).
- Definition: A port the core defines, in its own language, for one kind of external system it needs — OS provisioning, Workflow executor, host automation, store, metrics and alerts, BMC and hypervisor, the Local Repository, and the Directory.
- Allowed meaning: A required interface plus optional capability interfaces, expressed only in core vocabulary (OS Provisioning State, Deploy Target, Machine, Node Role, the Local Repository's formats, the Directory's users, groups, and sudo rules) and owned by the core's domain or application layer. Adapters depend on it, never the reverse. Each Provider Interface has a conformance test that every adapter must pass. Controllers change the outside world only through Provider Interfaces.
- Disallowed meaning: Not a product or a specific external system (MAAS is an adapter, not the interface). Not a place for vendor status codes, identifiers, defaults, or errors. Not an Integration — an Integration registers an adapter at a Site.
- Synonyms: Port.
- Deprecated terms: None.
- Examples: "OS provisioning is a Provider Interface; MAAS is its current adapter." / "The Workflow executor interface is implemented by Temporal today and could be implemented by another engine without changing the core." / "The Local Repository interface speaks of `deb` and `oci` content, never of Pulp or apt." / "The Directory interface speaks of users, groups, and sudo rules, never of LDAP entries."
- Related terms: Provider Adapter, OS Provisioning Provider, Integration, Convergence, Extension Module, Local Repository, Directory.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md) (core providers and extensions), which makes the core ports and adapters. On 2026-11-06 the Local Repository became a Provider Interface ([decision 078](../../../decisions/078-local-repository.md) (local repository)). On 2026-11-13 the Directory became a Provider Interface ([decision 082](../../../decisions/082-accounts-pool-access-and-directory.md) (accounts, pool access, and directory)).
