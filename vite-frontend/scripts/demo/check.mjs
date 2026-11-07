// Checks the built online demo the way a visitor sees it: serves the build
// under /flyx/demo/, opens every page in headless Chrome (as the admin, the
// panel's only account) and fails on page errors, pages that bounce to the login
// screen, and API calls the recording doesn't answer ("[demo] not
// recorded", see README.md). No dependencies: Chrome DevTools Protocol over
// Node's built-in WebSocket.
//
//   node scripts/demo/check.mjs <demo build dir>
//   CHROME=/path/to/chrome to pick the browser.
import { spawn } from "node:child_process";
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";

const dir = path.resolve(process.argv[2] || "dist-demo");
const BASE = "/flyx/demo/";
// Admins' 监控 redirects to 节点, which is already on the list.
const ADMIN_PAGES = ["dashboard", "node", "inbound", "relay", "forward", "tunnel", "subscription", "panel-sharing", "config", "profile"];
const SETTLE_MS = 2500;

const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".json": "application/json", ".webmanifest": "application/manifest+json", ".woff2": "font/woff2" };
const server = http.createServer((req, res) => {
  const url = decodeURIComponent(req.url.split("?")[0]);
  if (!url.startsWith(BASE)) return res.writeHead(404).end();
  let file = path.join(dir, url.slice(BASE.length));
  if (!file.startsWith(dir)) return res.writeHead(403).end();
  if (fs.existsSync(file) && fs.statSync(file).isDirectory()) file = path.join(file, "index.html");
  if (!fs.existsSync(file)) return res.writeHead(404).end();
  res.writeHead(200, { "Content-Type": TYPES[path.extname(file)] || "application/octet-stream" });
  fs.createReadStream(file).pipe(res);
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const origin = `http://127.0.0.1:${server.address().port}`;

const onPath = (bin) => (process.env.PATH || "").split(path.delimiter).some((d) => fs.existsSync(path.join(d, bin)));
const chromeBin = process.env.CHROME || ["google-chrome", "chromium", "chromium-browser"].find(onPath) || "google-chrome";
const profile = fs.mkdtempSync(path.join(os.tmpdir(), "demo-check-"));
const chrome = spawn(chromeBin, ["--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
  "--remote-debugging-port=0", `--user-data-dir=${profile}`, "--window-size=1366,900", "about:blank"]);
const wsURL = await new Promise((resolve, reject) => {
  let err = "";
  chrome.stderr.on("data", (d) => {
    err += d;
    const m = err.match(/DevTools listening on (ws:\/\/\S+)/);
    if (m) resolve(m[1]);
  });
  chrome.on("exit", (code) => reject(new Error(`chrome exited (${code}): ${err}`)));
  setTimeout(() => reject(new Error("chrome did not start")), 30000);
});

const ws = new WebSocket(wsURL);
await new Promise((r) => ws.addEventListener("open", r, { once: true }));
let nextId = 0;
const pending = new Map();
const listeners = [];
ws.addEventListener("message", (ev) => {
  const msg = JSON.parse(ev.data);
  if (msg.id && pending.has(msg.id)) {
    const { resolve, reject } = pending.get(msg.id);
    pending.delete(msg.id);
    msg.error ? reject(new Error(msg.error.message)) : resolve(msg.result);
  } else if (msg.method) {
    for (const l of listeners) l(msg);
  }
});
const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
  const id = ++nextId;
  pending.set(id, { resolve, reject });
  ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
});
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const { targetId } = await send("Target.createTarget", { url: "about:blank" });
const { sessionId } = await send("Target.attachToTarget", { targetId, flatten: true });
const page = (method, params) => send(method, params, sessionId);
await page("Page.enable");
await page("Runtime.enable");

let current = "";
const problems = [];
const notes = [];
listeners.push((msg) => {
  if (msg.sessionId !== sessionId) return;
  if (msg.method === "Runtime.exceptionThrown") {
    const d = msg.params.exceptionDetails;
    problems.push(`${current}: page error: ${d.exception?.description?.split("\n")[0] || d.text}`);
  }
  if (msg.method === "Runtime.consoleAPICalled") {
    const text = msg.params.args.map((a) => a.value ?? a.description ?? "").join(" ");
    if (text.startsWith("[demo] not recorded")) problems.push(`${current}: ${text}`);
    else if (msg.params.type === "error") notes.push(`${current}: console.error: ${text.slice(0, 200)}`);
  }
});
const evaluate = async (expression) => (await page("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true })).result.value;

async function visit(role, route) {
  current = `${role} #/${route}`;
  await page("Page.navigate", { url: `${origin}${BASE}#/${route}` });
  await sleep(SETTLE_MS);
  const state = await evaluate(`({ hash: location.hash, text: document.body.innerText.length })`);
  if (!state.hash.startsWith(`#/${route}`)) problems.push(`${current}: ended up at ${state.hash} (signed out?)`);
  else if (state.text < 40) problems.push(`${current}: blank page`);
}

current = "admin (first load)";
await page("Page.navigate", { url: `${origin}${BASE}` });
await sleep(SETTLE_MS);
for (const r of ADMIN_PAGES) await visit("admin", r);

for (const n of notes) console.log(`note  ${n}`);
for (const p of problems) console.log(`FAIL  ${p}`);
console.log(problems.length
  ? `\n${problems.length} problem(s). Recording missing for new pages? See scripts/demo/README.md.`
  : `demo ok: ${ADMIN_PAGES.length} pages`);

// Chrome keeps writing its profile until it has exited; clean up after
// that, and never let cleanup decide the result.
ws.close();
server.close();
await new Promise((r) => {
  chrome.once("exit", r);
  chrome.kill();
  setTimeout(r, 5000);
});
try {
  fs.rmSync(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
} catch {
  // a leftover temp directory is harmless
}
process.exit(problems.length ? 1 : 0);
