#!/usr/bin/env python3
"""Check a GPU node's health through AMD SMI, which needs only the amdgpu driver (decision 083).

Reads the device list, the uncorrectable ECC counts, and the firmware versions with the amd-smi
CLI and prints one JSON object: available (amd-smi ran), devices, healthy, problems (what fails
the node), unsupported (queries the device does not answer, such as ECC on a virtual function),
and notes. It never changes the node.
"""

import argparse
import json
import os
import shutil
import subprocess

# amd-smi is usually on the PATH; a ROCm installed under /opt/rocm puts it there.
AMD_SMI = shutil.which("amd-smi") or next(
    (path for path in ("/opt/rocm/bin/amd-smi", "/usr/bin/amd-smi") if os.access(path, os.X_OK)), None)


def run(args):
    """Runs amd-smi with args and returns (parsed JSON or None, stderr text)."""
    try:
        done = subprocess.run([AMD_SMI] + args + ["--json"], capture_output=True, text=True, timeout=120)
    except (OSError, subprocess.TimeoutExpired) as error:
        return None, str(error)
    if done.returncode != 0:
        return None, (done.stderr or done.stdout).strip()[-400:]
    try:
        return json.loads(done.stdout), ""
    except ValueError:
        return None, "amd-smi printed no JSON"


def as_list(document):
    """amd-smi prints a list of per-GPU objects, or an object holding one under a key."""
    if isinstance(document, list):
        return document
    if isinstance(document, dict):
        for value in document.values():
            if isinstance(value, list):
                return value
        return [document]
    return []


def uncorrectable(entry):
    """Sums every uncorrectable count an ECC entry reports; None when it reports none."""
    total, seen = 0, False
    stack = [entry]
    while stack:
        value = stack.pop()
        if isinstance(value, dict):
            for key, inner in value.items():
                if "uncorrectable" in str(key).lower() and isinstance(inner, (int, float)):
                    total += int(inner)
                    seen = True
                elif isinstance(inner, (dict, list)):
                    stack.append(inner)
        elif isinstance(value, list):
            stack.extend(value)
    return total if seen else None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--kfd-gpus", type=int, required=True, help="GPUs the node's KFD topology lists")
    args = parser.parse_args()
    report = {"available": False, "healthy": False, "devices": 0, "problems": [], "unsupported": [], "notes": []}
    if AMD_SMI is None:
        report["error"] = "amd-smi is not on the PATH"
        print(json.dumps(report))
        return
    devices, error = run(["list"])
    if devices is None:
        report["error"] = error
        print(json.dumps(report))
        return
    report["available"] = True
    report["devices"] = len(as_list(devices))
    if report["devices"] == 0:
        report["problems"].append("amd-smi lists no GPU")
    elif report["devices"] < args.kfd_gpus:
        # A partitioned GPU (CPX) or a virtual function may list fewer devices than KFD's logical
        # GPUs; the GRES follow KFD, so this is a note, not a failure.
        report["notes"].append("amd-smi lists %d devices, KFD %d GPUs" % (report["devices"], args.kfd_gpus))
    ecc, error = run(["metric", "--ecc"])
    if ecc is None:
        report["unsupported"].append("ECC (%s)" % (error or "no answer"))
    else:
        counts = [uncorrectable(entry) for entry in as_list(ecc)]
        known = [count for count in counts if count is not None]
        if not known:
            report["unsupported"].append("ECC (no counts reported)")
        elif sum(known) > 0:
            report["problems"].append("%d uncorrectable ECC errors" % sum(known))
    firmware, error = run(["firmware"])
    if firmware is None or not as_list(firmware):
        report["unsupported"].append("firmware (%s)" % (error or "no versions reported"))
    report["healthy"] = not report["problems"]
    print(json.dumps(report))


if __name__ == "__main__":
    main()
