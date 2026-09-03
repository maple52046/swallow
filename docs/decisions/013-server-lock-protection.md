# 013. Provider-owned Server lock is a global mutation guard

- Status: Accepted
- Date: 2026-09-03

## Context

MAAS owns Machine lock state and already exposes it through the Server
provisioning projection. Swallow previously treated that value mostly as
diagnostic data: some provisioning paths checked it, other Server and automation
paths did not, and the Dashboard did not consistently expose it. An Ansible
Operation can also bypass MAAS mutation checks entirely.

## Decision

MAAS remains the source of truth and executor for Lock and Unlock. Swallow treats
the observed lock as a platform-wide guard against every operator or worker path
that can mutate the Server, its provider record, or the host. Application use
cases use a shared policy instead of reproducing provider-state comparisons.

Automation validates target lock state when work is accepted and again after a
pending Operation is claimed but before the runner starts. A running Operation
is not interrupted if a provider lock appears later. Provisioning Tasks check
again immediately before changing network links and fail retryably if locked.

Lock is available only for a deployed Server, matching the provider
capability; every other lifecycle state is refused before a provider write. Lock
is also refused while an Operation or Provisioning Task is active. Unlock does
not resume or create work. Cluster Delete remains
available because it removes Swallow records only. Monitoring and other reads
remain available, and a locked Server has `unmanaged` exporter ownership so
Swallow never changes its exporter installation.

## Alternatives considered

- Rely only on MAAS rejection: rejected because Ansible changes hosts without
  going through MAAS and provider error messages arrive too late.
- Persist a second Swallow lock: rejected because two independently mutable
  protection states would drift and require reconciliation semantics.
- Cancel running work when a lock appears: rejected because interruption can
  leave an externally executed operation in an indeterminate partial state.
- Remove locked Servers from monitoring discovery: rejected because Lock is a
  mutation policy, not a health or visibility state.

## Consequences

Provider lock availability is required before new mutation work is accepted;
an unverifiable state fails closed. Operators can still diagnose locked Servers,
and pending work cannot slip through after an external lock. Provider adapters
remain the anticorruption boundary for lock execution and live observation.

## Current status

Implemented by the Server Lock Protection workflow on 2026-09-03.
