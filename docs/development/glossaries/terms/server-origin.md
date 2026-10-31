# Server Origin

- Bounded context: swallow-wide (Compute Resource).
- Definition: How a Server arrived in swallow: `discovered` (its provisioner enlisted the machine, for example by PXE enlistment in MAAS, and reconciliation found it), `enrolled` (swallow registered the machine with its provisioner, such as a virtual machine added through swallow), or `imported` (the machine was registered with an operating system already installed).
- Allowed meaning: A fact recorded when the Server is first stored and kept afterwards; reconciliation never rewrites it. It is display and audit information only. Whatever the origin, the Server's first observation is adopted as its spec and swallow is authoritative from then on.
- Disallowed meaning: Not an ownership mode (no origin makes the provisioner authoritative), not the provisioning state, and not Server Enrollment itself (one way a Server can get the `enrolled` origin).
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The large site enlists racks by PXE, so its Servers are `discovered`; the extra node added from the dashboard is `enrolled`."
- Related terms: Server, Server Enrollment, Controller, Desired State.
- Change note: Added 2026-10-31 for [decision 071](../../../decisions/071-provisioner-realized-desired-state.md). A Server stored before then reads as `discovered`.
