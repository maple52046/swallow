# 073. Hold the OS's package maintenance while swallow automation runs

- Status: Accepted
- Date: 2026-10-31
- 現行規格：[core-and-extensions.md](../development/core-and-extensions.md) §5.2

## Context

Ubuntu images run their own package maintenance: `apt-daily.timer` refreshes the package lists and
`apt-daily-upgrade.timer` runs unattended-upgrades. Both timers are `Persistent=true`, and an image
carries the timer stamps of the day it was built. So on every boot of an older image, the missed
upgrade starts at a random time within the first hour (`RandomizedDelaySec=60m`). That window
overlaps swallow's ensure-os, platform, and software Tasks.

In the lab, a Slurm deploy with DME, Native OCI, PMIx, and Docker CE failed at `Install crun`. The
unattended-upgrades run had started six minutes into `install-platform`. It held the dpkg lock for
four minutes while it upgraded about 70 packages, among them systemd, udev, and openssh-server.
Earlier symptoms of the same cause had been patched one by one:

- SSH reconnect retries in the runner.
- A pinned k0s API address, because systemd-networkd restarted while a join token was created.
- Retried handlers, because a D-Bus reset made systemd restarts fail.
- A one-time dpkg lock gate at the start of `deploy-slurm`.
- `until/retries` copied onto 18 package tasks. New tasks kept missing it.

None of these stop the cause, and the apt module's `lock_timeout` does not help either: in
ansible-core 2.18 it retries only python-apt's own locking. The `apt-get` or `dpkg` it then runs
fails at once when unattended-upgrades holds the lock, and Ubuntu gives only the `apt` binary a lock
timeout.

## Decision

1. **swallow holds the OS's automatic package maintenance on a host while one of its playbooks
   changes packages there.** It does not disable the maintenance and does not only wait for the
   lock. The hold covers the timers of apt-daily and unattended-upgrades, and of dnf-makecache and
   dnf-automatic.
2. **The hold changes runtime state only.** It stops timers that are active and later starts exactly
   those again; it never disables or masks them. Its state lives under `/run`, so a reboot always
   restores the operator's policy.
3. **The hold never interrupts maintenance that is already running.** Taking the hold waits for it
   (bounded). When it outlasts the wait, the play fails with `[package_maintenance_busy]` and the
   Task can be retried.
4. **The hold is a lease with a deadline.** Each playbook run holds its own lease, renewed by the
   next playbook. A transient systemd timer gives the host back at the latest deadline, so a failed,
   canceled, or lost run still gives the host back after its TTL. A release ends the lease after a
   short grace, so the consecutive Tasks of one Workflow stay covered. Concurrent Tasks on one host
   hold separate leases.
5. **It is host automation content, not runner behavior.** A shared role takes the hold. Every
   shipped playbook that changes packages takes the hold in its first play and releases it in its
   last. Every package task still retries. A content test enforces both.
6. swallow also makes `apt-get` wait for a lock held outside swallow, through
   `/etc/apt/apt.conf.d/50swallow-lock-timeout` (`DPkg::Lock::Timeout`).

## Alternatives considered

- **Only make every package operation wait for the lock** (a lock timeout and uniform retries).
  It fixes the failing installs, but an upgrade during a deployment still restarts sshd,
  systemd-networkd, polkit, and whatever needrestart picks under the play.
- **Disable unattended-upgrades on Servers swallow manages.** It removes the cause but takes over the
  operator's security update policy, which swallow does not own.
- **Configure it through cloud-init at provisioning.** Existing-OS Servers are never provisioned by
  swallow, and a Deployment Template's user-data belongs to the operator.
- **Have the Ansible runner wrap every playbook with the hold.** It needs no change per playbook,
  but the core adapter would depend on module content and would hold hosts for playbooks that
  change no packages.
- **Run the pending upgrade to completion before automation starts.** It adds minutes to every
  deploy, and `apt-daily.timer` (up to 12 hours of random delay) can still start later.

## Consequences

- The OS's updates are deferred, not skipped. The missed run starts after swallow is done, at the
  latest one TTL after the last playbook of a failed run.
- A deploy that starts while an upgrade is already running waits for it instead of failing at a
  random task.
- The scattered workarounds remain as defense in depth. Only the one-time lock gate in
  `deploy-slurm` is removed.
- A new playbook that changes packages must take the hold; the content test fails until it does.
