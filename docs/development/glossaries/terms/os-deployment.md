# OS Deployment

- Bounded context: OS Provisioning.
- Definition: An asynchronous request for a Server's OS Provisioning Provider to
  install one OS Image on the provider machine projected by that Server.
- Allowed meaning: Provider-backed intent addressed by Swallow `serverId`, with
  progress observed through the Server provisioning axis. One request may submit
  the same image, ephemeral, cloud-init, and automatic-or-static network intent
  (automatic is realized by provider auto-assign — see
  [ADR 018](../../../decisions/018-automatic-addressing-provider-auto-assign.md)) for
  several Servers managed by one Integration. The deployment mode is named by its
  [Deploy Target](deploy-target.md) (`disk` | `ram`); `ram` is the memory-backed
  mode the wire, MAAS adapter, and stored deployment still call `ephemeral`, and
  the two map one-to-one at the boundary. While the provider reports the machine as deploying, its newest
  installation event (for MAAS a stage such as *Configuring OS*) is the deployment's
  progress; a stage that stops advancing for the stall window is a stalled
  deployment that needs operator attention, even though the provider still calls it
  deploying.
- Disallowed meaning: Installing the Swallow control plane, running post-install
  automation, or a Swallow-owned job with its own execution lifecycle.
- Synonyms: Deployment, when the OS Provisioning context is unambiguous.
- Deprecated terms: Provisioning Job; `in memory` or `in-memory` when used as the
  name of the deployment mode.
- Examples: A batch request asks one MAAS Integration to deploy the same Ubuntu
  image to seven ready Servers; each Server later moves through `deploying` and
  `deployed` independently.
- Related terms: OS Image, Deploy Target, Deployment Template, Network
  Configuration, Server Status, Server Lock, Operation, Installation.
- Change note: Added to distinguish provider-owned OS work from Swallow-owned
  Operations and platform Installation; expanded when Swallow began owning the
  provider-neutral deployment network intent. Updated 2026-09-21: the memory-backed
  mode is the `ram` [Deploy Target](deploy-target.md) (its provider realization is
  still `ephemeral`), which supersedes `Ephemeral` as the sole operator-facing name
  per [decision 035](../../../decisions/035-os-image-verification-and-deploy-target.md).
  Updated 2026-10-03: provider-stage progress and stall, per
  [decision 046](../../../decisions/046-os-deployment-progress-and-stall.md).
