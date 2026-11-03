# Software Assignment

- Bounded context: Software Deployment (swallow-owned intent and execution).
- Definition: A swallow-owned durable record of the desired and last-applied state of one
  Managed Software kind on one Server, keyed by `(serverId, kind)`. It is the single source of
  truth for what software swallow has installed where, and it drives listing,
  duplicate-prevention, and uninstall.
- Allowed meaning: One record per Server per software kind, carrying the kind, any variant
  roles (NFS `server`/`client`), a kind-specific `spec` (for example an NFS export path and
  client allow-list, a client mount URL, or Docker CE's explicit `enableApi` boolean — absent
  only on records written before that variant existed, which read as disabled), a `state`
  (`pending` | `installed` | `failed` | `uninstalling` | `absent`), the Workflow that last
  wrote it, and the last successful apply time. It is swallow-owned intent plus a last-applied
  fact, updated by the install/uninstall Workflow's internal record step — including the record
  step of a software Job composed into a platform deployment (Prometheus and Grafana of a Slurm
  deploy with Omnistat, or the Device Metrics Exporter on its GPU compute nodes) and of the Node
  Exporter that every OS deployment installs on the Servers it provisions. The Node Exporter's
  Controller reads its assignments as observed state and starts installs that write them like any
  other. A Slurm
  Platform's successful uninstall marks its Servers' `device-metrics-exporter` assignments
  `absent`, because the uninstall removes that exporter from the hosts.
- Disallowed meaning: Not a fourth Server status axis — the three axes
  (`provisioning`/`membership`/`health`) are unchanged, and a Software Assignment is neither
  provisioning state nor membership nor health. Not a live probe of the host's packages: it is
  the last-applied intent, not a continuous package scan. Not a Platform membership record. It
  must not outlive the software on disk: a Workflow Task that takes a Server's OS away (release,
  recover, or returning a Server borrowed by OS image verification) marks its assignments
  `absent` when it succeeds, and reconcile does the same when it observes the machine without an
  OS (`ready` or `allocated`) after a removal outside swallow. A Server in `rescue` or `broken`
  keeps its assignments, because its OS is still on disk.
- Synonyms: None.
- Deprecated terms: None.
- Examples: "The Server detail lists two Software Assignments: `docker-ce` installed and `nfs`
  installed as a client." / "Releasing a Server marks its `installed` assignments `absent`, so
  the list never claims software the wiped disk no longer has." / "A second install request for
  an already-`installed`, mutually-exclusive kind is refused before a Workflow is created."
- Related terms: Managed Software, Docker Host Explorer, Server, Server Status, Workflow, Release.
- Change note: Added 2026-09-24 ([decision 038](../../../decisions/038-software-deployment.md) (software deployment))
  as the swallow-owned record backing single-software deployment, deliberately distinct from
  Platform membership and from the three Server status axes. 2026-10-02
  ([decision 043](../../../decisions/043-docker-host-management.md) (docker host management)): an installed `docker-ce`
  assignment with `enableApi` is the only eligibility source for the Docker Host Explorer.
  2026-10-21 ([decision 063](../../../decisions/063-omnistat-slurm-observability.md) (omnistat slurm observability)): composed
  software Jobs record assignments too. 2026-10-30
  ([decision 068](../../../decisions/068-device-metrics-exporter-managed-software.md) (device metrics exporter managed software)): the
  Device Metrics Exporter of a Slurm Platform has assignments, cleared by the Platform's uninstall.
  2026-11-03: every OS deployment records a `node-exporter` assignment, and a Server's
  assignments are cleared by the Task that removed its OS or by reconcile rather than by a
  periodic pass over Servers that left `deployed`, so a Server in `rescue` or `broken` keeps them.
  2026-11-04: the Node Exporter's Controller observes its assignments and installs where one is
  missing, `absent`, or `failed`; the record itself is unchanged.
