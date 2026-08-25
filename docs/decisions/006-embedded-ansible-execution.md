# 006. Embedded Ansible execution

- Status: Accepted
- Date: 2026-08-25

## Context

ADR 004 delegated long-running execution to AWX. The supported production installation
for AWX is Kubernetes Operator based, while Swallow's first release must run on one
Ubuntu 24.04 host, support air-gapped installation, and have both Compose and native
systemd paths. Operating AWX would therefore add an orchestration platform larger than
the control plane it serves.

There is no production AWX data requiring compatibility. Swallow already owns target
policy, stable server identity, dynamic inventory, and AES-GCM credential storage.

## Decision

Swallow owns Ansible execution. The API persists every accepted operation as `pending`
before execution. An embedded dispatcher atomically claims a MongoDB lease and runs a
manifest-listed playbook through a pinned `ansible-runner` environment. Only one run per
site may hold a lease; different sites may run concurrently.

The release owns the playbook manifest and dependency lock. Arbitrary filesystem paths
are rejected. Site credentials are encrypted in MongoDB, written to a mode `0600`
directory under `/run/swallow/jobs` only for the run, and removed afterward. SSH
host-key verification is mandatory.

Run logs and runner artifacts are retained under `/var/lib/swallow/jobs`. If a process
or host stops during a run, an expired lease becomes `indeterminate` and is not retried
automatically. The public model uses an owned `execution` reference rather than an AWX
mirror. The dynamic inventory endpoint remains available for diagnostics and external
tools; the embedded runner calls the same application use case directly.

## Alternatives considered

- Keep AWX: rejected because its production topology conflicts with the first-release
  single-host and air-gap constraints.
- Invoke arbitrary `ansible-playbook` paths: rejected because it bypasses release
  provenance and allows configuration to select unreviewed code.
- Mount the Docker socket and launch job containers: rejected because it grants the API
  host-level control and expands the attack surface.
- Add a separate worker service immediately: deferred. The lease and durable operation
  model allow this migration when HA is introduced without changing the public API.

## Consequences

Swallow now owns dispatch, leases, secret materialization, artifacts, log retention, and
the indeterminate failure state. Releases must include the pinned Execution Environment,
playbook bundle, manifest, and native offline Python environment. Upgrade tooling must
refuse while an operation is active unless an operator explicitly forces interruption.

## Current status

Implemented as the first-release installation and operation-execution baseline.
