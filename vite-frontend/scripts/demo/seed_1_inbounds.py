"""Step 1: nodes, kernels, inbounds and imported nodes (see README.md)."""
import json, sqlite3, sys, time, urllib.request, random

B = "http://127.0.0.1:16370/api/v1"
DB = sys.argv[1]
now = int(time.time() * 1000)

def post(path, body=None, token=None):
    req = urllib.request.Request(B + path, data=json.dumps(body or {}).encode(), method="POST",
                                 headers={"Content-Type": "application/json", **({"Authorization": token} if token else {})})
    with urllib.request.urlopen(req) as r:
        return json.load(r)

tok = post("/user/login", {"username": "admin_user", "password": "admin_user"})["data"]["token"]
api = lambda p, b=None: post(p, b, tok)
db = sqlite3.connect(DB)
x = db.execute

NODES = [  # name, ip
    ("香港 HKT", "203.0.113.11"), ("日本 东京", "203.0.113.12"), ("美国 圣何塞", "198.51.100.13"),
    ("新加坡", "198.51.100.14"), ("上海 移动中转", "192.0.2.21"), ("广州 电信中转", "192.0.2.22"),
]
ids = {}
for i, (name, ip) in enumerate(NODES):
    x("""INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks,
         created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
         VALUES(?,?,?,?,'','30000-39999','','3.3.24',0,0,0,?,?,0,'[::]','[::]',?)""",
      (name, f"demo-secret-{i}", ip, ip, now - 86400000 * (30 - i), now, i))
    ids[name] = db.execute("SELECT last_insert_rowid()").fetchone()[0]
kernels = {}
for node, kt, ver in [("香港 HKT", "xray", "25.10.15"), ("日本 东京", "xray", "25.10.15"), ("日本 东京", "snell", "5.0.1"),
                      ("美国 圣何塞", "singbox", "1.14.3"), ("新加坡", "singbox", "1.14.3")]:
    x("INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?,?,?,'pending',?,?)",
      (ids[node], kt, ver, now, now))
    kernels[(node, kt)] = db.execute("SELECT last_insert_rowid()").fetchone()[0]
db.commit()

INB = [
    ("香港 HKT", "xray", {"name": "香港 Reality", "protocol": "vless", "port": 443, "settings": {"flow": "xtls-rprx-vision"},
                          "security": {"type": "reality", "sni": "www.microsoft.com"}}),
    ("日本 东京", "xray", {"name": "东京 Reality", "protocol": "vless", "port": 443, "settings": {"flow": "xtls-rprx-vision"},
                          "security": {"type": "reality", "sni": "www.apple.com"}}),
    ("日本 东京", "snell", {"name": "东京 Snell", "protocol": "snell", "port": 6160, "settings": {"snellVersion": 5}}),
    ("美国 圣何塞", "singbox", {"name": "圣何塞 Hysteria2", "protocol": "hysteria2", "port": 443, "settings": {"portHopping": "40000-50000"},
                              "security": {"type": "tls", "sni": "www.bing.com", "certMode": "self"}}),
    ("美国 圣何塞", "singbox", {"name": "圣何塞 AnyTLS", "protocol": "anytls", "port": 8443,
                              "security": {"type": "tls", "sni": "www.bing.com", "certMode": "self"}}),
    ("新加坡", "singbox", {"name": "新加坡 TUIC", "protocol": "tuic", "port": 443,
                         "security": {"type": "tls", "sni": "www.bing.com", "certMode": "self"}}),
    ("新加坡", "singbox", {"name": "新加坡 Shadowsocks", "protocol": "shadowsocks", "port": 8388,
                         "settings": {"method": "2022-blake3-aes-128-gcm"}}),
]
inb = {}
for node, kt, body in INB:
    res = api("/inbound/create", {"nodeId": ids[node], "kernelType": kt, **body})
    print("inbound", body["name"], res["code"], res["msg"][:60])
for row in db.execute("SELECT id, name FROM node_inbound"):
    inb[row[1]] = row[0]

ss = lambda name, host, port: "ss://" + __import__("base64").urlsafe_b64encode(b"aes-256-gcm:demo-password").decode().rstrip("=") + f"@{host}:{port}#" + urllib.parse.quote(name)
import urllib.parse
links = "\n".join([ss("机场 · 台湾 01", "203.0.113.31", 10001), ss("机场 · 韩国 01", "203.0.113.32", 10002), ss("机场 · 英国 01", "203.0.113.33", 10003)])
print("import", api("/inbound/import", {"links": links})["msg"])
json.dump({"ids": ids, "kernels": {f"{k[0]}|{k[1]}": v for k, v in kernels.items()}, "inb": inb, "token": tok},
          open(DB + ".seed.json", "w"), ensure_ascii=False)
