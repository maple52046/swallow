# Provisioning Task

- Bounded context: OS Provisioning.
- Definition: Durable Swallow-owned coordination state for provider-backed work
  that must continue across asynchronous Server provisioning transitions.
- Allowed meaning: Tracking release network cleanup phases, attempts, error
  detail, and retry. The `release-os` Workflow Task that released the Server
  advances it under that Workflow's resource lease, while the provider remains
  owner of machine execution.
- Disallowed meaning: An Ansible Operation, an OS deployment job, provider event
  history, a generic background-job framework, or work with a lease or queue of
  its own.
- Synonyms: None.
- Deprecated terms: Provisioning Job.
- Examples: After MAAS accepts Release, the release Task waits until the Server
  is Ready and removes only the unchanged static links its Provisioning Task
  captured earlier.
- Related terms: IP Binding, OS Deployment, Workflow, Server Status.
- Change note: Added because release cleanup must survive process restarts but
  does not execute a playbook and therefore cannot be represented as Operation.
  2026-11-01: the background queue that advanced it (its own Mongo lease, in the
  api and worker processes) was removed; the release Task runs it, so every
  Server mutation goes through a Workflow (AR-23).
