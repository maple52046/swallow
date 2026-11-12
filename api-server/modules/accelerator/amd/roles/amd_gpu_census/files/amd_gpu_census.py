#!/usr/bin/env python3
"""Count this host's AMD GPUs two ways: the PCI functions of vendor 0x1002 whose class is a display
controller (0x03) or a processing accelerator (0x12), and the GPU nodes of the kernel's KFD topology
(nodes with SIMDs; CPU nodes have none). A GPU the driver did not bring up is in the first count and
missing from the second. A function bound to vfio-pci is passed through to a VM on purpose: it is
listed in passedThrough and not counted in pci. Prints one JSON line and always exits 0; it reads
sysfs only. --root prefixes every sysfs path, for tests.
"""
import argparse
import glob
import json
import os

AMD_VENDOR = "0x1002"
GPU_CLASSES = ("0x03", "0x12")


def read(path):
    try:
        with open(path, encoding="ascii") as handle:
            return handle.read().strip()
    except OSError:
        return ""


def pci_gpus(root):
    devices, passed_through = [], []
    for device in sorted(glob.glob(root + "/sys/bus/pci/devices/*")):
        if read(os.path.join(device, "vendor")) != AMD_VENDOR or not read(os.path.join(device, "class")).startswith(GPU_CLASSES):
            continue
        driver = os.path.basename(os.path.realpath(os.path.join(device, "driver")))
        (passed_through if driver == "vfio-pci" else devices).append(os.path.basename(device))
    return devices, passed_through


def kfd_gpus(root):
    nodes = []
    for properties in sorted(glob.glob(root + "/sys/class/kfd/kfd/topology/nodes/*/properties")):
        values = dict(line.split(None, 1) for line in read(properties).splitlines() if " " in line)
        if int(values.get("simd_count", "0") or 0) > 0:
            nodes.append(os.path.basename(os.path.dirname(properties)))
    return nodes


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default="")
    root = parser.parse_args().root.rstrip("/")
    (pci, passed_through), kfd = pci_gpus(root), kfd_gpus(root)
    print(json.dumps({"pci": len(pci), "kfd": len(kfd), "pciDevices": pci, "passedThrough": passed_through,
                      "kfdNodes": kfd, "kfdPresent": os.path.isdir(root + "/sys/class/kfd/kfd")}))


if __name__ == "__main__":
    main()
