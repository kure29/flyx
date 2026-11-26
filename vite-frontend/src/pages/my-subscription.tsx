import { useCallback, useEffect, useState } from "react";
import toast from "react-hot-toast";
import { Copy, QrCode, RefreshCw } from "lucide-react";

import {
  getUserSubscriptions,
  resetUserSubscription,
  type UserSubscription,
} from "@/api";
import { AnimatedPage } from "@/components/animated-page";
import { SubTrafficStats } from "@/components/lazy-charts";
import { PageLoadingState } from "@/components/page-state";
import {
  EmptyHint,
  ListGroup,
  ListRow,
  PageHeader,
  formatDate,
  type RowStatus,
} from "@/components/page-kit";
import { copyText, panelOrigin, QrImage } from "@/pages/inbound/share";
import { SubClientGrid } from "@/pages/subscription/client-grid";
import { Segmented } from "@/pages/subscription/controls";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { useI18n } from "@/i18n";
import { formatTraffic } from "@/utils/traffic";

function subStatusRow(status: UserSubscription["status"]): RowStatus {
  switch (status) {
    case "ok":
      return "ok";
    case "over_quota":
      return "warn";
    case "expired":
      return "bad";
    default:
      return "off";
  }
}

function subStatusLabel(
  status: UserSubscription["status"],
  t: (zh: string) => string,
): {
  text: string;
  color: "success" | "warning" | "danger" | "default";
} {
  switch (status) {
    case "ok":
      return { text: t("正常"), color: "success" };
    case "over_quota":
      return { text: t("流量已用完"), color: "warning" };
    case "expired":
      return { text: t("已过期"), color: "danger" };
    default:
      return { text: t("已停用"), color: "default" };
  }
}

function subLink(s: UserSubscription, format: string): string {
  const base = panelOrigin() + s.path;

  return format ? `${base}?format=${encodeURIComponent(format)}` : base;
}

export default function MySubscriptionPage() {
  const { t } = useI18n();
  const [items, setItems] = useState<UserSubscription[]>([]);
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState<UserSubscription | null>(null);
  const [tab, setTab] = useState<"import" | "traffic">("import");
  const [showQr, setShowQr] = useState(false);
  const [resetting, setResetting] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getUserSubscriptions();

      if (res.code === 0 && Array.isArray(res.data)) {
        setItems(res.data);
        setSelected((prev) =>
          prev ? (res.data.find((x) => x.id === prev.id) ?? null) : null,
        );
      }
    } catch {
      toast.error(t("加载订阅失败"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void load();
  }, [load]);

  const handleReset = async (s: UserSubscription) => {
    setResetting(true);
    try {
      const res = await resetUserSubscription(s.id);

      if (res.code !== 0) {
        toast.error(res.msg || t("重置失败"));

        return;
      }
      toast.success(t("已重置链接与凭据，请在客户端重新导入订阅"));
      if (res.data) {
        setItems((prev) =>
          prev.map((item) => (item.id === s.id ? res.data : item)),
        );
        setSelected(res.data);
      } else {
        await load();
      }
    } catch {
      toast.error(t("重置失败"));
    } finally {
      setResetting(false);
    }
  };

  if (loading && items.length === 0) {
    return (
      <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
        <PageLoadingState message="正在加载..." />
      </AnimatedPage>
    );
  }

  return (
    <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
      <PageHeader
        description={t("查看分配给你的订阅链接、流量与到期时间")}
        title={t("我的订阅")}
      />

      {items.length === 0 ? (
        <EmptyHint text={t("管理员还没有给你分配订阅。")} />
      ) : (
        <ListGroup count={items.length} title={t("订阅列表")}>
          {items.map((s) => {
            const used = s.uploadBytes + s.downloadBytes;
            const badge = subStatusLabel(s.status, t);
            const flowText =
              s.trafficLimit > 0
                ? `${formatTraffic(used)} / ${formatTraffic(s.trafficLimit)}`
                : `${formatTraffic(used)} / ${t("不限")}`;
            const expText =
              s.expireTime > 0
                ? t("到期 {date}", { date: formatDate(s.expireTime) })
                : t("永不过期");

            return (
              <ListRow
                key={s.id}
                badges={
                  <Chip color={badge.color} size="sm" variant="flat">
                    {badge.text}
                  </Chip>
                }
                meta={
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-default-500">
                    <span>{t("{count} 个节点", { count: s.nodeCount })}</span>
                    <span>{t("流量 {flow}", { flow: flowText })}</span>
                    <span>{expText}</span>
                    {s.resetDay > 0 && (
                      <span>{t("每月 {day} 号重置", { day: s.resetDay })}</span>
                    )}
                  </div>
                }
                status={subStatusRow(s.status)}
                title={s.name}
                onPress={() => {
                  setSelected(s);
                  setTab("import");
                  setShowQr(false);
                }}
              />
            );
          })}
        </ListGroup>
      )}

      <Modal
        isOpen={Boolean(selected)}
        scrollBehavior="inside"
        size="lg"
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      >
        <ModalContent>
          {selected && (
            <>
              <ModalHeader className="flex items-center justify-between gap-2">
                <span>{selected.name}</span>
                <Chip
                  color={subStatusLabel(selected.status, t).color}
                  size="sm"
                  variant="flat"
                >
                  {subStatusLabel(selected.status, t).text}
                </Chip>
              </ModalHeader>
              <ModalBody className="space-y-4">
                {(() => {
                  const used = selected.uploadBytes + selected.downloadBytes;
                  const pct =
                    selected.trafficLimit > 0
                      ? Math.min(100, (used / selected.trafficLimit) * 100)
                      : 0;

                  return (
                    <div className="rounded-xl border border-divider p-3.5 space-y-2 bg-default-50/50">
                      <div className="flex items-baseline justify-between text-sm">
                        <span className="font-medium">
                          {formatTraffic(used)}
                          {selected.trafficLimit > 0
                            ? ` / ${formatTraffic(selected.trafficLimit)}`
                            : t("（不限流量）")}
                        </span>
                        <span className="text-xs text-default-500">
                          {selected.expireTime > 0
                            ? t("到期 {date}", {
                                date: formatDate(selected.expireTime),
                              })
                            : t("永不过期")}
                        </span>
                      </div>
                      {selected.trafficLimit > 0 && (
                        <div className="h-1.5 rounded-full bg-default-200 overflow-hidden">
                          <div
                            className={`h-full rounded-full ${
                              pct >= 100
                                ? "bg-danger"
                                : pct >= 90
                                  ? "bg-warning"
                                  : "bg-primary"
                            }`}
                            style={{ width: `${pct}%` }}
                          />
                        </div>
                      )}
                      <div className="flex flex-wrap items-center justify-between text-xs text-default-500 gap-2">
                        <span>
                          ↑ {formatTraffic(selected.uploadBytes)} · ↓{" "}
                          {formatTraffic(selected.downloadBytes)}
                        </span>
                        {selected.resetDay > 0 && (
                          <span>
                            {t("每月 {day} 号自动重置", {
                              day: selected.resetDay,
                            })}
                          </span>
                        )}
                      </div>
                    </div>
                  );
                })()}

                <Segmented
                  options={[
                    ["import", t("导入订阅")],
                    ["traffic", t("流量统计")],
                  ]}
                  value={tab}
                  onChange={setTab}
                />

                {tab === "import" ? (
                  <div className="space-y-3">
                    <div className="flex flex-wrap gap-2">
                      <Button
                        color="primary"
                        size="sm"
                        startContent={<Copy className="w-3.5 h-3.5" />}
                        onPress={() =>
                          copyText(subLink(selected, ""), t("订阅链接已复制"))
                        }
                      >
                        {t("复制通用链接")}
                      </Button>
                      <Button
                        size="sm"
                        startContent={<QrCode className="w-3.5 h-3.5" />}
                        variant="flat"
                        onPress={() => setShowQr((v) => !v)}
                      >
                        {showQr ? t("隐藏二维码") : t("二维码")}
                      </Button>
                      <Button
                        color="warning"
                        isLoading={resetting}
                        size="sm"
                        startContent={<RefreshCw className="w-3.5 h-3.5" />}
                        variant="flat"
                        onPress={() => void handleReset(selected)}
                      >
                        {t("重置链接与凭据")}
                      </Button>
                    </div>

                    {showQr && (
                      <div className="flex justify-center py-2">
                        <QrImage text={subLink(selected, "")} />
                      </div>
                    )}

                    <SubClientGrid
                      configClients={selected.templates ?? []}
                      link={(fmt) => subLink(selected, fmt)}
                      name={selected.name}
                      rules={selected.rules}
                    />
                  </div>
                ) : (
                  <SubTrafficStats id={selected.id} scope="user" />
                )}
              </ModalBody>
              <ModalFooter>
                <Button variant="flat" onPress={() => setSelected(null)}>
                  {t("关闭")}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}
