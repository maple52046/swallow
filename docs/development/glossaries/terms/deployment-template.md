# Deployment Template

- Bounded context: OS Provisioning.
- Definition: swallow-owned, reusable OS Deployment intent scoped to exactly one
  OS Provisioning Provider Integration.
- Allowed meaning: A name, description, OS Image reference, [Deploy Target](deploy-target.md)
  (`disk` | `ram`; stored as the `ephemeral` flag), DHCP-or-static network intent,
  and optional encrypted write-only cloud-init user data applied consistently to
  one or more eligible Servers. A static IP and provider interface identity are
  selected per deployment target and are never stored by the template. It may
  carry an optional minimum hardware floor (CPU cores, memory, storage, GPU model,
  GPU count; each optional) that a Server joining a Node Group installed from the
  template must meet. A Platform's Node Group installs its new members from one
  template; a template a Platform generates from its machine preparation
  (`<platform>-<group>`) is an ordinary template once created. A template is owned by
  one Pool: the Pool's members read it and deploy with it, its owners change and
  delete it, and only an admin moves it to another Pool. It records its creator,
  which never decides access and is unchanged when that User leaves the Pool. A
  template a Platform generates is owned by the Platform's Pool.
- Disallowed meaning: A User's personal template: the creator owns nothing. Packages, scripts, playbooks, mutable automation content,
  a copy of an OS Image, or an execution/progress record. Not a hardware
  specification or a role: the floor is an eligibility check, the same template
  installs machines with different CPUs and memory, and roles, partitions, and
  ROCm settings belong to the Platform.
- Synonyms: Deploy template.
- Deprecated terms: Provisioning Profile. That earlier concept combined an image
  with automation content and must not be revived.
- Examples: A team deploys seven Servers from one template, then selects the same
  template when adding two more Servers to that Integration. A Slurm Platform's
  `mi300x` Node Group installs from a template whose floor is eight MI300X GPUs, so
  a spec write adding a seven-GPU Server is refused naming the shortfall.
- Related terms: OS Deployment, OS Image, Deploy Target, Network Configuration,
  Operation, Automation Configuration, Node Group, Minimum Resource Requirement.
- Change note: Added after confirming that reusable provider deployment intent is
  distinct from the retired automation-owning Provisioning Profile; expanded to
  carry provider-neutral network intent without target-specific IP assignments.
  Updated 2026-09-21: the deployment mode is stated as a [Deploy Target](deploy-target.md)
  (the stored `ephemeral` flag is its boundary form) per
  [decision 035](../../../decisions/035-os-image-verification-and-deploy-target.md) (os image verification and deploy target).
  Updated 2026-11-14: the optional minimum hardware floor, use by Platform Node
  Groups (a referenced template cannot be deleted), and templates a Platform
  generates, per [decision 083](../../../decisions/083-platform-member-scaling.md)
  (Platform member scaling); the floor is distinct from a Platform type's Minimum
  Resource Requirement, which still applies on its own.
  Updated 2026-11-16: a template is owned by a Pool and records its creator, per
  [decision 009](../../../decisions/009-deployment-template-ownership.md) (deployment
  template ownership) and [decision 082](../../../decisions/082-accounts-pool-access-and-directory.md)
  (accounts, pool access, and directory); names are unique within an Integration and Pool.
