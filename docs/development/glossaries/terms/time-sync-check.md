# Time Sync Check

- Bounded context: Platform Management.
- Definition: How a Platform deployment treats a target whose clock is not in sync, decided per
  deployment. Before installing the Platform, the deployment examines every target. A clock is in
  sync when the host reports it synchronized and, when the Installation provides a Control-Plane
  NTP Server, it is within one second of that server.
- Allowed meaning: One of the values below, chosen when a Platform is deployed. Each Platform type
  performs the check itself, as the first part of installing that Platform; Kubernetes and Slurm
  both do.
- Disallowed meaning: Not the configuration of time on a Server: that happens once, when the OS is
  provisioned. A Time Sync Check never changes a host. Not a step of its own beside the Platform's
  installation, not a continuous health signal, and not part of Server Status.
- Synonyms: Clock check.
- Deprecated terms: None.
- Values:
  - `enforce`: A target that is not in sync stops the deployment before the Platform is installed.
    This is the default. The dashboard switch "Require synchronized clocks" is on.
  - `warn`: A target that is not in sync is reported, and the deployment continues. The dashboard
    switch is off.
- Examples: "A Slurm deployment stopped at the Time Sync Check because one node's clock was not in
  sync, so MUNGE would have rejected its credentials." / "The lab deploy used `warn` because the
  nodes had no reachable NTP server yet."
- Related terms: Platform, Control-Plane NTP Server, OS Deployment, Workflow.
- Change note: Added 2026-10-28 with decision 065, when Platform deployments stopped configuring
  time and only check it. Revised 2026-10-29: the check moved from a step shared by every Platform
  into each Platform's own installation.
