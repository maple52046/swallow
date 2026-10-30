# Provider Adapter

- Bounded context: swallow-wide (core and extensions).
- Definition: The replaceable implementation of a Provider Interface for one external system, translating that system's vocabulary into the core's.
- Allowed meaning: Today's adapters are MAAS (OS provisioning), Temporal (Workflow executor), Ansible (host automation), MongoDB (store), Prometheus and Alertmanager (metrics and alerts), and Redfish and libvirt (BMC and hypervisor). An adapter is an anti-corruption layer: its system's states, identifiers, defaults, errors, and lifecycle semantics are translated inside it and never cross into the core. It declares its capabilities, reports Observed State, and classifies its errors as transient, permanent, or missing capability. Its identifier (a provider kind such as `maas`) is opaque data that selects it; values already stored or on the API stay unchanged. An adapter that needs connection settings is registered per Site as an Integration; the store and the Workflow executor are chosen once per installation.
- Disallowed meaning: Not part of the core, and never a source of Desired State. Not a module: an adapter says how swallow talks to a system, a module says what swallow manages. The core never branches on an adapter's name.
- Synonyms: Adapter.
- Deprecated terms: None.
- Examples: "The MAAS adapter maps MAAS Commissioning onto `inspecting`." / "The MAAS rack's boot port belongs in the MAAS adapter, not in the Boot ISO domain."
- Related terms: Provider Interface, OS Provisioning Provider, Integration, Observed State, Extension Module, Support Matrix.
- Change note: Added 2026-10-30 for [decision 070](../../../decisions/070-core-providers-and-extensions.md); MAAS and Temporal are adapters, not part of the core.
