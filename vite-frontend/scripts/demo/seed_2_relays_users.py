"""Step 2: tunnels, relays, forwards, users, limits, groups, latency points."""
import json, sqlite3, sys, time, urllib.request, random, math
B = "http://127.0.0.1:16370/api/v1"; DB = sys.argv[1]; st = json.load(open(DB + ".seed.json"))
tok, ids = st["token"], st["ids"]
now = int(time.time() * 1000)
def api(path, body=None):
    req = urllib.request.Request(B + path, data=json.dumps(body or {}).encode(), method="POST",
                                 headers={"Content-Type": "application/json", "Authorization": tok})
    with urllib.request.urlopen(req) as r:
        res = json.load(r)
    if res.get("code") != 0:
        print("!!", path, res.get("msg"))
    return res
db = sqlite3.connect(DB); x = db.execute
inb = {r[1]: (r[0], r[2]) for r in x("SELECT id, name, port FROM node_inbound")}
ip = {r[0]: r[1] for r in x("SELECT id, server_ip FROM node")}
land = {r[0]: r[1] for r in x("SELECT id, node_id FROM node_inbound")}

def tunnel(name, typ, entry, exit_=None):
    x("INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx) VALUES(?,1.0,?,'tls',99999,?,?,1,NULL,0)",
      (name, typ, now, now))
    tid = x("SELECT last_insert_rowid()").fetchone()[0]
    x("INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?,1,?,NULL,'round',1,'tls')", (tid, entry))
    if exit_:
        x("INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?,3,?,31000,'round',1,'tls')", (tid, exit_))
    return tid

def forward(tid, entry, port, remote, name, user=(1, "admin_user"), inflow=0, outflow=0):
    x("""INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
         VALUES(?,?,?,?,?,'fifo',?,?,?,?,1,0)""", (user[0], user[1], name, tid, remote, inflow, outflow, now, now))
    fid = x("SELECT last_insert_rowid()").fetchone()[0]
    x("INSERT INTO forward_port(forward_id, node_id, port) VALUES(?,?,?)", (fid, entry, port))
    return fid

sh, gz = ids["上海 移动中转"], ids["广州 电信中转"]
t_sh = tunnel("上海移动 端口转发", 1, sh)
t_gz = tunnel("广州电信 端口转发", 1, gz)
t_iplc = tunnel("广州 → 香港 隧道", 2, gz, ids["香港 HKT"])
G = 1024 ** 3
for (tid, entry, port, iname, title) in [
    (t_sh, sh, 31001, "香港 Reality", "上海移动 → 香港"), (t_sh, sh, 31002, "东京 Reality", "上海移动 → 东京"),
    (t_gz, gz, 32001, "圣何塞 AnyTLS", "广州电信 → 圣何塞"), (t_gz, gz, 32002, "新加坡 Shadowsocks", "广州电信 → 新加坡"),
]:
    iid, iport = inb[iname]
    fid = forward(tid, entry, port, f"{ip[land[iid]]}:{iport}", title, inflow=random.randint(5, 80) * G, outflow=random.randint(1, 9) * G)
    x("INSERT INTO node_inbound_relay(inbound_id, forward_id, name, share_host, managed, created_time) VALUES(?,?,?,'',1,?)", (iid, fid, title, now))
db.commit()

# latency targets
targets = [
    {"name": "北京电信", "host": "bj-ct.example.com", "port": 80, "enabled": True, "pinned": True},
    {"name": "上海联通", "host": "sh-cu.example.com", "port": 80, "enabled": True, "pinned": True},
    {"name": "广东移动", "host": "gd-cm.example.com", "port": 80, "enabled": True, "pinned": True},
    {"name": "北京联通", "host": "bj-cu.example.com", "port": 80, "enabled": True},
    {"name": "上海电信", "host": "sh-ct.example.com", "port": 80, "enabled": True},
    {"name": "广东电信", "host": "gd-ct.example.com", "port": 80, "enabled": True},
]
api("/config/update-single", {"name": "latency_targets", "value": json.dumps(targets, ensure_ascii=False)})
api("/config/update-single", {"name": "app_name", "value": "FLYX 演示"})
json.dump({**st, "tunnels": [t_sh, t_gz, t_iplc]}, open(DB + ".seed.json", "w"), ensure_ascii=False)
print("ok", users)
