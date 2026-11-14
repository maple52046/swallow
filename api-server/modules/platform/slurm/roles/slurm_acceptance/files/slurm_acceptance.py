#!/usr/bin/env python3
"""Accept a Slurm cluster, for the slurm-acceptance Validation Suite (docs/development/validation.md §7).

Run as root on a host that reaches the controller (a login node or a controller). Everything it
needs it reads from the cluster: the nodes with their CPUs and GRES, whether the default partition
hands out whole nodes, and, from one job on every node, which of swallow's features each node has
(oci.conf, the OCI probe bundle, the MPI probe, the container runtime). Then it runs jobs, each with a
time limit, under one job name:

  srun-all-nodes  one task on every node checked at once (it reports, a deployment does not gate on it)
  shared-node     two one-CPU jobs without a memory request run on one node together, unless the
                  partition hands out whole nodes
  oci-container   a rootless native container step as an unprivileged user, and on every GPU node a
                  one-GPU container step that can open exactly one GPU (decision 066)
  mpi-pmix        srun --mpi=pmix_v5 launches ranks that pass a barrier: two on one node, one on
                  each of two nodes, one in a container on each node; a failing rank fails the step
  gpu-gres        every GPU node offers GPU GRES, and a job gets exactly the GPUs it asks for: none
                  without a request, one with one, every GPU for the whole node. Each job counts the
                  GPU devices it can open, which needs only the amdgpu driver.
  gpu-container   when the run names a container image and runtime, every GPU node pulls AMD's ROCm
                  image straight from its registry if it lacks it, builds the HIP probe in it with
                  hipcc for gfx90a, gfx942, and gfx950, and runs it in the same three jobs with only
                  the job's devices: its kernel must run on each of them (decision 083). A node that
                  cannot pull the image is skipped with the reason; a probe that does not build or
                  run fails. The host's own ROCm is never used.

--nodes limits the node checks to those Servers (a deployment's gate names the nodes it adds), so
the work on the others is not disturbed. A feature that is off is skipped; one that is on but whose
probe is missing is an error, which a redeployment repairs. With --drain (gate mode) the GPU nodes
that fail gpu-gres are drained, as a deployment has always done; a report run only reports.

Prints {"checks": [...]} in the validation_report result shape and exits 0 unless its own arguments
are wrong.
"""

import argparse
import base64
import concurrent.futures
import json
import re
import subprocess
import time

DETAIL_TAIL = 400
UNPRIVILEGED = "nobody"


class Slurm:
    def __init__(self, job_name):
        self.job_name = job_name

    def run(self, argv, timeout=180, user=None):
        if user:
            argv = ["runuser", "-u", user, "--"] + argv
        try:
            proc = subprocess.run(argv, capture_output=True, text=True, timeout=timeout, check=False)
        except (OSError, subprocess.TimeoutExpired) as exc:
            return 1, "", str(exc)
        return proc.returncode, proc.stdout, proc.stderr

    def srun(self, args, command, time_limit=5, user=None, timeout=300, immediate=120):
        # --immediate gives up on nodes that are down or busy instead of waiting out the timeout.
        argv = ["srun", f"--job-name={self.job_name}", f"--time={time_limit}", f"--immediate={immediate}"] + args + command
        if user:
            argv = ["srun", "--chdir=/tmp"] + argv[1:]
        return self.run(argv, timeout=timeout, user=user)

    def retry(self, attempts, delay, call):
        for attempt in range(attempts):
            code, out, err = call()
            if code == 0 or attempt == attempts - 1:
                return code, out, err
            time.sleep(delay)
        return 1, "", "not attempted"


def tail(*parts):
    return " ".join(p.strip() for p in parts if p and p.strip())[-DETAIL_TAIL:]


def json_lines(text):
    reports = []
    for line in text.splitlines():
        line = line.strip()
        if line.startswith("{"):
            try:
                reports.append(json.loads(line))
            except ValueError:
                pass
    return reports


def gpu_count(gres):
    return sum(int(entry.split(":")[-1]) for entry in gres if entry.startswith("gpu") and entry.split(":")[-1].isdigit())


def read_nodes(slurm):
    code, out, err = slurm.run(["sinfo", "--noheader", "--Node", "--format=%N|%c|%G|%t"])
    if code != 0:
        raise RuntimeError("sinfo failed: " + tail(err, out))
    nodes = {}
    for line in out.splitlines():
        fields = line.strip().split("|")
        if len(fields) != 4 or fields[0] in nodes:
            continue
        gres = [re.sub(r"\(.*?\)", "", g) for g in fields[2].split(",") if g and g != "(null)"]
        nodes[fields[0]] = {"name": fields[0], "cpus": int(fields[1]) if fields[1].isdigit() else 0,
                            "gres": gres, "gpus": gpu_count(gres), "state": fields[3]}
    return list(nodes.values())


def whole_nodes(slurm):
    """Whether the default partition hands out whole nodes. slurm.conf's OverSubscribe=EXCLUSIVE
    reads back as OverSubscribe=EXCLUSIVE on older Slurm and as Exclusive=NODE (OverSubscribe=NO)
    on 26.05, so either counts."""
    code, out, _ = slurm.run(["scontrol", "show", "partition", "--oneliner"])
    for line in out.splitlines() if code == 0 else []:
        if "Default=YES" in line:
            exclusive = re.search(r"\bExclusive=(\S+)", line)
            return "OverSubscribe=EXCLUSIVE" in line or bool(exclusive and exclusive.group(1) != "NO")
    return False


# A configless compute node has no /etc/slurm/oci.conf: slurmd keeps the controller's copy in its
# configuration cache, whose directory depends on the Slurm version.
CENSUS = ('printf "%s oci=%s bundle=%s mpi=%s hip=%s runtime=%s\\n" "$SLURMD_NODENAME" '
          '"$(for f in /etc/slurm/oci.conf /run/slurm/conf/oci.conf /var/spool/slurmd/conf-cache/oci.conf; '
          'do test -f "$f" && echo 1 && exit; done; echo 0)" "$(test -d {bundle} && echo 1 || echo 0)" '
          '"$(test -x {mpi} && echo 1 || echo 0)" "$(test -x {hip} && echo 1 || echo 0)" '
          '"$({runtime_check} && echo 1 || echo 0)"')

# The GPU devices a job may open: Slurm's device cgroup admits only the render nodes of the GPUs
# allocated to the job, so the ones that open are its GPUs. Prints them space-separated.
OPENABLE_GPUS = ('for d in /dev/dri/renderD*; do [ -e "$d" ] || continue; '
                 '( exec 3<"$d" ) 2>/dev/null && printf "%s " "$d"; done; echo')

# The gfx targets the HIP probe is built for: MI200, MI300, and MI350 (decision 083).
GPU_PROBE_TARGETS = ("gfx90a", "gfx942", "gfx950")
# How long a node may take to pull the ROCm image, which is several GB, before the check is skipped.
IMAGE_PULL_SECONDS = 1800

# Prepares the container GPU check on a node, in a job without GPUs: pulls the image straight from its
# registry when the node lacks it, writes the probe's source into a directory of this run, and builds
# the probe there with the image's hipcc. Prints PULL_FAILED or BUILD_FAILED with the command's last
# lines, or READY.
CONTAINER_PREPARE = ('dir={dir}; mkdir -p "$dir" && printf %s {source} | base64 -d > "$dir/probe.hip" || {{ echo BUILD_FAILED; exit 0; }}; '
                     'if ! {cli} image inspect {image} >/dev/null 2>&1; then '
                     'out=$(timeout {pull} {cli} pull {image} 2>&1) || {{ echo PULL_FAILED; echo "$out" | tail -n 5; exit 0; }}; fi; '
                     'out=$({cli} run --rm --pull=never -v "$dir:/w" {image} /opt/rocm/bin/hipcc -O2 {targets} '
                     '-o /w/probe /w/probe.hip 2>&1) || {{ echo BUILD_FAILED; echo "$out" | tail -n 5; exit 0; }}; echo READY')

# Runs the built probe in the image with only the job's GPU devices and prints its report.
CONTAINER_GPU = ('devs=$(' + OPENABLE_GPUS + '); flags=""; for d in $devs; do flags="$flags --device=$d"; done; '
                 'echo "{{\\"openable\\": $(echo $devs | wc -w)}}"; '
                 '{cli} run --rm --pull=never --device=/dev/kfd $flags --security-opt seccomp=unconfined '
                 '-v {dir}:/w:ro {image} /w/probe --expect=$(echo $devs | wc -w) --runs={runs}')


def parse_census(out):
    census = {}
    for line in out.splitlines():
        fields = line.split()
        if len(fields) == 6:
            census[fields[0]] = {k: v == "1" for k, v in (f.split("=", 1) for f in fields[1:])}
    return census


def take_census(slurm, nodes, paths):
    """One task on every node at once (srun-all-nodes); nodes it missed are asked one by one."""
    names = [n["name"] for n in nodes]
    command = ["sh", "-c", CENSUS.format(bundle=paths["bundle"], mpi=paths["mpi"], hip=paths["hip"], runtime_check=paths["runtime_check"])]
    code, out, err = slurm.srun([f"--nodes={len(names)}", f"--ntasks={len(names)}", "--ntasks-per-node=1",
                                 "--nodelist=" + ",".join(names)], command, time_limit=2, immediate=60)
    census = parse_census(out)
    together = {"ok": code == 0 and all(n in census for n in names), "detail": tail(err, out) if code != 0 else ""}
    missing = [n for n in names if n not in census]
    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        for _, out, _ in pool.map(lambda n: slurm.srun(["--nodes=1", "--ntasks=1", "--nodelist=" + n], command,
                                                         time_limit=2, immediate=60), missing):
            census.update(parse_census(out))
    return census, together


class Results:
    def __init__(self, servers, platform_id):
        self.servers, self.platform_id, self.checks = servers, platform_id, []

    def target(self, node=None):
        server = self.servers.get(node) or self.servers.get((node or "").split(".")[0]) if node else None
        return {"kind": "server", "id": server} if server else {"kind": "platform", "id": self.platform_id}

    def add(self, check, verdict, summary, node=None, evidence=""):
        if node and self.target(node)["kind"] == "platform":
            summary = f"node {node}: {summary}"
        self.checks.append({"check": check, "target": self.target(node), "verdict": verdict,
                            "summary": summary, "evidence": evidence[-8000:]})


VERDICT_RANK = {"failed": 3, "error": 2, "passed": 1, "skipped": 0}


def merged(checks):
    """One result per check and target: the run keeps only the last result for a pair, so the nodes
    judged on the Platform are folded into one, with the worst verdict and every summary."""
    out = {}
    for check in checks:
        key = (check["check"], check["target"]["kind"], check["target"]["id"])
        if key not in out:
            out[key] = dict(check)
            continue
        kept = out[key]
        if VERDICT_RANK[check["verdict"]] > VERDICT_RANK[kept["verdict"]]:
            kept["verdict"] = check["verdict"]
        kept["summary"] = f"{kept['summary']}; {check['summary']}"
        kept["evidence"] = "\n".join(e for e in (kept["evidence"], check["evidence"]) if e)[-8000:]
    return list(out.values())


def check_all_nodes(results, nodes, census, together, expected):
    names = {n["name"] for n in nodes}
    absent = [s for s in expected if s not in {results.servers.get(n) or results.servers.get(n.split(".")[0]) for n in names}]
    unanswered = sorted(names - set(census))
    if together["ok"] and not absent:
        results.add("srun-all-nodes", "passed", f"one task ran on each of the {len(names)} nodes at once")
        return
    problems = []
    if not together["ok"]:
        problems.append("a step over every node did not complete" + (": " + together["detail"] if together["detail"] else ""))
    if unanswered:
        problems.append("no task ran on " + ", ".join(unanswered))
    if absent:
        problems.append("members that are not Slurm nodes: " + ", ".join(absent))
    results.add("srun-all-nodes", "failed", "; ".join(problems))


def check_shared_node(slurm, results, nodes, census, exclusive):
    if exclusive:
        results.add("shared-node", "skipped", "the default partition hands out whole nodes (OverSubscribe=EXCLUSIVE)")
        return
    node = next((n for n in nodes if n["cpus"] >= 2 and n["name"] in census), None)
    if node is None:
        results.add("shared-node", "skipped", "no reachable node has two CPUs")
        return
    jobs = []
    for _ in range(2):
        code, out, err = slurm.run(["sbatch", "--parsable", f"--job-name={slurm.job_name}", "--ntasks=1", "--time=5",
                                    "--nodelist=" + node["name"], "--output=/dev/null", "--wrap=sleep 120"])
        if code == 0 and out.strip():
            jobs.append(out.strip().split(";")[0])
        else:
            results.add("shared-node", "error", "a one-CPU job could not be submitted: " + tail(err, out))
            for job in jobs:
                slurm.run(["scancel", job])
            return
    states = []
    try:
        for _ in range(12):
            code, out, _ = slurm.run(["squeue", "--noheader", "--format=%T", "--jobs=" + ",".join(jobs)])
            states = out.split()
            if states.count("RUNNING") == 2:
                break
            time.sleep(5)
    finally:
        for job in jobs:
            slurm.run(["scancel", job])
    if states.count("RUNNING") == 2:
        results.add("shared-node", "passed", f"two one-CPU jobs ran on {node['name']} together")
    else:
        results.add("shared-node", "failed", f"two one-CPU jobs did not run together on {node['name']} "
                    f"(states: {', '.join(states) or 'none'}); check DefMemPerCPU and the partition's OverSubscribe setting")


def check_oci(slurm, results, nodes, census, paths):
    if not any(c.get("oci") for c in census.values()):
        results.add("oci-container", "skipped", "native OCI containers are off")
        return
    problems = {}
    for node in nodes:
        info = census.get(node["name"])
        if info and info.get("oci") and not info.get("bundle"):
            problems.setdefault(node["name"], []).append(("error", "the OCI probe bundle is missing; redeploy the Platform"))
    first = next((n["name"] for n in nodes if census.get(n["name"], {}).get("bundle")), None)
    if first:
        code, out, err = slurm.retry(3, 5, lambda: slurm.srun(
            ["--nodes=1", "--ntasks=1", "--nodelist=" + first, "--container=" + paths["bundle"]],
            ["/bin/cat", "/proc/self/uid_map"], user=UNPRIVILEGED))
        mapping = out.split()
        if code != 0 or len(mapping) < 2 or mapping[1] == "0":
            problems.setdefault(first, []).append(("failed", "a rootless container step did not run (" + tail(out, err) + ")"))
    gpu_nodes = [n for n in nodes if n["gpus"] > 0 and census.get(n["name"], {}).get("bundle")]

    def gpu_container(node):
        # The native container counts the GPU devices it can open; the host's ROCm is not used.
        code, out, err = slurm.srun(["--nodes=1", "--ntasks=1", "--nodelist=" + node["name"], "--gres=gpu:1",
                                     "--container=" + paths["bundle"]], ["/bin/sh", "-c", OPENABLE_GPUS],
                                    time_limit=10, user=UNPRIVILEGED)
        opened = out.split()
        if code != 0 or len(opened) != 1:
            return node, "failed", f"a one-GPU container could open {len(opened)} GPU devices, want 1 (" + tail(err, out) + ")"
        return node, "passed", ""

    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        for node, verdict, detail in pool.map(gpu_container, gpu_nodes):
            if verdict != "passed":
                problems.setdefault(node["name"], []).append((verdict, detail))
    judged = {n["name"] for n in gpu_nodes} | ({first} if first else set()) | set(problems)
    for name in sorted(judged):
        found = problems.get(name, [])
        if not found:
            results.add("oci-container", "passed", "containers run rootless and see only their GPUs", node=name)
            continue
        verdict = "failed" if any(v == "failed" for v, _ in found) else "error"
        results.add("oci-container", verdict, "; ".join(d for _, d in found), node=name)


def check_mpi(slurm, results, nodes, census, paths):
    if not any(c.get("mpi") for c in census.values()):
        results.add("mpi-pmix", "skipped", "MPI with PMIx is off")
        return
    missing = [n for n, c in census.items() if not c.get("mpi")]
    if missing:
        results.add("mpi-pmix", "error", "the MPI probe is missing on " + ", ".join(sorted(missing)) + "; redeploy the Platform")
        return
    names = [n["name"] for n in nodes if n["name"] in census]
    two = len(names) >= 2
    oci = all(census[n].get("oci") and census[n].get("bundle") for n in names[:2])
    runs = [("two ranks on one node", 1, 2, ["--nodes=1", "--ntasks=2", "--nodelist=" + names[0]])]
    if two:
        runs.append(("one rank on each of two nodes", 2, 2, ["--nodes=2", "--ntasks-per-node=1", "--nodelist=" + ",".join(names[:2])]))
    if oci:
        layout = (["--nodes=2", "--ntasks-per-node=1", "--nodelist=" + ",".join(names[:2])] if two
                  else ["--nodes=1", "--ntasks=1", "--nodelist=" + names[0]])
        runs.append(("inside a container", 2 if two else 1, 2 if two else 1, layout + ["--container=" + paths["bundle"]]))
    failures = []
    code, out, _ = slurm.run(["srun", "--mpi=list"])
    if "pmix_v5" not in out:
        failures.append("srun --mpi=list does not offer pmix_v5")
    for name, node_count, ranks, args in runs:
        code, out, err = slurm.srun(["--mpi=pmix_v5"] + args, [paths["mpi"]], user=UNPRIVILEGED)
        reports = json_lines(out)
        good = (code == 0 and len(reports) == ranks and all(r.get("barrier") for r in reports)
                and {r.get("size") for r in reports} == {ranks} and sorted(r.get("rank") for r in reports) == list(range(ranks))
                and len({r.get("pmixNamespace") for r in reports}) == 1 and reports[0].get("pmixNamespace")
                and len({r.get("node") for r in reports}) == node_count)
        if not good:
            failures.append(name + ": " + tail(out, err))
    code, _, _ = slurm.srun(["--mpi=pmix_v5", "--nodes=1", "--ntasks=2", "--nodelist=" + names[0]],
                            [paths["mpi"], "--fail-rank=1"], user=UNPRIVILEGED)
    if code == 0:
        failures.append("a step whose rank exited 3 reported success")
    if failures:
        results.add("mpi-pmix", "failed", "; ".join(failures))
    else:
        results.add("mpi-pmix", "passed", f"MPI ranks launch through PMIx ({', '.join(r[0] for r in runs)}), and a failing rank fails its step")


def check_gpus(slurm, results, nodes, census, paths, expected_gpu_servers, drain):
    gpu_nodes = [n for n in nodes if n["gpus"] > 0]
    by_server = {results.servers.get(n["name"]) or results.servers.get(n["name"].split(".")[0]): n for n in nodes}
    for server in expected_gpu_servers:
        node = by_server.get(server)
        if node is None or node["gpus"] == 0:
            results.checks.append({"check": "gpu-gres", "target": {"kind": "server", "id": server}, "verdict": "failed",
                                   "summary": "Slurm reports no GPU GRES for this node" if node else "the Server is not a Slurm node",
                                   "evidence": ""})
    if not gpu_nodes:
        if not expected_gpu_servers:
            results.add("gpu-gres", "skipped", "no node offers GPU GRES")
        return

    def probe(node):
        name = node["name"]
        if name not in census:
            return node, "error", "no job reached the node"
        full = node["gres"][0] if len(node["gres"]) == 1 else f"gpu:{node['gpus']}"
        problems = []
        for check, attempts, args, want, runs in (
                ("no GPU request", 6, [], 0, 1),
                ("one GPU", 3, ["--gres=gpu:1"], 1, 1),
                ("every GPU", 1, ["--gres=" + full], node["gpus"], 3)):
            command = ["/bin/sh", "-c", 'echo "{\\"openable\\": $(echo $(' + OPENABLE_GPUS + ') | wc -w)}"']
            code, out, err = slurm.retry(attempts, 5, lambda: slurm.srun(
                ["--nodes=1", "--ntasks=1", "--nodelist=" + name] + args, command, time_limit=15, timeout=600))
            reports = json_lines(out)
            opened = next((r.get("openable") for r in reports if "openable" in r), None)
            if code != 0 and opened is None:
                problems.append(f"{check}: " + tail(err, out))
                continue
            if opened != want:
                problems.append(f"{check}: the job could open {opened} GPU devices, want {want}")
        if problems:
            return node, "failed", "; ".join(problems)
        return node, "passed", f"Slurm hands out its {node['gpus']} GPUs ({full}) and jobs open exactly their GPUs"

    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        outcomes = list(pool.map(probe, gpu_nodes))
    for node, verdict, summary in outcomes:
        if verdict == "failed" and drain:
            code, out, err = slurm.run(["scontrol", "update", "nodename=" + node["name"], "state=drain",
                                        "reason=swallow: GPU probe failed"])
            summary += "; drained" if code == 0 else "; draining it failed: " + tail(err, out)
        results.add("gpu-gres", verdict, summary, node=node["name"])


def check_gpu_container(slurm, results, nodes, census, paths, run_id, drain):
    """gpu-container: on each GPU node, the HIP probe built in AMD's ROCm image runs on exactly the GPUs
    a job is given. A node that cannot pull the image is skipped with the reason (the image is user
    workload the node fetches itself, so a node without upstream access cannot run it); a probe that
    does not build, or whose kernel fails, fails the node."""
    gpu_nodes = [n for n in nodes if n["gpus"] > 0]
    if not gpu_nodes:
        return
    if not paths.get("cli") or not paths.get("image"):
        results.add("gpu-container", "skipped", "the Platform has no container runtime, so no container runs a GPU kernel")
        return
    directory = f"/var/tmp/swallow-gpu-probe-{run_id[:8]}"
    targets = " ".join("--offload-arch=" + target for target in GPU_PROBE_TARGETS)

    def probe(node):
        name = node["name"]
        if name not in census or not census[name].get("runtime"):
            return node, "error", f"{paths['cli']} is not on the node; redeploy the Platform"
        prepare = CONTAINER_PREPARE.format(dir=directory, source=paths["probe_source"], cli=paths["cli"], image=paths["image"],
                                           pull=IMAGE_PULL_SECONDS, targets=targets)
        code, out, err = slurm.srun(["--nodes=1", "--ntasks=1", "--nodelist=" + name], ["/bin/sh", "-c", prepare],
                                    time_limit=IMAGE_PULL_SECONDS // 60 + 15, timeout=IMAGE_PULL_SECONDS + 900)
        lines = out.strip().splitlines()
        if "PULL_FAILED" in lines:
            return node, "skipped", (f"warning: the node could not pull {paths['image']} (" + tail(*lines[lines.index("PULL_FAILED") + 1:]) +
                                     "), so no GPU kernel ran in a container; the GPU GRES and AMD SMI checks still gate the node")
        if "READY" not in lines:
            detail = tail(*lines[lines.index("BUILD_FAILED") + 1:]) if "BUILD_FAILED" in lines else tail(err, out)
            return node, "failed", f"the HIP probe did not build in {paths['image']}: {detail}"
        full = node["gres"][0] if len(node["gres"]) == 1 else f"gpu:{node['gpus']}"
        problems = []
        for check, attempts, args, want, runs in (
                ("no GPU request", 6, [], 0, 1),
                ("one GPU", 3, ["--gres=gpu:1"], 1, 1),
                ("every GPU", 1, ["--gres=" + full], node["gpus"], 3)):
            command = ["/bin/sh", "-c", CONTAINER_GPU.format(cli=paths["cli"], image=paths["image"], dir=directory, runs=runs)]
            code, out, err = slurm.retry(attempts, 5, lambda: slurm.srun(
                ["--nodes=1", "--ntasks=1", "--nodelist=" + name] + args, command, time_limit=15, timeout=600))
            report = next((r for r in reversed(json_lines(out)) if "ok" in r), {})
            if code != 0 or not report.get("ok") or int(report.get("devices", -1)) != want:
                problems.append(f"{check}: the HIP probe in a container failed (" + tail(str(report.get("error", "")), err, out) + ")")
        slurm.srun(["--nodes=1", "--ntasks=1", "--nodelist=" + name], ["/bin/sh", "-c", f"rm -rf {directory}"], time_limit=2, timeout=180)
        if problems:
            return node, "failed", "; ".join(problems)
        return node, "passed", f"a HIP kernel built for {', '.join(GPU_PROBE_TARGETS)} runs in {paths['image']} on exactly the GPUs each job is given"

    with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
        outcomes = list(pool.map(probe, gpu_nodes))
    for node, verdict, summary in outcomes:
        if verdict == "failed" and drain:
            code, out, err = slurm.run(["scontrol", "update", "nodename=" + node["name"], "state=drain",
                                        "reason=swallow: GPU kernel failed in a container"])
            summary += "; drained" if code == 0 else "; draining it failed: " + tail(err, out)
        results.add("gpu-container", verdict, summary, node=node["name"])


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--platform-id", required=True)
    parser.add_argument("--servers", default="{}", help="JSON object: Slurm node name to Server id")
    parser.add_argument("--expected-nodes", default="[]", help="JSON list: the Servers that should be Slurm nodes")
    parser.add_argument("--gpu-servers", default="[]", help="JSON list: the Servers that must offer GPU GRES")
    parser.add_argument("--drain", action="store_true", help="drain the GPU nodes that fail gpu-gres (gate mode)")
    parser.add_argument("--hip-probe", default="/usr/local/libexec/swallow/hip-probe")
    parser.add_argument("--mpi-probe", default="/usr/local/libexec/swallow/mpi-probe")
    parser.add_argument("--oci-bundle", default="/var/lib/swallow/oci/probe")
    parser.add_argument("--nodes", default="[]", help="JSON list: the Servers the node checks run on; empty is every node")
    parser.add_argument("--container-image", default="", help="the ROCm image the container GPU check builds its probe in")
    parser.add_argument("--container-runtime", default="", help="docker-ce or podman, to run that image with")
    parser.add_argument("--gpu-probe-source", default="", help="the HIP probe's source, base64-encoded")
    args = parser.parse_args()
    slurm = Slurm("swallow-validation-" + args.run_id[:8])
    results = Results(json.loads(args.servers), args.platform_id)
    cli = {"docker-ce": "docker", "podman": "podman"}.get(args.container_runtime, "")
    runtime_check = f"command -v {cli} >/dev/null 2>&1" if cli else "false"
    # The source travels base64-encoded inside the job's command; it is validated here so a bad value
    # fails the run's arguments rather than every node's build.
    source = base64.b64encode(base64.b64decode(args.gpu_probe_source, validate=True)).decode() if args.gpu_probe_source else ""
    paths = {"bundle": args.oci_bundle, "mpi": args.mpi_probe, "hip": args.hip_probe,
             "image": args.container_image if cli and source else "", "cli": cli, "runtime_check": runtime_check,
             "probe_source": source}
    scope = set(json.loads(args.nodes))
    checks = ("srun-all-nodes", "shared-node", "oci-container", "mpi-pmix", "gpu-gres", "gpu-container")
    try:
        code, out, err = slurm.retry(6, 5, lambda: slurm.run(["scontrol", "ping"]))
        if code != 0:
            raise RuntimeError("the controller does not answer scontrol ping: " + tail(out, err))
        nodes = read_nodes(slurm)
        if scope:
            # Only the Servers in scope are checked; the work on the other nodes is left alone.
            nodes = [n for n in nodes if (results.servers.get(n["name"]) or results.servers.get(n["name"].split(".")[0])) in scope]
        if not nodes:
            raise RuntimeError("sinfo lists no node" + (" of the Servers to check" if scope else ""))
        census, together = take_census(slurm, nodes, paths)
        expected = json.loads(args.expected_nodes)
        if scope:
            expected = [server for server in expected if server in scope]
        check_all_nodes(results, nodes, census, together, expected)
        check_shared_node(slurm, results, nodes, census, whole_nodes(slurm))
        check_oci(slurm, results, nodes, census, paths)
        check_mpi(slurm, results, nodes, census, paths)
        check_gpus(slurm, results, nodes, census, paths, json.loads(args.gpu_servers), args.drain)
        check_gpu_container(slurm, results, nodes, census, paths, args.run_id, args.drain)
    except RuntimeError as exc:
        judged = {c["check"] for c in results.checks}
        for check in checks:
            if check not in judged:
                results.add(check, "error", "the cluster could not be read: " + str(exc))
    print(json.dumps({"checks": merged(results.checks)}))


if __name__ == "__main__":
    main()
