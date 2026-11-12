#!/usr/bin/env python3
"""Probe every peer of a network-mesh run from this host.

For each peer: whether ICMP echo reaches it and its round trip, whether an unfragmented packet of
the path's MTU (the MTU of the interface the route to the peer leaves through, or --mtu) gets
through, and whether its TCP port (SSH by default) accepts a connection. Prints one JSON object and
exits 0 unless its own arguments are wrong, so an unreachable peer is a verdict, not an error. It
changes nothing on the host.

Usage: mesh_probe.py [--mtu N] [--port N] <peer id>=<address> ...
"""

import argparse
import concurrent.futures
import ipaddress
import json
import re
import socket
import subprocess

RECEIVED = re.compile(r"(\d+) (?:packets )?received")
RTT = re.compile(r"= [\d.]+/([\d.]+)/")
OUTPUT_TAIL = 400
# The largest ICMP echo payload of one IP packet; loopback's MTU is larger.
MAX_PAYLOAD = 65507


def run(argv, timeout=30):
    try:
        proc = subprocess.run(argv, capture_output=True, text=True, timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        return 1, str(exc)
    return proc.returncode, (proc.stdout + proc.stderr).strip()


def ping(address, size=None, count=3):
    argv = ["ping", "-n", "-q", "-c", str(count), "-W", "2", "-i", "0.2"]
    if size is not None:
        argv += ["-M", "do", "-s", str(size)]
    argv.append(address)
    code, output = run(argv)
    received = RECEIVED.search(output)
    rtt = RTT.search(output)
    return {
        "ok": code == 0 and received is not None and int(received.group(1)) > 0,
        "rtt_ms": float(rtt.group(1)) if rtt else None,
        "output": output[-OUTPUT_TAIL:],
    }


def route_mtu(address):
    code, output = run(["ip", "-o", "route", "get", address], timeout=5)
    device = re.search(r"\bdev (\S+)", output) if code == 0 else None
    if device is None:
        return None
    try:
        with open(f"/sys/class/net/{device.group(1)}/mtu", encoding="ascii") as mtu:
            return int(mtu.read().strip())
    except (OSError, ValueError):
        return None


def port_open(address, port):
    try:
        with socket.create_connection((address, port), timeout=3):
            return True, ""
    except OSError as exc:
        return False, str(exc)


def probe(peer_id, address, mtu, port):
    result = {"id": peer_id, "address": address}
    reach = ping(address)
    result.update(reachable=reach["ok"], rtt_ms=reach["rtt_ms"], ping_output=reach["output"])
    path_mtu = mtu or route_mtu(address)
    result["mtu"] = path_mtu
    if path_mtu is None:
        result.update(mtu_ok=None, mtu_output="no route to read the MTU from")
    else:
        header = 48 if ipaddress.ip_address(address).version == 6 else 28
        full = ping(address, size=min(path_mtu - header, MAX_PAYLOAD), count=2)
        result.update(mtu_ok=full["ok"], mtu_output=full["output"])
    result["port_open"], result["port_error"] = port_open(address, port)
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--mtu", type=int, default=0)
    parser.add_argument("--port", type=int, default=22)
    parser.add_argument("peers", nargs="*")
    args = parser.parse_args()
    peers = []
    for entry in args.peers:
        peer_id, sep, address = entry.partition("=")
        if not sep or not peer_id or not address:
            parser.error(f"peer {entry!r} is not <id>=<address>")
        ipaddress.ip_address(address)
        peers.append((peer_id, address))
    with concurrent.futures.ThreadPoolExecutor(max_workers=16) as pool:
        results = list(pool.map(lambda peer: probe(peer[0], peer[1], args.mtu, args.port), peers))
    print(json.dumps({"peers": results}))


if __name__ == "__main__":
    main()
