# Managed Software

- Bounded context: Software Deployment (swallow-owned intent and execution).
- Definition: A single piece of host software, together with its allowed variants, that
  Swallow can install on and uninstall from one or more already-deployed Servers through the
  Workflow/Job/Task/Runner model. The deploy target is one software (for example Docker CE,
  Podman, or NFS), not a runtime composed of several components.
- Allowed meaning: A named software kind such as `docker-ce`, `podman`, or `nfs`, each with
  kind-specific variants — for NFS the `server` and `client` roles are variants of the same
  software; for a container runtime an optional pinned version is a variant. A Managed
  Software install is a convergent Workflow (`configure-<kind>`) whose remote host work is one
  idempotent playbook; uninstall is the symmetric `uninstall-<kind>`. The same software kind
  can also be composed as a Job into a platform deployment (for example a future Slurm or
  Kubernetes deploy composing `configure-nfs`), sharing the same Ansible roles. Docker CE and
  Podman are distinct kinds and are mutually exclusive on one Server.
- Disallowed meaning: Not a Platform — the target is a single software, not a multi-component
  runtime, and Managed Software has no cluster membership, no platform credential, and no
  whole-batch target claim. Not defined by whether OS provisioning runs, whether a batch of
  Servers is claimed, or whether membership exists; those are consequences, not the
  definition. Not the Swallow control-plane Installation. NFS `server` and `client` are
  variants of one software, not two platforms; two software kinds installed on one Server are
  still two Managed Software records, not a platform.
- Synonyms: Software deployment (the act of installing a Managed Software).
- Deprecated terms: None.
- Examples: "Installing Docker CE on three deployed Servers is one Managed Software
  Workflow." / "An NFS install can make one Server a `server` and others `client` in a single
  Workflow; each Server still gets its own Software Assignment." / "A future Slurm deploy
  composes the `nfs` software as a Job rather than duplicating its playbook."
- Related terms: Software Assignment, Platform, Workflow, Job, Task, Runner, Server.
- Change note: Added 2026-09-24 ([decision 038](../../../decisions/038-software-deployment.md))
  to name single-software deployment as distinct from platform deployment, with the deploy
  target's granularity as the authoritative boundary.
