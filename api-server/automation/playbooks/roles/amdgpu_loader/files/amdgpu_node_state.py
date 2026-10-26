#!/usr/bin/env python3
"""Report the amdgpu driver state of this node as JSON (decision 063).

Usage: amdgpu_node_state.py [--root DIR] [--desired BASE64_JSON]

--desired is a base64-encoded JSON object of the module options the deployment wants. The report
says whether the driver is loaded, which desired options differ from the loaded parameters or are
not exposed by the driver at all, and which options a topology rule derives from the KFD topology.

Rules name an observable condition, never a GPU model or virtualization mode:

  duplicate-peer-links  a KFD GPU node reaches the same peer node through both an io_link and a
                        p2p_link. libhsakmt then overflows its per-node peer array and HIP processes
                        corrupt the heap when they exit; pcie_p2p=0 stops KFD creating the p2p_link.

--root lets tests point the script at a fixture tree instead of /.
"""
import argparse
import base64
import glob
import json
import os

TRUE_VALUES = {"1", "y", "yes", "true", "on"}
FALSE_VALUES = {"0", "n", "no", "false", "off"}


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


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default="/")
    parser.add_argument("--desired", default="")
    args = parser.parse_args()
    desired = json.loads(base64.b64decode(args.desired).decode()) if args.desired else {}

    module_dir = os.path.join(args.root, "sys/module/amdgpu")
    loaded = os.path.isdir(module_dir)
    parameters, mismatched, missing = {}, [], []
    if loaded:
        for name, value in sorted(desired.items()):
            path = os.path.join(module_dir, "parameters", name)
            try:
                with open(path) as handle:
                    current = handle.read().strip()
            except OSError:
                missing.append(name)
                continue
            parameters[name] = current
            if not same_value(current, value):
                mismatched.append(name)

    nodes = gpu_nodes(args.root)
    rules, reasons = topology_rules(nodes)
    print(json.dumps({
        "loaded": loaded,
        "parameters": parameters,
        "mismatched": mismatched,
        "missing": missing,
        "gpuNodes": len(nodes),
        "rules": rules,
        "ruleReasons": reasons,
    }))


if __name__ == "__main__":
    main()
