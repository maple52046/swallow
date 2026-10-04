# OS Provisioning State

- Bounded context: OS Provisioning.
- Definition: The swallow-defined, provider-neutral lifecycle state of a Server's provider machine, carried as `provisioning.state` on the Server provisioning axis. swallow owns the value set; each OS Provisioning Provider adapter maps its own lifecycle onto it and keeps its own label only as the display-only provider state.
- Allowed meaning: The closed value set and each value's meaning:

  | Value | UI label | Meaning |
  | --- | --- | --- |
  | `new` | New | Discovered by the provider, hardware not inspected yet, so not deployable. |
  | `inspecting` | Inspecting | The provider is inventorying the machine's hardware. MAAS calls this Commissioning; Ironic calls it introspection. |
  | `ready` | Ready | In the provider's available pool and able to accept an OS Deployment. This is also where Release ends. |
  | `allocated` | Allocated | Reserved but not deployed, for example a deployment that never completed. |
  | `deploying` | Deploying | An OS Deployment is installing an operating system. |
  | `deployed` | Deployed | An operating system is installed and running. |
  | `releasing` | Releasing | The machine is being returned to the available pool, including any disk erasure. |
  | `testing` | Testing | The provider is running hardware tests. |
  | `rescue` | Rescue | The provider's diagnostic environment (see Rescue Mode). |
  | `broken` | Broken | The provider marked the machine unusable. |
  | `failed` | Failed | The last lifecycle action failed. |
  | `retired` | Retired | Withdrawn from service. |
  | `unknown` | Unknown | The adapter received a provider state it does not recognise. |

  The lowercase value is the domain term that flows through filters, API payloads, and persistence; the UI label is display only. `inspecting`, `deploying`, `releasing`, and `testing` are in-progress states: work is running and the state will change without operator action. Every provider must be able to express `releasing`, because Release is a provider-neutral action. An adapter maps several provider states onto one value when they mean the same thing (MAAS Disk erasing is `releasing`; Failed commissioning is `failed`). Swallow records when it first observed the current value (state since); that is Swallow's own observation, as precise as its observation cadence, not the provider's transition time, and it is what an in-progress state's running time counts from. Swallow's own result of its latest OS deployment (the deployment axis: `deploying | verifying | succeeded | failed | requires_attention | canceled`) is a separate value that a client may show beside or ahead of this state.
- Disallowed meaning: Not the provider's own label (`providerState`, for example MAAS "Commissioning" or "Disk erasing"), which is display only and must never be branched on. Not Swallow's deployment outcome. Not power state, lock, membership, or health. A provider-specific word must not become a value of this set; when a provider adds a lifecycle step, its adapter maps it to an existing value.
- Synonyms: Provisioning state, when the OS Provisioning context is clear.
- Deprecated terms: `commissioning` as a value — it is the MAAS name of `inspecting`. "Not deployed" as the label of an idle Server — it was a second name for `ready`; use Ready.
- Examples: "MAAS reports a machine as Commissioning; swallow stores `inspecting` with provider state `Commissioning`." / "After Release is accepted the Server moves through `releasing` to `ready`, and the Server list reads Releasing, then Ready." / "Filter the Server list with `provisioningState=releasing` to find Servers being returned to the pool."
- Related terms: Server Status, OS Provisioning Provider, Machine, Release, OS Deployment, Provider Recovery, Rescue Mode, Server Lock.
- Change note: Added 2026-10-04 by [decision 048](../../../decisions/048-os-provisioning-generic-states.md). The value set already existed inside Server Status, but it used the MAAS word `commissioning`, and the dashboard treated the whole axis as MAAS-only, hiding `releasing` behind an invented "Not deployed" label. This term makes swallow the owner of the vocabulary, renames `commissioning` to `inspecting`, and retires "Not deployed". Same day: added the recorded state-since time behind the in-progress running time.
