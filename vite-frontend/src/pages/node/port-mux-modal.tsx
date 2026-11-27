import type { NodePortMuxApiData, NodePortMuxRule } from "@/api/types";

import { useCallback, useEffect, useState } from "react";
import toast from "react-hot-toast";
import { ChevronDown, ChevronRight, Copy, Plus, Trash2 } from "lucide-react";

import { getNodePortMux, saveNodePortMux } from "@/api";
import {
  KERNEL_LABELS,
  KERNEL_STATUS,
  PROTOCOL_LABELS,
} from "@/pages/inbound/presets";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { copyWithToast } from "@/utils/clipboard";

interface PortMuxNode {
  id: number;
  name: string;
}

interface RuleDraft {
  sni: string;
  target: string;
  proxyProtocol: boolean;
  remark: string;
}

const CADDY_SNIPPET = `{
  https_port 8443
}

panel.example.com {
  bind 127.0.0.1
  reverse_proxy 127.0.0.1:6365
}`;

const NGINX_SNIPPET = `server {
  listen 127.0.0.1:8443 ssl http2;
  server_name panel.example.com;

  ssl_certificate     /path/to/fullchain.pem;
  ssl_certificate_key /path/to/privkey.pem;

  location / {
    proxy_pass http://127.0.0.1:6365;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
  }
}`;

export function NodePortMuxModal({
  node,
  onClose,
}: {
  node: PortMuxNode | null;
  onClose: () => void;
}) {
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [data, setData] = useState<NodePortMuxApiData | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [listen, setListen] = useState("::");
  const [port, setPort] = useState("443");
  const [rules, setRules] = useState<RuleDraft[]>([]);
  const [fallbackTarget, setFallbackTarget] = useState("");
  const [fallbackProxy, setFallbackProxy] = useState(false);
  const [showGuide, setShowGuide] = useState(false);

  const hydrate = useCallback((v: NodePortMuxApiData) => {
    setData(v);
    setEnabled(v.enabled);
    setListen(v.listen || "::");
    setPort(String(v.port || 443));
    setRules(
      (v.rules ?? []).map((r) => ({
        sni: r.sni,
        target: r.target,
        proxyProtocol: Boolean(r.proxyProtocol),
        remark: r.remark ?? "",
      })),
    );
    setFallbackTarget(v.fallbackTarget ?? "");
    setFallbackProxy(Boolean(v.fallbackProxy));
  }, []);

  useEffect(() => {
    if (!node) {
      setData(null);
      setShowGuide(false);

      return;
    }
    let cancelled = false;

    setLoading(true);
    getNodePortMux(node.id)
      .then((res) => {
        if (cancelled) return;
        if (res.code === 0 && res.data) {
          hydrate(res.data);
        } else {
          toast.error(res.msg || "读取端口复用配置失败");
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [node, hydrate]);

  const addRule = () => {
    setRules((prev) => [
      ...prev,
      { sni: "", target: "127.0.0.1:8443", proxyProtocol: false, remark: "" },
    ]);
  };

  const updateRule = <K extends keyof RuleDraft>(
    idx: number,
    key: K,
    value: RuleDraft[K],
  ) => {
    setRules((prev) =>
      prev.map((r, i) => (i === idx ? { ...r, [key]: value } : r)),
    );
  };

  const removeRule = (idx: number) => {
    setRules((prev) => prev.filter((_, i) => i !== idx));
  };

  const handleSave = async () => {
    if (!node) return;
    const portNum = Number(port.trim());

    if (!Number.isInteger(portNum) || portNum < 1 || portNum > 65535) {
      toast.error("监听端口必须在 1-65535 之间");

      return;
    }
    const cleanRules: NodePortMuxRule[] = [];

    for (let i = 0; i < rules.length; i++) {
      const r = rules[i];
      const sni = r.sni.trim();
      const target = r.target.trim();

      if (!sni && !target && !r.remark.trim()) continue;
      if (!sni) {
        toast.error(`第 ${i + 1} 条外部规则请填写 SNI 域名`);

        return;
      }
      if (!target) {
        toast.error(`第 ${i + 1} 条外部规则请填写目标地址`);

        return;
      }
      cleanRules.push({
        sni,
        target,
        proxyProtocol: r.proxyProtocol,
        remark: r.remark.trim(),
      });
    }

    setSaving(true);
    try {
      const res = await saveNodePortMux({
        nodeId: node.id,
        enabled,
        listen: listen.trim() || "::",
        port: portNum,
        rules: cleanRules,
        fallbackTarget: fallbackTarget.trim(),
        fallbackProxy,
      });

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      if (res.data) {
        hydrate(res.data);
      }
      if (res.msg && res.msg !== "操作成功") {
        toast(res.msg, { icon: "⚠️", duration: 6000 });
      } else {
        toast.success(enabled ? "端口复用已保存并下发" : "已关闭端口复用");
      }
    } finally {
      setSaving(false);
    }
  };

  const statusMeta =
    KERNEL_STATUS[data?.status ?? "stopped"] ?? KERNEL_STATUS.stopped;
  const displayPort = port.trim() || "443";

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={Boolean(node)}
      placement="center"
      scrollBehavior="inside"
      size="2xl"
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <ModalContent>
        {(onModalClose) => (
          <>
            <ModalHeader className="flex flex-col gap-1">
              <div className="flex items-center gap-2">
                <h2 className="text-base font-semibold">
                  443 端口复用（SNI 分流）
                </h2>
                {data && (
                  <Chip color={statusMeta.color} size="sm" variant="flat">
                    {statusMeta.label}
                  </Chip>
                )}
              </div>
              {node && (
                <p className="text-xs font-normal text-default-500">
                  {node.name} · 由节点 Agent 按 TLS ClientHello SNI
                  无解密分流，无需安装第三方组件
                </p>
              )}
            </ModalHeader>
            <ModalBody>
              {loading ? (
                <div className="flex justify-center py-10">
                  <Spinner size="md" />
                </div>
              ) : (
                <div className="space-y-4">
                  <div className="flex items-start justify-between gap-3 rounded-lg border border-divider p-3">
                    <div>
                      <p className="text-sm font-medium">
                        启用 TCP SNI 端口复用
                      </p>
                      <p className="text-xs text-default-500 mt-0.5">
                        开启后，同机多个监听 {displayPort} 端口的 TLS / Reality
                        / ShadowTLS 入站（跨 Xray / sing-box / Snell）可与面板
                        HTTPS / 网站共用 {displayPort} 端口；UDP 入站（如
                        Hysteria2 / TUIC）不受影响可直接共用。
                      </p>
                    </div>
                    <Switch
                      aria-label="启用端口复用"
                      isSelected={enabled}
                      size="sm"
                      onValueChange={setEnabled}
                    />
                  </div>

                  {data?.lastError && (
                    <div className="rounded-lg bg-danger-50 px-3 py-2 text-xs text-danger dark:bg-danger-100/10">
                      上次下发异常：{data.lastError}
                    </div>
                  )}

                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <Input
                      description="通常为 443；开启前请确保宿主机上 Caddy / Nginx 已让出该端口"
                      inputMode="numeric"
                      label="复用监听端口"
                      placeholder="443"
                      value={port}
                      variant="bordered"
                      onChange={(e) =>
                        setPort(e.target.value.replace(/[^\d]/g, ""))
                      }
                    />
                    <Input
                      description="默认 ::（双栈），不支持 IPv6 时自动回退 0.0.0.0"
                      label="监听地址"
                      placeholder="::"
                      value={listen}
                      variant="bordered"
                      onChange={(e) => setListen(e.target.value)}
                    />
                  </div>

                  {/* 自动接入的节点入站 */}
                  <div className="space-y-2 rounded-lg border border-divider p-3">
                    <div className="flex items-center justify-between">
                      <p className="text-sm font-medium">
                        已自动接入的代理入站（{data?.inbounds?.length ?? 0}）
                      </p>
                    </div>
                    {data?.inbounds && data.inbounds.length > 0 ? (
                      <div className="space-y-1.5">
                        {data.inbounds.map((ib) => (
                          <div
                            key={ib.id}
                            className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-default-100/70 px-2.5 py-1.5 text-xs"
                          >
                            <div className="flex flex-wrap items-center gap-1.5">
                              <span className="font-medium text-foreground">
                                {ib.name}
                              </span>
                              <Chip size="sm" variant="flat">
                                {KERNEL_LABELS[ib.kernelType] ?? ib.kernelType}
                              </Chip>
                              <Chip size="sm" variant="flat">
                                {PROTOCOL_LABELS[ib.protocol] ?? ib.protocol}
                              </Chip>
                              {!ib.enabled && (
                                <Chip color="default" size="sm" variant="flat">
                                  已停用
                                </Chip>
                              )}
                            </div>
                            <div className="font-mono text-default-600">
                              {ib.sni} → 127.0.0.1:{ib.muxPort || "自动分配"}
                            </div>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="text-xs text-default-500">
                        暂无监听 {displayPort} 端口的 TCP
                        入站。启用后，在「节点」页面新建监听 {displayPort}{" "}
                        端口的 VLESS + Reality / Trojan + TLS / ShadowTLS
                        等入站会自动接入。
                      </p>
                    )}
                  </div>

                  {/* 外部回落 / 面板 HTTPS 规则 */}
                  <div className="space-y-2.5 rounded-lg border border-divider p-3">
                    <div className="flex items-center justify-between gap-2">
                      <div>
                        <p className="text-sm font-medium">
                          外部服务 / 面板 HTTPS 分流规则
                        </p>
                        <p className="text-xs text-default-500">
                          按 SNI 将面板域名或建站域名转发至本机 Caddy / Nginx
                          的内部 HTTPS 端口（如 127.0.0.1:8443）
                        </p>
                      </div>
                      <Button
                        size="sm"
                        startContent={<Plus className="h-3.5 w-3.5" />}
                        variant="flat"
                        onPress={addRule}
                      >
                        添加规则
                      </Button>
                    </div>

                    {rules.length === 0 ? (
                      <p className="text-xs text-default-400">
                        暂无外部规则；若仅需把所有未匹配的域名统一回落到面板
                        HTTPS，也可直接填写下方「默认回落目标」。
                      </p>
                    ) : (
                      <div className="space-y-2">
                        {rules.map((rule, idx) => (
                          <div
                            key={idx}
                            className="space-y-2 rounded-md border border-divider p-2.5"
                          >
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                              <Input
                                aria-label="SNI 域名"
                                label="SNI 域名"
                                placeholder="panel.example.com 或 *.example.com"
                                size="sm"
                                value={rule.sni}
                                variant="bordered"
                                onChange={(e) =>
                                  updateRule(idx, "sni", e.target.value)
                                }
                              />
                              <Input
                                aria-label="目标地址"
                                label="转发目标 (host:port)"
                                placeholder="127.0.0.1:8443"
                                size="sm"
                                value={rule.target}
                                variant="bordered"
                                onChange={(e) =>
                                  updateRule(idx, "target", e.target.value)
                                }
                              />
                            </div>
                            <div className="flex flex-wrap items-center justify-between gap-2">
                              <div className="flex flex-wrap items-center gap-3 flex-1 min-w-0">
                                <Input
                                  aria-label="备注"
                                  className="max-w-[220px]"
                                  placeholder="备注（可选，如 面板 HTTPS）"
                                  size="sm"
                                  value={rule.remark}
                                  variant="bordered"
                                  onChange={(e) =>
                                    updateRule(idx, "remark", e.target.value)
                                  }
                                />
                                <Switch
                                  isSelected={rule.proxyProtocol}
                                  size="sm"
                                  onValueChange={(v) =>
                                    updateRule(idx, "proxyProtocol", v)
                                  }
                                >
                                  <span className="text-xs">
                                    PROXY protocol v1
                                  </span>
                                </Switch>
                              </div>
                              <Button
                                isIconOnly
                                aria-label="删除规则"
                                color="danger"
                                size="sm"
                                variant="light"
                                onPress={() => removeRule(idx)}
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  {/* 默认回落目标 */}
                  <div className="space-y-2 rounded-lg border border-divider p-3">
                    <p className="text-sm font-medium">默认回落目标（可选）</p>
                    <div className="flex flex-col sm:flex-row sm:items-center gap-3">
                      <Input
                        className="flex-1"
                        description="未命中任何 SNI 或非 TLS 流量时转发到此地址（例如面板 Caddy / Nginx 的 127.0.0.1:8443）；留空则直接断开"
                        placeholder="127.0.0.1:8443"
                        value={fallbackTarget}
                        variant="bordered"
                        onChange={(e) => setFallbackTarget(e.target.value)}
                      />
                      <Switch
                        isSelected={fallbackProxy}
                        size="sm"
                        onValueChange={setFallbackProxy}
                      >
                        <span className="text-xs whitespace-nowrap">
                          PROXY protocol v1
                        </span>
                      </Switch>
                    </div>
                  </div>

                  {/* 面板 HTTPS 配置参考 */}
                  <div className="rounded-lg border border-divider">
                    <button
                      className="flex w-full items-center justify-between px-3 py-2 text-left text-xs font-medium text-default-600 hover:text-foreground"
                      type="button"
                      onClick={() => setShowGuide((v) => !v)}
                    >
                      <span>
                        面板 HTTPS 共用 443 配置参考（Caddy / Nginx 改监听
                        127.0.0.1:8443）
                      </span>
                      {showGuide ? (
                        <ChevronDown className="h-4 w-4" />
                      ) : (
                        <ChevronRight className="h-4 w-4" />
                      )}
                    </button>
                    {showGuide && (
                      <div className="space-y-3 border-t border-divider p-3 text-xs">
                        <p className="text-default-500">
                          当面板与代理节点部署在同一台服务器时：先将 Caddy /
                          Nginx 的 HTTPS 监听改为{" "}
                          <code className="font-mono">127.0.0.1:8443</code> 让出
                          443，再在此处开启 443
                          端口复用并把面板域名（或默认回落）指向{" "}
                          <code className="font-mono">127.0.0.1:8443</code>
                          ，之后即可正常创建 443 端口的 VLESS + Reality 节点。
                        </p>
                        <div className="space-y-1">
                          <div className="flex items-center justify-between">
                            <span className="font-medium">Caddyfile 示例</span>
                            <Button
                              size="sm"
                              startContent={<Copy className="h-3 w-3" />}
                              variant="light"
                              onPress={() =>
                                copyWithToast(CADDY_SNIPPET, "Caddyfile 示例")
                              }
                            >
                              复制
                            </Button>
                          </div>
                          <pre className="overflow-x-auto rounded-md bg-default-100 p-2 font-mono text-[11px] leading-relaxed">
                            {CADDY_SNIPPET}
                          </pre>
                        </div>
                        <div className="space-y-1">
                          <div className="flex items-center justify-between">
                            <span className="font-medium">Nginx 示例</span>
                            <Button
                              size="sm"
                              startContent={<Copy className="h-3 w-3" />}
                              variant="light"
                              onPress={() =>
                                copyWithToast(NGINX_SNIPPET, "Nginx 示例")
                              }
                            >
                              复制
                            </Button>
                          </div>
                          <pre className="overflow-x-auto rounded-md bg-default-100 p-2 font-mono text-[11px] leading-relaxed">
                            {NGINX_SNIPPET}
                          </pre>
                        </div>
                      </div>
                    )}
                  </div>
                </div>
              )}
            </ModalBody>
            <ModalFooter>
              <Button variant="light" onPress={onModalClose}>
                关闭
              </Button>
              <Button
                color="primary"
                isDisabled={loading}
                isLoading={saving}
                onPress={handleSave}
              >
                保存并下发
              </Button>
            </ModalFooter>
          </>
        )}
      </ModalContent>
    </Modal>
  );
}
