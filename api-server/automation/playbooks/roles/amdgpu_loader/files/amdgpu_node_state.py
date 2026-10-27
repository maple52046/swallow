#!/usr/bin/env python3
"""Report the amdgpu driver state of this node as JSON (decision 063).

Usage: amdgpu_node_state.py [--root DIR] [--parm-file FILE] [--desired BASE64_JSON]

--desired is a base64-encoded JSON object of the module options the deployment wants. The report
says whether the driver is loaded, which desired options the installed driver does not have at all
(with the closest name it does have), which differ from the loaded parameters, which the driver has
but does not expose for reading, and which options a topology rule derives from the KFD topology.
It also says whether anything holds the driver (its module reference count and the processes that
have a GPU device open) and whether the root filesystem lives in memory, where a reboot erases the
operating system.

Rules name an observable condition, never a GPU model or virtualization mode:

  duplicate-peer-links  a KFD GPU node reaches the same peer node through both an io_link and a
                        p2p_link. libhsakmt then overflows its per-node peer array and HIP processes
                        corrupt the heap when they exit; pcie_p2p=0 stops KFD creating the p2p_link.

--root lets tests point the script at a fixture tree instead of /. The driver's option names come
from "modinfo -F parm amdgpu" on the real root, or from --parm-file; a fixture root without
--parm-file skips the name check.
"""
import argparse
import base64
import difflib
import glob
import json
import os
import re
import shutil
import subprocess

TRUE_VALUES = {"1", "y", "yes", "true", "on"}
FALSE_VALUES = {"0", "n", "no", "false", "off"}
MEMORY_FILESYSTEMS = {"overlay", "tmpfs", "ramfs", "rootfs"}
GPU_DEVICE = re.compile(r"^/dev/(kfd|dri/renderD\d+)$")
PARM_NAME = re.compile(r"^([A-Za-z0-9_]+):")


def read_properties(path):
    props = {}
    try:
        with open(path) as handle:
            for line in handle:
                parts = line.split()
                if len(parts) == 2:
                    props[parts[0]] = parts[1]
    except OSError:
        pass
    return props


def read_text(path):
    try:
        with open(path) as handle:
            return handle.read().strip()
    except OSError:
        return None


def link_peers(node_dir, kind):
    peers = set()
    for path in glob.glob(os.path.join(node_dir, kind, "*", "properties")):
        peer = read_properties(path).get("node_to")
        if peer is not None:
            peers.add(peer)
    return peers


def gpu_nodes(root):
    nodes = []
    for node_dir in sorted(glob.glob(os.path.join(root, "sys/class/kfd/kfd/topology/nodes/*"))):
        props = read_properties(os.path.join(node_dir, "properties"))
        if int(props.get("gfx_target_version", "0") or 0) > 0:
            nodes.append(node_dir)
    return nodes


def topology_rules(nodes):
    rules, reasons = {}, {}
    for node_dir in nodes:
        shared = link_peers(node_dir, "io_links") & link_peers(node_dir, "p2p_links")
        if shared:
            rules["pcie_p2p"] = "0"
            reasons["pcie_p2p"] = (
                "duplicate-peer-links: KFD node %s reaches node %s through both an io_link and a "
                "p2p_link" % (os.path.basename(node_dir), ",".join(sorted(shared)))
            )
            break
    return rules, reasons


def same_value(loaded, desired):
    loaded = loaded.strip()
    wanted = str(desired).strip()
    if loaded in ("Y", "N"):
        if wanted.lower() in TRUE_VALUES:
            return loaded == "Y"
        if wanted.lower() in FALSE_VALUES:
            return loaded == "N"
    return loaded == wanted


def driver_option_names(root, parm_file):
    """The installed driver's option names, or None when they cannot be read."""
    if parm_file:
        text = read_text(parm_file)
    elif root == "/":
        modinfo = shutil.which("modinfo") or next(
            (path for path in ("/sbin/modinfo", "/usr/sbin/modinfo") if os.path.exists(path)), None)
        if modinfo is None:
            return None
        try:
            text = subprocess.run([modinfo, "-F", "parm", "amdgpu"], capture_output=True, text=True,
                                  timeout=30, check=True).stdout
        except (OSError, subprocess.SubprocessError):
            return None
    else:
        return None
    if text is None:
        return None
    return {match.group(1) for match in map(PARM_NAME.match, text.splitlines()) if match}


def root_filesystem(root):
    """The type of the filesystem mounted last on /, which is the one the system runs on."""
    fstype = None
    try:
        with open(os.path.join(root, "proc/self/mounts")) as handle:
            for line in handle:
                fields = line.split()
                if len(fields) >= 3 and fields[1] == "/":
                    fstype = fields[2]
    except OSError:
        pass
    return fstype


def gpu_holders(root):
    """Processes that have /dev/kfd or a DRM render node open, as "PID COMMAND"."""
    holders = []
    for pid_dir in sorted(glob.glob(os.path.join(root, "proc/[0-9]*")), key=lambda p: int(os.path.basename(p))):
        try:
            targets = [os.readlink(fd) for fd in glob.glob(os.path.join(pid_dir, "fd", "*"))]
        except OSError:
            continue
        if any(GPU_DEVICE.match(target) for target in targets):
            holders.append("%s %s" % (os.path.basename(pid_dir), read_text(os.path.join(pid_dir, "comm")) or "?"))
    return holders


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default="/")
    parser.add_argument("--parm-file", default="")
    parser.add_argument("--desired", default="")
    args = parser.parse_args()
    desired = (json.loads(base64.b64decode(args.desired).decode()) if args.desired else None) or {}

    known = driver_option_names(args.root, args.parm_file)
    unknown, suggestions = [], {}
    if known is not None:
        for name in sorted(desired):
            if name not in known:
                unknown.append(name)
                close = difflib.get_close_matches(name, sorted(known), n=1, cutoff=0.6)
                if close:
                    suggestions[name] = close[0]

    module_dir = os.path.join(args.root, "sys/module/amdgpu")
    loaded = os.path.isdir(module_dir)
    parameters, mismatched, missing, unverifiable = {}, [], [], []
    if loaded:
        for name, value in sorted(desired.items()):
            current = read_text(os.path.join(module_dir, "parameters", name))
            if current is None:
                if known is None:
                    missing.append(name)
                elif name in known:
                    unverifiable.append(name)
                continue
            parameters[name] = current
            if not same_value(current, value):
                mismatched.append(name)
    refcnt = read_text(os.path.join(module_dir, "refcnt")) if loaded else None

    fstype = root_filesystem(args.root)
    nodes = gpu_nodes(args.root)
    rules, reasons = topology_rules(nodes)
    print(json.dumps({
        "loaded": loaded,
        "parameters": parameters,
        "mismatched": mismatched,
        "missing": missing,
        "unknown": unknown,
        "suggestions": suggestions,
        "unverifiable": unverifiable,
        "refcnt": int(refcnt) if refcnt and refcnt.lstrip("-").isdigit() else None,
        "holders": gpu_holders(args.root),
        "rootFilesystem": fstype,
        "memoryBackedRoot": fstype in MEMORY_FILESYSTEMS,
        "gpuNodes": len(nodes),
        "rules": rules,
        "ruleReasons": reasons,
    }))


if __name__ == "__main__":
    main()
