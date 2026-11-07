// Visits every page (and the main dialogs) against the seeded backend and
// records each /api/v1 response, keyed by path and request body.
import { chromium } from "playwright-core";
import fs from "node:fs";
const B = "http://127.0.0.1:16370/api/v1";
const out = process.argv[2];
const login = (u, p) => fetch(B + "/user/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ username: u, password: p }) }).then(r => r.json()).then(r => r.data);
const browser = await chromium.launch({ executablePath: process.env.CHROMIUM });
const rec = {};
const errs = [];
async function session(role, user, pass, flows) {
  const d = await login(user, pass);
  const ctx = await browser.newContext({ viewport: { width: 1366, height: 1000 }, baseURL: "http://127.0.0.1:3114" });
  const page = await ctx.newPage(); page.setDefaultTimeout(4000);
  page.on("pageerror", (e) => errs.push(role + ": " + e.message));
  rec[role] = {};
  page.on("response", async (res) => {
    const u = new URL(res.url());
    if (!u.pathname.startsWith("/api/v1/") || res.request().method() === "OPTIONS") return;
    let json; try { json = await res.json(); } catch { return; }
    const path = u.pathname.slice("/api/v1/".length) + u.search;
    const body = res.request().postData() || "";
    (rec[role][path] ||= []);
    if (!rec[role][path].some((e) => e.body === body)) rec[role][path].push({ body, res: json });
  });
  await page.goto("/");
  await page.evaluate((s) => { localStorage.setItem("token", s.d.token); localStorage.setItem("role_id", String(s.d.role_id)); localStorage.setItem("name", s.d.name); localStorage.setItem("admin", String(s.d.role_id === 0)); }, { d });
  for (const [route, steps] of flows) {
    try {
      await page.goto(route); await page.waitForTimeout(1800);
      for (const step of steps || []) {
        try { await step(page); await page.waitForTimeout(900); } catch (e) { errs.push(`${role} ${route} step: ${e.message.split("\n")[0]}`); }
        await page.keyboard.press("Escape").catch(() => {}); await page.waitForTimeout(300);
      }
    } catch (e) { errs.push(`${role} ${route}: ${e.message.split("\n")[0]}`); }
  }
  await ctx.close();
}
const click = (text, opts = {}) => async (p) => { await p.getByText(text, opts).first().click(); };
const chain = (...fns) => async (p) => { for (const f of fns) { await f(p); await p.waitForTimeout(700); } };
await session("admin", "admin_user", "admin_user", [
  ["/dashboard"],
  ["/node", [click("香港 HKT")]],
  ["/inbound", [click("香港 Reality"), chain(click(/^内核/)), chain(click("订阅源"))]],
  ["/relay", [click("上海移动 → 香港")]],
  ["/forward"], ["/tunnel"], ["/panel-sharing"], ["/monitor"],
  ["/subscription", [
    click("小明 · 手机"),
    chain(click("小明 · 手机"), (p) => p.getByRole("button", { name: "编辑" }).first().click(), (p) => p.getByRole("tab", { name: /规则/ }).click(), (p) => p.getByRole("tab", { name: /预览/ }).click()),
    chain((p) => p.getByRole("button", { name: /^模板/ }).click(), click("我的分流"), (p) => p.getByRole("button", { name: "检查", exact: true }).click()),
  ]],
  ["/config"],
  ["/profile"],
]);
fs.writeFileSync(out, JSON.stringify(rec));
const n = (r) => Object.keys(rec[r]).length;
console.log("admin paths", n("admin"));
console.log(errs.slice(0, 30).join("\n"));
await browser.close();
