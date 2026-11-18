#!/usr/bin/env python3
"""Read what the amdgpu driver reports about RAS on the given AMD GPUs (PCI addresses, from the
amd_gpu_census role) for the amd-gpu-ras Validation Suite, and print one JSON object; the playbook
judges it. It always exits 0 and never changes the host:

- sysfs: each GPU's driver, its RAS feature mask (ras/features), and the ue/ce counts of every
  ras/<block>_err_count; ras/aca_umc (MI300 bare metal) is kept raw, since its format is not
  documented. The kernel log's "ras initialized" lines are evidence only.
- debugfs: which GPUs the driver keeps a CPER ring for (dri/<n>/amdgpu_ring_cper, mapped to a GPU
  through dri/<n>/name). Without debugfs the CPER state is debugfs_unavailable; with no ring, no_ring
  (GPUs before MI300, such as MI250X).
- AMD SMI: `amd-smi ras --cper` exports the ring's records into a private temporary directory, and
  `amd-smi ras --afid --cper-file` decodes each record's AFID. The directory is removed before the
  report is printed, even when the run is stopped (SIGHUP is ignored, SIGTERM unwinds), and every
  amd-smi call has a timeout. No --follow, no rdcd.

The AFID table carries only each AFID's category, error type, and severity. AMD's service-action
guidance for an AFID is not public and is deliberately absent; an AFID outside the table is reported
as its number, never guessed. --root prefixes every sysfs and debugfs path, and --amd-smi and --dmesg
name the tools, for tests.
"""
import argparse
import glob
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import time

# The ring severity enum (0, 1, 2), in that order.
UNCORRECTED, FATAL, CORRECTED = "NONFATAL-UNCORRECTED", "FATAL", "NONFATAL-CORRECTED"
SEVERITIES = (UNCORRECTED, FATAL, CORRECTED)
UNKNOWN = "UNKNOWN"

# AFID 15-34: (category, error type, severity), as verified on MI300 series with AMD SMI 26.2.x.
AFIDS = {
    15: ("Off-package", "WAFL", "Corrected"),
    16: ("Off-package", "WAFL", "Fatal"),
    17: ("Off-package", "XGMI", "Corrected"),
    18: ("Off-package", "XGMI", "Fatal"),
    19: ("HBM", "Bad Page Retirement Threshold", "Fatal"),
    20: ("HBM", "On-die ECC", "Fatal"),
    21: ("HBM", "End-to-end CRC", "Fatal"),
    22: ("HBM", "On-die ECC", "Uncorrected, non-fatal"),
    23: ("HBM", "End-to-end CRC", "Uncorrected, non-fatal"),
    24: ("HBM", "All", "Corrected"),
    25: ("HBM", "All Other", "Fatal"),
    26: ("Device Internal", "Hardware Assertion (HWA)", "Fatal"),
    27: ("Device Internal", "Hardware Timeout (WDT)", "Fatal"),
    28: ("Device Internal", "All Other", "Uncorrected, non-fatal"),
    29: ("Device Internal", "All Other", "Corrected"),
    30: ("Device Internal", "All Other", "Fatal"),
    31: ("CPER Format", "Malformed CPER", "All"),
    32: ("CPER Format", "Incomplete ACA Data", "All"),
    33: ("CPER Format", "Invalid ACA Data", "All"),
    34: ("Unidentified", "Unidentified", "Corrected"),
}
AFID_SEVERITY = {"Fatal": FATAL, "Uncorrected, non-fatal": UNCORRECTED, "Corrected": CORRECTED}

# Bounds that keep the helper well inside the Task's timeout and the result's limits.
EXPORT_TIMEOUT = 120
DECODE_TIMEOUT = 20
DECODE_BUDGET = 180
DECODE_LIMIT = 256
KERNEL_LOG_TIMEOUT = 20
EVIDENCE_LIMIT = 7800
MEASUREMENT_LIMIT = 20

# What an amd-smi without AFID decoding says when asked for it.
AFID_UNSUPPORTED = re.compile(r"not supported|unrecognized argument|unknown option|invalid choice|no such option", re.I)


def read(path):
    try:
        with open(path, encoding="utf-8", errors="replace") as handle:
            return handle.read().strip()
    except OSError:
        return ""


def bounded(lines):
    """Joins lines up to the evidence limit and says how many did not fit."""
    kept, size = [], 0
    for index, line in enumerate(lines):
        if size + len(line) + 1 > EVIDENCE_LIMIT:
            kept.append("... %d more" % (len(lines) - index))
            break
        kept.append(line)
        size += len(line) + 1
    return "\n".join(kept)


def run(command, timeout):
    """Runs command and returns (exit code or None when it did not finish, stdout, stderr)."""
    try:
        done = subprocess.run(command, capture_output=True, text=True, timeout=timeout, stdin=subprocess.DEVNULL)
    except subprocess.TimeoutExpired:
        return None, "", "%s did not finish within %d seconds" % (os.path.basename(command[0]), timeout)
    except OSError as error:
        return None, "", str(error)
    return done.returncode, done.stdout, done.stderr


def gpu_ras(root, bdf):
    """One GPU's driver, RAS feature mask, error counts per block, and raw ACA UMC data."""
    device = "%s/sys/bus/pci/devices/%s" % (root, bdf)
    driver = os.path.join(device, "driver")
    ras = os.path.join(device, "ras")
    gpu = {"bdf": bdf, "driver": os.path.basename(os.path.realpath(driver)) if os.path.lexists(driver) else "",
           "ras": os.path.isdir(ras), "mask": None, "blocks": {}, "aca": ""}
    if not gpu["ras"]:
        return gpu
    mask = re.search(r"0x[0-9a-fA-F]+", read(os.path.join(ras, "features")))
    gpu["mask"] = int(mask.group(0), 16) if mask else None
    for path in sorted(glob.glob(os.path.join(ras, "*_err_count"))):
        counts = {key: int(value) for key, value in re.findall(r"^\s*(ue|ce|de)\s*:\s*(\d+)", read(path), re.M)}
        if counts:
            gpu["blocks"][os.path.basename(path)[:-len("_err_count")]] = counts
    aca = os.path.join(ras, "aca_umc")
    if os.path.isfile(aca):
        gpu["aca"] = read(aca)[:400]
    elif os.path.isdir(aca):
        gpu["aca"] = "entries: " + " ".join(sorted(os.listdir(aca)))
    return gpu


def kernel_log(dmesg):
    """The kernel log's "ras initialized" lines, or why there are none."""
    code, out, err = run([dmesg], KERNEL_LOG_TIMEOUT)
    if code != 0:
        return ["kernel log unreadable: " + (err.strip() or "dmesg exited %s" % code)[-200:]]
    lines = [line.strip() for line in out.splitlines() if "ras initialized" in line.lower()]
    return lines[-16:] or ["the kernel log holds no \"ras initialized\" line (it may have rotated)"]


def cper_rings(root, gpus):
    """The GPUs the driver keeps a CPER ring for, or None when debugfs cannot be read."""
    dri = root + "/sys/kernel/debug/dri"
    if not os.path.isdir(dri):
        return None
    rings = set()
    for name in glob.glob(os.path.join(dri, "*", "name")):
        device = re.search(r"\bdev=(\S+)", read(name))
        if device and device.group(1) in gpus and os.path.exists(os.path.join(os.path.dirname(name), "amdgpu_ring_cper")):
            rings.add(device.group(1))
    return sorted(rings)


def find_amd_smi(given):
    """The amd-smi to run: the given one, else the PATH's, else ROCm's; None when there is none."""
    def runnable(path):
        return os.path.isfile(path) and os.access(path, os.X_OK)
    if given:
        return given if runnable(given) else None
    return shutil.which("amd-smi") or next(
        (path for path in ("/opt/rocm/bin/amd-smi", "/usr/bin/amd-smi") if runnable(path)), None)


def severity_of(value):
    """Reads a severity from an enum value or a name (FATAL, NONFATAL-CORRECTED, non_fatal_corrected, ...)."""
    if isinstance(value, int) and not isinstance(value, bool):
        return SEVERITIES[value] if 0 <= value < len(SEVERITIES) else None
    text = str(value).upper().replace("_", "-").replace("NON-FATAL", "NONFATAL")
    if "UNCORRECTED" in text:
        return UNCORRECTED
    if "CORRECTED" in text:
        return CORRECTED
    if "FATAL" in text:
        return FATAL
    return None


def json_severity(document):
    """The first severity a record's JSON companion names under a key with "severity" in it."""
    if isinstance(document, dict):
        for key, value in document.items():
            if "severity" in str(key).lower() and not isinstance(value, (dict, list)):
                found = severity_of(value)
                if found:
                    return found
        document = list(document.values())
    if isinstance(document, list):
        for value in document:
            found = json_severity(value)
            if found:
                return found
    return None


def record_severity(path, afids):
    """A record's severity: its JSON companion, then its file name (fatal-3.cper), then its AFIDs."""
    companion = path[:-len(".cper")] + ".json"
    if os.path.isfile(companion):
        try:
            with open(companion, encoding="utf-8") as handle:
                found = json_severity(json.load(handle))
            if found:
                return found
        except (OSError, ValueError):
            pass
    found = severity_of(re.sub(r"[-_]?\d+$", "", os.path.basename(path)[:-len(".cper")]))
    if found:
        return found
    for afid in afids:
        found = AFID_SEVERITY.get(AFIDS.get(afid, ("", "", ""))[2])
        if found:
            return found
    return None


def decode_afids(amd_smi, path):
    """Returns (AFIDs, error, unsupported) for one record."""
    code, out, err = run([amd_smi, "ras", "--afid", "--cper-file", path], DECODE_TIMEOUT)
    said = (out + "\n" + err).strip()
    if AFID_UNSUPPORTED.search(said):
        return [], said[-300:], True
    text = out.replace(path, " ").replace(os.path.basename(path), " ")
    afids = [int(number) for number in re.findall(r"(?<![\w.-])\d+(?![\w.-])", text)]
    if code != 0 or not afids:
        return [], (said or "amd-smi exited %s and printed no AFID" % code)[-300:], False
    return afids, "", False


def describe(afid):
    if afid in AFIDS:
        return "AFID %d (%s)" % (afid, " / ".join(AFIDS[afid]))
    return "AFID %d (not in the AFID 15-34 table)" % afid


def export_and_decode(amd_smi, folder):
    """Exports the CPER records into folder and decodes them; folder must exist and be private."""
    command = [amd_smi, "ras", "--cper", "--severity", "all", "--gpu", "all", "--folder", folder]
    code, out, err = run(command, EXPORT_TIMEOUT)
    files = sorted(glob.glob(os.path.join(folder, "**", "*.cper"), recursive=True))
    cper = {"exportCommand": " ".join(["amd-smi"] + command[1:-1] + ["<private directory>"]),
            "exportExit": code, "records": len(files), "afidUnsupported": False, "afidError": "",
            "counts": {UNCORRECTED: 0, FATAL: 0, CORRECTED: 0, UNKNOWN: 0}, "undecoded": 0, "lines": []}
    if code != 0:
        cper["state"] = "export_failed"
        cper["exportError"] = ((err or out).strip() or "amd-smi exited %s" % code)[-400:]
        return cper
    cper["state"] = "exported"
    deadline = time.monotonic() + DECODE_BUDGET
    for index, path in enumerate(files):
        afids, error = [], ""
        if cper["afidUnsupported"]:
            error = "not decoded"
        elif index >= DECODE_LIMIT or time.monotonic() > deadline:
            error = "not decoded: past the decoding limit"
        else:
            afids, error, unsupported = decode_afids(amd_smi, path)
            if unsupported:
                cper["afidUnsupported"], cper["afidError"] = True, error
        if not afids:
            cper["undecoded"] += 1
        severity = record_severity(path, afids) or UNKNOWN
        cper["counts"][severity] += 1
        detail = ", ".join(describe(afid) for afid in afids) or error
        cper["lines"].append("%s: %s, %s" % (os.path.relpath(path, folder), severity, detail))
    return cper


def main():
    # Finish (each call is bounded) and clean up even when the Ansible session goes away.
    signal.signal(signal.SIGHUP, signal.SIG_IGN)
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default="")
    parser.add_argument("--gpu", action="append", default=[], help="a GPU's PCI address; repeat for each")
    parser.add_argument("--amd-smi", default="")
    parser.add_argument("--dmesg", default="dmesg")
    parser.add_argument("--max-correctable", type=int, default=-1, help="for the measurements' expected values")
    args = parser.parse_args()
    root = args.root.rstrip("/")
    gpus = [gpu_ras(root, bdf) for bdf in args.gpu]
    driven = [gpu for gpu in gpus if gpu["driver"] == "amdgpu"]

    report = {
        "gpus": len(gpus),
        "rasEnabled": [gpu["bdf"] for gpu in driven if gpu["ras"] and gpu["mask"]],
        "rasMissing": [gpu["bdf"] for gpu in gpus if gpu["driver"] != "amdgpu" or not gpu["ras"]],
        "rasOff": [gpu["bdf"] for gpu in driven if gpu["ras"] and not gpu["mask"]],
    }
    lines = []
    for gpu in gpus:
        if gpu["driver"] != "amdgpu":
            lines.append("%s: driven by %s, not amdgpu" % (gpu["bdf"], gpu["driver"] or "no driver"))
        elif not gpu["ras"]:
            lines.append("%s: no RAS interface (ras/ is missing)" % gpu["bdf"])
        else:
            lines.append("%s: feature mask %s" % (gpu["bdf"], "unreadable" if gpu["mask"] is None else hex(gpu["mask"])))
    report["rasEvidence"] = bounded(lines + ["kernel log:"] + kernel_log(args.dmesg))

    counted = [gpu for gpu in driven if gpu["blocks"]]
    measurements, lines = [], []
    for gpu in counted:
        gpu["ue"] = sum(counts.get("ue", 0) for counts in gpu["blocks"].values())
        gpu["ce"] = sum(counts.get("ce", 0) for counts in gpu["blocks"].values())
        blocks = "; ".join("%s %s" % (block, " ".join("%s %d" % item for item in sorted(counts.items())))
                           for block, counts in sorted(gpu["blocks"].items()))
        lines.append("%s: ue %d, ce %d (%s)" % (gpu["bdf"], gpu["ue"], gpu["ce"], blocks))
        if gpu["aca"]:
            lines.append("%s: aca_umc %s" % (gpu["bdf"], gpu["aca"]))
        measurements.append({"name": "uncorrectable_errors[%s]" % gpu["bdf"], "value": gpu["ue"], "unit": "count", "expected": "= 0"})
        measurements.append({"name": "correctable_errors[%s]" % gpu["bdf"], "value": gpu["ce"], "unit": "count",
                             "expected": "<= %d" % args.max_correctable if args.max_correctable >= 0 else ""})
    if len(measurements) > MEASUREMENT_LIMIT:
        # Too many GPUs for one measurement each: the totals and the worst GPU instead.
        worst = max(counted, key=lambda gpu: gpu["ce"])
        measurements = [
            {"name": "uncorrectable_errors", "value": sum(gpu["ue"] for gpu in counted), "unit": "count", "expected": "= 0"},
            {"name": "correctable_errors", "value": sum(gpu["ce"] for gpu in counted), "unit": "count", "expected": ""},
            {"name": "correctable_errors[%s]" % worst["bdf"], "value": worst["ce"], "unit": "count",
             "expected": "<= %d" % args.max_correctable if args.max_correctable >= 0 else ""},
        ]
    report.update({
        "eccGpus": len(counted),
        "ueGpus": [gpu["bdf"] for gpu in counted if gpu["ue"] > 0],
        "ueTotal": sum(gpu["ue"] for gpu in counted),
        "ceMax": max([gpu["ce"] for gpu in counted] or [0]),
        "ceMaxGpu": max(counted, key=lambda gpu: gpu["ce"])["bdf"] if counted else "",
        "eccEvidence": bounded(lines),
        "eccMeasurements": measurements,
    })

    rings = cper_rings(root, [gpu["bdf"] for gpu in driven])
    cper = {"ringGpus": rings or []}
    amd_smi = find_amd_smi(args.amd_smi)
    if rings is None:
        cper["state"] = "debugfs_unavailable"
    elif not rings:
        cper["state"] = "no_ring"
    elif not amd_smi:
        cper["state"] = "amd_smi_missing"
    else:
        folder = tempfile.mkdtemp(prefix="swallow-cper-")
        try:
            cper.update(export_and_decode(amd_smi, folder))
        finally:
            shutil.rmtree(folder, ignore_errors=True)
    counts = cper.pop("counts", {})
    cper.update({"fatal": counts.get(FATAL, 0), "uncorrected": counts.get(UNCORRECTED, 0),
                 "corrected": counts.get(CORRECTED, 0), "unknown": counts.get(UNKNOWN, 0)})
    cper["evidence"] = bounded(cper.pop("lines", []))
    report["cper"] = cper
    print(json.dumps(report))


if __name__ == "__main__":
    main()
