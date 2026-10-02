# Software Assignment

- Bounded context: Software Deployment (swallow-owned intent and execution).
- Definition: A swallow-owned durable record of the desired and last-applied state of one
  Managed Software kind on one Server, keyed by `(serverId, kind)`. It is the single source of
  truth for what software Swallow has installed where, and it drives listing,
  duplicate-prevention, and uninstall.
- Allowed meaning: One record per Server per software kind, carrying the kind, any variant
  roles (NFS `server`/`client`), a kind-specific `spec` (for example an NFS export path and
  client allow-list, a client mount URL, or Docker CE's explicit `enableApi` boolean — absent
  only on records written before that variant existed, which read as disabled), a `state`
  (`pending` | `installed` | `failed` | `uninstalling` | `absent`), the Workflow that last
  wrote it, and the last successful apply time. It is swallow-owned intent plus a last-applied
  fact, updated by the install/uninstall Workflow's internal record step.
- Disallowed meaning: Not a fourth Server status axis — the three axes
  (`provisioning`/`membership`/`health`) are unchanged, and a Software Assignment is neither
  provisioning state nor membership nor health. Not a live probe of the host's packages: it is
  the last-applied intent, not a continuous package scan. Not a Platform membership record. It
  must not outlive the software on disk: when a Server leaves `deployed` (release, recover,
  reinstall) its assignments become `absent`.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The Server detail lists two Software Assignments: `docker-ce` installed and `nfs`
  installed as a client." / "Releasing a Server marks its `installed` assignments `absent`, so
  the list never claims software the wiped disk no longer has." / "A second install request for
  an already-`installed`, mutually-exclusive kind is refused before a Workflow is created."
- Related terms: Managed Software, Docker Host Explorer, Server, Server Status, Workflow, Release.
- Change note: Added 2026-09-24 ([decision 038](../../../decisions/038-software-deployment.md))
  as the swallow-owned record backing single-software deployment, deliberately distinct from
  Platform membership and from the three Server status axes. 2026-10-02
  ([decision 043](../../../decisions/043-docker-host-management.md)): an installed `docker-ce`
  assignment with `enableApi` is the only eligibility source for the Docker Host Explorer.
