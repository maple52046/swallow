#!/usr/bin/env python3
# Managed by Swallow (deploy-slurm): builds the OCI bundle deploy-slurm runs to prove native OCI
# containers (decision 066). It is not a workload image: its read-only rootfs is empty directories
# with the node's own /usr, /etc, and /opt (ROCm) bind-mounted read-only, so the same probes that run
# on the host (the HIP probe, the MPI probe) run inside it. /dev is bound whole: the job's device
# cgroup, not the bundle, decides which GPUs a container may open. The network namespace is the
# host's, because MPI ranks of different nodes talk over it and slurmstepd's PMIx server listens on
# the loopback address. /run is a tmpfs, so the runtime can create the step's spool mount at the
# host's path (where the PMIx server directory is), and /run/munge is the host's, because PMIx
# authenticates ranks with MUNGE. The spec is crun's own rootless default with those changes, and the
# bundle is rewritten only when it differs, so a rerun changes nothing.
# Prints {"changed": bool}.
import argparse
import json
import os
import subprocess
import sys
import tempfile


def rootless_spec(crun):
    with tempfile.TemporaryDirectory() as tmp:
        subprocess.run([crun, "spec", "--rootless"], cwd=tmp, check=True, capture_output=True)
        with open(os.path.join(tmp, "config.json")) as handle:
            return json.load(handle)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--bundle", required=True)
    parser.add_argument("--crun", default="/usr/bin/crun")
    args = parser.parse_args()

    rootfs = os.path.join(args.bundle, "rootfs")
    changed = False
    directories = ["usr", "etc", "opt", "proc", "dev", "sys", "tmp", "run", "home", "var", "root"]
    for directory in directories:
        path = os.path.join(rootfs, directory)
        if not os.path.isdir(path):
            os.makedirs(path)
            changed = True
    for link in ("bin", "sbin", "lib", "lib64"):
        path = os.path.join(rootfs, link)
        if not os.path.lexists(path):
            os.symlink("usr/" + link, path)
            changed = True

    spec = rootless_spec(args.crun)
    spec["root"] = {"path": rootfs, "readonly": True}
    spec["process"]["terminal"] = False
    spec["process"]["args"] = ["/bin/true"]
    spec["hostname"] = "swallow-oci-probe"
    read_only = ["rbind", "nosuid", "ro"]
    spec["mounts"] = [
        {"destination": "/proc", "type": "proc", "source": "proc"},
        {"destination": "/dev", "type": "bind", "source": "/dev", "options": ["rbind", "nosuid"]},
        {"destination": "/sys", "type": "bind", "source": "/sys", "options": ["rbind", "nosuid", "noexec", "nodev", "ro"]},
        {"destination": "/usr", "type": "bind", "source": "/usr", "options": read_only},
        {"destination": "/etc", "type": "bind", "source": "/etc", "options": read_only},
        {"destination": "/opt", "type": "bind", "source": "/opt", "options": read_only},
        {"destination": "/tmp", "type": "tmpfs", "source": "tmpfs", "options": ["nosuid", "nodev", "mode=1777"]},
        {"destination": "/run", "type": "tmpfs", "source": "tmpfs", "options": ["nosuid", "nodev", "mode=755"]},
        {"destination": "/run/munge", "type": "bind", "source": "/run/munge", "options": ["rbind", "nosuid", "nodev", "ro"]},
    ]
    spec["linux"]["namespaces"] = [ns for ns in spec["linux"]["namespaces"] if ns["type"] != "network"]

    config = os.path.join(args.bundle, "config.json")
    content = json.dumps(spec, indent=2, sort_keys=True) + "\n"
    current = None
    if os.path.exists(config):
        with open(config) as handle:
            current = handle.read()
    if current != content:
        with open(config + ".tmp", "w") as handle:
            handle.write(content)
        os.chmod(config + ".tmp", 0o644)
        os.replace(config + ".tmp", config)
        changed = True
    print(json.dumps({"changed": changed}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
