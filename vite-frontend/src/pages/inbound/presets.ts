import type { SplitRuleForm } from "./split-rules";
import type {
  InboundApiItem,
  InboundMutationPayload,
  KernelCapabilitiesApiData,
  KernelProtocolCap,
} from "@/api/types";

export const PROTOCOL_LABELS: Record<string, string> = {
  vless: "VLESS",
  vmess: "VMess",
  trojan: "Trojan",
  shadowsocks: "Shadowsocks",
  hysteria2: "Hysteria2",
  tuic: "TUIC v5",
  anytls: "AnyTLS",
  snell: "Snell",
  naive: "NaïveProxy",
  socks: "SOCKS5",
  http: "HTTP",
  wireguard: "WireGuard",
};

export const KERNEL_LABELS: Record<string, string> = {
  xray: "Xray",
  singbox: "sing-box",
  snell: "Snell",
};

export const TRANSPORT_LABELS: Record<string, string> = {
  tcp: "TCP",
  ws: "WebSocket",
  grpc: "gRPC",
  xhttp: "XHTTP",
  quic: "QUIC",
  udp: "UDP",
};

export const SECURITY_LABELS: Record<string, string> = {
  none: "无",
  tls: "TLS",
  reality: "Reality",
  shadowtls: "ShadowTLS v3",
};

export const CERT_MODE_LABELS: Record<string, string> = {
  self: "自动生成自签证书",
  file: "使用服务器上的证书文件",
  acme: "自动申请证书（ACME）",
};

export const ACME_DNS_LABELS: Record<string, string> = {
  "": "HTTP 验证（需要服务器 80 端口）",
  cloudflare: "Cloudflare DNS",
  alidns: "阿里云 DNS",
};

export const KERNEL_STATUS: Record<
  string,
  { label: string; color: "success" | "warning" | "danger" | "default" }
> = {
  running: { label: "运行中", color: "success" },
  stopped: { label: "已停止", color: "default" },
  pending: { label: "待下发", color: "warning" },
  error: { label: "异常", color: "danger" },
  // The server is offline: the last reported state may be out of date.
  offline: { label: "服务器离线", color: "default" },
};

/** A kernel status for an inbound: "内核运行中", or "服务器离线". */
export function kernelStatusText(status: string): string {
  if (status === "offline") return KERNEL_STATUS.offline.label;

  return `内核${KERNEL_STATUS[status]?.label ?? "异常"}`;
}

export interface InboundForm {
  id?: number;
  nodeId: string;
  kernelType: string;
  name: string;
  protocol: string;
  listen: string;
  port: string;
  uuid: string;
  flowVision: boolean;
  password: string;
  method: string;
  snellVersion: string;
  snellMode: string;
  masquerade: string;
  backendPort: number;
  portHopping: string;
  username: string;
  /** VLESS Encryption (Xray); decryption keeps the stored server key. */
  vlessEnc: boolean;
  decryption: string;
  wgPrivateKey: string;
  wgPeerPrivateKey: string;
  wgPeerAddress: string;
  wgMtu: string;
  transportType: string;
  path: string;
  host: string;
  serviceName: string;
  xhttpMode: string;
  securityType: string;
  sni: string;
  alpn: string;
  fingerprint: string;
  certMode: string;
  certFile: string;
  keyFile: string;
  realityDest: string;
  realityPrivateKey: string;
  realityShortIds: string;
  realityMinClientVer: string;
  shadowTlsPassword: string;
  acmeEmail: string;
  acmeDns: string;
  acmeDnsKey: string;
  acmeDnsSecret: string;
  acmeDnsSet: boolean;
  shareAddressMode: "auto" | "ipv4" | "ipv6" | "custom";
  shareHost: string;
  remark: string;
  extra: string;
  relayOnly: boolean;
  acceptProxyProtocol: boolean;
  relayWhitelist: boolean;
  whitelistExtra: string;
  /** Chained outbound inbound id; "" leaves directly. */
  outboundId: string;
  outboundIpStrategy:
    | "auto"
    | "ipv4_only"
    | "ipv6_only"
    | "prefer_ipv4"
    | "prefer_ipv6";
  splitRules: SplitRuleForm[];
  enabled: boolean;
}

export interface Preset {
  key: string;
  label: string;
  description: string;
  kernel?: string; // only offered for this kernel
  protocol: string;
  transport: string;
  security: string;
  vision?: boolean;
  vlessEnc?: boolean;
  needsDomain?: boolean;
  certMode?: string;
}

export const PRESETS: Preset[] = [
  {
    key: "vless-reality-vision",
    label: "VLESS + Reality + Vision",
    description: "推荐：无需域名和证书，抗探测",
    protocol: "vless",
    transport: "tcp",
    security: "reality",
    vision: true,
  },
  {
    key: "vless-enc-vision",
    label: "VLESS + Encryption + Vision",
    description: "抗量子加密，无需域名和证书；客户端需 Xray 25.9+ 或 Mihomo",
    kernel: "xray",
    protocol: "vless",
    transport: "tcp",
    security: "none",
    vision: true,
    vlessEnc: true,
  },
  {
    key: "vless-xhttp-reality",
    label: "VLESS + XHTTP + Reality",
    description: "XHTTP 传输，适合中转/CDN 场景",
    protocol: "vless",
    transport: "xhttp",
    security: "reality",
  },
  {
    key: "ss2022",
    label: "Shadowsocks 2022",
    description: "轻量，客户端支持广泛",
    protocol: "shadowsocks",
    transport: "tcp",
    security: "none",
  },
  {
    key: "trojan-tls",
    label: "Trojan + TLS（自签）",
    description: "需要填写域名，自动生成自签证书",
    protocol: "trojan",
    transport: "tcp",
    security: "tls",
    needsDomain: true,
  },
  {
    key: "vmess-ws-tls",
    label: "VMess + WS + TLS",
    description: "兼容老客户端",
    protocol: "vmess",
    transport: "ws",
    security: "tls",
    needsDomain: true,
  },
  {
    key: "hysteria2",
    label: "Hysteria2",
    description: "基于 QUIC，弱网/高延迟线路表现好（自签证书）",
    kernel: "singbox",
    protocol: "hysteria2",
    transport: "quic",
    security: "tls",
  },
  {
    key: "tuic",
    label: "TUIC v5",
    description: "基于 QUIC，BBR 拥塞控制（自签证书）",
    kernel: "singbox",
    protocol: "tuic",
    transport: "quic",
    security: "tls",
  },
  {
    key: "anytls",
    label: "AnyTLS",
    description: "TLS 内多路复用，减少 TLS-in-TLS 特征",
    kernel: "singbox",
    protocol: "anytls",
    transport: "tcp",
    security: "tls",
  },
  {
    key: "ss2022-shadowtls",
    label: "SS2022 + ShadowTLS",
    description: "握手伪装成访问真实网站（仅 TCP）",
    kernel: "singbox",
    protocol: "shadowsocks",
    transport: "tcp",
    security: "shadowtls",
  },
  {
    key: "naive",
    label: "NaïveProxy",
    description: "伪装成正常 HTTPS 访问，需要域名（自动申请证书）",
    kernel: "singbox",
    protocol: "naive",
    transport: "tcp",
    security: "tls",
    needsDomain: true,
    certMode: "acme",
  },
  {
    key: "wireguard",
    label: "WireGuard",
    description:
      "组网，或给只支持 WireGuard 的设备用；所有人共用一份客户端配置",
    protocol: "wireguard",
    transport: "udp",
    security: "none",
  },
  {
    key: "socks5",
    label: "SOCKS5",
    description: "普通代理，不加密；适合内网或给其他程序用",
    protocol: "socks",
    transport: "tcp",
    security: "none",
  },
  {
    key: "http",
    label: "HTTP 代理",
    description: "普通 HTTP 代理，不加密；适合内网或给其他程序用",
    protocol: "http",
    transport: "tcp",
    security: "none",
  },
  {
    key: "snell",
    label: "Snell",
    description: "Surge 专用协议",
    protocol: "snell",
    transport: "tcp",
    security: "none",
  },
  {
    key: "snell-shadowtls",
    label: "Snell + ShadowTLS",
    description: "Surge 专用，握手伪装成访问真实网站",
    protocol: "snell",
    transport: "tcp",
    security: "shadowtls",
  },
];

/** Presets the given kernel can serve with its exact transport + security. */
export function presetsFor(
  caps: KernelCapabilitiesApiData | null,
  kernelType: string,
): Preset[] {
  return PRESETS.filter((p) => {
    if (p.kernel && p.kernel !== kernelType) return false;
    const cap = protocolCap(caps, kernelType, p.protocol);

    return Boolean(
      cap?.transports.some(
        (t) => t.type === p.transport && t.securities.includes(p.security),
      ),
    );
  });
}

export function emptyForm(
  caps: KernelCapabilitiesApiData | null,
  nodeId = "",
): InboundForm {
  return {
    nodeId,
    kernelType: "xray",
    name: "",
    protocol: "vless",
    listen: "::",
    port: "",
    uuid: "",
    flowVision: true,
    password: "",
    method: caps?.defaults.ssMethod ?? "2022-blake3-aes-128-gcm",
    snellVersion: "",
    snellMode: "",
    masquerade: "",
    backendPort: 0,
    portHopping: "",
    username: "",
    vlessEnc: false,
    decryption: "",
    wgPrivateKey: "",
    wgPeerPrivateKey: "",
    wgPeerAddress: "",
    wgMtu: "",
    transportType: "tcp",
    path: "",
    host: "",
    serviceName: "",
    xhttpMode: "auto",
    securityType: "reality",
    sni: caps?.defaults.realitySni ?? "",
    alpn: "",
    fingerprint: caps?.defaults.fingerprint ?? "chrome",
    certMode: "self",
    certFile: "",
    keyFile: "",
    realityDest: "",
    realityPrivateKey: "",
    realityShortIds: "",
    realityMinClientVer: "",
    shadowTlsPassword: "",
    acmeEmail: "",
    acmeDns: "",
    acmeDnsKey: "",
    acmeDnsSecret: "",
    acmeDnsSet: false,
    shareAddressMode: "auto",
    shareHost: "",
    remark: "",
    extra: "",
    relayOnly: false,
    acceptProxyProtocol: false,
    relayWhitelist: false,
    whitelistExtra: "",
    outboundId: "",
    outboundIpStrategy: "auto",
    splitRules: [],
    enabled: true,
  };
}

/** A name still holding a template or protocol label was filled in by us. */
export function isAutoName(name: string): boolean {
  const n = name.trim();

  return (
    n === "" ||
    PRESETS.some((p) => p.label === n) ||
    Object.values(PROTOCOL_LABELS).includes(n)
  );
}

export function applyPreset(
  form: InboundForm,
  preset: Preset,
  caps: KernelCapabilitiesApiData | null,
): InboundForm {
  const realitySni = caps?.defaults.realitySni ?? "";
  // A borrowed public site (Reality / ShadowTLS) must not leak into a TLS
  // SNI and vice versa; QUIC/AnyTLS fall back to the server default.
  const borrowed = (t: string) => t === "reality" || t === "shadowtls";
  let sni = form.sni;

  if (borrowed(preset.security)) {
    sni = borrowed(form.securityType) && form.sni ? form.sni : realitySni;
  } else if (borrowed(form.securityType)) {
    sni = "";
  }

  return {
    ...form,
    protocol: preset.protocol,
    transportType: preset.transport,
    securityType: preset.security,
    flowVision: Boolean(preset.vision),
    vlessEnc: Boolean(preset.vlessEnc),
    sni,
    certMode:
      preset.certMode ?? (form.certMode === "acme" ? "self" : form.certMode),
    name: isAutoName(form.name) ? preset.label : form.name,
  };
}

export function formFromItem(item: InboundApiItem): InboundForm {
  return {
    id: item.id,
    nodeId: String(item.nodeId),
    kernelType: item.kernelType,
    name: item.name,
    protocol: item.protocol,
    listen: item.listen,
    port: String(item.port),
    uuid: item.settings.uuid ?? "",
    flowVision: Boolean(item.settings.flow),
    password: item.settings.password ?? "",
    method: item.settings.method ?? "",
    snellVersion: item.settings.snellVersion
      ? String(item.settings.snellVersion)
      : "",
    snellMode: item.settings.snellMode ?? "",
    masquerade: item.settings.masquerade ?? "",
    backendPort: item.settings.backendPort ?? 0,
    portHopping: item.settings.portHopping ?? "",
    username: item.settings.username ?? "",
    vlessEnc: Boolean(item.settings.decryption),
    decryption: item.settings.decryption ?? "",
    wgPrivateKey: item.settings.privateKey ?? "",
    wgPeerPrivateKey: item.settings.peerPrivateKey ?? "",
    wgPeerAddress: item.settings.peerAddress ?? "",
    wgMtu: item.settings.mtu ? String(item.settings.mtu) : "",
    transportType: item.transport.type || "tcp",
    path: item.transport.path ?? "",
    host: item.transport.host ?? "",
    serviceName: item.transport.serviceName ?? "",
    xhttpMode: item.transport.mode || "auto",
    securityType: item.security.type || "none",
    sni: item.security.sni ?? "",
    alpn: (item.security.alpn ?? []).join(","),
    fingerprint: item.security.fingerprint ?? "",
    certMode: item.security.certMode || "self",
    certFile: item.security.certFile ?? "",
    keyFile: item.security.keyFile ?? "",
    realityDest: item.security.realityDest ?? "",
    realityPrivateKey: item.security.realityPrivateKey ?? "",
    realityShortIds: (item.security.realityShortIds ?? []).join(","),
    realityMinClientVer: item.security.realityMinClientVer ?? "",
    shadowTlsPassword: item.security.shadowTlsPassword ?? "",
    acmeEmail: item.security.acmeEmail ?? "",
    acmeDns: item.security.acmeDns ?? "",
    acmeDnsKey: "",
    acmeDnsSecret: "",
    acmeDnsSet: Boolean(item.security.acmeDnsSet),
    shareAddressMode:
      item.shareAddressMode ?? (item.shareHost ? "custom" : "auto"),
    shareHost: item.shareHost,
    remark: item.remark,
    extra: item.extra,
    relayOnly: item.relayOnly,
    acceptProxyProtocol: item.acceptProxyProtocol,
    relayWhitelist: item.relayWhitelist,
    whitelistExtra: item.whitelistExtra,
    outboundId: item.outboundId ? String(item.outboundId) : "",
    outboundIpStrategy: item.outboundIpStrategy || "auto",
    splitRules: splitFormFromItem(item),
    enabled: item.enabled,
  };
}

/** An inbound's 分流 rules as edited. */
export function splitFormFromItem(item: InboundApiItem): SplitRuleForm[] {
  return (item.splitRules ?? []).map((r) => ({
    categories: r.categories ?? [],
    domains: (r.domains ?? []).join(", "),
    ips: (r.ips ?? []).join(", "),
    action: r.action,
    outboundId: r.outboundId ? String(r.outboundId) : "",
  }));
}

export function splitPayload(
  rules: SplitRuleForm[],
): InboundMutationPayload["splitRules"] {
  return rules.map((r) => ({
    categories: r.categories,
    domains: splitList(r.domains),
    ips: splitList(r.ips),
    action: r.action,
    outboundId: r.action === "proxy" ? Number(r.outboundId) || 0 : undefined,
  }));
}

export const uuidProtocols = ["vless", "vmess", "tuic"];
export const passwordProtocols = [
  "trojan",
  "shadowsocks",
  "hysteria2",
  "tuic",
  "anytls",
  "snell",
  "naive",
  "socks",
  "http",
];

/** Protocols with a username next to the password. */
export const usernameProtocols = ["naive", "socks", "http"];

/** TLS on these protocols defaults its SNI server-side, so it is optional. */
export const sniOptionalProtocols = ["hysteria2", "tuic", "anytls"];

function splitList(v: string): string[] {
  return v
    .split(/[,，\s]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

export function visionAllowed(form: InboundForm): boolean {
  return (
    form.protocol === "vless" &&
    form.transportType === "tcp" &&
    (form.securityType !== "none" || (vlessEncAllowed(form) && form.vlessEnc))
  );
}

/** VLESS Encryption is an Xray feature. */
export function vlessEncAllowed(form: InboundForm): boolean {
  return form.protocol === "vless" && form.kernelType === "xray";
}

export function toPayload(form: InboundForm): InboundMutationPayload {
  const usesUUID = uuidProtocols.includes(form.protocol);
  const usesPassword = passwordProtocols.includes(form.protocol);

  return {
    id: form.id,
    nodeId: form.nodeId ? Number(form.nodeId) : undefined,
    kernelType: form.kernelType,
    name: form.name.trim(),
    protocol: form.protocol,
    listen: form.listen.trim(),
    port: form.port.trim() ? Number(form.port) : 0,
    settings: {
      uuid: usesUUID ? form.uuid.trim() : undefined,
      flow:
        visionAllowed(form) && form.flowVision ? "xtls-rprx-vision" : undefined,
      password: usesPassword ? form.password.trim() : undefined,
      method: form.protocol === "shadowsocks" ? form.method : undefined,
      snellVersion:
        form.protocol === "snell" && form.snellVersion
          ? Number(form.snellVersion)
          : undefined,
      snellMode:
        form.protocol === "snell" && form.snellVersion === "6"
          ? form.snellMode || undefined
          : undefined,
      masquerade:
        form.protocol === "hysteria2" ? form.masquerade.trim() : undefined,
      backendPort: form.backendPort || undefined,
      portHopping:
        form.protocol === "hysteria2"
          ? form.portHopping.trim() || undefined
          : undefined,
      username: usernameProtocols.includes(form.protocol)
        ? form.username.trim() || undefined
        : undefined,
      decryption:
        vlessEncAllowed(form) && form.vlessEnc
          ? form.decryption || "auto"
          : undefined,
      privateKey:
        form.protocol === "wireguard"
          ? form.wgPrivateKey || undefined
          : undefined,
      peerPrivateKey:
        form.protocol === "wireguard"
          ? form.wgPeerPrivateKey || undefined
          : undefined,
      peerAddress:
        form.protocol === "wireguard"
          ? form.wgPeerAddress.trim() || undefined
          : undefined,
      mtu:
        form.protocol === "wireguard" && form.wgMtu.trim()
          ? Number(form.wgMtu)
          : undefined,
    },
    transport: {
      type: form.transportType,
      path: form.path.trim() || undefined,
      host: form.host.trim() || undefined,
      serviceName: form.serviceName.trim() || undefined,
      mode: form.transportType === "xhttp" ? form.xhttpMode : undefined,
    },
    security: {
      type: form.securityType,
      sni: form.sni.trim() || undefined,
      alpn: form.alpn.trim() ? splitList(form.alpn) : undefined,
      fingerprint: form.fingerprint.trim() || undefined,
      certMode: form.securityType === "tls" ? form.certMode : undefined,
      certFile: form.certFile.trim() || undefined,
      keyFile: form.keyFile.trim() || undefined,
      realityDest: form.realityDest.trim() || undefined,
      realityPrivateKey: form.realityPrivateKey.trim() || undefined,
      realityShortIds: form.realityShortIds.trim()
        ? splitList(form.realityShortIds)
        : undefined,
      realityMinClientVer:
        form.securityType === "reality"
          ? form.realityMinClientVer.trim() || undefined
          : undefined,
      shadowTlsPassword:
        form.securityType === "shadowtls"
          ? form.shadowTlsPassword.trim() || undefined
          : undefined,
      ...(form.securityType === "tls" && form.certMode === "acme"
        ? {
            acmeEmail: form.acmeEmail.trim() || undefined,
            acmeDns: form.acmeDns || undefined,
            // Empty keeps the stored credentials when editing.
            acmeDnsKey: form.acmeDnsKey.trim() || undefined,
            acmeDnsSecret: form.acmeDnsSecret.trim() || undefined,
          }
        : {}),
    },
    extra: form.extra.trim(),
    shareAddressMode: form.shareAddressMode,
    shareHost: form.shareAddressMode === "custom" ? form.shareHost.trim() : "",
    remark: form.remark.trim(),
    relayOnly: form.relayOnly || form.acceptProxyProtocol,
    acceptProxyProtocol: form.acceptProxyProtocol,
    relayWhitelist: form.relayWhitelist,
    whitelistExtra: form.relayWhitelist ? form.whitelistExtra.trim() : "",
    outboundId: Number(form.outboundId) || 0,
    outboundIpStrategy: form.outboundIpStrategy,
    splitRules: splitPayload(form.splitRules),
    enabled: form.enabled,
  };
}

export function protocolCap(
  caps: KernelCapabilitiesApiData | null,
  kernelType: string,
  protocol: string,
): KernelProtocolCap | undefined {
  return caps?.kernels
    .find((k) => k.type === kernelType)
    ?.protocols?.find((p) => p.protocol === protocol);
}

export function describeInbound(item: InboundApiItem): string {
  if (item.importedSummary) return item.importedSummary;
  const parts = [PROTOCOL_LABELS[item.protocol] ?? item.protocol];

  const implicitTransport = [
    "shadowsocks",
    "snell",
    "hysteria2",
    "tuic",
    "anytls",
    "naive",
    "socks",
    "http",
    "wireguard",
  ];

  if (!implicitTransport.includes(item.protocol)) {
    parts.push(TRANSPORT_LABELS[item.transport.type] ?? item.transport.type);
  }
  if (item.protocol === "snell" && item.settings.snellVersion) {
    parts[0] = `Snell v${item.settings.snellVersion}`;
  }
  if (item.security.type && item.security.type !== "none") {
    parts.push(SECURITY_LABELS[item.security.type] ?? item.security.type);
  }
  if (item.settings.decryption) {
    parts.push("Encryption");
  }
  if (item.settings.flow) {
    parts.push("Vision");
  }
  if (item.security.certMode === "acme") {
    parts.push("ACME");
  }
  if (item.settings.portHopping) {
    parts.push("端口跳跃");
  }

  return parts.join(" + ");
}
