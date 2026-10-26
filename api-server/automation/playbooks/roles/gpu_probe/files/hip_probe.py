#!/usr/bin/env python3
"""Prove the GPUs this process can see are usable through the HIP runtime (decision 063).

Usage: hip-probe [--expect N] [--runs R] [--bytes B]

Every run starts a child process that loads libamdhip64 through ctypes and calls hipInit,
hipGetDeviceCount, and then, on every device, hipSetDevice, hipMalloc, hipMemset,
hipDeviceSynchronize, and hipFree. A run fails when a call fails, when the device count differs
from --expect, when the child does not exit 0 (a heap corruption at exit aborts it), or when it
prints an allocator corruption message. Only host-side runtime calls are made, so the probe needs
no compiler and works on any GPU architecture.

The runtime is searched in $ROCM_PATH/lib, /opt/rocm/lib, /opt/rocm-*/lib, then the loader path;
HIP_PROBE_RUNTIME names one library to load instead.

Prints one JSON line: {"ok": true, "runs": R, "devices": N} or {"ok": false, ...}. Exit codes:
0 every run passed, 1 a run failed, 2 the HIP runtime is not installed.
"""
import argparse
import ctypes
import glob
import json
import os
import subprocess
import sys

HIP_SUCCESS = 0
HIP_ERROR_NO_DEVICE = 100
CORRUPTION_MARKERS = (
    "free(): invalid",
    "munmap_chunk(): invalid pointer",
    "double free",
    "corrupted",
    "Segmentation fault",
)


def runtime_candidates():
    # HIP_PROBE_RUNTIME names the one library to load, for an unusual install path.
    explicit = os.environ.get("HIP_PROBE_RUNTIME")
    if explicit:
        yield explicit
        return
    rocm = os.environ.get("ROCM_PATH")
    if rocm:
        yield os.path.join(rocm, "lib", "libamdhip64.so")
    yield "/opt/rocm/lib/libamdhip64.so"
    for path in sorted(glob.glob("/opt/rocm-*/lib/libamdhip64.so"), reverse=True):
        yield path
    yield "libamdhip64.so"


def load_runtime():
    for candidate in runtime_candidates():
        try:
            return ctypes.CDLL(candidate)
        except OSError:
            continue
    return None


def probe_once(expect, size):
    hip = load_runtime()
    if hip is None:
        print(json.dumps({"ok": False, "error": "the HIP runtime (libamdhip64.so) is not installed"}))
        return 2

    def check(name, status):
        if status != HIP_SUCCESS:
            raise RuntimeError("%s returned hipError %d" % (name, status))

    count = ctypes.c_int(0)
    # A process that may see no GPU (a job without a GPU request) gets hipErrorNoDevice.
    status = hip.hipInit(ctypes.c_uint(0))
    if status == HIP_SUCCESS:
        status = hip.hipGetDeviceCount(ctypes.byref(count))
    if status == HIP_ERROR_NO_DEVICE:
        count.value = 0
    elif status != HIP_SUCCESS:
        print(json.dumps({"ok": False, "error": "the HIP runtime returned hipError %d" % status}))
        return 1
    if expect is not None and count.value != expect:
        print(json.dumps({"ok": False, "devices": count.value,
                          "error": "saw %d GPUs, expected %d" % (count.value, expect)}))
        return 1
    hip.hipMalloc.argtypes = [ctypes.POINTER(ctypes.c_void_p), ctypes.c_size_t]
    hip.hipMemset.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.c_size_t]
    hip.hipFree.argtypes = [ctypes.c_void_p]
    try:
        for device in range(count.value):
            check("hipSetDevice(%d)" % device, hip.hipSetDevice(ctypes.c_int(device)))
            pointer = ctypes.c_void_p()
            check("hipMalloc on device %d" % device, hip.hipMalloc(ctypes.byref(pointer), size))
            check("hipMemset on device %d" % device, hip.hipMemset(pointer, 0xA5, size))
            check("hipDeviceSynchronize on device %d" % device, hip.hipDeviceSynchronize())
            check("hipFree on device %d" % device, hip.hipFree(pointer))
    except RuntimeError as error:
        print(json.dumps({"ok": False, "devices": count.value, "error": str(error)}))
        return 1
    print(json.dumps({"ok": True, "devices": count.value,
                      "visible": os.environ.get("ROCR_VISIBLE_DEVICES")}))
    return 0


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--expect", type=int)
    parser.add_argument("--runs", type=int, default=3)
    parser.add_argument("--bytes", type=int, default=64 << 20)
    parser.add_argument("--once", action="store_true", help=argparse.SUPPRESS)
    args = parser.parse_args()
    if args.once:
        return probe_once(args.expect, args.bytes)

    devices, visible = None, None
    for run in range(1, args.runs + 1):
        command = [sys.executable, os.path.abspath(__file__), "--once", "--bytes", str(args.bytes)]
        if args.expect is not None:
            command += ["--expect", str(args.expect)]
        child = subprocess.run(command, capture_output=True, text=True)
        lines = child.stdout.strip().splitlines()
        try:
            report = json.loads(lines[-1]) if lines else {}
        except ValueError:
            report = {}
        corruption = [marker for marker in CORRUPTION_MARKERS if marker in child.stderr]
        if child.returncode == 2:
            print(json.dumps(report or {"ok": False, "error": "the HIP runtime is not installed"}))
            return 2
        if child.returncode != 0 or corruption or not report.get("ok"):
            print(json.dumps({
                "ok": False, "run": run, "returncode": child.returncode,
                "error": report.get("error") or ("allocator corruption: %s" % ", ".join(corruption) if corruption
                                                 else "the probe process exited with %d" % child.returncode),
                "devices": report.get("devices"),
                "stderr": child.stderr.strip()[-600:],
            }))
            return 1
        devices, visible = report.get("devices"), report.get("visible")
    print(json.dumps({"ok": True, "runs": args.runs, "devices": devices, "visible": visible}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
