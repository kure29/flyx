# Turns the raw recording into src/demo/fixtures.json: canonical request
# keys, and the agent-side results a recording without agents lacks
# (latency, return routes, kernel state) filled with plausible values.
import json, sys, hashlib
raw = json.load(open(sys.argv[1]))
out_path = sys.argv[2]

BASE = {"香港 HKT": 36, "日本 东京": 58, "美国 圣何塞": 152, "新加坡": 71, "上海 移动中转": 8, "广州 电信中转": 6}
# Route names as the panel sends them (kernel classifyRoute).
_FULL = {"CN2 GIA": "电信 CN2 GIA", "CN2 GT": "电信 CN2 GT", "163": "电信 163", "9929": "联通 9929", "4837": "联通 4837", "CMIN2": "移动 CMIN2", "CMI": "移动 CMI"}
ROUTES = {node: {p: _FULL[r] for p, r in pts.items()} for node, pts in {
    "香港 HKT": {"北京电信": "CN2 GIA", "上海电信": "CN2 GIA", "广东电信": "CN2 GIA", "上海联通": "9929", "北京联通": "9929", "广东移动": "CMIN2"},
    "日本 东京": {"北京电信": "CN2 GT", "上海电信": "CN2 GT", "广东电信": "163", "上海联通": "4837", "北京联通": "4837", "广东移动": "CMI"},
    "美国 圣何塞": {"北京电信": "CN2 GIA", "上海电信": "CN2 GIA", "广东电信": "CN2 GIA", "上海联通": "9929", "北京联通": "4837", "广东移动": "CMIN2"},
    "新加坡": {"北京电信": "163", "上海电信": "163", "广东电信": "163", "上海联通": "4837", "北京联通": "4837", "广东移动": "CMI"},
}.items()}
# A dual-stack node's IPv6 routes where they differ from IPv4.
ROUTES_V6 = {"日本 东京": {"北京电信": "电信 CN2 GIA", "上海电信": "电信 163", "广东电信": "电信 CN2 GIA", "上海联通": "联通 9929"}}
POINT_IDX = {"北京电信": 0, "上海联通": 1, "广东移动": 2, "北京联通": 3, "上海电信": 4, "广东电信": 5}

def jitter(*parts):
    return int(hashlib.md5("|".join(map(str, parts)).encode()).hexdigest()[:4], 16) / 65535

def fake_points(points, node):
    base = BASE.get(node, 60)
    out = []
    for p in points:
        name = p["name"]; i = POINT_IDX.get(name, 0)
        q = {"name": name, "ok": True, "latencyMs": int(base * (1 + 0.07 * i) * (0.92 + 0.2 * jitter(node, name)))}
        if p.get("pinned"): q["pinned"] = True
        if (node, name) in {("美国 圣何塞", "北京联通"), ("新加坡", "广东移动")}: q["lossPct"] = 2
        r = ROUTES.get(node, {}).get(name)
        if r: q["route"] = r
        r6 = ROUTES_V6.get(node, {}).get(name)
        if r6: q["routeV6"] = r6
        out.append(q)
    return out

def fix_probe(probe, node):
    if not isinstance(probe, dict): return probe
    if probe.get("points"):
        probe["points"] = fake_points(probe["points"], node)
        probe["ok"] = True
        probe.pop("error", None)
    elif not probe.get("skipped"):
        probe.update({"ok": True, "latencyMs": int(40 + 60 * jitter(node, "panel"))}); probe.pop("error", None)
    return probe

def canon(body):
    if not body: return ""
    try: return json.dumps(json.loads(body), sort_keys=True, ensure_ascii=False, separators=(",", ":"))
    except Exception: return body

node_names = {}
for e in raw["admin"].get("node/list", []):
    for n in e["res"].get("data") or []:
        node_names[n["id"]] = n["name"]

def hops(node, name, route):
    net = {"电信 CN2 GIA": "CN2", "电信 CN2 GT": "CN2", "电信 163": "163", "联通 9929": "9929", "联通 4837": "4837", "移动 CMIN2": "CMI", "移动 CMI": "CMI"}.get(route, "")
    base = BASE.get(node, 60)
    hs = [{"ttl": 1, "ip": "10.0.0.1", "rttMs": 0.4}, {"ttl": 2, "ip": "203.0.113.254", "rttMs": 1.1}]
    for k in range(3, 9):
        hs.append({"ttl": k, "ip": f"59.43.{40 + k}.{int(200 * jitter(node, name, k))}", "rttMs": round(base * k / 8, 1), **({"net": net} if net and 3 <= k <= 6 else {})})
    return hs

def hops6(node, name, route):
    net = {"电信 CN2 GIA": "CN2", "电信 163": "CT", "联通 9929": "9929", "联通 4837": "4837", "移动 CMI": "CMI"}.get(route, "")
    pre = {"CN2": "2400:9380:9021", "CT": "240e:0:a", "9929": "2408:8120:2", "4837": "2408:8000:9000", "CMI": "2402:4f00:1000"}.get(net, "2001:db8")
    base = BASE.get(node, 60)
    hs = [{"ttl": 1, "ip": "2001:db8:100::1", "rttMs": 0.5}, {"ttl": 2, "ip": "2001:db8:ff::fe", "rttMs": 1.3}]
    for k in range(3, 9):
        hs.append({"ttl": k, "ip": f"{pre}::{k:x}", "rttMs": round(base * k / 8 + 2 * jitter(node, name, k), 1), **({"net": net} if net and 3 <= k <= 6 else {})})
    return hs

for role, paths in raw.items():
    if role == "recordedAt": continue
    for path, entries in paths.items():
        for e in entries:
            res = e["res"]; data = res.get("data")
            if path == "node/list" and isinstance(data, list):
                for n in data:
                    n["status"] = 1
                    n["latency"] = fix_probe(n.get("latency"), n["name"])
                    n["version"] = n.get("version") or "3.3.24"
            if path == "inbound/list" and isinstance(data, list):
                for i in data:
                    i["probe"] = fix_probe(i.get("probe"), i.get("nodeName") if not i.get("external") else i["name"])
                    if not i.get("external"): i["kernelStatus"] = "running"
            if path == "inbound/relay/list" and isinstance(data, list):
                for rl in data:
                    lat = rl.get("latency")
                    if not isinstance(lat, dict): continue
                    entry = lat.get("entry", "")
                    land = rl["name"].split("→")[-1].strip()
                    landing = {"香港": "香港 HKT", "东京": "日本 东京", "圣何塞": "美国 圣何塞", "新加坡": "新加坡"}.get(land, "香港 HKT")
                    leg = int(BASE.get(landing, 50) * 0.55 + 4)
                    lat["hops"] = [{**h, "ok": True, "latencyMs": leg} for h in lat.get("hops", [])]
                    lat.pop("error", None)
                    lat["ok"] = True
                    lat["pathMs"] = leg
                    entry_base = BASE.get(entry, 8)
                    pts = []
                    for p in lat.get("points", []):
                        i = POINT_IDX.get(p["name"], 0)
                        q = {"name": p["name"], "ok": True, "latencyMs": int(entry_base * (1 + 0.3 * i) * (0.9 + 0.2 * jitter(entry, p["name"]))) + leg}
                        if p.get("pinned"): q["pinned"] = True
                        pts.append(q)
                    lat["points"] = pts
            if path.startswith("monitor/services") and isinstance(data, list):
                for m in data:
                    if isinstance(m, dict) and "success" in m and m.get("errorMessage") == "节点不在线":
                        m.update({"success": 1, "latencyMs": round(12 + 30 * jitter("svc", m.get("monitorId")), 1), "errorMessage": ""})
            if path == "kernel/list" and isinstance(data, list):
                for k in data:
                    k.update({"status": "running", "nodeOnline": True, "synced": True, "lastError": ""})
            if path == "node/route":
                nid = json.loads(e["body"] or "{}").get("id")
                node = node_names.get(nid, "")
                if data and ROUTES.get(node):
                    data["points"] = [{"name": p["name"], "route": ROUTES[node].get(p["name"]), "reached": True,
                                       "hops": hops(node, p["name"], ROUTES[node].get(p["name"]))} for p in data["points"]]
                    for p in data["points"]:
                        r6 = ROUTES_V6.get(node, {}).get(p["name"]) or (p["route"] if node in ROUTES_V6 else None)
                        if r6: p["ipv6"] = {"name": p["name"], "route": r6, "reached": True, "family": "IPv6", "hops": hops6(node, p["name"], r6)}
                elif data:
                    data["points"] = [{"name": p["name"], "reached": True, "hops": hops(node, p["name"], "")} for p in data["points"]]
            if path == "kernel/logs":
                res.update({"code": 0, "msg": "操作成功", "data": {"type": "", "running": True, "enabled": True, "pid": 2817, "version": "",
                            "error": "", "restarts": 0, "startedAt": raw["recordedAt"] - 86400000 * 3,
                            "logs": ["[Info] started", "[Info] inbound listening on [::]:443", "[Info] accepted tcp connection"]}})
            e["key"] = canon(e.pop("body"))

# The seeded panel has no node traffic yet: give each node a month of it.
GB = 1024 ** 3
raw["admin"]["node/traffic/month"] = [{"key": "", "res": {"code": 0, "msg": "操作成功", "ts": raw["recordedAt"], "data": {
    str(nid): [int((120 + 380 * jitter(name, "in")) * GB), int((110 + 360 * jitter(name, "out")) * GB)]
    for nid, name in node_names.items()}}}]

fixtures = {"recordedAt": raw["recordedAt"], "admin": raw["admin"]}
json.dump(fixtures, open(out_path, "w"), ensure_ascii=False, separators=(",", ":"))
print("written", out_path)
