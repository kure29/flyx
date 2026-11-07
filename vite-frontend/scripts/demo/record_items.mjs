import fs from "node:fs";
const B = "http://127.0.0.1:16370/api/v1";
const file = process.argv[2];
const rec = JSON.parse(fs.readFileSync(file, "utf8"));
const tok = (await fetch(B + "/user/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ username: "admin_user", password: "admin_user" }) }).then(r => r.json())).data.token;
const call = async (method, path, body) => {
  const res = await fetch(B + "/" + path, { method, headers: { Authorization: tok, "Content-Type": "application/json" }, body: method === "POST" ? JSON.stringify(body ?? {}) : undefined }).then(r => r.json());
  const bodyText = method === "POST" ? JSON.stringify(body ?? {}) : "";
  (rec.admin[path] ||= []);
  if (!rec.admin[path].some((e) => e.body === bodyText)) rec.admin[path].push({ body: bodyText, res });
  return res;
};
const nodes = (await call("POST", "node/list")).data;
const inbounds = (await call("POST", "inbound/list")).data;
const kernels = (await call("POST", "kernel/list")).data;
const users = (await call("POST", "user/list", {})).data;
const now = Date.now();
for (const n of nodes) {
  for (const hours of [24, 72, 168]) await call("POST", "node/latency/history", { id: n.id, hours });
  await call("POST", "node/route", { id: n.id });
  await call("GET", `monitor/nodes/${n.id}/metrics?start=${now - 86400000}&end=${now}`);
}
for (const i of inbounds) { await call("POST", "inbound/share", { id: i.id }); await call("POST", "inbound/relay/list", { inboundId: i.id }); }
for (const k of (Array.isArray(kernels) ? kernels : kernels.items ?? [])) await call("POST", "kernel/logs", { id: k.id });
for (const u of users) { await call("POST", "user/groups", { id: u.id }); await call("POST", "tunnel/user/list", { userId: u.id }); }
await call("GET", "monitor/tunnels"); await call("GET", "monitor/tunnels/quality");
await call("POST", "inbound/sub/info", { reset: false });
await call("POST", "subscription/template/catalog");
rec.recordedAt = now;
fs.writeFileSync(file, JSON.stringify(rec));
console.log(Object.keys(rec.admin).length, "admin paths");
