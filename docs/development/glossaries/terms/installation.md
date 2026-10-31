# Installation

- Bounded context: Swallow product delivery and lifecycle management.
- Definition: Installing, starting, upgrading, restoring, or uninstalling the Swallow
  control plane and its bundled runtime dependencies on a supported host.
- Allowed meaning: Swallow development, testing, and production environments; the
  container installation (native installation methods are not published yet); installation
  configuration, media, prerequisites, and
  lifecycle tooling. The production installation is one host (single VM): besides the
  control plane it installs and initializes the co-located MAAS region+rack, synchronizes the
  official `ubuntu/noble` OS Image into it, and registers that MAAS as the Site's provisioner
  Integration through the published API. "Production" names the installation contract
  (digest-pinned images, generated secrets, lifecycle tooling), not a multi-host topology.
- Disallowed meaning: Asking a provisioner such as MAAS to install an operating system on
  a managed server. That action is a server deployment. Synchronizing an OS Image into MAAS
  during installation is not a deployment either: no managed server changes.
- Synonyms: None.
- Deprecated terms: `Swallow deployment`, `production deployment`, and `deployment`
  when they describe installing or running Swallow itself.
- Examples: "The production installation uses exact OCI digests." / "Testing runs the
  same Swallow installation artifacts with isolated data." / "MAAS performs a server
  deployment; it does not install Swallow." / "The installation finished once the
  co-located MAAS reported `ubuntu/noble` complete; no Server was deployed."
- Related terms: Deployment, Environment, Integration, OS Image. A swallow release (the
  published version and its artifacts) is not the provisioner [Release](release.md) action.
- Change note: Added to distinguish the Swallow product lifecycle from the OS deployment
  action that Swallow delegates to a provisioner. Updated 2026-10-01 for the single-VM
  production installation that co-locates and bootstraps MAAS, per decision 040. Corrected
  2026-10-31: native installation methods are not published, and a swallow release is not the
  provider Release.
