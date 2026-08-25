# Installation

- Bounded context: Swallow product delivery and lifecycle management.
- Definition: Installing, starting, upgrading, restoring, or uninstalling the Swallow
  control plane and its bundled runtime dependencies on a supported host.
- Allowed meaning: Swallow development, testing, and production environments; container
  and native installation methods; installation configuration, media, prerequisites, and
  lifecycle tooling.
- Disallowed meaning: Asking a provisioner such as MAAS to install an operating system on
  a managed server. That action is a server deployment.
- Synonyms: None.
- Deprecated terms: `Swallow deployment`, `production deployment`, and `deployment`
  when they describe installing or running Swallow itself.
- Examples: "The production installation uses exact OCI digests." / "Testing runs the
  same Swallow installation artifacts with isolated data." / "MAAS performs a server
  deployment; it does not install Swallow."
- Related terms: Deployment, Environment, Release.
- Change note: Added to distinguish the Swallow product lifecycle from the OS deployment
  action that Swallow delegates to a provisioner.
