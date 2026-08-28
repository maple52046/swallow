# OS Deployment

- Bounded context: OS Provisioning.
- Definition: An asynchronous request for a Server's OS Provisioning Provider to
  install one OS Image on the provider machine projected by that Server.
- Allowed meaning: Provider-backed intent addressed by Swallow `serverId`, with
  progress observed through the Server provisioning axis. One request may submit
  the same settings for several Servers managed by one Integration.
- Disallowed meaning: Installing the Swallow control plane, running post-install
  automation, or a Swallow-owned job with its own execution lifecycle.
- Synonyms: Deployment, when the OS Provisioning context is unambiguous.
- Deprecated terms: Provisioning Job.
- Examples: A batch request asks one MAAS Integration to deploy the same Ubuntu
  image to seven ready Servers; each Server later moves through `deploying` and
  `deployed` independently.
- Related terms: OS Image, Deployment Template, Server Status, Operation,
  Installation.
- Change note: Added to distinguish provider-owned OS work from Swallow-owned
  Operations and platform Installation.
