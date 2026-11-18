#!/usr/bin/env python3
"""Stress this host's AMD GPUs with open-source tools in place of AMD's AGFHC, for the amd-gpu-stress
Validation Suite (docs/development/validation.md §7), and print {"checks": [...]}: one result per
check, shaped as swallow_validation_results. It always exits 0.

What it runs, in this order, from a private directory it creates and always removes:

- The amd-gpu-ras helper (passed in, base64) reads RAS and CPER before the stress and after it;
  ras-after-stress fails on any new uncorrectable error or fatal or uncorrected CPER record.
- The ROCm Validation Suite (RVS) release for the host's ROCm major version, downloaded from the URL
  swallow_artifacts gives and checked against its SHA-256, unpacked here and owned by root (RVS
  loads only root-owned modules). Its per-SKU conf directory (conf/<profile>/) holds AMD's pass
  targets; nothing here hard-codes a threshold. gst (gemm) and iet (power) need the SKU's own conf;
  mem, babel, pebb, and pbqt fall back to RVS's generic conf when the SKU has none.
- rccl-tests, from a pinned source archive, built here with the host's hipcc and RCCL
  (MPI=0) and run as all_reduce_perf over every GPU (collectives).
- AMD SMI, sampled while the stress runs (thermal-throttle) and read for XGMI errors and link status
  (xgmi-links).

level quick keeps every action and target of the SKU's conf and shortens only how long each runs.
Every tool runs with a timeout in its own process group; SIGHUP is ignored and SIGTERM stops the
running tool, so a cancelled run still removes the private directory. --root prefixes sysfs paths
and the tool options name the tools, for tests.
"""
import argparse
import base64
import glob
import hashlib
import http.client
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.request

CHECKS = ("gemm", "hbm-integrity", "hbm-bandwidth", "pcie-bandwidth", "xgmi-bandwidth", "xgmi-links", "power",
          "collectives", "thermal-throttle", "ras-after-stress")
# The checks that need RVS; xgmi-links, collectives, and the telemetry do not.
RVS_CHECKS = ("gemm", "hbm-integrity", "hbm-bandwidth", "pcie-bandwidth", "xgmi-bandwidth", "power")

# Each RVS stage: the check it judges, its conf file, and whether only the SKU's conf will do
# (it holds the pass target).
STAGES = (
    ("gemm", "gst_single.conf", True),
    ("power", "iet_stress.conf", True),
    ("hbm-integrity", "mem.conf", False),
    ("hbm-bandwidth", "babel.conf", False),
    ("pcie-bandwidth", "pebb_single.conf", False),
    ("xgmi-bandwidth", "pbqt_single.conf", False),
)

# level quick: the most each action may run (ms), repeat, or iterate. Power keeps its ramp and three
# minutes at load: iet judges the average power, which 5 s never reached and 60 s missed on one of
# four MI250X packages in one of two runs (2026-11-19).
QUICK = {"duration": 5000, "count": 1, "num_passes": 5, "num_iter": 500}
QUICK_BY_CHECK = {"power": {"duration": 180000, "count": 1}}

EVIDENCE_LIMIT = 7800
SUMMARY_LIMIT = 900
MEASUREMENT_LIMIT = 20
DOWNLOAD_TIMEOUT = 600
SMALL_TIMEOUT = 60
BUILD_TIMEOUT = 900
SAMPLE_INTERVAL = 2.0
# What the deadline keeps back for the reads after the stress (XGMI, RAS) and the report.
RESERVE = 300

# The tool the main thread is running, whose process group is stopped when the helper is told to
# stop. The sampler thread's amd-smi calls are never recorded here, so they cannot hide the tool.
CURRENT = {"process": None}


class Unjudged(Exception):
    """A precondition that keeps checks from being judged: an error whose summary starts with code."""

    def __init__(self, code, message, evidence=""):
        super().__init__(message)
        self.code, self.message, self.evidence = code, message, evidence

    def summary(self):
        return "[%s] %s" % (self.code, self.message)


def bounded(lines, limit=EVIDENCE_LIMIT):
    """Joins lines up to limit UTF-8 bytes (the API bounds evidence in bytes) and says how many did not fit."""
    kept, size = [], 0
    for index, line in enumerate(lines):
        length = len(line.encode("utf-8")) + 1
        if size + length > limit:
            kept.append("... %d more" % (len(lines) - index))
            break
        kept.append(line)
        size += length
    return "\n".join(kept)


def tail(text, limit=2000):
    text = text.strip()
    return text if len(text) <= limit else "..." + text[-limit:]


def run(command, timeout, env=None, cwd=None, log=None):
    """Runs command in its own process group and returns (exit code or None when it did not finish,
    output). With log, the output goes to that file and is read back from it."""
    out = open(log, "w+", encoding="utf-8", errors="replace") if log else subprocess.PIPE
    try:
        process = subprocess.Popen(command, stdout=out, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL,
                                   env=env, cwd=cwd, start_new_session=True, text=True, errors="replace")
    except OSError as error:
        if log:
            out.close()
        return None, str(error)
    main = threading.current_thread() is threading.main_thread()
    if main:
        CURRENT["process"] = process
    try:
        output, _ = process.communicate(timeout=max(timeout, 1))
        code = process.returncode
    except subprocess.TimeoutExpired:
        stop(process)
        output, _ = process.communicate()
        code = None
    finally:
        if main:
            CURRENT["process"] = None
    if log:
        out.seek(0)
        output = out.read()
        out.close()
    return code, output or ""


def stage_timeout(args):
    """How long the next stage may run: its own timeout, cut to what the run's deadline leaves."""
    if args.deadline_at is None:
        return args.stage_timeout
    return int(min(args.stage_timeout, args.deadline_at - time.monotonic() - RESERVE))


def stop(process):
    """Stops a tool and everything it started."""
    for sig in (signal.SIGTERM, signal.SIGKILL):
        try:
            os.killpg(process.pid, sig)
        except (ProcessLookupError, PermissionError):
            return
        try:
            process.wait(timeout=10)
            return
        except subprocess.TimeoutExpired:
            continue


def clip(text, limit):
    """text cut to at most limit UTF-8 bytes, on a character boundary."""
    raw = text.encode("utf-8")
    return text if len(raw) <= limit else raw[:limit].decode("utf-8", "ignore")


def result(target, check, verdict, summary, evidence="", measurements=None):
    """One result as validation_report records it, inside the API's bounds (validation.md §4.2)."""
    return {"check": check, "target": {"kind": "server", "id": target}, "verdict": verdict,
            "summary": clip(summary, SUMMARY_LIMIT), "evidence": clip(evidence, EVIDENCE_LIMIT),
            "measurements": (measurements or [])[:MEASUREMENT_LIMIT]}


def measure(name, value, unit, expected=""):
    return {"name": name, "value": value, "unit": unit, "expected": expected}


# ---------------------------------------------------------------- RAS


def read_ras(args, work, label):
    """Runs the amd-gpu-ras helper and returns its report."""
    helper = os.path.join(work, "amd_gpu_ras.py")
    if not os.path.exists(helper):
        with open(helper, "wb") as handle:
            handle.write(base64.b64decode(args.ras_helper))
    command = [sys.executable, helper, "--root=" + args.root, "--dmesg=" + args.dmesg]
    if args.amd_smi:
        command.append("--amd-smi=" + args.amd_smi)
    command += ["--gpu=" + gpu for gpu in args.gpu]
    code, output = run(command, 900)
    try:
        return json.loads(output)
    except ValueError:
        raise Unjudged("ras_unreadable", "the RAS state %s the stress could not be read" % label, tail(output))


def ras_delta(before, after):
    """What changed between two RAS readings: (failures, notes, measurements, evidence lines)."""
    failures, notes, lines = [], [], []
    new_ue, new_ce = {}, {}
    for bdf, counts in sorted(after.get("counts", {}).items()):
        was = before.get("counts", {}).get(bdf, {"ue": 0, "ce": 0})
        new_ue[bdf] = counts["ue"] - was.get("ue", 0)
        new_ce[bdf] = counts["ce"] - was.get("ce", 0)
        lines.append("%s: ue %d -> %d, ce %d -> %d" % (bdf, was.get("ue", 0), counts["ue"], was.get("ce", 0), counts["ce"]))
    if not after.get("counts"):
        notes.append("no GPU exposes RAS error counts")
    ue_gpus = [bdf for bdf, value in new_ue.items() if value > 0]
    if ue_gpus:
        failures.append("new uncorrectable errors on " + " ".join(ue_gpus))
    cper_before, cper_after = before.get("cper", {}), after.get("cper", {})
    cper = {}
    if cper_before.get("state") == "exported" and cper_after.get("state") == "exported":
        for key in ("fatal", "uncorrected", "corrected", "unknown"):
            cper[key] = max(cper_after.get(key, 0) - cper_before.get(key, 0), 0)
        lines.append("CPER records before: %d, after: %d" % (cper_before.get("records", 0), cper_after.get("records", 0)))
        if cper["fatal"] or cper["uncorrected"]:
            failures.append("%d new fatal and %d new uncorrected CPER records" % (cper["fatal"], cper["uncorrected"]))
        if cper_after.get("evidence"):
            lines.append("CPER records after the stress:")
            lines.append(cper_after["evidence"])
    else:
        state = cper_after.get("state") or cper_before.get("state") or "unknown"
        notes.append("CPER records not compared (%s)" % state.replace("_", " "))
    measurements = [measure("new_uncorrectable_errors", sum(max(v, 0) for v in new_ue.values()), "count", "= 0"),
                    measure("new_correctable_errors_max_per_gpu", max([0] + list(new_ce.values())), "count", "")]
    if cper:
        measurements.append(measure("new_cper_fatal_or_uncorrected", cper["fatal"] + cper["uncorrected"], "count", "= 0"))
        measurements.append(measure("new_cper_corrected", cper["corrected"], "count", ""))
    return failures, notes, measurements, lines, new_ce, cper


# ---------------------------------------------------------------- AMD SMI


def amd_smi_json(args, *arguments):
    """Runs amd-smi with --json and returns the parsed output, or None."""
    if not args.amd_smi_path:
        return None
    code, output = run([args.amd_smi_path] + list(arguments) + ["--json"], SMALL_TIMEOUT)
    if code != 0:
        return None
    start = min([i for i in (output.find("{"), output.find("[")) if i >= 0] or [-1])
    if start < 0:
        return None
    try:
        return json.loads(output[start:])
    except ValueError:
        return None


def gpu_list(data):
    """The per-GPU entries of an amd-smi JSON document (a list, or a dict holding gpu_data)."""
    if isinstance(data, dict):
        data = data.get("gpu_data", [])
    return [entry for entry in data or [] if isinstance(entry, dict)]


def number(value):
    """A number from an amd-smi value ({"value": 5, "unit": "C"}, 5, "5"), or None for N/A."""
    if isinstance(value, dict):
        value = value.get("value")
    if isinstance(value, bool):
        return None
    if isinstance(value, (int, float)):
        return value
    try:
        return float(str(value))
    except ValueError:
        return None


def device_limits(args):
    """The temperature limits the GPUs report (slowdown and shutdown, by sensor), for the evidence."""
    found = set()
    for entry in gpu_list(amd_smi_json(args, "static", "--limit")):
        limits = entry.get("limit") or {}
        found.add(", ".join("%s %g C" % (key.replace("_temperature", "").replace("_", " "), number(value))
                            for key, value in sorted(limits.items())
                            if key.endswith("_temperature") and number(value) is not None))
    return "; ".join(sorted(item for item in found if item))


class Sampler(threading.Thread):
    """Samples temperature, power, throttle status, and the throttle accumulators of every GPU while
    the stress runs."""

    def __init__(self, args, names):
        super().__init__(daemon=True)
        self.args, self.names = args, names
        self.done = threading.Event()
        self.samples = 0
        self.max_temp = {}        # GPU -> (temperature, sensor)
        self.max_power = {}
        self.throttled = {}       # GPU -> samples whose throttle_status is not UNTHROTTLED
        self.first, self.last = {}, {}   # GPU -> accumulators
        self.errors = []
        self.limits = device_limits(args)

    def run(self):
        while not self.done.is_set():
            data = amd_smi_json(self.args, "metric", "-p", "-t", "-v")
            if data is None:
                self.errors.append(time.strftime("%H:%M:%S") + " amd-smi metric did not answer")
            else:
                self.samples += 1
                for entry in gpu_list(data):
                    self.take(self.names.get(entry.get("gpu"), str(entry.get("gpu"))), entry)
            self.done.wait(SAMPLE_INTERVAL)

    def take(self, gpu, entry):
        for sensor, value in (entry.get("temperature") or {}).items():
            reading = number(value)
            if reading is not None and reading > self.max_temp.get(gpu, (-1, ""))[0]:
                self.max_temp[gpu] = (reading, sensor)
        power = entry.get("power") or {}
        watts = number(power.get("socket_power"))
        if watts is not None:
            self.max_power[gpu] = max(self.max_power.get(gpu, 0), watts)
        status = str(power.get("throttle_status", ""))
        if status and status not in ("UNTHROTTLED", "N/A"):
            self.throttled[gpu] = self.throttled.get(gpu, 0) + 1
        accumulated = {key: number(value) for key, value in (entry.get("throttle") or {}).items()
                       if key.endswith("_accumulated") and number(value) is not None}
        if accumulated:
            self.first.setdefault(gpu, accumulated)
            self.last[gpu] = accumulated

    def stop(self):
        self.done.set()
        self.join(timeout=SMALL_TIMEOUT + 5)


# ---------------------------------------------------------------- RVS


def rocm_major(rocm):
    """The major version of the ROCm at rocm, from .info/version, or None."""
    try:
        with open(os.path.join(rocm, ".info", "version"), encoding="utf-8") as handle:
            version = handle.read().strip()
    except OSError:
        return None, ""
    match = re.match(r"(\d+)", version)
    return (match.group(1), version) if match else (None, version)


def download(url, sha256, dest):
    """Downloads url to dest and checks its SHA-256."""
    digest = hashlib.sha256()
    try:
        with urllib.request.urlopen(url, timeout=DOWNLOAD_TIMEOUT) as response, open(dest, "wb") as out:
            while True:
                chunk = response.read(1 << 20)
                if not chunk:
                    break
                digest.update(chunk)
                out.write(chunk)
    except (OSError, ValueError, http.client.HTTPException) as error:
        raise Unjudged("artifact_download_failed", "%s could not be downloaded: %s" % (os.path.basename(dest), error))
    if digest.hexdigest() != sha256.lower():
        raise Unjudged("artifact_download_failed", "%s does not have its declared SHA-256" % os.path.basename(dest),
                       "got %s, want %s" % (digest.hexdigest(), sha256.lower()))


def unpack(archive, dest):
    """Unpacks archive into dest, owned by root when the helper runs as root."""
    os.makedirs(dest, exist_ok=True)
    try:
        with tarfile.open(archive) as tar:
            if hasattr(tarfile, "data_filter"):
                tar.extractall(dest, filter="data")
            else:
                for member in tar.getmembers():
                    if member.name.startswith("/") or ".." in member.name.split("/"):
                        raise tarfile.TarError("unsafe path " + member.name)
                tar.extractall(dest)
    except (OSError, tarfile.TarError) as error:
        raise Unjudged("artifact_unpack_failed", "%s could not be unpacked: %s" % (os.path.basename(archive), error))
    if os.geteuid() == 0:
        for base, dirs, files in os.walk(dest):
            for name in dirs + files:
                os.lchown(os.path.join(base, name), 0, 0)
        os.lchown(dest, 0, 0)


def rvs_env(rvs, rocm):
    env = dict(os.environ)
    env["LD_LIBRARY_PATH"] = ":".join([os.path.join(rvs, "lib"), os.path.join(rocm, "lib")]
                                      + ([env["LD_LIBRARY_PATH"]] if env.get("LD_LIBRARY_PATH") else []))
    return env


def missing_libraries(args, rvs, env):
    """The shared libraries RVS needs that the host's ROCm does not provide."""
    files = [os.path.join(rvs, "bin", "rvs")] + sorted(glob.glob(os.path.join(rvs, "lib", "rvs", "*.so*")))
    missing = set()
    for path in files:
        code, output = run([args.ldd, path], SMALL_TIMEOUT, env=env)
        missing.update(re.findall(r"^\s*(\S+)\s*=>\s*not found", output, re.M))
    return sorted(missing)


def rvs_gpus(output):
    """RVS's GPU ids by PCI address, from rvs -g."""
    return {bdf.lower(): gid for bdf, gid in re.findall(r"([0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.\d)\s*-\s*GPU\[\s*\d+\s*-\s*(\d+)\]", output)}


def identity(args):
    """The strings that name this host's GPU SKU: AMD SMI's market name and VBIOS, and sysfs product_name."""
    found = []
    data = amd_smi_json(args, "static", "--asic", "--vbios")
    for entry in gpu_list(data):
        for section in ("asic", "vbios"):
            for key, value in (entry.get(section) or {}).items():
                if key in ("market_name", "name", "part_number") and isinstance(value, str):
                    found.append(value)
    for path in glob.glob(args.root + "/sys/class/drm/card*/device/product_name"):
        try:
            with open(path, encoding="utf-8", errors="replace") as handle:
                found.append(handle.read().strip())
        except OSError:
            pass
    return sorted(set(value for value in found if value and value != "N/A"))


def choose_profile(conf, names, given):
    """The RVS conf directory for this SKU: the given one, else the longest directory name that one of
    names holds (ignoring case and spaces)."""
    profiles = sorted(entry for entry in os.listdir(conf) if os.path.isdir(os.path.join(conf, entry)))
    if given:
        if given not in profiles:
            raise Unjudged("rvs_profile_unknown", "RVS has no conf directory %s" % given,
                           "RVS conf directories: " + " ".join(profiles))
        return given
    haystack = [re.sub(r"\s+", "", name).upper() for name in names]
    matches = [profile for profile in profiles if any(profile.upper() in hay for hay in haystack)]
    longest = max((len(profile) for profile in matches), default=0)
    best = sorted(profile for profile in matches if len(profile) == longest)
    if len(best) != 1:
        raise Unjudged("rvs_profile_unknown",
                       "the GPU's SKU matches %s RVS conf directory; set the parameter rvsProfile"
                       % ("no" if not best else "more than one"),
                       "GPU identity: %s\nmatches: %s\nRVS conf directories: %s"
                       % ("; ".join(names) or "none", " ".join(best) or "none", " ".join(profiles)))
    return best[0]


def quick_conf(source, dest, caps):
    """Writes source with every action's run length capped by caps (level quick); actions and targets stay."""
    pattern = re.compile(r"^(\s*-?\s*)(%s)(\s*:\s*)(\d+)(.*)$" % "|".join(caps))
    lines = []
    with open(source, encoding="utf-8") as handle:
        for line in handle:
            match = pattern.match(line.rstrip("\n"))
            if match and int(match.group(4)) > caps[match.group(2)]:
                line = "%s%s%s%d%s\n" % (match.group(1), match.group(2), match.group(3), caps[match.group(2)], match.group(5))
            lines.append(line)
    with open(dest, "w", encoding="utf-8") as handle:
        handle.writelines(lines)


def actions(conf):
    """The action names of an RVS conf."""
    with open(conf, encoding="utf-8") as handle:
        return re.findall(r"^\s*-\s*name\s*:\s*(\S+)", handle.read(), re.M)


# A [RESULT] line: its action (the first bracket after the timestamp) and the rest.
RESULT = re.compile(r"^\[RESULT\]\s*\[\s*[\d.]+\s*\]\s*\[(?P<action>[^\]]+)\]\s*(?P<rest>.*)$")
# The GPUs a line names: "[GPU:: 51627]" or "[GPU:: 2 - 51627 - 0000:32:00.0]".
GPU_TAG = re.compile(r"\[GPU::\s*([^\]]+)\]")
BDF = re.compile(r"[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.\d")
# A row of RVS's closing summary table: | action | MODULE | PASS |, colour codes and all.
SUMMARY_ROW = re.compile(r"^\|\s*(\S+)\s*\|\s*([A-Z][A-Z0-9_]*)\s*\|\s*(?:\x1b\[[0-9;]*m)?\s*(PASS|FAIL)\b", re.M)
# Bandwidth figures: "31.290 GBps", "12.5 GB/s".
GBPS = re.compile(r"([\d.]+)\s*GB(?:ps|/s)\b")
# A babel table row: GPU id, function, then MiB/s now, max, min, average.
BABEL_ROW = re.compile(r"^\s*(\d+)\s+(Copy|Mul|Add|Triad|Dot)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*$", re.M)
MIB_TO_GB = 1.048576 / 1000


def gpu_name(tag, names):
    """A GPU as its PCI address when the tag or the RVS id says it, else as RVS names it."""
    bdf = BDF.search(tag)
    if bdf:
        return bdf.group(0).lower()
    gid = tag.strip().split()[-1]
    return names.get(gid, "GPU " + gid)


def result_lines(log, names):
    """Each [RESULT] line as (action, the GPUs it names, the rest)."""
    found = []
    for line in log.splitlines():
        match = RESULT.match(line.strip())
        if match:
            gpus = [gpu_name(tag, names) for tag in GPU_TAG.findall(match.group("rest"))]
            found.append((match.group("action"), gpus, match.group("rest")))
    return found


def summary_rows(log):
    """RVS's own verdict for each action, from its closing summary table."""
    return {action: verdict == "PASS" for action, _, verdict in SUMMARY_ROW.findall(log)}


def judge_gemm(log, names):
    """gst: each action's final line per GPU says GFLOPS, the target, and met: TRUE or FALSE."""
    finals = {}
    for action, gpus, rest in result_lines(log, names):
        match = re.search(r"GFLOPS\s+([\d.]+)\s+Target GFLOPS:\s*([\d.]+)\s+met:\s*(TRUE|FALSE)", rest, re.I)
        if match and gpus:
            finals[(action, gpus[0])] = (float(match.group(1)), float(match.group(2)), match.group(3).upper() == "TRUE")
    lines, measurements, failed = [], [], []
    for action in sorted({action for action, _ in finals}):
        rows = {gpu: value for (name, gpu), value in finals.items() if name == action}
        for gpu, (gflops, target, met) in sorted(rows.items()):
            lines.append("%s %s: %.0f GFLOPS, target %.0f, %s" % (action, gpu, gflops, target, "met" if met else "NOT met"))
            if not met:
                failed.append("%s on %s (%.0f of %.0f GFLOPS)" % (action, gpu, gflops, target))
        low = min(rows.values(), key=lambda value: value[0])
        measurements.append(measure("gflops_min[%s]" % action, round(low[0]), "GFLOPS", "RVS target %.0f" % low[1]))
    return finals, failed, lines, measurements


def judge_bandwidth(check, log, names):
    """babel, pebb, pbqt: the lowest figure of each action, as (lines, measurements, lowest by action)."""
    lowest, lines = {}, []
    if check == "hbm-bandwidth":
        rows = BABEL_ROW.findall(log)
        actions_seen = [action for action, _, _ in result_lines(log, names)] or ["babel"]
        for gid, function, _, _, _, average in rows:
            value = float(average) * MIB_TO_GB
            gpu = names.get(gid, "GPU " + gid)
            key = "%s %s" % (actions_seen[0], function)
            if key not in lowest or value < lowest[key][0]:
                lowest[key] = (value, gpu)
        for key, (value, gpu) in sorted(lowest.items()):
            lines.append("%s: lowest average %.1f GB/s (%s)" % (key, value, gpu))
    else:
        for action, gpus, rest in result_lines(log, names):
            figure = GBPS.search(rest)
            if figure:
                value = float(figure.group(1))
                if action not in lowest or value < lowest[action][0]:
                    lowest[action] = (value, " to ".join(gpus) or "?")
        for action, (value, where) in sorted(lowest.items()):
            lines.append("%s: lowest %.1f GB/s (%s)" % (action, value, where))
    return lines, lowest


def judge_stage(check, conf, code, log, names, gpus, args):
    """Judges one RVS stage from its log: (verdict, summary, evidence, measurements). RVS's own
    summary table decides pass or fail for each action; a minimum bandwidth the run gives is judged
    on top of it."""
    if code is None:
        return ("error", "[stress_timeout] RVS %s did not finish in the time it had" % os.path.basename(conf), tail(log), [])
    expected = actions(conf)
    rows = summary_rows(log)
    failed_actions = sorted(action for action, ok in rows.items() if not ok)
    unreported = [action for action in expected if action not in rows]
    lines, measurements, problems = [], [], []
    if check == "gemm":
        finals, failed, lines, measurements = judge_gemm(log, names)
        problems += failed
    elif check in ("power", "hbm-integrity"):
        peak = {}
        for action, gpus_named, rest in result_lines(log, names):
            match = re.search(r"Power\(W\)\s*([\d.]+)", rest)
            if match and gpus_named:
                peak[gpus_named[0]] = max(peak.get(gpus_named[0], 0.0), float(match.group(1)))
            if re.search(r"\b(FAIL|pass:\s*FALSE)\b", rest, re.I):
                lines.append("%s %s: %s" % (action, " ".join(gpus_named) or "-", GPU_TAG.sub("", rest).strip()[:200]))
        if peak:
            # Where two GPUs share a package (MI250X), only one of them reports the package's power.
            own = {gpu: watts for gpu, watts in peak.items() if watts > 0}
            lines = ["peak power: " + ", ".join("%s %.0f W" % item for item in sorted(peak.items()))] + lines
            if own:
                measurements = [measure("peak_power_lowest", round(min(own.values())), "W", "RVS target_power"),
                                measure("peak_power_highest", round(max(own.values())), "W", ""),
                                measure("gpus_reporting_power", len(own), "count", "")]
    else:
        minimum = {"hbm-bandwidth": args.min_hbm_gbps, "pcie-bandwidth": args.min_pcie_gbps,
                   "xgmi-bandwidth": args.min_xgmi_gbps}[check]
        lines, lowest = judge_bandwidth(check, log, names)
        for key, (value, where) in sorted(lowest.items()):
            measurements.append(measure("gbps_min[%s]" % key, round(value, 1), "GB/s", (">= %g" % minimum) if minimum >= 0 else ""))
            if minimum >= 0 and value < minimum:
                problems.append("%s %.1f GB/s (%s), below %g" % (key, value, where, minimum))
        if not lowest and not failed_actions:
            unreported = unreported or ["any bandwidth"]
    lines = ["RVS verdicts: " + (", ".join("%s %s" % (action, "PASS" if ok else "FAIL") for action, ok in rows.items()) or "none")] + lines
    module = os.path.basename(conf)
    if failed_actions or problems:
        return ("failed", "; ".join(filter(None, [("RVS failed " + ", ".join(failed_actions)) if failed_actions else "",
                                                    "; ".join(problems)])),
                bounded(lines), measurements)
    if unreported:
        return ("error", "[rvs_result_missing] RVS %s gave no verdict for %s (exit %s)" % (module, ", ".join(unreported), code),
                bounded(lines + [tail(log)]), measurements)
    if check in ("hbm-bandwidth", "pcie-bandwidth", "xgmi-bandwidth"):
        minimum = {"hbm-bandwidth": args.min_hbm_gbps, "pcie-bandwidth": args.min_pcie_gbps, "xgmi-bandwidth": args.min_xgmi_gbps}[check]
        low = min(measurements, key=lambda m: m["value"]) if measurements else None
        return ("passed", "RVS passed %d actions on %d GPUs; lowest %s%s" % (
            len(rows), gpus, ("%s GB/s (%s)" % (low["value"], low["name"])) if low else "?",
            (", at least %g GB/s" % minimum) if minimum >= 0 else "; no minimum given, so the bandwidth is recorded, not judged"),
            bounded(lines), measurements)
    return "passed", "RVS passed %d actions on %d GPUs (%s)" % (len(rows), gpus, ", ".join(rows)), bounded(lines), measurements


# ---------------------------------------------------------------- rccl-tests


def collectives(args, work, rocm, gpus):
    """Builds rccl-tests and runs all_reduce_perf over every GPU: (verdict, summary, evidence, measurements)."""
    hipcc = os.path.join(rocm, "bin", "hipcc")
    headers = glob.glob(os.path.join(rocm, "include", "rccl", "rccl.h"))
    library = glob.glob(os.path.join(rocm, "lib", "librccl.so*"))
    make = shutil.which(args.make) or (args.make if os.path.isfile(args.make) else None)
    missing = [what for what, ok in (("hipcc", os.path.exists(hipcc)), ("rccl.h", headers), ("librccl.so", library), ("make", make)) if not ok]
    if missing:
        return ("error", "[rccl_build_unavailable] rccl-tests cannot be built on this host: no " + ", ".join(missing),
                "looked in %s" % rocm, [])
    archive = os.path.join(work, "rccl-tests.tar.gz")
    download(args.rccl_tests_url, args.rccl_tests_sha256, archive)
    source = os.path.join(work, "rccl-tests")
    unpack(archive, source)
    tops = [path for path in glob.glob(os.path.join(source, "*")) if os.path.isfile(os.path.join(path, "Makefile"))]
    top = tops[0] if tops else source
    env = dict(os.environ)
    env["PATH"] = os.path.join(rocm, "bin") + ":" + env.get("PATH", "")
    code, output = run([make, "-C", top, "MPI=0", "HIP_HOME=" + rocm, "NCCL_HOME=" + rocm, "-j%d" % min(os.cpu_count() or 1, 32)],
                       min(BUILD_TIMEOUT, stage_timeout(args)), env=env, log=os.path.join(work, "rccl-build.log"))
    binary = os.path.join(top, "build", "all_reduce_perf")
    if code != 0 or not os.path.exists(binary):
        return ("error", "[rccl_build_failed] rccl-tests did not build (make exit %s)" % code, tail(output), [])
    env["LD_LIBRARY_PATH"] = os.path.join(rocm, "lib") + (":" + env["LD_LIBRARY_PATH"] if env.get("LD_LIBRARY_PATH") else "")
    command = [binary, "-b", "256M", "-e", "256M", "-f", "2", "-g", str(gpus), "-d", "half"]
    limit = stage_timeout(args)
    if limit <= 0:
        return "error", "[stress_timeout] the run's time ran out before all_reduce_perf could start", "", []
    code, output = run(command, limit, env=env, log=os.path.join(work, "rccl.log"))
    if code is None:
        return "error", "[stress_timeout] all_reduce_perf did not finish in the time it had", tail(output), []
    busbw = re.search(r"Avg bus bandwidth\s*:\s*([\d.]+)", output)
    wrong = re.search(r"Out of bounds values\s*:\s*(\d+)", output)
    errors = int(wrong.group(1)) if wrong else 0
    evidence = "all_reduce_perf -b 256M -e 256M -f 2 -g %d -d half\n%s" % (gpus, tail(output, 3000))
    if code != 0 or not busbw:
        return "failed", "all_reduce_perf over %d GPUs failed (exit %s)" % (gpus, code), evidence, []
    value = float(busbw.group(1))
    measurements = [measure("busbw", round(value, 1), "GB/s", (">= %g" % args.min_busbw_gbps) if args.min_busbw_gbps >= 0 else ""),
                    measure("wrong_values", errors, "count", "= 0")]
    if errors:
        return "failed", "all_reduce_perf over %d GPUs returned %d wrong values" % (gpus, errors), evidence, measurements
    if args.min_busbw_gbps >= 0 and value < args.min_busbw_gbps:
        return ("failed", "all_reduce over %d GPUs: %.1f GB/s bus bandwidth, below %g" % (gpus, value, args.min_busbw_gbps),
                evidence, measurements)
    return ("passed", "all_reduce over %d GPUs: %.1f GB/s bus bandwidth, no wrong value%s" % (
        gpus, value, "" if args.min_busbw_gbps >= 0 else " (no minimum given; recorded, not judged)"), evidence, measurements)


# ---------------------------------------------------------------- XGMI and telemetry


def xgmi_errors(args, names):
    """Each GPU's XGMI error status since the last read (reading clears it), or {} when not reported."""
    statuses = {}
    for entry in gpu_list(amd_smi_json(args, "metric", "-x")):
        status = entry.get("xgmi_err")
        if isinstance(status, str) and status != "N/A":
            statuses[names.get(entry.get("gpu"), str(entry.get("gpu")))] = status
    return statuses


def judge_xgmi(args, names, gpus, before):
    after = xgmi_errors(args, names)
    data = amd_smi_json(args, "xgmi") or {}
    links, down = [], []
    for entry in (data.get("link_status") or []) if isinstance(data, dict) else []:
        gpu = entry.get("bdf") or names.get(entry.get("gpu"), str(entry.get("gpu")))
        states = [str(state) for state in entry.get("link_status") or []]
        known = [state for state in states if state not in ("N/A", "SELF")]
        if known:
            links.append("%s: %s" % (gpu, " ".join(states)))
            down += ["%s link %d" % (gpu, i) for i, state in enumerate(states) if state.upper() in ("D", "DOWN", "DISABLED")]
    if gpus < 2:
        return "skipped", "one GPU, no XGMI peer", "", []
    if not after and not links:
        return "skipped", "AMD SMI reports neither XGMI errors nor XGMI link status for these GPUs", "", []
    errors = sorted("%s: %s" % item for item in after.items() if "NO_ERRORS" not in item[1])
    lines = ["XGMI error status since the stress began:"] + ["%s: %s" % item for item in sorted(after.items())]
    lines += ["XGMI link status:"] + (links or ["not reported"])
    if before:
        lines.append("before the stress: " + ", ".join(sorted(set(before.values()))))
    measurements = [measure("xgmi_error_gpus", len(errors), "count", "= 0"), measure("xgmi_links_down", len(down), "count", "= 0")]
    if errors or down:
        return ("failed", "; ".join(filter(None, [("XGMI errors on " + ", ".join(errors)) if errors else "",
                                                    ("XGMI links down: " + ", ".join(down)) if down else ""])),
                bounded(lines), measurements)
    return ("passed", "no XGMI error on %d GPUs during the stress%s" % (len(after), "; every reported link is up" if links else ""),
            bounded(lines), measurements)


def judge_thermal(sampler, args):
    if sampler.samples == 0:
        return ("error", "[telemetry_unavailable] AMD SMI gave no sample while the stress ran",
                bounded(sampler.errors[-20:]), [])
    hot = sorted((gpu, temp, sensor) for gpu, (temp, sensor) in sampler.max_temp.items() if temp > args.max_temperature)
    risen = []
    for gpu, last in sorted(sampler.last.items()):
        first = sampler.first.get(gpu, {})
        for key in ("prochot_accumulated", "socket_thermal_accumulated", "vr_thermal_accumulated", "hbm_thermal_accumulated"):
            if key in last and key in first and last[key] > first[key]:
                risen.append("%s %s +%g" % (gpu, key, last[key] - first[key]))
    lines = ["%d samples, every %.0f s" % (sampler.samples, SAMPLE_INTERVAL)]
    for gpu in sorted(set(sampler.max_temp) | set(sampler.max_power)):
        temp, sensor = sampler.max_temp.get(gpu, (None, ""))
        lines.append("%s: max %s C (%s), max %s W, throttle_status not UNTHROTTLED in %d samples%s" % (
            gpu, "?" if temp is None else "%g" % temp, sensor or "-", "%g" % sampler.max_power[gpu] if gpu in sampler.max_power else "?",
            sampler.throttled.get(gpu, 0), "" if gpu in sampler.last else "; throttle accumulators not reported"))
    if sampler.limits:
        lines.append("the GPUs' own limits (amd-smi static --limit): " + sampler.limits)
    lines += sampler.errors[-5:]
    top = max(sampler.max_temp.values(), default=(None, ""))[0]
    measurements = [measure("max_temperature", top, "C", "<= %g" % args.max_temperature)] if top is not None else []
    measurements.append(measure("thermal_throttle_increase", len(risen), "count", "= 0"))
    if top is None:
        return "error", "[telemetry_unavailable] AMD SMI reported no temperature while the stress ran", bounded(lines), measurements
    if hot or risen:
        return ("failed", "; ".join(filter(None, [
            ("above %g C: " % args.max_temperature + ", ".join("%s %g C (%s)" % row for row in hot)) if hot else "",
            ("thermal throttling rose: " + ", ".join(risen)) if risen else ""])), bounded(lines), measurements)
    return ("passed", "at most %g C during the stress (limit %g C)%s" % (
        top, args.max_temperature, "; no PROCHOT or thermal throttle accumulator rose" if sampler.last
        else "; these GPUs report no throttle accumulators"), bounded(lines), measurements)


# ---------------------------------------------------------------- main


def find_tool(given, *candidates):
    if given and os.path.isfile(given) and os.access(given, os.X_OK):
        return given
    if given:
        return None
    for candidate in candidates:
        path = shutil.which(candidate) if "/" not in candidate else (candidate if os.access(candidate, os.X_OK) else None)
        if path:
            return path
    return None


def stress(args, work, report):
    """Runs everything; report(check, verdict, summary, evidence, measurements) records each result."""
    before = read_ras(args, work, "before")
    if before.get("driven", 0) == 0:
        raise Unjudged("amdgpu_not_bound", "the amdgpu driver drives none of the %d AMD GPUs" % before.get("gpus", 0),
                       "\n".join(before.get("unbound", [])))
    driven = [bdf for bdf in args.gpu if not any(entry.startswith(bdf + " ") for entry in before.get("unbound", []))]
    gpus = len(driven)
    listing = gpu_list(amd_smi_json(args, "list"))
    names = {entry.get("gpu"): entry.get("bdf") for entry in listing if entry.get("bdf")}
    by_kfd = {str(entry.get("kfd_id")): entry.get("bdf") for entry in listing if entry.get("kfd_id") is not None}
    xgmi_before = xgmi_errors(args, names)

    rocm = args.rocm
    major, version = rocm_major(rocm)
    sampler = Sampler(args, names)
    sampler.start()
    try:
        try:
            release = {spec.split("|")[0]: spec.split("|")[1:] for spec in args.rvs}.get(major or "")
            if not release:
                raise Unjudged("rvs_unsupported_rocm", "no RVS release is declared for ROCm %s at %s (declared for ROCm %s)"
                               % (version or "unknown", rocm, ", ".join(sorted(spec.split("|")[0] for spec in args.rvs))))
            archive = os.path.join(work, "rvs.tar.gz")
            download(release[0], release[1], archive)
            rvs = os.path.join(work, "rvs")
            unpack(archive, rvs)
            env = rvs_env(rvs, rocm)
            missing = missing_libraries(args, rvs, env)
            if missing:
                raise Unjudged("rocm_libraries_missing", "the ROCm at %s (%s) lacks libraries RVS needs: %s"
                               % (rocm, version, " ".join(missing)))
            code, listed = run([os.path.join(rvs, "bin", "rvs"), "-g"], SMALL_TIMEOUT, env=env)
            ids = rvs_gpus(listed)
            rvs_names = {gid: bdf for bdf, gid in ids.items()}
            rvs_names.update({gid: bdf for gid, bdf in by_kfd.items() if gid not in rvs_names})
            if not ids:
                raise Unjudged("rvs_failed", "RVS lists no GPU (rvs -g exit %s)" % code, tail(listed))
            said = re.search(r"version\s+([\d.]+)", listed)
            rvs_version = said.group(1) if said else "?"
            conf_root = os.path.join(rvs, "share", "rocm-validation-suite", "conf")
            profile = choose_profile(conf_root, identity(args), args.profile)
            for check, name, own in STAGES:
                conf, origin = os.path.join(conf_root, profile, name), "conf/%s/%s" % (profile, name)
                if not os.path.isfile(conf):
                    if own:
                        report(check, "error", "[rvs_conf_missing] RVS has no %s for %s, so there is no pass target" % (name, profile))
                        continue
                    conf, origin = os.path.join(conf_root, name), "conf/%s (RVS has none for %s)" % (name, profile)
                if args.level == "quick":
                    shortened = os.path.join(work, "%s-%s" % (check, name))
                    quick_conf(conf, shortened, QUICK_BY_CHECK.get(check, QUICK))
                    conf = shortened
                log = os.path.join(work, check + ".log")
                limit = stage_timeout(args)
                if limit <= 0:
                    report(check, "error", "[stress_timeout] the run's time ran out before RVS %s could start" % name)
                    continue
                code, output = run([os.path.join(rvs, "bin", "rvs"), "-c", conf, "-d", "3"], limit, env=env, log=log)
                verdict, summary, evidence, measurements = judge_stage(check, conf, code, output, rvs_names, len(ids), args)
                where = "RVS %s, %s%s" % (rvs_version, origin, ", level quick" if args.level == "quick" else "")
                report(check, verdict, summary, where + "\n" + evidence, measurements)
        except Unjudged as error:
            for check in RVS_CHECKS:
                report(check, "error", error.summary(), error.evidence)
        try:
            report("collectives", *collectives(args, work, rocm, gpus))
        except Unjudged as error:
            report("collectives", "error", error.summary(), error.evidence)
    finally:
        sampler.stop()
    report("thermal-throttle", *judge_thermal(sampler, args))
    report("xgmi-links", *judge_xgmi(args, names, gpus, xgmi_before))

    after = read_ras(args, work, "after")
    failures, notes, measurements, lines, new_ce, cper = ras_delta(before, after)
    over = []
    if args.max_correctable >= 0:
        over = ["%s +%d" % item for item in sorted(new_ce.items()) if item[1] > args.max_correctable]
        if over:
            failures.append("more new correctable errors than the %d allowed per GPU: %s" % (args.max_correctable, ", ".join(over)))
    verdict = "failed" if failures else "passed"
    summary = "; ".join(failures) if failures else "no new uncorrectable error or fatal CPER record after the stress; " + (
        "at most %d new correctable errors on one GPU" % max([0] + list(new_ce.values())))
    report("ras-after-stress", verdict, summary + ("; " + "; ".join(notes) if notes else ""), bounded(lines), measurements)


def main():
    signal.signal(signal.SIGHUP, signal.SIG_IGN)

    def terminate(*_):
        if CURRENT["process"] is not None:
            stop(CURRENT["process"])
        sys.exit(143)
    signal.signal(signal.SIGTERM, terminate)

    parser = argparse.ArgumentParser()
    parser.add_argument("--target", required=True, help="this host's Server id")
    parser.add_argument("--gpu", action="append", default=[], help="an AMD GPU's PCI address; repeat for each")
    parser.add_argument("--level", choices=("full", "quick"), default="full")
    parser.add_argument("--profile", default="")
    parser.add_argument("--rvs", action="append", default=[], help="ROCm major|URL|SHA-256 of an RVS release")
    parser.add_argument("--rccl-tests-url", default="")
    parser.add_argument("--rccl-tests-sha256", default="")
    parser.add_argument("--ras-helper", required=True, help="the amd-gpu-ras helper, base64")
    parser.add_argument("--rocm", default="/opt/rocm")
    parser.add_argument("--root", default="")
    parser.add_argument("--amd-smi", default="")
    parser.add_argument("--dmesg", default="dmesg")
    parser.add_argument("--ldd", default="ldd")
    parser.add_argument("--make", default="make")
    parser.add_argument("--workdir", default="/var/tmp")
    parser.add_argument("--stage-timeout", type=int, default=3600)
    parser.add_argument("--deadline", type=int, default=0,
                        help="seconds the whole run may take; each stage gets at most what is left, less a reserve for the reads after it")
    parser.add_argument("--min-hbm-gbps", type=float, default=-1)
    parser.add_argument("--min-pcie-gbps", type=float, default=-1)
    parser.add_argument("--min-xgmi-gbps", type=float, default=-1)
    parser.add_argument("--min-busbw-gbps", type=float, default=-1)
    parser.add_argument("--max-temperature", type=float, default=95)
    parser.add_argument("--max-correctable", type=int, default=-1)
    args = parser.parse_args()
    args.root = args.root.rstrip("/")
    args.deadline_at = time.monotonic() + args.deadline if args.deadline > 0 else None
    args.amd_smi_path = find_tool(args.amd_smi, "amd-smi", os.path.join(args.rocm, "bin", "amd-smi"), "/opt/rocm/bin/amd-smi")

    results = {}

    def report(check, verdict, summary, evidence="", measurements=None):
        results.setdefault(check, result(args.target, check, verdict, summary, evidence, measurements))

    work = tempfile.mkdtemp(prefix="swallow-amd-gpu-stress-", dir=args.workdir)
    try:
        if not args.amd_smi_path:
            raise Unjudged("amd_smi_missing", "amd-smi is not installed or not on the PATH; it reads the GPUs' telemetry")
        stress(args, work, report)
    except Unjudged as error:
        for check in CHECKS:
            report(check, "error", error.summary(), error.evidence)
    except Exception as error:  # A defect here must still report every check, not hide behind an empty result.
        for check in CHECKS:
            report(check, "error", "[stress_helper_failed] %s: %s" % (type(error).__name__, error))
    finally:
        if CURRENT["process"] is not None:
            stop(CURRENT["process"])
        shutil.rmtree(work, ignore_errors=True)
    for check in CHECKS:
        report(check, "error", "[stress_helper_failed] the check was not reached")
    print(json.dumps({"checks": [results[check] for check in CHECKS]}))


if __name__ == "__main__":
    main()
