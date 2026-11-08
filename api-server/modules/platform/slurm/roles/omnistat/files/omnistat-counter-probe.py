"""Prove that this node's Omnistat can sample the GPU hardware counters swallow asks for (decision 063).

The omnistat role runs this before it turns the counters on, with the Omnistat environment's own
interpreter, as the exporter's user with CAP_PERFMON and the exporter's library path, in a process
of its own with a deadline. It does what Omnistat's ROCProfiler-SDK collector does when the exporter
starts: it loads the extension, initializes it, gets one sampler per GPU, starts each on the counters,
takes a sample, and stops. Upstream's collector ends the whole exporter when any of these fails, and
an older ROCProfiler-SDK can abort the process, so a node whose probe does not exit 0 keeps its
exporter without counters. It prints one JSON line on success.

Usage: omnistat-counter-probe.py --counters VALUBusy[,...]
"""

import argparse
import json
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--counters", required=True, help="comma-separated counter names")
    args = parser.parse_args()
    counters = [name for name in args.counters.split(",") if name]
    if not counters:
        sys.exit("no counters to sample")

    from omnistat.rocprofiler_sdk_extension import get_samplers, initialize

    initialize()
    samplers = get_samplers()
    if not samplers:
        sys.exit("ROCProfiler-SDK reports no GPU to sample")
    for sampler in samplers:
        sampler.start(counters)
    for index, sampler in enumerate(samplers):
        values = sampler.sample()
        if len(values) != len(counters):
            sys.exit(f"GPU {index} returned {len(values)} values for {len(counters)} counters")
    for sampler in samplers:
        sampler.stop()
    print(json.dumps({"gpus": len(samplers), "counters": counters}))


if __name__ == "__main__":
    main()
