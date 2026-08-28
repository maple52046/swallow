# Deployment Template

- Bounded context: OS Provisioning.
- Definition: Swallow-owned, reusable OS Deployment intent scoped to exactly one
  OS Provisioning Provider Integration.
- Allowed meaning: A name, description, OS Image reference, ephemeral flag, and
  optional encrypted write-only cloud-init user data applied consistently to one
  or more eligible Servers.
- Disallowed meaning: Packages, scripts, playbooks, mutable automation content,
  a copy of an OS Image, or an execution/progress record.
- Synonyms: Deploy template.
- Deprecated terms: Provisioning Profile. That earlier concept combined an image
  with automation content and must not be revived.
- Examples: A team deploys seven Servers from one template, then selects the same
  template when adding two more Servers to that Integration.
- Related terms: OS Deployment, OS Image, Operation, Automation Configuration.
- Change note: Added after confirming that reusable provider deployment intent is
  distinct from the retired automation-owning Provisioning Profile.
