# Deployment Key

- Bounded context: Access.
- Definition: The single SSH Key with purpose `deployment`: a system-owned key pair whose
  private key swallow holds, sealed at rest, and uses to log in to managed Servers for SSH
  readiness and Ansible execution.
- Allowed meaning: Exactly one per installation, owned by swallow rather than by any
  User. The installation creates it as ed25519 (`swallow-api deployment-key ensure`,
  run by `swallowctl install` and `upgrade` after `migrate`), so every installation has one
  without manual steps; the API process neither creates it nor needs it to start. OS and
  Platform deployment require it and are refused while it is missing. An operator may regenerate it or replace it
  with an existing unencrypted private key; it can never be deleted. Its private key is
  write-only: no API returns it. It is the default automation credential; a Site
  Automation Configuration's private key, when set, overrides it for that Site. Its public
  key is realized into provisioners exactly like every other SSH Key.
- Disallowed meaning: Not an Access Key and not a person's key. Not a per-Site credential.
  Not a rotation mechanism: replacing it does not re-authorize already-deployed Servers,
  which keep only the public key present when they were deployed.
- Synonyms: None.
- Deprecated terms: Automation key (when it means the default credential rather than a
  Site override).
- Examples: "`swallowctl install` creates the Deployment Key right after `migrate` and
  registers its public key in MAAS, so the first Kubernetes deploy passes SSH readiness
  with no Site credential." / "Regenerating the Deployment Key warns that Servers deployed
  earlier authorize only the previous key."
- Related terms: SSH Key, Automation Configuration, OS Deployment, Workflow, Runner,
  Installation.
- Change note: Added for SSH key management
  ([decision 039](../../../decisions/039-ssh-key-management-and-default-user.md)). Updated
  2026-10-01: created by the installation step instead of at API start, and required by OS
  and Platform deployment.
