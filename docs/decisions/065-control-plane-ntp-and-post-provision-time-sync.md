# 065. The installation serves NTP, and every provisioned Server is pointed at it after SSH

- Status: Accepted
- Date: 2026-10-28 (revised the same day, see "Revision")

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

## Decision

- **The installation serves NTP on the swallow host: the Control-Plane NTP Server**, on UDP 123
  at `SWALLOW_ADDRESS`. It is a host service, never a container. Installing it and choosing its
  upstream are separate steps:
  - *Installing* depends on the provisioner. With MAAS the installation installs nothing: MAAS's
    own chronyd is the server. With any other provisioner (none exists yet; Ironic is the expected
    one) `local-ntp.sh install` installs the distribution `chrony` with a swallow-owned
    configuration: the upstream, `allow` (all clients, or `SWALLOW_NTP_ALLOW`), and
    `local stratum 10`, so Servers keep a common time when the upstream is unreachable.
  - *The upstream* is the installation setting `SWALLOW_NTP_UPSTREAM`, controlled by
    `swallowctl ntp upstream show | set <server>... | default`. Unset means the default
    (`ntp.ubuntu.com`, MAAS's own default). `local-ntp.sh upstream` applies it: with MAAS it writes
    MAAS's `ntp_servers` and keeps `ntp_external_only=false`. MAAS's chrony configuration has no
    `makestep`, so once chronyd follows the new upstream the script has it step the clock once
    (`chronyc makestep`) when the host is more than a second off, instead of letting it slew at its
    maximum rate. An IP address is written `server <ip> iburst`, a name `pool <name> iburst`.
  - `swallowctl install`, `upgrade`, and `ensure` run both steps, and `doctor` checks the result.
- **Every provisioned Server is pointed at it, after SSH, by Ansible.** The api-server learns the
  server's address from the installation (`SWALLOW_API_NTP_SERVER`, which the production
  installation sets to `SWALLOW_ADDRESS`). Each `provision-<serverId>` Task, in a Deploy OS
  Workflow and in a Platform deployment's `ensure-os` Job alike, is followed by
  `configure-ntp-<serverId>`, which runs the `configure-ntp` playbook with that address (trusted
  variable `swallow_ntp_servers`, frozen at acceptance). It configures the time client the OS
  already has, in the order `systemd-timesyncd`, `chrony`, `ntpd`, and installs chrony only on a
  host with none; it writes a swallow-owned configuration that uses only that server, stops every
  other client, and waits a bounded time for a reply, failing with `[ntp_not_synchronized]`
  otherwise. `systemd-timesyncd` still prefers a DHCP-offered server, which on a subnet MAAS
  serves is the MAAS rack, the swallow host too. This needs runnable
  Site automation, and a deployment is refused at acceptance (`409 conflict`) when the
  installation has no NTP server address. Servers that were not provisioned by the Workflow
  (already deployed, or `existing_os`) are not reconfigured.
- **Platform deployments check time, they do not set it, and each Platform checks for itself.**
  The Kubernetes and Slurm install playbooks begin with the `time_sync_check` role over all
  targets, before k0s is downloaded and before Slurm's preflight and MUNGE. It reads each node's
  synchronization state and measures its offset from the installation's NTP server. The deploy
  request's `timeSyncCheck` decides what a problem does: `enforce` (default) fails the install step
  on every node before anything is installed; `warn` reports it and continues. It replaces the
  Slurm preflight's clock warning. A future Platform type adds the role when its runtime needs it.
- **Synthesizing cloud-init is the recorded fallback, not the first cut.** If a requirement ever
  appears for the clock to be right before SSH comes up, swallow may add an `ntp:` cloud-init
  section at deploy time; that needs its own plan and an amendment of ADR 058, and the Ansible
  Task stays as the path that configures hosts.

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

- The provisioning and platforms contracts gain the `configure-ntp-<serverId>` Task and the deploy
  field `timeSyncCheck`; the Workflow kind `configure-ntp` labels the Task and is never a standalone
  Workflow or a Site playbook mapping.
- Every OS deployment now needs runnable Site automation (`503 provider_unavailable` otherwise)
  and an installation NTP server address (`409 conflict` otherwise).
- swallowctl owns MAAS's `ntp_servers`: an upstream set in the MAAS UI is replaced by
  `SWALLOW_NTP_UPSTREAM`, or by the default when it is unset, on the next install, upgrade, or
  ensure.
- A large first correction is dangerous when the upstream derives its served time from the swallow
  host's own clock. On `root123-intel` the first chronos gateway (172.30.100.10) ran on the swallow
  host as a `chronyd -x` that computes time from the host clock. The host started 33.5 s behind;
  MAAS's chronyd slewed at +8.3% (no `makestep`), the gateway could not follow a rate change that
  large and kept serving host + 33.5 s, and the host ran past real time until the upstream was
  reverted, the kernel rate reset, and the clock stepped. Hence the one-time step. The underlying
  rule is stronger: the upstream must not take its time from the swallow host at all, so the lab
  moved its gateway to `tainan-node03` (`10.170.168.3`), which does not sync from this host.
- Servers already deployed keep their time configuration until they are provisioned again.

## Revision

The first version of this decision (same day) kept a Site-scoped NTP Configuration, added time
sync only when a Site listed servers, and ran one `configure-ntp` over every Platform target,
including `existing_os` hosts. The user replaced it with the design above: configure time at
every OS provisioning toward the installation's NTP server, separate installing the NTP server
from choosing its upstream, put the upstream under `swallowctl`, and let a Platform deployment
only check time sync, strictly by default. A second revision (2026-10-29) moved that check from a
shared `check-time-sync` Task into each Platform's install playbook and made `configure-ntp` use
the OS's own time client instead of installing chrony.

## Current status

Implemented in `api-server`, `dashboard`, and the production installation tooling, with unit,
end-to-end, and static playbook tests; the CLI passes `timeSyncCheck` through unchanged. MAAS's
chronyd on `root123-intel` follows `server 10.170.168.3 iburst` at stratum 2 within about 0.3 ms.

- 2026-10-29: a Deploy OS Workflow for the lab VM `lab-compute-2` (RAM, `ubuntu/noble`) ran
  `configure-ntp-<serverId>` in about a minute. The role chose `systemd-timesyncd`, got a reply
  in 3 s, and reported "synchronized via 10.170.168.20"; the VM then queried
  `10.170.168.20:123`. The chrony, ntpsec, no-client, and stop-the-other-client paths were run
  against `ubuntu/noble` and RHEL 9.8 root filesystems in containers, and the platform check's
  `enforce` and `warn` modes against this host.
- 2026-10-28, before the 10-29 revision: without `SWALLOW_API_NTP_SERVER` a Deploy OS request
  was refused with `409 conflict`; installing chrony took 465 s of an 8-minute
  `configure-ntp`, which led to using the OS's own client.
