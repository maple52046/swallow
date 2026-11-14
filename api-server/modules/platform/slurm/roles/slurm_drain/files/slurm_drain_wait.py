#!/usr/bin/env python3
"""Wait until no job runs on the drained nodes a scale-in removes (decision 083).

--nodes is a JSON list of {name, timeout, force}. Every poll prints the jobs still on each node.
When a node's timeout passes it either cancels the node's jobs (force) and waits --grace seconds for
them to end, or the script exits 3 with a last line naming the nodes that still run jobs. A forced
node whose canceled jobs still have not ended after the grace (a node whose machine failed cannot
end them) is set DOWN, which makes slurmctld end them, and no longer waited for. It exits 0 when
every node is empty or set DOWN, and 2 when squeue cannot be read.
"""

import argparse
import json
import subprocess
import sys
import time


def jobs_on(node):
    """Returns the running or pending job ids squeue lists on node."""
    done = subprocess.run(["squeue", "--noheader", "--format=%i", "--nodelist=" + node,
                           "--states=RUNNING,COMPLETING,CONFIGURING,SUSPENDED"],
                          capture_output=True, text=True, timeout=60)
    if done.returncode != 0:
        if "Invalid node name" in done.stderr:
            return []
        raise RuntimeError(done.stderr.strip() or "squeue failed")
    return [line.strip() for line in done.stdout.splitlines() if line.strip()]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--nodes", required=True)
    parser.add_argument("--poll", type=int, default=10)
    parser.add_argument("--grace", type=int, default=120)
    args = parser.parse_args()
    nodes = json.loads(args.nodes)
    start = time.monotonic()
    canceled = {}
    downed = []
    while True:
        try:
            counts = {node["name"]: jobs_on(node["name"]) for node in nodes if node["name"] not in downed}
        except (RuntimeError, OSError, subprocess.TimeoutExpired) as error:
            print("the queue of the leaving nodes cannot be read: %s" % error)
            return 2
        elapsed = int(time.monotonic() - start)
        busy = {name: jobs for name, jobs in counts.items() if jobs}
        print("after %ds: %s" % (elapsed, ", ".join("%s %d jobs" % (name, len(jobs)) for name, jobs in counts.items())), flush=True)
        if not busy:
            if canceled:
                print("canceled jobs on " + ", ".join(sorted(canceled)))
            if downed:
                print("set DOWN, their jobs did not end after they were canceled: " + ", ".join(downed))
            return 0
        overdue = []
        for node in nodes:
            name = node["name"]
            if name not in busy or elapsed < int(node["timeout"]):
                continue
            if node.get("force"):
                if name not in canceled:
                    subprocess.run(["scancel", "--nodelist=" + name], capture_output=True, text=True, timeout=60)
                    canceled[name] = elapsed
                    print("canceled jobs on %s after %ds" % (name, elapsed), flush=True)
                elif elapsed - canceled[name] > args.grace:
                    done = subprocess.run(["scontrol", "update", "nodename=" + name, "state=down",
                                           "reason=swallow: leaving the Platform, jobs did not end"],
                                          capture_output=True, text=True, timeout=60)
                    if done.returncode != 0 and "Invalid node name" not in done.stderr:
                        overdue.append("%s still runs %d jobs %ds after they were canceled and could not be set DOWN: %s" % (
                            name, len(busy[name]), elapsed - canceled[name], done.stderr.strip()))
                    else:
                        downed.append(name)
                        print("set %s DOWN: %d jobs did not end %ds after they were canceled" % (
                            name, len(busy[name]), elapsed - canceled[name]), flush=True)
            else:
                overdue.append("%s still runs %d jobs (%s) after the %ss drain timeout" % (
                    name, len(busy[name]), ",".join(busy[name][:5]), node["timeout"]))
        if overdue:
            print("leaving nodes still run jobs: " + "; ".join(overdue) +
                  "; wait, raise drainTimeoutSeconds, or force the removal, then retry")
            return 3
        time.sleep(args.poll)


if __name__ == "__main__":
    sys.exit(main())
