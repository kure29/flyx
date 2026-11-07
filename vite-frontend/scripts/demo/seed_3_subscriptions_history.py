"""Step 3: templates, subscriptions, monitors, 24h of metrics and latency, all online."""
import json, sqlite3, sys, time, urllib.request, random, math
B = "http://127.0.0.1:16370/api/v1"; DB = sys.argv[1]; st = json.load(open(DB + ".seed.json"))
tok, ids = st["token"], st["ids"]
now = int(time.time() * 1000); G = 1024 ** 3; DAY = 86400000
random.seed(7)
def api(path, body=None):
    req = urllib.request.Request(B + path, data=json.dumps(body or {}).encode(), method="POST",
                                 headers={"Content-Type": "application/json", "Authorization": tok})
    with urllib.request.urlopen(req) as r:
        res = json.load(r)
    if res.get("code") != 0:
        print("!!", path, res.get("msg"))
    return res
db = sqlite3.connect(DB); x = db.execute
inb = {r[1]: r[0] for r in x("SELECT id, name FROM node_inbound")}

MIHOMO = """mixed-port: 7890
mode: rule
proxies: [] # 面板填入订阅里的节点
proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies: [♻️ 自动选择, 🇭🇰 香港, 🇯🇵 日本, 🇺🇸 美国, DIRECT]
  - name: ♻️ 自动选择
    type: url-test
    include-all: true
    url: https://www.gstatic.com/generate_204
    interval: 300
  - name: 🇭🇰 香港
    type: url-test
    include-all: true
    filter: "(?i)港|HK|Hong Kong"
  - name: 🇯🇵 日本
    type: url-test
    include-all: true
    filter: "(?i)日本|东京|JP"
  - name: 🇺🇸 美国
    type: url-test
    include-all: true
    filter: "(?i)美国|圣何塞|US"
  - name: 🎬 流媒体
    type: select
    proxies: [🇭🇰 香港, 🇯🇵 日本, 🇺🇸 美国]
rule-providers:
  netflix: {type: http, behavior: domain, format: mrs, url: "https://github.com/MetaCubeX/meta-rules-dat/raw/meta/geo/geosite/netflix.mrs"}
rules:
  - GEOIP,LAN,DIRECT,no-resolve
  - RULE-SET,netflix,🎬 流媒体
  - GEOIP,CN,DIRECT
  - MATCH,🚀 节点选择
"""
SINGBOX = json.dumps({
    "dns": {"servers": [{"tag": "local", "type": "local"}]},
    "inbounds": [{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080}],
    "outbounds": [
        {"type": "selector", "tag": "🚀 节点选择", "outbounds": ["♻️ 自动选择", "{all}"]},
        {"type": "urltest", "tag": "♻️ 自动选择", "outbounds": ["{all}"]},
        {"type": "urltest", "tag": "🇭🇰 香港", "outbounds": ["{all}"], "filter": [{"action": "include", "keywords": ["港|HK"]}]},
        {"type": "direct", "tag": "direct"},
    ],
    "route": {"rules": [{"action": "sniff"}, {"ip_is_private": True, "outbound": "direct"}], "final": "🚀 节点选择", "default_domain_resolver": "local"},
}, ensure_ascii=False, indent=2)
t1 = api("/subscription/template/save", {"name": "我的分流", "kind": "clash", "content": MIHOMO, "sourceUrl": ""})["data"]["id"]
t2 = api("/subscription/template/save", {"name": "sing-box 简洁版", "kind": "singbox", "content": SINGBOX, "sourceUrl": ""})["data"]["id"]

def group(name, inbound_ids=None, **opts):
    body = {"name": name, "sourceAll": inbound_ids is None, "inboundIds": inbound_ids or [], "sourceIds": [], "options": opts}
    return api("/subscription/group/save", body)["data"]["id"]
g_all = group("全部节点", addFlag=True)
g_own = group("自建精选", [inb["香港 Reality"], inb["东京 Reality"], inb["圣何塞 Hysteria2"]], addFlag=True)
g_air = group("机场备用", [inb["机场 · 台湾 01"], inb["机场 · 韩国 01"], inb["机场 · 英国 01"]], addFlag=True)

def sub(name, groups, **kw):
    body = {"name": name, "kind": "share", "groupIds": groups, "options": {}, "expireTime": 0,
            "enabled": True, "remark": "", "trafficLimit": 0, "resetDay": 0}
    body.update(kw)
    return api("/subscription/save", body)["data"]["id"]
all_groups = [g["key"] for g in api("/subscription/list")["data"]["ruleGroups"]]
s1 = sub("全部节点", [g_all], options={"rules": True, "ruleGroups": all_groups})
s2 = sub("小明 · 手机", [g_all], options={"templates": {"clash": t1, "singbox": t2}}, trafficLimit=200 * G, resetDay=1,
         expireTime=now + 60 * DAY)
s3 = sub("阿强 · 电脑", [g_own], trafficLimit=100 * G, resetDay=15, expireTime=now + 5 * DAY)
s4 = sub("机场备用", [g_air, g_own])
x("UPDATE subscription SET upload_bytes=?, download_bytes=? WHERE id=?", (6 * G, 62 * G, s2))
x("UPDATE subscription SET upload_bytes=?, download_bytes=? WHERE id=?", (9 * G, 83 * G, s3))
for iid in inb.values():
    x("UPDATE node_inbound SET upload_bytes=?, download_bytes=? WHERE id=?", (random.randint(1, 20) * G, random.randint(20, 300) * G, iid))

# monitors
for name, typ, target, node in [("Google", "tcp", "www.google.com:443", "香港 HKT"), ("Netflix", "tcp", "www.netflix.com:443", "日本 东京"),
                                ("Cloudflare DNS", "icmp", "1.1.1.1", "新加坡")]:
    x("INSERT INTO service_monitor(name, type, target, interval_sec, timeout_sec, node_id, enabled, created_time, updated_time) VALUES(?,?,?,60,5,?,1,?,?)",
      (name, typ, target, ids[node], now, now))
    mid = x("SELECT last_insert_rowid()").fetchone()[0]
    for i in range(120):
        ok = random.random() > 0.02
        x("INSERT INTO service_monitor_result(monitor_id, node_id, timestamp, success, latency_ms, status_code, error_message) VALUES(?,?,?,?,?,0,?)",
          (mid, ids[node], now - i * 60000, 1 if ok else 0, round(random.uniform(8, 40), 1) if ok else 0, "" if ok else "timeout"))

# 24h of node metrics (every 5 min) and latency samples (every 10 min)
base_lat = {"香港 HKT": 38, "日本 东京": 62, "美国 圣何塞": 158, "新加坡": 72, "上海 移动中转": 9, "广州 电信中转": 7}
points = ["北京电信", "上海联通", "广东移动", "北京联通", "上海电信", "广东电信"]
for name, nid in ids.items():
    net_in = net_out = 0
    for i in range(288):
        t = now - (287 - i) * 300000
        hour = (t / 3600000) % 24
        busy = 0.5 + 0.5 * math.sin((hour - 14) / 24 * 2 * math.pi)
        sp_in = int((20 + 180 * busy) * 1024 * 1024 / 8 * random.uniform(0.7, 1.3))
        sp_out = int(sp_in * random.uniform(0.9, 1.1))
        net_in += sp_in * 300; net_out += sp_out * 300
        x("""INSERT INTO node_metric(node_id, timestamp, cpu_usage, mem_usage, disk_usage, net_in_bytes, net_out_bytes, net_in_speed, net_out_speed,
             load1, load5, load15, tcp_conns, udp_conns, uptime) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)""",
          (nid, t, round(5 + 40 * busy * random.uniform(0.6, 1.2), 1), round(30 + 15 * busy, 1), 23.5, net_in, net_out, sp_in, sp_out,
           round(0.2 + 1.5 * busy, 2), round(0.3 + 1.2 * busy, 2), round(0.4 + busy, 2), int(80 + 600 * busy), int(10 + 60 * busy), 86400 * 21 + i * 300))
    for i in range(144):
        t = now - (143 - i) * 600000
        hour = (t / 3600000) % 24
        evening = 1 + (0.6 if 19 <= hour <= 23 else 0)
        for j, p in enumerate(points):
            lat = int(base_lat[name] * (1 + 0.08 * j) * evening * random.uniform(0.92, 1.1))
            loss = 0 if random.random() > 0.08 else round(random.uniform(1, 8))
            x("INSERT INTO node_latency_sample(node_id, created_time, point, ok, latency_ms, loss_pct) VALUES(?,?,?,1,?,?)", (nid, t, p, lat, loss))

# everything online
x("UPDATE node SET status = 1")
x("UPDATE node_kernel SET status = 'running', installed_version = version")
x("UPDATE node_inbound SET enabled = 1")
db.commit()
json.dump({**st, "subs": [s1, s2, s3, s4], "templates": [t1, t2]}, open(DB + ".seed.json", "w"), ensure_ascii=False)
print("ok")
