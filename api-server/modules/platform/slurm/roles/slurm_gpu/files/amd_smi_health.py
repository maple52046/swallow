#!/usr/bin/env python3
"""Check a GPU node's health through AMD SMI, which needs only the amdgpu driver (decision 083).

Reads the device list, the uncorrectable ECC counts, the firmware versions, and the GPUs' market
names with the amd-smi CLI and prints one JSON object: available (amd-smi ran), devices, healthy,
problems (what fails the node), unsupported (queries the device does not answer, such as ECC on a
virtual function), notes, and model_features (each GPU model as a Slurm feature, for example
"mi308x", read from the GPU itself because a virtual function's inventory often names no model).
It never changes the node.
"""

import argparse
import json
import os
import re
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


def model_feature(name):
    """A GPU market name as the Slurm feature the Slurm module makes of a model name: the product
    inside brackets when there are any ("Aqua Vanjaram [Instinct MI308X]"), its first alternative,
    without the "AMD" and "Instinct" brand words, lower case with every other character an
    underscore, at most 32 characters. Empty for an empty name."""
    name = name.strip()
    if "[" in name and "]" in name[name.index("["):]:
        name = name[name.index("[") + 1:name.rindex("]")]
    name = name.split("/")[0].strip()
    name = re.sub(r"^(amd\s+)?(instinct\s+)?", "", name, flags=re.IGNORECASE)
    return re.sub(r"[^a-z0-9]+", "_", name.lower()).strip("_")[:32]


def market_names(document):
    """Every market_name value anywhere in an amd-smi document."""
    names, stack = [], [document]
    while stack:
        value = stack.pop()
        if isinstance(value, dict):
            for key, inner in value.items():
                if str(key).lower() == "market_name" and isinstance(inner, str):
                    names.append(inner)
                elif isinstance(inner, (dict, list)):
                    stack.append(inner)
        elif isinstance(value, list):
            stack.extend(value)
    return names


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--kfd-gpus", type=int, required=True, help="GPUs the node's KFD topology lists")
    args = parser.parse_args()
    report = {"available": False, "healthy": False, "devices": 0, "problems": [], "unsupported": [], "notes": [], "model_features": []}
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
    asic, error = run(["static", "--asic"])
    features = sorted({model_feature(name) for name in market_names(asic)} - {""}) if asic is not None else []
    if not features:
        report["notes"].append("no GPU model name (%s)" % (error or "amd-smi reports none"))
    report["model_features"] = features
    report["healthy"] = not report["problems"]
    print(json.dumps(report))


if __name__ == "__main__":
    main()
