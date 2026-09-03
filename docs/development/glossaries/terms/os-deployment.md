# OS Deployment

- Bounded context: OS Provisioning.
- Definition: An asynchronous request for a Server's OS Provisioning Provider to
  install one OS Image on the provider machine projected by that Server.
- Allowed meaning: Provider-backed intent addressed by Swallow `serverId`, with
  progress observed through the Server provisioning axis. One request may submit
  the same image, ephemeral, cloud-init, and DHCP-or-static network intent for
  several Servers managed by one Integration. `Ephemeral` is the canonical
  operator-facing name when the deployed OS runs from memory and leaves disks untouched.
- Disallowed meaning: Installing the Swallow control plane, running post-install
  automation, or a Swallow-owned job with its own execution lifecycle.
- Synonyms: Deployment, when the OS Provisioning context is unambiguous.
- Deprecated terms: Provisioning Job; `in memory` or `in-memory` when used as the
  name of the deployment mode.
- Examples: A batch request asks one MAAS Integration to deploy the same Ubuntu
  image to seven ready Servers; each Server later moves through `deploying` and
  `deployed` independently.
- Related terms: OS Image, Deployment Template, Network Configuration, Server
  Status, Server Lock, Operation, Installation.
- Change note: Added to distinguish provider-owned OS work from Swallow-owned
  Operations and platform Installation; expanded when Swallow began owning the
  provider-neutral deployment network intent; standardized `Ephemeral` as the
  only operator-facing name for memory-backed deployment.
