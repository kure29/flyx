import type {
  InboundApiItem,
  KernelApiItem,
  KernelCapabilitiesApiData,
  NodeApiItem,
} from "@/api/types";

import { ChevronDown, ChevronRight } from "lucide-react";
import { useMemo, useState } from "react";
import toast from "react-hot-toast";

import {
  ACME_DNS_LABELS,
  CERT_MODE_LABELS,
  PROTOCOL_LABELS,
  SECURITY_LABELS,
  TRANSPORT_LABELS,
  type InboundForm,
  applyPreset,
  isAutoName,
  passwordProtocols,
  presetsFor,
  protocolCap,
  sniOptionalProtocols,
  toPayload,
  usernameProtocols,
  uuidProtocols,
  visionAllowed,
  vlessEncAllowed,
} from "./presets";
import { DROP_NOTICE, savedDespiteDrop } from "./save-recovery";
import { OutboundIPSelect } from "./outbound-ip-select";

import { createInbound, updateInbound } from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input, Textarea } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Switch } from "@/shadcn-bridge/heroui/switch";

interface Props {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  caps: KernelCapabilitiesApiData | null;
  nodes: NodeApiItem[];
  kernels: KernelApiItem[];
  /** All inbounds, to confirm a save whose answer was lost. */
  inbounds: InboundApiItem[];
  initial: InboundForm;
  onSaved: () => void;
  onInstallKernel: (nodeId: number, kernelType: string) => void;
}

const XHTTP_MODES = ["auto", "packet-up", "stream-up", "stream-one"];

export function eligibleNodes(nodes: NodeApiItem[]): NodeApiItem[] {
  return nodes.filter(
    (n) => Number(n.isRemote ?? 0) !== 1 && n.forwardMode !== "nftables",
  );
}

/**
 * The node's IPv4 or IPv6 address for the share address: its own field,
 * else its server IP when that is of the family (as the panel resolves it).
 */
function nodeFamilyIP(node: NodeApiItem | undefined, v6: boolean): string {
  for (const raw of [
    v6 ? node?.serverIpV6 : node?.serverIpV4,
    node?.serverIp,
  ]) {
    const host = String(raw ?? "")
      .trim()
      .replace(/^\[|\]$/g, "");

    if (v6 ? host.includes(":") : /^\d{1,3}(\.\d{1,3}){3}$/.test(host))
      return host;
  }

  return "";
}

/** Templates shown before "更多模板". */
const PRESETS_SHOWN = 3;

export function InboundFormModal({
  isOpen,
  onOpenChange,
  caps,
  nodes,
  kernels,
  inbounds,
  initial,
  onSaved,
  onInstallKernel,
}: Props) {
  const [form, setForm] = useState<InboundForm>(initial);
  const [initialKey, setInitialKey] = useState(initial);
  const [saving, setSaving] = useState(false);
  // New inbounds show only the essentials; editing opens the details.
  const advancedOpen = (f: InboundForm) =>
    Boolean(f.id) || f.outboundIpStrategy !== "auto";
  const [showAdvanced, setShowAdvanced] = useState(() => advancedOpen(initial));
  const [showAllPresets, setShowAllPresets] = useState(false);
  const isEdit = Boolean(initial.id);
  const selectedNode = nodes.find((node) => String(node.id) === form.nodeId);
  const shareV4 = nodeFamilyIP(selectedNode, false);
  const shareV6 = nodeFamilyIP(selectedNode, true);

  // Reset when the parent opens the modal with a different item.
  if (initialKey !== initial) {
    setInitialKey(initial);
    setForm(initial);
    setShowAdvanced(advancedOpen(initial));
    setShowAllPresets(false);
  }

  const set = <K extends keyof InboundForm>(key: K, value: InboundForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  const implementedKernels = useMemo(
    () => (caps?.kernels ?? []).filter((k) => k.implemented && !k.relay),
    [caps],
  );
  const kernelInfo = caps?.kernels.find((k) => k.type === form.kernelType);
  const installedKernel = kernels.find(
    (k) => String(k.nodeId) === form.nodeId && k.kernelType === form.kernelType,
  );
  // snell-server 6.x only serves v6 clients, so new inbounds default to it.
  const defaultSnellVersion =
    form.kernelType === "snell" && installedKernel?.version?.startsWith("6.")
      ? "6"
      : "5";
  // NaïveProxy needs a certificate clients trust, so no self-signed option.
  const certModes = (kernelInfo?.certModes ?? ["self", "file"]).filter(
    (m) => !(form.protocol === "naive" && m === "self"),
  );
  const certMode = certModes.includes(form.certMode)
    ? form.certMode
    : (certModes[0] ?? "self");
  const pCap = protocolCap(caps, form.kernelType, form.protocol);
  const transports = pCap?.transports ?? [];
  const securities =
    transports.find((t) => t.type === form.transportType)?.securities ?? [];

  // Reality / ShadowTLS borrow a public site's name; it must never leak into
  // a TLS SNI (a self-signed cert would be issued for it), and vice versa.
  const borrowed = (t: string) => t === "reality" || t === "shadowtls";
  const nextSni = (prevSecurity: string, prevSni: string, security: string) => {
    if (borrowed(security)) {
      return borrowed(prevSecurity) && prevSni
        ? prevSni
        : (caps?.defaults.realitySni ?? "");
    }

    return borrowed(prevSecurity) ? "" : prevSni;
  };

  const changeKernel = (kernelType: string) => {
    const info = caps?.kernels.find((k) => k.type === kernelType);
    const first = presetsFor(caps, kernelType)[0];

    setForm((prev) => {
      const next: InboundForm = {
        ...prev,
        kernelType,
        outboundIpStrategy:
          kernelType === "xray" || kernelType === "singbox"
            ? prev.outboundIpStrategy
            : "auto",
      };

      if (info?.protocols?.some((p) => p.protocol === prev.protocol)) {
        return next;
      }
      if (first) return applyPreset(next, first, caps);
      const proto = info?.protocols?.[0];

      return {
        ...next,
        protocol: proto?.protocol ?? prev.protocol,
        transportType: proto?.transports[0]?.type ?? "tcp",
        securityType: proto?.transports[0]?.securities[0] ?? "none",
      };
    });
  };

  const pickSecurity = (secs: string[], current: string) =>
    secs.includes(current) ? current : (secs[0] ?? "none");

  const changeProtocol = (protocol: string) => {
    const cap = protocolCap(caps, form.kernelType, protocol);
    const transport = cap?.transports.some((t) => t.type === form.transportType)
      ? form.transportType
      : (cap?.transports[0]?.type ?? "tcp");
    const secs =
      cap?.transports.find((t) => t.type === transport)?.securities ?? [];

    setForm((prev) => {
      const security = pickSecurity(secs, prev.securityType);

      return {
        ...prev,
        protocol,
        transportType: transport,
        securityType: security,
        sni: nextSni(prev.securityType, prev.sni, security),
        name:
          prev.name.trim() && isAutoName(prev.name)
            ? (PROTOCOL_LABELS[protocol] ?? protocol)
            : prev.name,
      };
    });
  };

  const changeTransport = (transportType: string) => {
    const secs =
      transports.find((t) => t.type === transportType)?.securities ?? [];

    setForm((prev) => {
      const security = pickSecurity(secs, prev.securityType);

      return {
        ...prev,
        transportType,
        securityType: security,
        sni: nextSni(prev.securityType, prev.sni, security),
      };
    });
  };

  const changeSecurity = (securityType: string) => {
    setForm((prev) => ({
      ...prev,
      securityType,
      sni: nextSni(prev.securityType, prev.sni, securityType),
    }));
  };

  const validate = (): string | null => {
    if (!form.nodeId) return "请选择服务器";
    if (!installedKernel) return "该服务器还没有安装所选内核";
    if (!form.name.trim()) return "请输入名称";
    if (form.shareAddressMode === "custom" && !form.shareHost.trim())
      return "请输入自定义分享地址";
    if (form.shareAddressMode === "ipv4" && !shareV4)
      return "请先在服务器设置 IPv4 地址";
    if (form.shareAddressMode === "ipv6" && !shareV6)
      return "请先在服务器设置 IPv6 地址";
    if (form.port.trim()) {
      const port = Number(form.port);

      if (!Number.isInteger(port) || port < 1 || port > 65535) {
        return "端口必须在 1-65535 之间";
      }
    }
    if (
      form.securityType === "tls" &&
      !form.sni.trim() &&
      !sniOptionalProtocols.includes(form.protocol)
    ) {
      return "TLS 需要填写域名";
    }
    if (form.securityType === "tls" && certMode === "acme") {
      if (!form.sni.trim()) return "自动申请证书需要填写解析到该服务器的域名";
      const needKey = !(isEdit && form.acmeDnsSet);

      if (form.acmeDns === "cloudflare" && needKey && !form.acmeDnsKey.trim()) {
        return "请填写 Cloudflare API Token";
      }
      if (
        form.acmeDns === "alidns" &&
        needKey &&
        (!form.acmeDnsKey.trim() || !form.acmeDnsSecret.trim())
      ) {
        return "请填写阿里云 AccessKey ID 和 Secret";
      }
    }
    if (form.protocol === "hysteria2" && form.portHopping.trim()) {
      const m = form.portHopping.trim().match(/^(\d+)\s*[-:]\s*(\d+)$/);

      if (
        !m ||
        Number(m[1]) < 1024 ||
        Number(m[2]) > 65535 ||
        Number(m[1]) >= Number(m[2])
      ) {
        return "端口跳跃格式为 起始-结束（1024-65535），例如 20000-30000";
      }
    }
    if (form.securityType === "tls" && certMode === "file") {
      if (!form.certFile.trim() || !form.keyFile.trim()) {
        return "请填写证书和私钥在服务器上的路径";
      }
    }
    if (form.extra.trim()) {
      try {
        const parsed = JSON.parse(form.extra);

        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
          return "高级配置必须是 JSON 对象";
        }
      } catch {
        return "高级配置不是合法的 JSON";
      }
    }

    return null;
  };

  const submit = async () => {
    const err = validate();

    if (err) {
      toast.error(err);
      // The field may be in the folded section.
      setShowAdvanced(true);

      return;
    }
    setSaving(true);
    try {
      const payload = toPayload({
        ...form,
        certMode,
        snellVersion: form.snellVersion || defaultSnellVersion,
      });
      const res = isEdit
        ? await updateInbound(payload)
        : await createInbound(payload);

      if (res.code !== 0) {
        const prev = inbounds.find((i) => i.id === form.id);

        if (
          isEdit &&
          prev &&
          (await savedDespiteDrop(res, prev.id, prev.updatedTime))
        ) {
          toast.success(DROP_NOTICE, { duration: 6000 });
          onOpenChange(false);
          onSaved();

          return;
        }
        toast.error(res.msg || "保存失败");

        return;
      }
      if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️", duration: 6000 });
      } else {
        toast.success(isEdit ? "已保存并下发" : "已创建并下发");
      }
      onOpenChange(false);
      onSaved();
    } finally {
      setSaving(false);
    }
  };

  const nodeOptions = eligibleNodes(nodes);

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isOpen}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={onOpenChange}
    >
      <ModalContent>
        {(onClose) => (
          <>
            <ModalHeader>
              <h2 className="text-base font-semibold">
                {isEdit ? "编辑入站" : "新建入站"}
              </h2>
            </ModalHeader>
            <ModalBody>
              <div className="space-y-4">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <Select
                    isDisabled={isEdit}
                    label="服务器"
                    placeholder="选择服务器"
                    selectedKeys={form.nodeId ? [form.nodeId] : []}
                    onSelectionChange={(keys) =>
                      set("nodeId", String(Array.from(keys)[0] ?? ""))
                    }
                  >
                    {nodeOptions.map((n) => (
                      <SelectItem
                        key={String(n.id)}
                        textValue={
                          n.status === 1 ? n.name : `${n.name}（离线）`
                        }
                      >
                        {n.status === 1 ? n.name : `${n.name}（离线）`}
                      </SelectItem>
                    ))}
                  </Select>
                  <Select
                    isDisabled={isEdit}
                    label="内核"
                    selectedKeys={[form.kernelType]}
                    onSelectionChange={(keys) =>
                      changeKernel(String(Array.from(keys)[0] ?? "xray"))
                    }
                  >
                    {implementedKernels.map((k) => (
                      <SelectItem key={k.type} textValue={k.name}>
                        {k.name}
                      </SelectItem>
                    ))}
                  </Select>
                </div>

                {form.nodeId && !installedKernel && (
                  <div className="flex items-center justify-between gap-3 rounded-lg bg-warning/10 px-3 py-2 text-sm">
                    <span className="text-warning-700 dark:text-warning-400">
                      该服务器还没有安装 {kernelInfo?.name ?? form.kernelType}
                    </span>
                    <Button
                      color="warning"
                      size="sm"
                      variant="flat"
                      onPress={() =>
                        onInstallKernel(Number(form.nodeId), form.kernelType)
                      }
                    >
                      去安装
                    </Button>
                  </div>
                )}

                {!isEdit && (
                  <div>
                    <p className="mb-1.5 text-sm text-default-600">模板</p>
                    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                      {(() => {
                        const all = presetsFor(caps, form.kernelType);
                        const isActive = (p: (typeof all)[number]) =>
                          form.protocol === p.protocol &&
                          form.transportType === p.transport &&
                          form.securityType === p.security;
                        // Keep a chosen template in view even when folded.
                        const folded =
                          !showAllPresets &&
                          all.length > PRESETS_SHOWN + 1 &&
                          !all.slice(PRESETS_SHOWN).some(isActive);
                        const shown = folded
                          ? all.slice(0, PRESETS_SHOWN)
                          : all;

                        return (
                          <>
                            {shown.map((p) => (
                              <button
                                key={p.key}
                                className={`rounded-lg border px-3 py-2 text-left transition-colors ${
                                  isActive(p)
                                    ? "border-foreground"
                                    : "border-divider hover:bg-default-50"
                                }`}
                                type="button"
                                onClick={() =>
                                  setForm((prev) => applyPreset(prev, p, caps))
                                }
                              >
                                <div className="text-sm">{p.label}</div>
                                <div className="truncate text-xs text-default-400">
                                  {p.description}
                                </div>
                              </button>
                            ))}
                            {folded && (
                              <button
                                className="flex items-center gap-1 rounded-lg border border-dashed border-divider px-3 py-2 text-left text-sm text-default-500 transition-colors hover:bg-default-50 hover:text-foreground"
                                type="button"
                                onClick={() => setShowAllPresets(true)}
                              >
                                更多模板（{all.length - PRESETS_SHOWN}）
                                <ChevronDown className="h-4 w-4" />
                              </button>
                            )}
                          </>
                        );
                      })()}
                    </div>
                  </div>
                )}

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  <Input
                    label="名称"
                    placeholder="例如：东京-Reality"
                    value={form.name}
                    variant="bordered"
                    onChange={(e) => set("name", e.target.value)}
                  />
                  <Input
                    label="端口"
                    placeholder="留空随机"
                    type="number"
                    value={form.port}
                    variant="bordered"
                    onChange={(e) => set("port", e.target.value)}
                  />
                </div>

                {(form.protocol === "socks" || form.protocol === "http") && (
                  <p className="text-xs text-warning">
                    {PROTOCOL_LABELS[form.protocol]}
                    不加密，流量和密码都是明文，适合内网或经加密线路中转后使用
                  </p>
                )}
                {form.securityType === "tls" && (
                  <div className="space-y-3 rounded-lg border border-divider p-3">
                    <p className="text-sm font-medium">TLS</p>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                      <Input
                        label="域名 (SNI)"
                        placeholder={
                          sniOptionalProtocols.includes(form.protocol) &&
                          certMode === "self"
                            ? "留空使用 www.bing.com（自签）"
                            : "node.example.com"
                        }
                        value={form.sni}
                        variant="bordered"
                        onChange={(e) => set("sni", e.target.value)}
                      />
                      <Select
                        label="证书"
                        selectedKeys={[certMode]}
                        onSelectionChange={(keys) =>
                          set(
                            "certMode",
                            String(Array.from(keys)[0] ?? certMode),
                          )
                        }
                      >
                        {certModes.map((m) => (
                          <SelectItem
                            key={m}
                            textValue={CERT_MODE_LABELS[m] ?? m}
                          >
                            {CERT_MODE_LABELS[m] ?? m}
                          </SelectItem>
                        ))}
                      </Select>
                    </div>
                    {certMode === "acme" && (
                      <div className="space-y-3">
                        <p className="text-xs text-default-500">
                          由 sing-box 向 Let&apos;s Encrypt
                          申请并自动续期，域名需要解析到该服务器。 HTTP
                          验证要求服务器 80 端口可从公网访问；用 DNS
                          验证则不需要。
                        </p>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                          <Input
                            label="邮箱（可选）"
                            placeholder="用于证书到期提醒"
                            value={form.acmeEmail}
                            variant="bordered"
                            onChange={(e) => set("acmeEmail", e.target.value)}
                          />
                          <Select
                            label="验证方式"
                            selectedKeys={[form.acmeDns || "http"]}
                            onSelectionChange={(keys) => {
                              const v = String(Array.from(keys)[0] ?? "http");

                              setForm((prev) => ({
                                ...prev,
                                acmeDns: v === "http" ? "" : v,
                                acmeDnsKey: "",
                                acmeDnsSecret: "",
                                acmeDnsSet: false,
                              }));
                            }}
                          >
                            {Object.entries(ACME_DNS_LABELS).map(
                              ([k, label]) => (
                                <SelectItem key={k || "http"} textValue={label}>
                                  {label}
                                </SelectItem>
                              ),
                            )}
                          </Select>
                        </div>
                        {form.acmeDns && (
                          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                            <Input
                              label={
                                form.acmeDns === "cloudflare"
                                  ? "API Token"
                                  : "AccessKey ID"
                              }
                              placeholder={
                                form.acmeDnsSet
                                  ? "已保存，留空不修改"
                                  : undefined
                              }
                              type="password"
                              value={form.acmeDnsKey}
                              variant="bordered"
                              onChange={(e) =>
                                set("acmeDnsKey", e.target.value)
                              }
                            />
                            {form.acmeDns === "alidns" && (
                              <Input
                                label="AccessKey Secret"
                                placeholder={
                                  form.acmeDnsSet
                                    ? "已保存，留空不修改"
                                    : undefined
                                }
                                type="password"
                                value={form.acmeDnsSecret}
                                variant="bordered"
                                onChange={(e) =>
                                  set("acmeDnsSecret", e.target.value)
                                }
                              />
                            )}
                          </div>
                        )}
                      </div>
                    )}
                    {certMode === "file" && (
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <Input
                          label="证书路径"
                          placeholder="/etc/ssl/fullchain.pem"
                          value={form.certFile}
                          variant="bordered"
                          onChange={(e) => set("certFile", e.target.value)}
                        />
                        <Input
                          label="私钥路径"
                          placeholder="/etc/ssl/privkey.pem"
                          value={form.keyFile}
                          variant="bordered"
                          onChange={(e) => set("keyFile", e.target.value)}
                        />
                      </div>
                    )}
                    {form.protocol !== "hysteria2" &&
                      form.protocol !== "tuic" && (
                        <Input
                          label="ALPN（可选）"
                          placeholder="h2,http/1.1"
                          value={form.alpn}
                          variant="bordered"
                          onChange={(e) => set("alpn", e.target.value)}
                        />
                      )}
                  </div>
                )}

                <button
                  aria-expanded={showAdvanced}
                  className="flex items-center gap-1 whitespace-nowrap text-sm text-default-500 hover:text-foreground"
                  type="button"
                  onClick={() => setShowAdvanced((v) => !v)}
                >
                  <ChevronRight
                    className={`h-4 w-4 transition-transform ${showAdvanced ? "rotate-90" : ""}`}
                  />
                  高级设置
                  {!showAdvanced && (
                    <span className="hidden text-xs text-default-400 sm:inline">
                      （协议、传输、UUID、密钥、监听与中转；留空自动生成）
                    </span>
                  )}
                </button>
                {showAdvanced && (
                  <div className="space-y-4">
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                      <Select
                        label="协议"
                        selectedKeys={[form.protocol]}
                        onSelectionChange={(keys) =>
                          changeProtocol(String(Array.from(keys)[0] ?? "vless"))
                        }
                      >
                        {(kernelInfo?.protocols ?? []).map((p) => (
                          <SelectItem
                            key={p.protocol}
                            textValue={
                              PROTOCOL_LABELS[p.protocol] ?? p.protocol
                            }
                          >
                            {PROTOCOL_LABELS[p.protocol] ?? p.protocol}
                          </SelectItem>
                        ))}
                      </Select>
                      <Select
                        isDisabled={transports.length <= 1}
                        label="传输"
                        selectedKeys={[form.transportType]}
                        onSelectionChange={(keys) =>
                          changeTransport(String(Array.from(keys)[0] ?? "tcp"))
                        }
                      >
                        {transports.map((t) => (
                          <SelectItem
                            key={t.type}
                            textValue={TRANSPORT_LABELS[t.type] ?? t.type}
                          >
                            {TRANSPORT_LABELS[t.type] ?? t.type}
                          </SelectItem>
                        ))}
                      </Select>
                      <Select
                        isDisabled={securities.length <= 1}
                        label="安全"
                        selectedKeys={[form.securityType]}
                        onSelectionChange={(keys) =>
                          changeSecurity(String(Array.from(keys)[0] ?? "none"))
                        }
                      >
                        {securities.map((s) => (
                          <SelectItem
                            key={s}
                            textValue={SECURITY_LABELS[s] ?? s}
                          >
                            {SECURITY_LABELS[s] ?? s}
                          </SelectItem>
                        ))}
                      </Select>
                    </div>

                    {uuidProtocols.includes(form.protocol) && (
                      <Input
                        label="UUID"
                        placeholder="留空自动生成"
                        value={form.uuid}
                        variant="bordered"
                        onChange={(e) => set("uuid", e.target.value)}
                      />
                    )}
                    {vlessEncAllowed(form) && (
                      <div className="flex items-start gap-2">
                        <Switch
                          isSelected={form.vlessEnc}
                          size="sm"
                          onValueChange={(v) => set("vlessEnc", v)}
                        />
                        <div>
                          <p className="text-sm">
                            VLESS Encryption（抗量子加密）
                          </p>
                          <p className="text-xs text-default-500">
                            不用证书也能防探测，密钥自动生成。客户端需要 Xray
                            25.9+（v2rayN 等）或 Mihomo；sing-box、Surge
                            等客户端的订阅里会略过它
                          </p>
                        </div>
                      </div>
                    )}
                    {visionAllowed(form) && (
                      <div className="flex items-center gap-2">
                        <Switch
                          isSelected={form.flowVision}
                          size="sm"
                          onValueChange={(v) => set("flowVision", v)}
                        />
                        <span className="text-sm">启用 XTLS Vision 流控</span>
                      </div>
                    )}
                    {form.protocol === "shadowsocks" && (
                      <Select
                        label="加密方式"
                        selectedKeys={[form.method]}
                        onSelectionChange={(keys) =>
                          setForm((prev) => ({
                            ...prev,
                            method: String(Array.from(keys)[0] ?? prev.method),
                            password: "",
                          }))
                        }
                      >
                        {(caps?.ssMethods ?? []).map((m) => (
                          <SelectItem key={m} textValue={m}>
                            {m}
                          </SelectItem>
                        ))}
                      </Select>
                    )}
                    {usernameProtocols.includes(form.protocol) && (
                      <Input
                        label="用户名"
                        placeholder="留空自动生成"
                        value={form.username}
                        variant="bordered"
                        onChange={(e) => set("username", e.target.value)}
                      />
                    )}
                    {passwordProtocols.includes(form.protocol) && (
                      <Input
                        description={
                          form.protocol === "shadowsocks" &&
                          form.method.startsWith("2022-")
                            ? "SS2022 需要 base64 密钥，建议留空自动生成"
                            : undefined
                        }
                        label={form.protocol === "snell" ? "PSK" : "密码"}
                        placeholder="留空自动生成"
                        value={form.password}
                        variant="bordered"
                        onChange={(e) => set("password", e.target.value)}
                      />
                    )}

                    {form.protocol === "snell" && (
                      <Select
                        description={
                          form.kernelType === "singbox"
                            ? "客户端使用的协议版本；sing-box 内置 Snell 需要 1.14 及以上，v6 的 PSK 至少 12 位"
                            : "客户端使用的协议版本，要和服务器上的 Snell 一致：v4 / v5 用 5.x（也可 4.x 跑 v4），v6 用 6.0.0（只接受 v6 客户端）"
                        }
                        label="Snell 版本"
                        selectedKeys={[
                          form.snellVersion || defaultSnellVersion,
                        ]}
                        onSelectionChange={(keys) =>
                          set(
                            "snellVersion",
                            String(Array.from(keys)[0] ?? "5"),
                          )
                        }
                      >
                        {["6", "5", "4"].map((v) => (
                          <SelectItem key={v} textValue={`v${v}`}>
                            {`v${v}`}
                          </SelectItem>
                        ))}
                      </Select>
                    )}
                    {form.protocol === "snell" && form.snellVersion === "6" && (
                      <Select
                        description="客户端要选同样的模式；unshaped 不做流量整形，更快但更容易被识别"
                        label="流量整形"
                        selectedKeys={[form.snellMode || "default"]}
                        onSelectionChange={(keys) => {
                          const v = String(Array.from(keys)[0] ?? "default");

                          set("snellMode", v === "default" ? "" : v);
                        }}
                      >
                        <SelectItem key="default" textValue="默认">
                          默认
                        </SelectItem>
                        <SelectItem key="unshaped" textValue="unshaped">
                          unshaped
                        </SelectItem>
                      </Select>
                    )}
                    {form.protocol === "wireguard" && (
                      <div className="space-y-2">
                        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                          <Input
                            description="服务端用同一网段的 .1"
                            label="客户端地址"
                            placeholder="10.66.66.2"
                            value={form.wgPeerAddress}
                            variant="bordered"
                            onChange={(e) =>
                              set("wgPeerAddress", e.target.value)
                            }
                          />
                          <Input
                            label="MTU"
                            placeholder="1420"
                            value={form.wgMtu}
                            variant="bordered"
                            onChange={(e) => set("wgMtu", e.target.value)}
                          />
                        </div>
                        <p className="text-xs text-default-500">
                          密钥自动生成，所有人共用一份客户端配置（在“分享”里）；需要更换时用“换密钥”
                        </p>
                      </div>
                    )}
                    {form.protocol === "hysteria2" && (
                      <Input
                        description="非客户端访问时返回此网站内容，留空不伪装"
                        label="伪装网站（可选）"
                        placeholder="https://www.bing.com"
                        value={form.masquerade}
                        variant="bordered"
                        onChange={(e) => set("masquerade", e.target.value)}
                      />
                    )}
                    {form.protocol === "hysteria2" && (
                      <Input
                        description="客户端在这段 UDP 端口间随机切换，缓解运营商对单端口 UDP 的限速；服务器上由 nftables 转到入站端口。经中转连接时不跳跃"
                        label="端口跳跃（可选）"
                        placeholder="例如 20000-30000"
                        value={form.portHopping}
                        variant="bordered"
                        onChange={(e) => set("portHopping", e.target.value)}
                      />
                    )}

                    {(form.transportType === "ws" ||
                      form.transportType === "xhttp") && (
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <Input
                          label="路径"
                          placeholder="留空随机"
                          value={form.path}
                          variant="bordered"
                          onChange={(e) => set("path", e.target.value)}
                        />
                        <Input
                          label="Host（可选）"
                          value={form.host}
                          variant="bordered"
                          onChange={(e) => set("host", e.target.value)}
                        />
                      </div>
                    )}
                    {form.transportType === "xhttp" && (
                      <Select
                        label="XHTTP 模式"
                        selectedKeys={[form.xhttpMode]}
                        onSelectionChange={(keys) =>
                          set(
                            "xhttpMode",
                            String(Array.from(keys)[0] ?? "auto"),
                          )
                        }
                      >
                        {XHTTP_MODES.map((m) => (
                          <SelectItem key={m} textValue={m}>
                            {m}
                          </SelectItem>
                        ))}
                      </Select>
                    )}
                    {form.transportType === "grpc" && (
                      <Input
                        label="gRPC serviceName"
                        placeholder="grpc"
                        value={form.serviceName}
                        variant="bordered"
                        onChange={(e) => set("serviceName", e.target.value)}
                      />
                    )}

                    {form.securityType === "reality" && (
                      <div className="space-y-3 rounded-lg border border-divider p-3">
                        <p className="text-sm font-medium">Reality</p>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                          <Input
                            description="客户端使用的 SNI，需是支持 TLS1.3 的大站"
                            label="伪装域名"
                            value={form.sni}
                            variant="bordered"
                            onChange={(e) => set("sni", e.target.value)}
                          />
                          <Input
                            description="留空则为 伪装域名:443"
                            label="目标地址 (dest)"
                            placeholder="www.example.com:443"
                            value={form.realityDest}
                            variant="bordered"
                            onChange={(e) => set("realityDest", e.target.value)}
                          />
                        </div>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                          <Input
                            label="私钥"
                            placeholder="留空自动生成"
                            value={form.realityPrivateKey}
                            variant="bordered"
                            onChange={(e) =>
                              set("realityPrivateKey", e.target.value)
                            }
                          />
                          <Input
                            label="shortId"
                            placeholder="留空自动生成，多个用逗号分隔"
                            value={form.realityShortIds}
                            variant="bordered"
                            onChange={(e) =>
                              set("realityShortIds", e.target.value)
                            }
                          />
                        </div>
                        {form.kernelType === "xray" && (
                          <Input
                            description="低于此版本的客户端会被拒绝。Xray 新版默认 26.3.27，会导致 Clash / Mihomo 等客户端连不上，保持 1.0.0 即可兼容所有客户端"
                            label="最低客户端版本"
                            placeholder="1.0.0"
                            value={form.realityMinClientVer}
                            variant="bordered"
                            onChange={(e) =>
                              set("realityMinClientVer", e.target.value)
                            }
                          />
                        )}
                      </div>
                    )}

                    {form.securityType === "shadowtls" && (
                      <div className="space-y-3 rounded-lg border border-divider p-3">
                        <p className="text-sm font-medium">ShadowTLS v3</p>
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                          <Input
                            description="握手转发到该网站的 443 端口，需支持 TLS1.3"
                            label="伪装网站"
                            value={form.sni}
                            variant="bordered"
                            onChange={(e) => set("sni", e.target.value)}
                          />
                          <Input
                            label="ShadowTLS 密码"
                            placeholder="留空自动生成"
                            value={form.shadowTlsPassword}
                            variant="bordered"
                            onChange={(e) =>
                              set("shadowTlsPassword", e.target.value)
                            }
                          />
                        </div>
                      </div>
                    )}

                    <div className="space-y-3 border-t border-divider pt-4">
                      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                        <Input
                          description="默认 :: 监听所有地址；127.0.0.1 仅本机（配合隧道中转）"
                          label="监听地址"
                          value={form.listen}
                          variant="bordered"
                          onChange={(e) => set("listen", e.target.value)}
                        />
                        <div className="space-y-2">
                          <Select
                            disabledKeys={[
                              ...(shareV4 ? [] : ["ipv4"]),
                              ...(shareV6 ? [] : ["ipv6"]),
                            ]}
                            label="分享地址"
                            selectedKeys={[form.shareAddressMode]}
                            onSelectionChange={(keys) =>
                              set(
                                "shareAddressMode",
                                String(
                                  Array.from(keys)[0] ?? "auto",
                                ) as InboundForm["shareAddressMode"],
                              )
                            }
                          >
                            <SelectItem
                              key="auto"
                              textValue="自动（服务器默认地址）"
                            >
                              自动（服务器默认地址）
                            </SelectItem>
                            <SelectItem
                              key="ipv4"
                              textValue={`服务器 IPv4${shareV4 ? ` · ${shareV4}` : "（未设置）"}`}
                            >
                              服务器 IPv4
                              {shareV4 ? ` · ${shareV4}` : "（未设置）"}
                            </SelectItem>
                            <SelectItem
                              key="ipv6"
                              textValue={`服务器 IPv6${shareV6 ? ` · ${shareV6}` : "（未设置）"}`}
                            >
                              服务器 IPv6
                              {shareV6 ? ` · ${shareV6}` : "（未设置）"}
                            </SelectItem>
                            <SelectItem
                              key="custom"
                              textValue="自定义域名 / IP"
                            >
                              自定义域名 / IP
                            </SelectItem>
                          </Select>
                          {form.shareAddressMode === "custom" && (
                            <Input
                              aria-label="自定义分享地址"
                              placeholder="域名或 IP，不含端口"
                              value={form.shareHost}
                              variant="bordered"
                              onChange={(e) => set("shareHost", e.target.value)}
                            />
                          )}
                          <p className="text-xs text-default-500">
                            决定分享和订阅中的入口地址，与出站策略独立。IPv4 /
                            IPv6 随服务器设置更新；监听地址需支持对应类型。
                          </p>
                        </div>
                      </div>
                      <OutboundIPSelect
                        kernelType={form.kernelType}
                        value={form.outboundIpStrategy}
                        onChange={(value) => set("outboundIpStrategy", value)}
                      />
                      <div className="space-y-3 rounded-lg border border-divider p-3">
                        <p className="text-sm font-medium">中转相关</p>
                        <div className="flex items-start gap-2">
                          <Switch
                            isDisabled={form.acceptProxyProtocol}
                            isSelected={
                              form.relayOnly || form.acceptProxyProtocol
                            }
                            size="sm"
                            onValueChange={(v) => set("relayOnly", v)}
                          />
                          <div>
                            <p className="text-sm">仅通过中转提供</p>
                            <p className="text-xs text-default-500">
                              订阅和分享中不再给出这个节点的直连地址
                            </p>
                          </div>
                        </div>
                        <div className="flex items-start gap-2">
                          <Switch
                            isDisabled={
                              form.kernelType !== "xray" &&
                              !form.acceptProxyProtocol
                            }
                            isSelected={form.acceptProxyProtocol}
                            size="sm"
                            onValueChange={(v) => set("acceptProxyProtocol", v)}
                          />
                          <div>
                            <p className="text-sm">
                              获取真实客户端 IP（PROXY protocol）
                            </p>
                            <p className="text-xs text-default-500">
                              {form.kernelType === "xray"
                                ? "中转转发会附带客户端地址，节点的内核日志可见真实 IP。开启后直连将无法使用，自动变为仅中转"
                                : "目前只有 Xray 内核支持"}
                            </p>
                          </div>
                        </div>
                        <div className="flex items-start gap-2">
                          <Switch
                            isDisabled={
                              !!selectedNode?.nftError && !form.relayWhitelist
                            }
                            isSelected={form.relayWhitelist}
                            size="sm"
                            onValueChange={(v) => set("relayWhitelist", v)}
                          />
                          <div>
                            <p className="text-sm">
                              仅允许中转机访问（防火墙白名单）
                            </p>
                            <p className="text-xs text-default-500">
                              只放行已对接中转的入口服务器 IP
                              和本机，其他来源访问该端口一律丢弃；需要服务器安装
                              nftables
                            </p>
                            {selectedNode?.nftError ? (
                              <p className="text-xs text-danger mt-0.5">
                                该服务器无法使用：{selectedNode.nftError}
                                {form.relayWhitelist ? "。请关闭后再保存" : ""}
                              </p>
                            ) : null}
                          </div>
                        </div>
                        {form.relayWhitelist && (
                          <Input
                            description="中转机出口 IP 与服务器地址不同时，在此补充；支持 IP 和网段，逗号分隔"
                            label="额外放行"
                            placeholder="203.0.113.9, 10.0.0.0/8"
                            value={form.whitelistExtra}
                            variant="bordered"
                            onChange={(e) =>
                              set("whitelistExtra", e.target.value)
                            }
                          />
                        )}
                      </div>
                      <Input
                        label="备注"
                        value={form.remark}
                        variant="bordered"
                        onChange={(e) => set("remark", e.target.value)}
                      />
                      <Textarea
                        description="内核原生字段，深度合并到该入站配置中（例如 sniffing、sockopt）"
                        label="高级配置 (JSON)"
                        minRows={3}
                        placeholder='{"streamSettings":{"sockopt":{"tcpFastOpen":true}}}'
                        value={form.extra}
                        variant="bordered"
                        onChange={(e) => set("extra", e.target.value)}
                      />
                    </div>
                  </div>
                )}
              </div>
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onClose}>
                取消
              </Button>
              <Button color="primary" isLoading={saving} onPress={submit}>
                {isEdit ? "保存并下发" : "创建并下发"}
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
