# Invoked only by the opt-in Go test inside a fresh user/network namespace.
# Client and target get their own child network namespaces; no host interfaces
# or firewall tables are touched.
import ctypes
import json
import os
import select
import signal
import socket
import subprocess
import sys
import time


def run(*args):
    subprocess.run(args, check=True, stdout=subprocess.DEVNULL)


def child(role, reader, writer):
    if ctypes.CDLL(None, use_errno=True).unshare(0x40000000) != 0:
        raise OSError(ctypes.get_errno(), "unshare network")
    writer.write("ready\n"); writer.flush()
    reader.readline()
    subnet = 1 if role == "client" else 2
    run("ip", "link", "set", "lo", "up")
    run("ip", "link", "set", "eth0", "up")
    run("ip", "addr", "add", f"10.250.{subnet}.2/24", "dev", "eth0")
    run("ip", "-6", "addr", "add", f"fd00:{subnet}::2/64", "dev", "eth0", "nodad")
    run("ip", "route", "add", "default", "via", f"10.250.{subnet}.1")
    run("ip", "-6", "route", "add", "default", "via", f"fd00:{subnet}::1")
    if role == "server":
        sockets = []
        for family, host in [(socket.AF_INET, "10.250.2.2"), (socket.AF_INET6, "fd00:2::2")]:
            for kind in [socket.SOCK_STREAM, socket.SOCK_DGRAM]:
                s = socket.socket(family, kind)
                if family == socket.AF_INET6:
                    s.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
                s.bind((host, 8080))
                if kind == socket.SOCK_STREAM: s.listen()
                sockets.append(s)
        listeners = [s for s in sockets if s.type == socket.SOCK_STREAM]
        writer.write("ready\n"); writer.flush()
        while True:
            for s in select.select(sockets, [], [])[0]:
                if s in listeners:
                    conn, _ = s.accept(); sockets.append(conn)
                elif s.type == socket.SOCK_DGRAM:
                    data, addr = s.recvfrom(1024); s.sendto(data, addr)
                else:
                    data = s.recv(1024)
                    if data: s.sendall(data)
                    else: sockets.remove(s); s.close()
    else:
        sockets = []
        for family, host, port in [(socket.AF_INET, "10.250.1.1", 20000), (socket.AF_INET6, "fd00:1::1", 20001)]:
            for kind in [socket.SOCK_STREAM, socket.SOCK_DGRAM]:
                s = socket.socket(family, kind); s.settimeout(2)
                s.connect((host, port)); sockets.append(s)
        writer.write("ready\n"); writer.flush()
        for command in reader:
            results = []
            for s in sockets:
                s.settimeout(0.4 if command.strip() == "blocked" else 3)
                s.sendall(b"x")
                try: results.append(bool(s.recv(1024)))
                except TimeoutError: results.append(False)
            writer.write(json.dumps(results) + "\n"); writer.flush()


def start(role):
    cr, pw = os.pipe(); pr, cw = os.pipe()
    pid = os.fork()
    if pid == 0:
        os.close(pw); os.close(pr)
        child(role, os.fdopen(cr), os.fdopen(cw, "w"))
        os._exit(0)
    os.close(cr); os.close(cw)
    reader, writer = os.fdopen(pr), os.fdopen(pw, "w")
    assert reader.readline().strip() == "ready"
    return pid, reader, writer


def timed_out(*_):
    raise TimeoutError("namespace test timed out")

signal.signal(signal.SIGALRM, timed_out)
signal.alarm(35)
children = []
try:
    run("ip", "link", "set", "lo", "up")
    run("sysctl", "-qw", "net.ipv4.ip_forward=1")
    run("sysctl", "-qw", "net.ipv6.conf.all.forwarding=1")
    for subnet, role in [(2, "server"), (1, "client")]:
        pid, reader, writer = start(role)
        children.append((pid, reader, writer))
        dev = f"r{subnet}"
        run("ip", "link", "add", dev, "type", "veth", "peer", "name", "eth0")
        run("ip", "link", "set", "eth0", "netns", str(pid))
        run("ip", "link", "set", dev, "up")
        run("ip", "addr", "add", f"10.250.{subnet}.1/24", "dev", dev)
        run("ip", "-6", "addr", "add", f"fd00:{subnet}::1/64", "dev", dev, "nodad")
        if role == "client": run("sh", sys.argv[1])
        writer.write("start\n"); writer.flush()
        assert reader.readline().strip() == "ready"
    _, reader, writer = children[-1]
    def probe(command, expected):
        writer.write(command + "\n"); writer.flush()
        result = json.loads(reader.readline())
        assert result == [expected] * 4, (command, result)
    probe("before", True)
    before = json.loads(subprocess.check_output(["nft", "-j", "list", "table", "inet", "flyx"]))
    run("sh", sys.argv[2]); run("sh", sys.argv[2])
    probe("blocked", False)
    after = json.loads(subprocess.check_output(["nft", "-j", "list", "table", "inet", "flyx"]))
    def counters(doc):
        result = {}
        for item in doc["nftables"]:
            rule = item.get("rule")
            if not rule: continue
            for expr in rule["expr"]:
                if "counter" in expr:
                    result[(rule["handle"], rule.get("comment"))] = expr["counter"]
        return result
    old, current = counters(before), counters(after)
    assert old.keys() == current.keys(), "blocking replaced NAT rules"
    for key, count in old.items():
        assert current[key]["bytes"] >= count["bytes"], "blocking reset counters"
    run("sh", sys.argv[3]); run("sh", sys.argv[3])
    probe("resumed", True)
    print("IPv4/IPv6 TCP/UDP established flows blocked and resumed; NAT counters preserved")
finally:
    for pid, _, _ in children:
        try: os.kill(pid, 9)
        except ProcessLookupError: pass
        os.waitpid(pid, 0)
