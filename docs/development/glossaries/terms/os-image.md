# OS Image

- Bounded context: OS Provisioning.
- Definition: An operating system artifact that an OS Provisioning Provider can
  currently install on a managed machine.
- Allowed meaning: Live provider-owned catalog data identified by the opaque
  value the same provider accepts on an OS Deployment request. Swallow may show
  and select the value but does not own, mirror, upload, delete, or synchronize
  the artifact.
- Disallowed meaning: A Swallow release, container image, package bundle,
  post-install script collection, or durable Swallow-owned image record.
- Synonyms: Provisioning image (deprecated UI wording).
- Deprecated terms: Provisioning Image.
- Examples: An operator selects the MAAS image `ubuntu/jammy` for Servers managed
  by that MAAS Integration. Another Integration may expose a different catalog.
- Related terms: OS Deployment, Deployment Template, Server.
- Change note: Added to make the provider ownership and integration scope used by
  the OS provisioning API explicit.
