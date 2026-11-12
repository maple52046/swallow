#!/usr/bin/env python3
"""Smoke-test a Kubernetes cluster through kubectl, for the kubernetes-smoke Validation Suite.

It reads the nodes; on every Ready node that takes workloads (schedulable, no NoSchedule or
NoExecute taint) it runs a pod through the scheduler; it resolves the API server's Service name
through cluster DNS from a pod; and it fetches a page from a pod through a ClusterIP Service, from a
pod on another node when there is one. Every object it creates carries the label
swallow.io/validation-run=<run id> in its namespace and is deleted before it exits, whatever
happened. The pods run as an unprivileged user under the restricted Pod Security profile.

Prints one JSON object, {"checks": [...]}, in the result shape of the validation_report role
(docs/development/validation.md §4.2): nodes-ready for every node and for every member that should
be a node and is not, pod-scheduling for every Ready node, cluster-dns and service-routing for the
Platform. --servers maps node names to Server ids; a node it does not name is judged as the
Platform. Exits 0 unless its own arguments are wrong.
"""

import argparse
import json
import shlex
import subprocess
import time

RUN_LABEL = "swallow.io/validation-run"
ROLE_LABEL = "swallow.io/validation-role"
NO_WORKLOAD_EFFECTS = ("NoSchedule", "NoExecute")
SECURITY = {
    "runAsNonRoot": True, "runAsUser": 65534, "allowPrivilegeEscalation": False,
    "capabilities": {"drop": ["ALL"]}, "seccompProfile": {"type": "RuntimeDefault"},
}
RESOURCES = {"requests": {"cpu": "10m", "memory": "16Mi"}, "limits": {"memory": "64Mi"}}
DETAIL_TAIL = 600


class Kubectl:
    def __init__(self, command, namespace):
        self.argv, self.namespace = shlex.split(command), namespace

    def run(self, *args, stdin=None, namespaced=True, timeout=60):
        argv = self.argv + (["--namespace", self.namespace] if namespaced else []) + list(args)
        try:
            proc = subprocess.run(argv, input=stdin, capture_output=True, text=True, timeout=timeout, check=False)
        except (OSError, subprocess.TimeoutExpired) as exc:
            return 1, "", str(exc)
        return proc.returncode, proc.stdout, proc.stderr

    def json(self, *args, namespaced=True):
        code, out, err = self.run(*args, "--output=json", namespaced=namespaced)
        if code != 0:
            raise RuntimeError((err or out).strip()[-DETAIL_TAIL:])
        return json.loads(out)

    def apply(self, objects):
        code, out, err = self.run("apply", "--filename=-", stdin=json.dumps({"apiVersion": "v1", "kind": "List", "items": objects}))
        if code != 0:
            raise RuntimeError((err or out).strip()[-DETAIL_TAIL:])


def node_report(node):
    conditions = {c.get("type"): c for c in node.get("status", {}).get("conditions", [])}
    ready = conditions.get("Ready", {})
    taints = [f"{t.get('key')}:{t.get('effect')}" for t in node.get("spec", {}).get("taints", [])
              if t.get("effect") in NO_WORKLOAD_EFFECTS]
    is_ready = ready.get("status") == "True"
    return {
        "name": node["metadata"]["name"],
        "hostname": node["metadata"].get("labels", {}).get("kubernetes.io/hostname", node["metadata"]["name"]),
        "ready": is_ready,
        "reason": "" if is_ready else (ready.get("message") or ready.get("reason") or "no Ready condition"),
        "workload": is_ready and not node.get("spec", {}).get("unschedulable", False) and not taints,
        "taints": taints,
    }


def pod(name, run_id, role, image, command, node=None, labels=None):
    spec = {
        "restartPolicy": "Never", "activeDeadlineSeconds": 300, "automountServiceAccountToken": False,
        "securityContext": {"runAsNonRoot": True, "seccompProfile": {"type": "RuntimeDefault"}},
        "containers": [{"name": "probe", "image": image, "command": ["sh", "-c", command],
                        "securityContext": SECURITY, "resources": RESOURCES}],
    }
    if node:
        spec["nodeSelector"] = {"kubernetes.io/hostname": node}
    return {"apiVersion": "v1", "kind": "Pod",
            "metadata": {"name": name, "labels": {RUN_LABEL: run_id, ROLE_LABEL: role, **(labels or {})}},
            "spec": spec}


def pod_state(item):
    status = item.get("status", {})
    phase = status.get("phase", "Unknown")
    reason = status.get("reason", "")
    for container in status.get("containerStatuses", []):
        waiting = container.get("state", {}).get("waiting")
        terminated = container.get("state", {}).get("terminated")
        if waiting and waiting.get("reason") not in ("ContainerCreating", None):
            reason = f"{waiting.get('reason')}: {waiting.get('message', '')}".strip(": ")
        if terminated and terminated.get("exitCode", 0) != 0:
            reason = f"exited {terminated.get('exitCode')}: {terminated.get('reason', '')}".strip(": ")
    for condition in status.get("conditions", []):
        if condition.get("type") == "PodScheduled" and condition.get("status") == "False":
            reason = condition.get("message") or condition.get("reason") or "not scheduled"
    ready = any(c.get("type") == "Ready" and c.get("status") == "True" for c in status.get("conditions", []))
    return phase, reason, ready


def wait_for(kubectl, run_id, names, deadline, until_ready=()):
    """Polls until every pod of names has ended (and every one of until_ready runs and is ready) or
    deadline passes; returns {name: (phase, reason, ready)}."""
    states = {}
    while True:
        try:
            items = kubectl.json("get", "pods", f"--selector={RUN_LABEL}={run_id}")["items"]
        except RuntimeError:
            items = []
        states = {item["metadata"]["name"]: pod_state(item) for item in items}
        pending = [n for n in names if n not in until_ready and states.get(n, ("Pending",))[0] not in ("Succeeded", "Failed")]
        unready = [n for n in until_ready if not states.get(n, ("", "", False))[2]]
        if not pending and not unready or time.monotonic() >= deadline:
            return states
        time.sleep(3)


def logs(kubectl, name):
    code, out, err = kubectl.run("logs", name, timeout=30)
    return (out if code == 0 else err).strip()[-DETAIL_TAIL:]


def smoke(kubectl, run_id, image, timeout):
    report = {"nodes": [], "pods": [], "dns": None, "service": None, "error": ""}
    nodes = [node_report(n) for n in kubectl.json("get", "nodes", namespaced=False)["items"]]
    report["nodes"] = nodes
    workload = [n for n in nodes if n["workload"]]
    if not workload:
        report["error"] = "no Ready node takes workloads"
        return report
    code, _, err = kubectl.run("get", "namespace", kubectl.namespace, namespaced=False)
    if code != 0:
        code, out, err = kubectl.run("create", "namespace", kubectl.namespace, namespaced=False)
        if code != 0 and "AlreadyExists" not in err:
            raise RuntimeError((err or out).strip()[-DETAIL_TAIL:])
        kubectl.run("label", "namespace", kubectl.namespace, "pod-security.kubernetes.io/enforce=restricted",
                    "--overwrite", namespaced=False)

    short = run_id[:8]
    node_pods = {f"smoke-{short}-node-{i}": n for i, n in enumerate(workload)}
    server, client, dns = f"smoke-{short}-server", f"smoke-{short}-client", f"smoke-{short}-dns"
    service = f"smoke-{short}"
    objects = [pod(name, run_id, "node", image, "echo ok", node=n["hostname"]) for name, n in node_pods.items()]
    objects.append(pod(dns, run_id, "dns", image, "nslookup kubernetes.default.svc.cluster.local"))
    objects.append(pod(server, run_id, "server", image,
                       "mkdir -p /tmp/www && echo swallow-smoke > /tmp/www/index.html && exec httpd -f -p 8080 -h /tmp/www",
                       node=workload[0]["hostname"], labels={"app.kubernetes.io/name": service}))
    objects.append({"apiVersion": "v1", "kind": "Service",
                    "metadata": {"name": service, "labels": {RUN_LABEL: run_id}},
                    "spec": {"selector": {RUN_LABEL: run_id, "app.kubernetes.io/name": service},
                             "ports": [{"port": 8080, "targetPort": 8080}]}})
    kubectl.apply(objects)
    deadline = time.monotonic() + timeout
    states = wait_for(kubectl, run_id, list(node_pods) + [dns, server], deadline, until_ready=(server,))

    for name, node in node_pods.items():
        phase, reason, _ = states.get(name, ("Missing", "the pod was not created", False))
        report["pods"].append({"node": node["name"], "phase": phase, "reason": reason})
    phase, reason, _ = states.get(dns, ("Missing", "", False))
    report["dns"] = {"ok": phase == "Succeeded", "detail": logs(kubectl, dns) or reason}

    phase, reason, ready = states.get(server, ("Missing", "", False))
    if not ready:
        report["service"] = {"ok": False, "detail": f"the server pod is not ready ({phase}: {reason})"}
        return report
    elsewhere = next((n for n in workload[1:]), workload[0])
    kubectl.apply([pod(client, run_id, "client", image,
                       f"wget -q -O - -T 10 http://{service}.{kubectl.namespace}.svc.cluster.local:8080/",
                       node=elsewhere["hostname"])])
    states = wait_for(kubectl, run_id, [client], deadline)
    phase, reason, _ = states.get(client, ("Missing", "", False))
    output = logs(kubectl, client)
    report["service"] = {"ok": phase == "Succeeded" and "swallow-smoke" in output,
                         "detail": (f"from node {elsewhere['name']}: " + (output or reason or phase))[-DETAIL_TAIL:]}
    return report


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


def results(report, servers, expected_nodes, platform_id):
    """Turns the report into results: each node's checks on its Server (or the Platform when no
    Server maps to it), and the cluster's checks on the Platform."""

    def target(node):
        server = servers.get(node) or servers.get(node.split(".")[0])
        return {"kind": "server", "id": server} if server else {"kind": "platform", "id": platform_id}

    def named(node, text):
        return text if servers.get(node) or servers.get(node.split(".")[0]) else f"node {node}: {text}"

    checks = []
    platform = {"kind": "platform", "id": platform_id}
    if report["error"] and not report["nodes"]:
        for check in ("nodes-ready", "pod-scheduling", "cluster-dns", "service-routing"):
            checks.append({"check": check, "target": platform, "verdict": "error",
                           "summary": "the cluster could not be read: " + report["error"], "evidence": ""})
        return checks
    seen = set()
    for node in report["nodes"]:
        name = node["name"]
        seen.add(servers.get(name) or servers.get(name.split(".")[0]))
        checks.append({"check": "nodes-ready", "target": target(name), "verdict": "passed" if node["ready"] else "failed",
                       "summary": named(name, "Ready" if node["ready"] else "not Ready: " + node["reason"]), "evidence": ""})
    for server in expected_nodes:
        if server not in seen:
            checks.append({"check": "nodes-ready", "target": {"kind": "server", "id": server}, "verdict": "failed",
                           "summary": "the member is not a node of the cluster", "evidence": ""})
    pods = {p["node"]: p for p in report["pods"]}
    for node in report["nodes"]:
        name = node["name"]
        if not node["workload"]:
            why = ("not Ready" if not node["ready"] else "tainted " + ", ".join(node["taints"]) if node["taints"]
                   else "cordoned")
            checks.append({"check": "pod-scheduling", "target": target(name), "verdict": "skipped",
                           "summary": named(name, f"takes no workloads ({why})"), "evidence": ""})
            continue
        result = pods.get(name)
        if result is None:
            checks.append({"check": "pod-scheduling", "target": target(name), "verdict": "error",
                           "summary": named(name, "no pod was run: " + (report["error"] or "unknown")), "evidence": ""})
            continue
        ok = result["phase"] == "Succeeded"
        checks.append({"check": "pod-scheduling", "target": target(name), "verdict": "passed" if ok else "failed",
                       "summary": named(name, "a pod ran to completion" if ok else f"the pod is {result['phase']}: {result['reason']}"),
                       "evidence": ""})
    for check, key, passed in (("cluster-dns", "dns", "a pod resolves kubernetes.default through cluster DNS"),
                               ("service-routing", "service", "a pod reaches another through a ClusterIP Service")):
        outcome = report[key]
        if outcome is None:
            checks.append({"check": check, "target": platform, "verdict": "error",
                           "summary": "not checked: " + (report["error"] or "unknown"), "evidence": ""})
        else:
            checks.append({"check": check, "target": platform, "verdict": "passed" if outcome["ok"] else "failed",
                           "summary": passed if outcome["ok"] else "failed", "evidence": outcome["detail"]})
    return checks


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--kubectl", required=True)
    parser.add_argument("--namespace", required=True)
    parser.add_argument("--run-id", required=True)
    parser.add_argument("--image", required=True)
    parser.add_argument("--platform-id", required=True)
    parser.add_argument("--servers", default="{}", help="JSON object: node name to Server id")
    parser.add_argument("--expected-nodes", default="[]", help="JSON list: the Servers that should be nodes")
    parser.add_argument("--timeout", type=int, default=240)
    args = parser.parse_args()
    servers, expected = json.loads(args.servers), json.loads(args.expected_nodes)
    kubectl = Kubectl(args.kubectl, args.namespace)
    try:
        report = smoke(kubectl, args.run_id, args.image, args.timeout)
    except (RuntimeError, ValueError, KeyError) as exc:
        report = {"nodes": [], "pods": [], "dns": None, "service": None, "error": str(exc)[-DETAIL_TAIL:]}
    finally:
        kubectl.run("delete", "pods,services", f"--selector={RUN_LABEL}={args.run_id}", "--wait=false",
                    "--ignore-not-found", timeout=60)
    print(json.dumps({"checks": merged(results(report, servers, expected, args.platform_id))}))


if __name__ == "__main__":
    main()
