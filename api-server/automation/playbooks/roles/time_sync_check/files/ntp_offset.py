#!/usr/bin/env python3
"""Measure this host's clock offset from one NTP server with an SNTP client query.

Used by the time_sync_check role (decision 065). It sends a few NTPv4 client requests to UDP 123
and prints the sample with the smallest round trip as JSON: {"offset": seconds the server is ahead
of this host, "delay": round trip in seconds, "stratum": server stratum}. It never changes the
clock. Exit status 1 means no reply arrived; the reason is on stderr.
"""
import json
import socket
import struct
import sys
import time

NTP_EPOCH_OFFSET = 2208988800
ATTEMPTS = 4


def to_seconds(seconds, fraction):
    return seconds - NTP_EPOCH_OFFSET + fraction / 2**32


def query(sock, address):
    t1 = time.time()
    sock.sendto(b"\x23" + 47 * b"\0", address)  # LI 0, version 4, mode 3 (client)
    data, _ = sock.recvfrom(48)
    t4 = time.time()
    if len(data) < 48:
        raise OSError("short NTP reply")
    fields = struct.unpack("!12I", data[:48])
    t2 = to_seconds(fields[8], fields[9])
    t3 = to_seconds(fields[10], fields[11])
    return {"offset": ((t2 - t1) + (t3 - t4)) / 2, "delay": (t4 - t1) - (t3 - t2), "stratum": data[1]}


def main():
    if len(sys.argv) != 2:
        print("usage: ntp_offset.py <server>", file=sys.stderr)
        return 2
    server = sys.argv[1]
    try:
        family, _, _, _, address = socket.getaddrinfo(server, 123, type=socket.SOCK_DGRAM)[0]
    except OSError as error:
        print("cannot resolve %s: %s" % (server, error), file=sys.stderr)
        return 1
    samples, last_error = [], None
    with socket.socket(family, socket.SOCK_DGRAM) as sock:
        sock.settimeout(2)
        for _ in range(ATTEMPTS):
            try:
                samples.append(query(sock, address))
            except OSError as error:
                last_error = error
    if not samples:
        print("no NTP reply from %s on UDP 123: %s" % (server, last_error), file=sys.stderr)
        return 1
    print(json.dumps(min(samples, key=lambda sample: sample["delay"])))
    return 0


if __name__ == "__main__":
    sys.exit(main())
