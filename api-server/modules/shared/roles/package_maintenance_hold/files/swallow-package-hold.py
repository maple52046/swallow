#!/usr/bin/python3
"""Hold this host's automatic package maintenance while swallow automation runs on it.

The OS's own maintenance (apt-daily and unattended-upgrades on Debian/Ubuntu, dnf-makecache and
dnf-automatic on the RHEL family) changes packages on a timer that knows nothing of swallow. On a freshly booted image it
starts at a random point of the first hour, holds the package lock for minutes, and restarts sshd,
systemd-networkd and whatever needrestart picks. swallow therefore holds it while one of its
playbooks runs on the host (decision 073):

  acquire LEASE TTL WAIT GRACE  take or renew LEASE until now+TTL, stop the maintenance timers that
                                are active, then wait up to WAIT seconds for maintenance that is
                                already running; on timeout the lease ends GRACE seconds later
  release LEASE GRACE           end LEASE GRACE seconds from now (never later than it would have)
  expire                        drop the leases that are due; with none left, start the timers again
  status                        print the hold as JSON

Only runtime state changes. Timers are stopped and started, never disabled or masked, and the state
lives under /run, so a reboot always restores the operator's policy. Only the timers this script
stopped are started again. A transient systemd timer runs `expire` at the latest lease deadline, so
a playbook that fails, is canceled, or loses its runner still gives the host back after its TTL.
Maintenance that is already running is never interrupted: acquire waits for it.

Every command prints one JSON object on stdout. Errors go to stderr prefixed with a swallow error
code, [package_maintenance_busy] (exit 75) or [package_maintenance_hold_failed] (exit 1).
Python 3.6 compatible: RHEL 8's platform-python is the oldest interpreter it runs under.
"""

import argparse
import contextlib
import fcntl
import json
import os
import re
import subprocess
import sys
import time

# Tests point every path at a scratch tree; on a host it is empty.
ROOT = os.environ.get("SWALLOW_PACKAGE_HOLD_ROOT", "")
STATE_DIR = ROOT + "/run/swallow/package-hold"
LEASE_DIR = STATE_DIR + "/leases"
TIMERS_FILE = STATE_DIR + "/timers"
LOCK_FILE = STATE_DIR + "/lock"
PROC = ROOT + "/proc"
EXPIRY_PREFIX = "swallow-package-hold-expiry-"

# The timers that start the OS's package maintenance, and the services that run it. The index
# refreshes (apt-daily, dnf-makecache) count too: they hold the package lock while they run.
MAINTENANCE_TIMERS = (
    "apt-daily.timer",
    "apt-daily-upgrade.timer",
    "dnf-makecache.timer",
    "dnf-automatic.timer",
    "dnf-automatic-download.timer",
    "dnf-automatic-install.timer",
    "dnf-automatic-notifyonly.timer",
    "dnf5-automatic.timer",
)
MAINTENANCE_SERVICES = tuple(timer[: -len(".timer")] + ".service" for timer in MAINTENANCE_TIMERS)

# The locks apt and dpkg hold while they change the package database or its caches.
PACKAGE_LOCKS = tuple(
    ROOT + path
    for path in (
        "/var/lib/dpkg/lock-frontend",
        "/var/lib/dpkg/lock",
        "/var/lib/apt/lists/lock",
        "/var/cache/apt/archives/lock",
    )
)

POLL_SECONDS = 5
# An expiry timer fires up to a second before the deadline it was armed for (whole seconds), so a
# lease this close to its deadline counts as due rather than re-arming a timer for the same instant.
DUE_SLACK_SECONDS = 2
BUSY_EXIT = 75
LEASE_PATTERN = re.compile(r"^[A-Za-z0-9._-]{1,64}$")


class HoldError(Exception):
    """A failure reported as [package_maintenance_hold_failed]."""

    code = "package_maintenance_hold_failed"
    exit_status = 1


class BusyError(HoldError):
    """Maintenance that was already running did not finish within the wait."""

    code = "package_maintenance_busy"
    exit_status = BUSY_EXIT


def run(argv, check=False):
    """Run a command and return (returncode, stdout); a failure raises only when check is set."""
    proc = subprocess.run(
        argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True
    )
    if check and proc.returncode != 0:
        raise HoldError(
            "%s failed (rc=%d): %s" % (" ".join(argv), proc.returncode, proc.stderr.strip())
        )
    return proc.returncode, proc.stdout


def unit_states(units):
    """Return each unit's active state in order; systemd reports a unit it lacks as inactive."""
    _, out = run(["systemctl", "is-active"] + list(units))
    lines = out.splitlines()
    lines += ["inactive"] * (len(units) - len(lines))
    return list(zip(units, lines))


@contextlib.contextmanager
def state_lock():
    """Serialize every change to the hold: parallel Tasks on one host share it."""
    os.makedirs(LEASE_DIR, mode=0o700, exist_ok=True)
    with open(LOCK_FILE, "a") as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        yield


def write_atomically(path, text):
    tmp = path + ".tmp"
    with open(tmp, "w") as handle:
        handle.write(text)
    os.replace(tmp, path)


def read_leases():
    """Return {lease: deadline}; a file that does not parse holds nothing."""
    leases = {}
    if not os.path.isdir(LEASE_DIR):
        return leases
    for name in os.listdir(LEASE_DIR):
        if not LEASE_PATTERN.match(name) or name.endswith(".tmp"):
            continue
        try:
            with open(os.path.join(LEASE_DIR, name)) as handle:
                leases[name] = int(json.load(handle)["deadline"])
        except (OSError, ValueError, KeyError, TypeError):
            continue
    return leases


def write_lease(lease, deadline):
    write_atomically(os.path.join(LEASE_DIR, lease), json.dumps({"deadline": deadline}))


def read_timers():
    """Return the timers this script stopped and has not started again."""
    try:
        with open(TIMERS_FILE) as handle:
            return [line.strip() for line in handle if line.strip()]
    except OSError:
        return []


def write_timers(timers):
    if timers:
        write_atomically(TIMERS_FILE, "".join(timer + "\n" for timer in timers))
    elif os.path.exists(TIMERS_FILE):
        os.remove(TIMERS_FILE)


def expiry_timers():
    """Return the transient expiry timers that are loaded."""
    _, out = run(
        ["systemctl", "list-units", "--all", "--plain", "--no-legend", "--type=timer",
         EXPIRY_PREFIX + "*"]
    )
    return [line.split()[0] for line in out.splitlines() if line.strip()]


def arm_expiry(deadline):
    """Make exactly one transient timer run `expire` at deadline.

    The unit is named after the deadline, so re-arming from inside a running expire service never
    collides with that service's own name, and an unchanged deadline leaves its timer alone.
    """
    name = EXPIRY_PREFIX + str(deadline)
    loaded = expiry_timers()
    for unit in loaded:
        if unit != name + ".timer":
            run(["systemctl", "stop", unit])
    if name + ".timer" in loaded:
        return
    run(["systemctl", "reset-failed", EXPIRY_PREFIX + "*"])
    run(
        [
            "systemd-run",
            "--quiet",
            "--unit=" + name,
            "--on-active=%ds" % max(1, deadline - int(time.time())),
            "--timer-property=AccuracySec=1s",
            "--timer-property=RemainAfterElapse=no",
            "--description=Give back the OS package maintenance swallow held",
            sys.executable,
            os.path.abspath(__file__),
            "expire",
        ],
        check=True,
    )


def process_name(pid):
    try:
        with open("%s/%d/comm" % (PROC, pid)) as handle:
            return handle.read().strip()
    except OSError:
        return "?"


def lock_holders():
    """Return (lock path, pid, process name) for every package lock held, read from /proc/locks.

    Reading instead of probing by taking the lock: an apt-get that starts in that instant would
    otherwise fail at once.
    """
    wanted = {}
    for path in PACKAGE_LOCKS:
        try:
            info = os.stat(path)
        except OSError:
            continue
        wanted[(os.major(info.st_dev), os.minor(info.st_dev), info.st_ino)] = path
    if not wanted:
        return []
    try:
        with open(PROC + "/locks") as handle:
            lines = handle.readlines()
    except OSError:
        return []
    holders = []
    for line in lines:
        # "1: POSIX  ADVISORY  WRITE 15178 00:1c:4242 0 EOF"; a blocked waiter's line has "->".
        fields = line.split()
        if len(fields) < 6 or fields[1] == "->":
            continue
        try:
            pid = int(fields[4])
            major, minor, inode = fields[5].split(":")
            key = (int(major, 16), int(minor, 16), int(inode))
        except ValueError:
            continue
        if key in wanted:
            holders.append((wanted[key], pid, process_name(pid)))
    return holders


def busy_maintenance():
    """Return what is changing packages right now: the maintenance services still running, and the
    processes holding a package lock."""
    units = [unit for unit, state in unit_states(MAINTENANCE_SERVICES) if state not in ("inactive", "failed")]
    return units, lock_holders()


def describe_busy(units, holders):
    return ", ".join(units + ["%s (pid %d %s)" % holder for holder in holders])


def acquire(lease, ttl, wait, grace):
    with state_lock():
        write_lease(lease, int(time.time()) + ttl)
        arm_expiry(max(read_leases().values()))
        held = read_timers()
        # Re-stop timers already held too: an apt upgrade's postinst can start them again.
        active = [timer for timer, state in unit_states(MAINTENANCE_TIMERS) if state == "active"]
        stopped = [timer for timer in active if timer not in held]
        # Record before stopping, so a crash in between can only leave a timer recorded, not lost.
        write_timers(held + stopped)
        if active:
            run(["systemctl", "stop"] + active, check=True)
    started = time.time()
    # What the run waited for, by unit or process name: one upgrade runs many dpkg processes.
    waited_for = []
    while True:
        units, holders = busy_maintenance()
        if not units and not holders:
            break
        for item in units + [name for _, _, name in holders]:
            if item not in waited_for:
                waited_for.append(item)
        if time.time() - started >= wait:
            release(lease, grace)
            raise BusyError(
                "the OS's package maintenance is still running after %ds: %s"
                % (int(time.time() - started), describe_busy(units, holders))
            )
        time.sleep(POLL_SECONDS)
    return {
        "lease": lease,
        "held": held + stopped,
        "stopped": stopped,
        "waitedSeconds": int(round(time.time() - started)) if waited_for else 0,
        "waitedFor": waited_for,
    }


def release(lease, grace):
    with state_lock():
        leases = read_leases()
        if lease in leases:
            write_lease(lease, min(leases[lease], int(time.time()) + grace))
            leases = read_leases()
        if leases:
            arm_expiry(max(leases.values()))
    return {"lease": lease, "leases": sorted(leases)}


def expire():
    with state_lock():
        due_by = int(time.time()) + DUE_SLACK_SECONDS
        live = {}
        for lease, deadline in read_leases().items():
            if deadline > due_by:
                live[lease] = deadline
            else:
                os.remove(os.path.join(LEASE_DIR, lease))
        if live:
            arm_expiry(max(live.values()))
            return {"leases": sorted(live), "started": []}
        started = []
        for timer in read_timers():
            # A timer whose package was removed meanwhile cannot start; that is not an error.
            if run(["systemctl", "start", timer])[0] == 0:
                started.append(timer)
        write_timers([])
        for unit in expiry_timers():
            run(["systemctl", "stop", unit])
    return {"leases": [], "started": started}


def status():
    units, holders = busy_maintenance()
    return {
        "leases": read_leases(),
        "held": read_timers(),
        "expiry": expiry_timers(),
        "busy": describe_busy(units, holders),
    }


def lease_name(value):
    if not LEASE_PATTERN.match(value):
        raise argparse.ArgumentTypeError("a lease is 1-64 of [A-Za-z0-9._-]")
    return value


def seconds(value):
    number = int(value)
    if number < 0:
        raise argparse.ArgumentTypeError("seconds must not be negative")
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    commands = parser.add_subparsers(dest="command")
    acquire_cmd = commands.add_parser("acquire")
    acquire_cmd.add_argument("lease", type=lease_name)
    acquire_cmd.add_argument("ttl", type=seconds)
    acquire_cmd.add_argument("wait", type=seconds)
    acquire_cmd.add_argument("grace", type=seconds)
    release_cmd = commands.add_parser("release")
    release_cmd.add_argument("lease", type=lease_name)
    release_cmd.add_argument("grace", type=seconds)
    commands.add_parser("expire")
    commands.add_parser("status")
    args = parser.parse_args()
    try:
        if args.command == "acquire":
            result = acquire(args.lease, args.ttl, args.wait, args.grace)
        elif args.command == "release":
            result = release(args.lease, args.grace)
        elif args.command == "expire":
            result = expire()
        elif args.command == "status":
            result = status()
        else:
            parser.error("a command is required")
    except HoldError as err:
        sys.stderr.write("[%s] %s\n" % (err.code, err))
        return err.exit_status
    except OSError as err:
        sys.stderr.write("[%s] %s\n" % (HoldError.code, err))
        return HoldError.exit_status
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
