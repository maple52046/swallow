# Server Lock

- Bounded context: Compute Resource, observed from OS Provisioning and enforced swallow-wide.
- Definition: A provider-owned protection state that prevents Swallow from changing a Server, its provisioner record, or its host until an operator explicitly unlocks it.
- Allowed meaning: `provisioning.locked=true` is an observed MAAS fact. Swallow may request Lock or Unlock, but does not persist a second lock. Locked Servers remain readable, refreshable, discoverable for monitoring, and eligible for record-only Platform Delete. Lock is available only while the Server is deployed and is refused while an Operation or Provisioning Task is active. Unlock never resumes work automatically.
- Disallowed meaning: Lock is not provisioning state, health, ownership, maintenance mode, or evidence that an exporter is absent. It must not be inferred from an action failure or used to hide the Server.
- Synonyms: Machine lock when referring specifically to the MAAS provider concept.
- Deprecated terms: None.
- Examples: "Unlock `tainan-node01` before deploying an OS." / "The Server is locked, but its metrics and events remain available."
- Related terms: Server, Server Status, Exporter Ownership, OS Deployment, Operation, Provisioning Task.
- Change note: Added when provider lock became the common mutation guard rather than a display-only provisioning attribute.
