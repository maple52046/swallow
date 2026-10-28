#!/usr/bin/env python3
# Managed by Swallow (configure-device-metrics-exporter): the Device Metrics Exporter identity gate.
#
# Slurm attributes a job to a GPU by its index, and the exporter joins that index to its gpu_id
# label, so attribution is only right when the exporter reports one distinct gpu_id per GPU. An
# endpoint that answers, or a gpu_nodes_total that matches, proves nothing: on MI308X SR-IOV
# virtual functions every GPU's series carries gpu_id="0", and a one-GPU job is then attributed to
# all of them. This compares the GPUs the KFD topology lists with what the exporter reports and
# prints one JSON line:
#   ok              the endpoint served GPU health series (false only when the exporter is broken)
#   identity        "distinct" when every GPU has its own gpu_id, else "collapsed"
#   kfdGpus, gpuNodesTotal, healthSeries, distinctGpuIds, deploymentModes   the evidence
# It exits 0 when ok, 1 otherwise; a collapsed identity is reported, not a failure.
import argparse
import glob
import json
import re
import sys
import time
import urllib.request

LABEL = re.compile(r'(\w+)="((?:[^"\\]|\\.)*)"')


def kfd_gpus(topology):
    """Counts the KFD nodes that are GPUs (CPU nodes have no gfx target)."""
    count = 0
    for path in glob.glob(topology + "/nodes/*/properties"):
        with open(path) as handle:
            for line in handle:
                parts = line.split()
                if len(parts) == 2 and parts[0] == "gfx_target_version" and parts[1] != "0":
                    count += 1
    return count


def parse(text, prefix):
    """Returns the gpu_nodes_total value and the label sets of the gpu_health series."""
    nodes_total, health = None, []
    for line in text.splitlines():
        if line.startswith(prefix + "gpu_nodes_total"):
            nodes_total = int(float(line.rsplit(" ", 1)[1]))
        elif line.startswith(prefix + "gpu_health{"):
            labels = line[line.index("{") + 1:line.rindex("}")]
            health.append(dict(LABEL.findall(labels)))
    return nodes_total, health


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True, help="the exporter's /metrics URL")
    parser.add_argument("--prefix", default="amd_")
    parser.add_argument("--topology", default="/sys/class/kfd/kfd/topology")
    parser.add_argument("--wait", type=float, default=120, help="seconds to wait for health series")
    args = parser.parse_args()

    report = {"ok": False, "identity": "collapsed", "kfdGpus": kfd_gpus(args.topology),
              "gpuNodesTotal": None, "healthSeries": 0, "distinctGpuIds": 0,
              "deploymentModes": {}, "error": ""}
    deadline = time.monotonic() + args.wait
    health = []
    while True:
        try:
            with urllib.request.urlopen(args.url, timeout=20) as response:
                nodes_total, health = parse(response.read().decode(), args.prefix)
            report["gpuNodesTotal"] = nodes_total
            report["error"] = ""
        except Exception as error:  # the exporter may still be starting
            report["error"] = str(error)
        if health or time.monotonic() >= deadline:
            break
        time.sleep(5)

    if not health:
        report["error"] = report["error"] or "the exporter served no gpu_health series"
        print(json.dumps(report))
        return 1
    gpu_ids = {series.get("gpu_id", "") for series in health}
    modes = {}
    for series in health:
        mode = series.get("deployment_mode", "")
        modes[mode] = modes.get(mode, 0) + 1
    report.update(ok=True, healthSeries=len(health), distinctGpuIds=len(gpu_ids), deploymentModes=modes)
    if report["kfdGpus"] > 0 and len(gpu_ids) == report["kfdGpus"] and len(health) == report["kfdGpus"]:
        report["identity"] = "distinct"
    print(json.dumps(report))
    return 0


if __name__ == "__main__":
    sys.exit(main())
