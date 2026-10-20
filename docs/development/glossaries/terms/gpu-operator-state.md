# GPU Operator State

- Bounded context: Platform Management, owned by `api-server`; displayed by `dashboard` and
  `cli`.
- Definition: Where the latest Swallow change to a Platform's GPU Operator stands, projected from
  the Workflow Task that applies it.
- Allowed meaning: The values are:

  | Value | Meaning |
  | --- | --- |
  | `installing` | an install or a settings change is accepted or running (including while the deployment that carries it is still provisioning) |
  | `ready` | the last install or change succeeded: the operator reports Ready and every managed node exposes its GPUs |
  | `failed` | the last install, change, or uninstall failed or was canceled; the cluster may still run the previous settings |
  | `uninstalling` | an uninstall is accepted or running |
  | `absent` | the last uninstall succeeded; Swallow manages no GPU Operator on the Platform |

  Only the Workflow recorded as the latest change may move the state, so an older Workflow
  finishing late never overwrites a newer one.
- Disallowed meaning: Not Platform Lifecycle State (the Platform stays `active` while its GPU
  Operator is uninstalled) and not a live health read of the operator.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The deployment is still installing the OS, so the GPU Operator State is
  `installing`." / "The switch to DRA was refused because a Pod still used a GPU, so the state is
  `failed` and the cluster still runs the Device Plugin."
- Related terms: GPU Operator, GPU Allocation Mode, Platform Lifecycle State, Workflow, Task.
- Change note: Added with the Swallow-managed AMD GPU Operator (decision 062).
