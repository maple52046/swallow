# Machine

- Bounded context: OS Provisioning.
- Definition: An entry in an OS Provisioning Provider's inventory — the provider's own view of a physical or virtual machine. A Server is swallow's projection of a Machine; the two are one-to-one while both exist.
- Allowed meaning: Provider-owned data identified by a provider-side id (for example a MAAS `system_id`), reaching swallow only through the reconciler. Its lifetime lasts until it is re-enrolled or removed from the provider, and its status describes provisioning readiness only. If a Machine is re-enrolled under a new provider id, the reconciler recognises the hardware and re-points the existing Server rather than creating a second one.
- Disallowed meaning: Not a Server (swallow's projection), not a Kubernetes or Slurm node, not a container or workload. The provider's id is not stable enough to be swallow's `serverId`, which is why Machine stays a distinct concept instead of collapsing into Server. Nothing reads a provider inline to serve a request.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "A Machine is re-enrolled and gets a new `system_id`; the reconciler re-links the existing Server." / "Machine data reaches swallow only through reconciliation."
- Related terms: Server, OS Provisioning Provider, Integration, Server Status.
- Change note: Migrated 2026-09-05 from the narrative `docs/glossaries/provisioning.md` (reorg D2).
