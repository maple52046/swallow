# Validation Impact

- Bounded context: Validation.
- Definition: What running a Validation Suite does to its targets beyond observing them, declared by
  the suite so swallow knows where it may run and what may interrupt it.
- Allowed meaning:
  - `observe` — reads the targets' state, or probes it so lightly and briefly that running workloads
    cannot notice (a few pings between Servers, a DNS lookup, a port check); runs anywhere a run is
    allowed, including on Servers a Platform claims.
  - `workload` — holds or saturates resources that workloads use for a noticeable time (GPU compute
    or memory, a bandwidth test, scheduler nodes); a Servers suite with this impact is refused on
    Servers a Platform claims, whose own suites run through the Platform instead.
  - `disruptive` — interrupts service (a controller failover, a reboot); a run must be confirmed when
    requested, and no Workflow preempts it.
  A run of a suite whose impact is not `disruptive` is preempted by any Workflow that needs its
  Servers. A user suite declares its impact; `workload` is its default.
- Disallowed meaning: Not a Destructive Change: a run changes no desired state and needs no Change
  Plan.
- Synonyms: Impact, when the context is clear.
- Deprecated terms: None.
- Examples: "`network-mesh` is `observe`: it sends a few pings and opens one port between the
  Servers." / "A bandwidth test is `workload`: it fills the links that jobs use." / "An HA failover
  suite is `disruptive`, so starting it asks the user to confirm."
- Related terms: Validation Suite, Validation Run, Platform Claim, Destructive Change.
- Change note: Added 2026-11-12 for [decision 081](../../../decisions/081-validation-suites-and-runs.md)
  (validation suites and runs). The same day `observe` and `workload` gained a threshold: a lab run
  showed that "consumes resources" alone made a few pings `workload` and kept the network checks
  off a Platform's members, which the user judged `observe`.
