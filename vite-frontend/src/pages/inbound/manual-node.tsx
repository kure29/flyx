import { Segmented } from "@/pages/subscription/controls";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";

// A node typed in field by field. It is turned into a standard share link
// and imported like a pasted one, so the backend parses and checks it the
// same way.

export type ManualProtocol =
  | "ss"
  | "vmess"
  | "vless"
  | "trojan"
  | "hysteria2"
  | "tuic"
  | "anytls"
  | "socks5";

type Security = "none" | "tls" | "reality";
type Network = "tcp" | "ws" | "grpc";

export interface ManualNode {
  protocol: ManualProtocol;
  name: string;
  host: string;
  port: string;
  method: string;
  uuid: string;
  password: string;
  username: string;
  alterId: string;
  flow: string;
  security: Security;
  sni: string;
  fingerprint: string;
  insecure: boolean;
  publicKey: string;
  shortId: string;
  network: Network;
  path: string;
  wsHost: string;
  serviceName: string;
  obfsPassword: string;
  ports: string;
  congestion: string;
}

export const MANUAL_PROTOCOLS: Array<[ManualProtocol, string]> = [
  ["ss", "Shadowsocks"],
  ["vless", "VLESS"],
  ["vmess", "VMess"],
  ["trojan", "Trojan"],
  ["hysteria2", "Hysteria2"],
  ["tuic", "TUIC v5"],
  ["anytls", "AnyTLS"],
  ["socks5", "SOCKS5"],
];

const FALLBACK_SS_METHODS = [
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305",
  "aes-128-gcm",
  "aes-256-gcm",
  "chacha20-ietf-poly1305",
];

const FINGERPRINTS = [
  "",
  "chrome",
  "firefox",
  "safari",
  "edge",
  "ios",
  "random",
];

export function emptyManualNode(): ManualNode {
  return {
    protocol: "ss",
    name: "",
    host: "",
    port: "",
    method: "2022-blake3-aes-128-gcm",
    uuid: "",
    password: "",
    username: "",
    alterId: "0",
    flow: "",
    security: "tls",
    sni: "",
    fingerprint: "",
    insecure: false,
    publicKey: "",
    shortId: "",
    network: "tcp",
    path: "",
    wsHost: "",
    serviceName: "",
    obfsPassword: "",
    ports: "",
    congestion: "bbr",
  };
}

function base64UTF8(s: string, url: boolean): string {
  const bytes = new TextEncoder().encode(s);
  let bin = "";

  for (const b of bytes) bin += String.fromCharCode(b);
  const b64 = btoa(bin);

  return url
    ? b64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
    : b64;
}

function randomUUID(): string {
  if (typeof crypto.randomUUID === "function") return crypto.randomUUID();
  // randomUUID needs a secure context; panels are often served over http.
  const b = crypto.getRandomValues(new Uint8Array(16));

  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = [...b].map((x) => x.toString(16).padStart(2, "0")).join("");

  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

const usesUUID = (p: ManualProtocol) =>
  p === "vmess" || p === "vless" || p === "tuic";
const usesPassword = (p: ManualProtocol) => p !== "vmess" && p !== "vless";
const usesTransport = (p: ManualProtocol) =>
  p === "vmess" || p === "vless" || p === "trojan";
const alwaysTLS = (p: ManualProtocol) =>
  p === "trojan" || p === "hysteria2" || p === "tuic" || p === "anytls";

function tlsOn(n: ManualNode): boolean {
  if (alwaysTLS(n.protocol)) return true;
  if (n.protocol === "vless") return n.security !== "none";
  if (n.protocol === "vmess") return n.security === "tls";

  return false;
}

/** The node as a share link, or an error message for a missing field. */
export function manualNodeLink(n: ManualNode): {
  link?: string;
  error?: string;
} {
  const host = n.host.trim().replace(/^\[|\]$/g, "");
  const port = Number(n.port);

  if (!host) return { error: "请填写地址" };
  if (/[\s/?#@]/.test(host)) return { error: "地址只填域名或 IP" };
  if (!Number.isInteger(port) || port < 1 || port > 65535)
    return { error: "端口应为 1–65535" };
  if (usesUUID(n.protocol) && !n.uuid.trim()) return { error: "请填写 UUID" };
  if (usesPassword(n.protocol) && n.protocol !== "socks5" && !n.password)
    return { error: "请填写密码" };
  if (n.protocol === "vless" && n.security === "reality" && !n.publicKey.trim())
    return { error: "Reality 需要填写公钥" };

  const addr = `${host.includes(":") ? `[${host}]` : host}:${port}`;
  const name = n.name.trim() || addr;
  const tag = `#${encodeURIComponent(name)}`;
  const enc = encodeURIComponent;
  const q = new URLSearchParams();
  const set = (k: string, v: string) => v.trim() && q.set(k, v.trim());
  const tls = tlsOn(n);

  if (tls) {
    set("sni", n.sni);
    set("fp", n.fingerprint);
    if (n.insecure && n.security !== "reality") q.set("insecure", "1");
  }
  if (usesTransport(n.protocol)) {
    q.set("type", n.network);
    if (n.network === "ws") {
      set("path", n.path);
      set("host", n.wsHost);
    }
    if (n.network === "grpc") set("serviceName", n.serviceName);
  }
  const query = () => (q.toString() ? `?${q.toString()}` : "");

  switch (n.protocol) {
    case "ss":
      return {
        link: `ss://${base64UTF8(`${n.method}:${n.password}`, true)}@${addr}${tag}`,
      };
    case "vmess": {
      const v: Record<string, string | boolean> = {
        v: "2",
        ps: name,
        add: host,
        port: String(port),
        id: n.uuid.trim(),
        aid: String(Number(n.alterId) || 0),
        scy: "auto",
        net: n.network,
        type: "none",
        host: n.network === "ws" ? n.wsHost.trim() : "",
        path:
          n.network === "ws"
            ? n.path.trim()
            : n.network === "grpc"
              ? n.serviceName.trim()
              : "",
        tls: tls ? "tls" : "",
        sni: tls ? n.sni.trim() : "",
        fp: tls ? n.fingerprint : "",
      };

      if (tls && n.insecure) v.allowInsecure = true;

      return { link: `vmess://${base64UTF8(JSON.stringify(v), false)}` };
    }
    case "vless":
      q.set("encryption", "none");
      q.set("security", n.security);
      if (n.security === "reality") {
        set("pbk", n.publicKey);
        set("sid", n.shortId);
      }
      set("flow", n.flow);

      return { link: `vless://${enc(n.uuid.trim())}@${addr}${query()}${tag}` };
    case "trojan":
      q.set("security", "tls");

      return { link: `trojan://${enc(n.password)}@${addr}${query()}${tag}` };
    case "hysteria2":
      if (n.obfsPassword) {
        q.set("obfs", "salamander");
        q.set("obfs-password", n.obfsPassword);
      }
      set("mport", n.ports);

      return {
        link: `hysteria2://${enc(n.password)}@${addr}/${query()}${tag}`,
      };
    case "tuic":
      q.set("alpn", "h3");
      set("congestion_control", n.congestion);
      q.set("udp_relay_mode", "native");

      return {
        link: `tuic://${enc(n.uuid.trim())}:${enc(n.password)}@${addr}${query()}${tag}`,
      };
    case "anytls":
      return { link: `anytls://${enc(n.password)}@${addr}/${query()}${tag}` };
    case "socks5": {
      const auth =
        n.username || n.password
          ? `${enc(n.username)}:${enc(n.password)}@`
          : "";

      return { link: `socks5://${auth}${addr}${tag}` };
    }
  }
}

// Select keys must not be empty; "" options travel under this one.
const NONE_KEY = "__none";

function Pick({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Array<[string, string]>;
  onChange: (v: string) => void;
}) {
  return (
    <Select
      label={label}
      selectedKeys={[value || NONE_KEY]}
      onSelectionChange={(keys) => {
        const k = Array.from(keys)[0];

        if (k !== undefined) onChange(String(k) === NONE_KEY ? "" : String(k));
      }}
    >
      {options.map(([k, text]) => (
        <SelectItem key={k || NONE_KEY} textValue={text}>
          {text}
        </SelectItem>
      ))}
    </Select>
  );
}

export function ManualNodeFields({
  value: n,
  onChange,
  ssMethods,
}: {
  value: ManualNode;
  onChange: (n: ManualNode) => void;
  ssMethods: string[];
}) {
  const set = (patch: Partial<ManualNode>) => onChange({ ...n, ...patch });
  const methods = ssMethods.length > 0 ? ssMethods : FALLBACK_SS_METHODS;
  const tls = tlsOn(n);
  const field = (
    key: keyof ManualNode,
    label: string,
    extra?: { placeholder?: string; description?: string; type?: string },
  ) => (
    <Input
      description={extra?.description}
      label={label}
      placeholder={extra?.placeholder}
      type={extra?.type}
      value={String(n[key])}
      variant="bordered"
      onChange={(e) => set({ [key]: e.target.value })}
    />
  );

  return (
    <div className="space-y-3">
      <Pick
        label="协议"
        options={MANUAL_PROTOCOLS}
        value={n.protocol}
        onChange={(v) => {
          const protocol = v as ManualProtocol;

          set({
            protocol,
            // VMess has no Reality; the other TLS protocols always use TLS.
            security:
              alwaysTLS(protocol) ||
              (protocol === "vmess" && n.security === "reality")
                ? "tls"
                : n.security,
          });
        }}
      />
      {field("name", "名称", { placeholder: "留空用 地址:端口" })}
      <div className="grid grid-cols-3 gap-3">
        <div className="col-span-2">
          {field("host", "地址", { placeholder: "域名或 IP" })}
        </div>
        {field("port", "端口", { type: "number", placeholder: "443" })}
      </div>

      {n.protocol === "ss" && (
        <Pick
          label="加密方式"
          options={methods.map((m) => [m, m])}
          value={n.method}
          onChange={(method) => set({ method })}
        />
      )}
      {usesUUID(n.protocol) && (
        <Input
          endContent={
            <Button
              size="sm"
              variant="light"
              onPress={() => set({ uuid: randomUUID() })}
            >
              生成
            </Button>
          }
          label="UUID"
          value={n.uuid}
          variant="bordered"
          onChange={(e) => set({ uuid: e.target.value })}
        />
      )}
      {n.protocol === "socks5" && field("username", "用户名（可选）")}
      {usesPassword(n.protocol) &&
        field("password", n.protocol === "socks5" ? "密码（可选）" : "密码", {
          description:
            n.protocol === "ss" && n.method.startsWith("2022-")
              ? "SS2022 的密码是 Base64 密钥，长度要和加密方式匹配"
              : undefined,
        })}
      {n.protocol === "vmess" &&
        field("alterId", "alterId", { type: "number", placeholder: "0" })}
      {n.protocol === "vless" && (
        <Pick
          label="流控 (flow)"
          options={[
            ["", "无"],
            ["xtls-rprx-vision", "xtls-rprx-vision"],
          ]}
          value={n.flow}
          onChange={(flow) => set({ flow })}
        />
      )}
      {n.protocol === "tuic" && (
        <Pick
          label="拥塞控制"
          options={[
            ["bbr", "bbr"],
            ["cubic", "cubic"],
            ["new_reno", "new_reno"],
          ]}
          value={n.congestion}
          onChange={(congestion) => set({ congestion })}
        />
      )}

      {(n.protocol === "vless" || n.protocol === "vmess") && (
        <div className="space-y-1.5">
          <p className="text-sm">安全</p>
          <Segmented
            options={
              n.protocol === "vless"
                ? [
                    ["none", "无"],
                    ["tls", "TLS"],
                    ["reality", "Reality"],
                  ]
                : [
                    ["none", "无"],
                    ["tls", "TLS"],
                  ]
            }
            value={n.security}
            onChange={(security) => set({ security: security as Security })}
          />
        </div>
      )}
      {tls && (
        <div className="space-y-3 rounded-xl border border-divider p-3">
          {field("sni", "SNI", {
            placeholder: "留空用地址",
            description:
              n.security === "reality" && n.protocol === "vless"
                ? "Reality 伪装的网站域名，比如 www.microsoft.com"
                : undefined,
          })}
          {n.protocol === "vless" && n.security === "reality" && (
            <>
              {field("publicKey", "公钥 (pbk)")}
              {field("shortId", "shortId (sid)", { placeholder: "可留空" })}
            </>
          )}
          {n.protocol !== "hysteria2" && n.protocol !== "tuic" && (
            <Pick
              label="指纹 (fp)"
              options={FINGERPRINTS.map((f) => [f, f || "默认"])}
              value={n.fingerprint}
              onChange={(fingerprint) => set({ fingerprint })}
            />
          )}
          {!(n.protocol === "vless" && n.security === "reality") && (
            <Checkbox
              isSelected={n.insecure}
              onValueChange={(insecure) => set({ insecure })}
            >
              <span className="text-sm">跳过证书验证（自签证书时打开）</span>
            </Checkbox>
          )}
        </div>
      )}

      {usesTransport(n.protocol) && (
        <div className="space-y-1.5">
          <p className="text-sm">传输</p>
          <Segmented
            options={[
              ["tcp", "TCP"],
              ["ws", "WebSocket"],
              ["grpc", "gRPC"],
            ]}
            value={n.network}
            onChange={(network) => set({ network: network as Network })}
          />
        </div>
      )}
      {usesTransport(n.protocol) && n.network === "ws" && (
        <div className="grid grid-cols-2 gap-3">
          {field("path", "路径", { placeholder: "/" })}
          {field("wsHost", "Host", { placeholder: "可留空" })}
        </div>
      )}
      {usesTransport(n.protocol) &&
        n.network === "grpc" &&
        field("serviceName", "serviceName")}

      {n.protocol === "hysteria2" && (
        <div className="grid grid-cols-2 gap-3">
          {field("obfsPassword", "混淆密码", { placeholder: "没有就留空" })}
          {field("ports", "端口跳跃", { placeholder: "如 20000-30000" })}
        </div>
      )}
    </div>
  );
}
