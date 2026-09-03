# Provisioning Task

- Bounded context: OS Provisioning.
- Definition: Durable Swallow-owned coordination state for provider-backed work
  that must continue across asynchronous Server provisioning transitions.
- Allowed meaning: Tracking release network cleanup phases, attempts, lease,
  error detail, and retry while the provider remains owner of machine execution.
- Disallowed meaning: An Ansible Operation, an OS deployment job, provider event
  history, or a generic background-job framework.
- Synonyms: None.
- Deprecated terms: Provisioning Job.
- Examples: After MAAS accepts Release, a Provisioning Task waits until the
  Server is Ready and removes only the unchanged static links captured earlier.
- Related terms: IP Binding, OS Deployment, Operation, Server Status.
- Change note: Added because release cleanup must survive process restarts but
  does not execute a playbook and therefore cannot be represented as Operation.
