# 065. The installation serves NTP, and every provisioned Server is pointed at it after SSH

- Status: Accepted
- Date: 2026-10-28 (revised 2026-10-28 and 2026-10-29)
- 現行規格：[installation.md](../development/installation.md) §6 時間同步

## Context

Every runtime swallow deploys depends on agreeing clocks: MUNGE rejects credentials across
hosts whose clocks disagree, etcd and TLS reject certificates outside their validity window, and
correlated logs and metrics are useless when hosts drift. Until now swallow did nothing about
time. The Slurm preflight only warned when `timedatectl` did not report a synchronized clock, and
whether a host synchronized at all depended on its image and network.

Two facts about the existing system shape the answer:

- The production installation co-locates MAAS 3.6 region+rack on the swallow host
  ([ADR 040](040-single-vm-production-installation.md)). The MAAS snap runs its own `chronyd`
  on host UDP 123 with `allow` for every client and `local stratum 8 orphan`. It synchronizes from
  the MAAS setting `ntp_servers` (default `ntp.ubuntu.com`), and unless `ntp_external_only` is set,
  MAAS hands the machines it deploys its rack controller as their NTP server through cloud-init.
  On a network that cannot reach `ntp.ubuntu.com` (the `root123-intel` lab reaches time only
  through its chronos NTP-over-HTTP gateway), that chronyd falls back to its local clock and serves
  unsynchronized time.
- Hosts that keep their OS (`existing_os`, [ADR 053](053-server-enrollment-and-automatic-inspection.md))
  never pass through MAAS cloud-init, and swallow composes no cloud-init of its own
  ([ADR 058](058-deployment-dhcp-and-subnet-dns.md)).

While this was implemented, `root123-intel` showed that a large first correction is dangerous
when the upstream derives its served time from the swallow host's own clock. The first chronos
gateway ran on the swallow host as a `chronyd -x` that computes time from the host clock. The host
started 33.5 s behind; MAAS's chronyd slewed at +8.3% (its configuration has no `makestep`), the
gateway could not follow a rate change that large and kept serving host + 33.5 s, and the host ran
past real time until the upstream was reverted, the kernel rate reset, and the clock stepped.
Hence the one-time step below, and the stronger rule that the upstream must not take its time from
the swallow host at all; the lab moved its gateway to a host that does not sync from this one.

## Decision

- **The installation serves NTP on the swallow host: the Control-Plane NTP Server**, on UDP 123
  at `SWALLOW_ADDRESS`. It is a host service, never a container. Installing it and choosing its
  upstream are separate steps:
  - *Installing* depends on the provisioner. With MAAS the installation installs nothing: MAAS's
    own chronyd is the server. With any other provisioner (none exists yet; Ironic is the expected
    one) the installation installs the distribution `chrony` with a swallow-owned configuration
    that keeps serving its local clock when the upstream is unreachable, so Servers keep a common
    time.
  - *The upstream* is an installation setting controlled by `swallowctl ntp upstream`; unset
    means `ntp.ubuntu.com`, MAAS's own default. With MAAS, swallowctl owns MAAS's NTP servers and
    keeps MAAS handing this host to the machines it deploys. A large first correction is stepped
    once instead of slewed.
  - The upstream must not take its time from the swallow host, directly or through a gateway that
    computes time from that host's clock.
- **Every provisioned Server is pointed at it, after SSH, by Ansible.** Each OS provisioning, in a
  Deploy OS Workflow and in a Platform deployment's `ensure-os` Job alike, is followed by a
  per-Server `configure-ntp` Task toward the installation's NTP server address. It configures the
  time client the OS already has, installs chrony only on a host with none, stops every other
  client, and waits a bounded time for a reply. A deployment is refused at acceptance when the installation
  has no NTP server address. Servers that were not provisioned by the Workflow (already deployed,
  or `existing_os`) are not reconfigured.
- **Platform deployments check time, they do not set it, and each Platform checks for itself.**
  The Kubernetes and Slurm install playbooks begin by checking every target's clock against the
  installation's NTP server before installing anything; the deploy request chooses whether a
  problem fails the deployment (`enforce`, the default) or is only reported (`warn`). It replaces
  the Slurm preflight's clock warning. A future Platform type adds the check when its runtime needs
  it.
- **Synthesizing cloud-init is the recorded fallback, not the first cut.** If a requirement ever
  appears for the clock to be right before SSH comes up, swallow may add an `ntp:` cloud-init
  section at deploy time; that needs its own plan and an amendment of ADR 058, and the Ansible
  Task stays as the path that configures hosts.

Revision history: 2026-10-28 removed the Site NTP Configuration (time sync only when a Site listed
servers, one `configure-ntp` over every Platform target including `existing_os` hosts) in favour of
the installation's NTP server at every OS provisioning; 2026-10-29 removed the shared
`check-time-sync` Task and the always-installed chrony (original text in git history).

## Alternatives considered

- **A Site-scoped list of NTP servers, edited per Site:** implemented first and removed the same
  day by the user. Servers use the NTP server swallow provides; a per-Site override is not needed
  and made "which time does this Server follow" a question with two answers.
- **Configure NTP in the Platform deployment instead of at OS provisioning:** rejected. Time is
  part of an OS being ready, not of a platform; a Platform deployment only verifies it, and the
  operator chooses per deployment whether a problem blocks it.
- **One time-check Task shared by every Platform deployment (`check-time-sync` in `ensure-os`):**
  implemented and removed on 2026-10-29 by the user. Whether a runtime needs synchronized clocks
  is that Platform's concern, so the check belongs in its own installation, not in a step every
  Platform pays for.
- **Always install chrony on provisioned Servers:** implemented and removed on 2026-10-29. It cost
  minutes per deployment on a slow package mirror (465 s in the lab) while the image's own client
  does the job.
- **Synthesize cloud-init `ntp:` at deploy time:** deferred, kept as the fallback above. It
  contradicts ADR 058's "swallow composes no cloud-init", needs merge rules for operator user
  data, and cannot re-converge a host after the address changes.
- **Run chrony in a container with host networking:** rejected. It needs `CAP_SYS_TIME` to
  discipline the host clock from inside a container, ties fleet time to the Compose stack's
  restarts and upgrades, and on the production host it would still collide with MAAS on UDP 123.
- **Install host chrony beside MAAS's chronyd:** rejected. Only one of them can bind UDP 123, and
  two daemons disciplining one clock fight each other.
- **Rely only on MAAS's cloud-init NTP:** rejected as the only mechanism. It picks rack addresses
  per subnet outside swallow's control and is specific to MAAS.
- **A Managed Software `chrony` kind:** rejected. Time sync is part of every OS provisioning, not
  an optional install an operator remembers to compose.

## Consequences

- The provisioning and platforms contracts gain the per-Server `configure-ntp` Task and the deploy
  field `timeSyncCheck`; the Workflow kind `configure-ntp` labels the Task and is never a standalone
  Workflow or a Site playbook mapping.
- Every OS deployment now needs runnable Site automation and an installation NTP server address.
- swallowctl owns MAAS's NTP servers: an upstream set in the MAAS UI is replaced on the next
  install, upgrade, or ensure.
- Servers already deployed keep their time configuration until they are provisioned again.
