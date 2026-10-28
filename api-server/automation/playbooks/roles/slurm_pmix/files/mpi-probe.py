#!/usr/bin/python3
# Managed by Swallow (deploy-slurm): the MPI probe that proves srun --mpi=pmix_v5 (decision 066).
# A process count proves nothing about PMIx wire-up, so every rank initializes MPI through the PMIx
# server slurmstepd runs, reports its PMIx namespace and rank next to its MPI rank, local rank, and
# world size, and passes a barrier and an allreduce with every other rank before it prints one JSON
# line. node is the Slurm node the rank runs on, which inside a container differs from the
# container's own hostname. --fail-rank makes that rank exit 3 after the barrier, to prove a failing rank fails the step.
import argparse
import json
import os
import socket
import sys

from mpi4py import MPI


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--fail-rank", type=int, default=-1)
    args = parser.parse_args()

    world = MPI.COMM_WORLD
    world.Barrier()
    total = world.allreduce(1, op=MPI.SUM)
    report = {
        "host": socket.gethostname(),
        "node": os.environ.get("SLURMD_NODENAME", socket.gethostname()),
        "job": os.environ.get("SLURM_JOB_ID", ""),
        "step": os.environ.get("SLURM_STEP_ID", ""),
        "pmixNamespace": os.environ.get("PMIX_NAMESPACE", ""),
        "pmixRank": os.environ.get("PMIX_RANK", ""),
        "rank": world.Get_rank(),
        "localRank": int(os.environ.get("OMPI_COMM_WORLD_LOCAL_RANK", os.environ.get("SLURM_LOCALID", "-1"))),
        "size": world.Get_size(),
        "barrier": total == world.Get_size(),
    }
    print(json.dumps(report), flush=True)
    if world.Get_rank() == args.fail_rank:
        sys.exit(3)
    return 0


if __name__ == "__main__":
    sys.exit(main())
