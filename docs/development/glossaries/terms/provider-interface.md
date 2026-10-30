# Provider Interface

- Bounded context: swallow-wide (core and extensions).
- Definition: A port the core defines, in its own language, for one kind of external system it needs — OS provisioning, Workflow executor, host automation, store, metrics and alerts, BMC and hypervisor.
- Allowed meaning: A required interface plus optional capability interfaces, expressed only in core vocabulary (OS Provisioning State, Deploy Target, Machine, Node Role) and owned by the core's domain or application layer. Adapters depend on it, never the reverse. Each Provider Interface has a conformance test that every adapter must pass. Controllers change the outside world only through Provider Interfaces.
- Disallowed meaning: Not a product or a specific external system (MAAS is an adapter, not the interface). Not a place for vendor status codes, identifiers, defaults, or errors. Not an Integration — an Integration registers an adapter at a Site.
- Synonyms: Port.
- Deprecated terms: None.
- Examples: "OS provisioning is a Provider Interface; MAAS is its current adapter." / "The Workflow executor interface is implemented by Temporal today and could be implemented by another engine without changing the core."
- Related terms: Provider Adapter, OS Provisioning Provider, Integration, Convergence, Extension Module.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md), which makes the core ports and adapters.
