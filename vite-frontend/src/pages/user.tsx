import type { TunnelApiItem } from "@/api/types";

import { useCallback, useEffect, useState } from "react";
import toast from "react-hot-toast";
import { Plus } from "lucide-react";

import {
  createUser,
  deleteUser,
  getSubscriptions,
  getTunnelList,
  getUserList,
  resetUserFlow,
  updateUser,
  type SubscriptionItem,
  type UserApiItem,
} from "@/api";
import { ActionSheet } from "@/components/action-sheet";
import { AnimatedPage } from "@/components/animated-page";
import { PageLoadingState } from "@/components/page-state";
import {
  EmptyHint,
  ListGroup,
  ListRow,
  PageHeader,
  formatDate,
  type RowStatus,
} from "@/components/page-kit";
import { TrafficLimitField } from "@/components/traffic-limit-field";
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
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { useI18n } from "@/i18n";
import {
  formatTraffic,
  parseTrafficInput,
  preferredTrafficUnit,
  TRAFFIC_UNIT_MIB,
  type TrafficUnit,
} from "@/utils/traffic";

interface UserFormState {
  id?: number;
  user: string;
  pwd: string;
  enabled: boolean;
  flowInput: string;
  flowUnit: TrafficUnit;
  flowResetTime: string;
  num: string;
  expDate: string;
  tunnelIds: number[];
  subscriptionIds: number[];
}

function emptyForm(): UserFormState {
  return {
    user: "",
    pwd: "",
    enabled: true,
    flowInput: "",
    flowUnit: "GB",
    flowResetTime: "0",
    num: "0",
    expDate: "",
    tunnelIds: [],
    subscriptionIds: [],
  };
}

function msToDateInput(ms: number): string {
  if (!ms || ms <= 0) return "";
  const d = new Date(ms);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");

  return `${y}-${m}-${day}`;
}

function dateInputToEndOfDayMs(val: string): number {
  const s = val.trim();

  if (!s) return 0;
  const parts = s.split("-").map(Number);

  if (parts.length !== 3 || parts.some((n) => !Number.isFinite(n))) return 0;
  const d = new Date(parts[0], parts[1] - 1, parts[2], 23, 59, 59, 999);

  return d.getTime();
}

function userRowStatus(u: UserApiItem): RowStatus {
  if (u.status === 0) return "off";
  if (u.expired || u.overQuota) return "bad";
  if (u.flowLimitBytes > 0 && u.inFlow + u.outFlow >= u.flowLimitBytes * 0.9) {
    return "warn";
  }

  return "ok";
}

export default function UserPage() {
  const { t } = useI18n();
  const [users, setUsers] = useState<UserApiItem[]>([]);
  const [tunnels, setTunnels] = useState<TunnelApiItem[]>([]);
  const [subs, setSubs] = useState<SubscriptionItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [sheetUser, setSheetUser] = useState<UserApiItem | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form, setForm] = useState<UserFormState>(emptyForm());
  const [deleteTarget, setDeleteTarget] = useState<UserApiItem | null>(null);

  const loadAll = useCallback(async () => {
    setLoading(true);
    try {
      const [uRes, tRes, sRes] = await Promise.all([
        getUserList(),
        getTunnelList(),
        getSubscriptions(),
      ]);

      if (uRes.code === 0 && Array.isArray(uRes.data)) {
        setUsers(uRes.data);
      }
      if (tRes.code === 0 && Array.isArray(tRes.data)) {
        setTunnels(tRes.data);
      }
      if (sRes.code === 0 && sRes.data?.items) {
        setSubs(sRes.data.items);
      }
    } catch {
      toast.error(t("加载用户列表失败"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    void loadAll();
  }, [loadAll]);

  const openCreate = () => {
    setForm(emptyForm());
    setModalOpen(true);
  };

  const openEdit = (u: UserApiItem) => {
    const limitMiB = u.flowMiB > 0 ? u.flowMiB : u.flow > 0 ? u.flow * 1024 : 0;
    const unit: TrafficUnit =
      limitMiB > 0 ? preferredTrafficUnit(limitMiB) : "GB";
    const flowInput =
      limitMiB > 0 ? String(limitMiB / TRAFFIC_UNIT_MIB[unit]) : "";

    setForm({
      id: u.id,
      user: u.user,
      pwd: "",
      enabled: u.status === 1,
      flowInput,
      flowUnit: unit,
      flowResetTime: String(u.flowResetTime || 0),
      num: String(u.num || 0),
      expDate: msToDateInput(u.expTime),
      tunnelIds: u.tunnelIds ?? [],
      subscriptionIds: u.subscriptionIds ?? [],
    });
    setSheetUser(null);
    setModalOpen(true);
  };

  const toggleId = (list: number[], id: number) =>
    list.includes(id) ? list.filter((x) => x !== id) : [...list, id];

  const handleSave = async () => {
    const username = form.user.trim();

    if (!username) {
      toast.error(t("请填写用户名"));

      return;
    }
    if (!form.id && form.pwd.length < 6) {
      toast.error(t("密码长度至少为 6 位"));

      return;
    }
    if (form.id && form.pwd && form.pwd.length < 6) {
      toast.error(t("新密码长度至少为 6 位"));

      return;
    }
    const resetDay = Math.max(0, Math.min(28, Number(form.flowResetTime) || 0));
    const num = Math.max(0, Math.floor(Number(form.num) || 0));
    let flowMiB = 0;

    if (form.flowInput.trim() !== "") {
      const parsed = parseTrafficInput(form.flowInput, form.flowUnit);

      if (parsed === null || parsed < 0) {
        toast.error(t("流量上限格式不正确"));

        return;
      }
      flowMiB = Math.round(parsed);
    }

    const payload = {
      id: form.id,
      user: username,
      pwd: form.pwd || undefined,
      status: form.enabled ? 1 : 0,
      expTime: dateInputToEndOfDayMs(form.expDate),
      flow: Math.floor(flowMiB / 1024),
      flowMiB,
      flowResetTime: resetDay,
      num,
      tunnelIds: form.tunnelIds,
      subscriptionIds: form.subscriptionIds,
    };

    setSaving(true);
    try {
      const res = form.id
        ? await updateUser(payload)
        : await createUser(payload);

      if (res.code !== 0) {
        toast.error(res.msg || t("保存失败"));

        return;
      }
      toast.success(form.id ? t("已保存") : t("用户已创建"));
      setModalOpen(false);
      await loadAll();
    } catch {
      toast.error(t("保存失败"));
    } finally {
      setSaving(false);
    }
  };

  const handleResetFlow = async (u: UserApiItem) => {
    try {
      const res = await resetUserFlow({ id: u.id });

      if (res.code !== 0) {
        toast.error(res.msg || t("重置流量失败"));

        return;
      }
      toast.success(t("已重置中转流量，符合条件的转发已自动恢复"));
      setSheetUser(null);
      await loadAll();
    } catch {
      toast.error(t("重置流量失败"));
    }
  };

  const handleToggleStatus = async (u: UserApiItem) => {
    const nextStatus = u.status === 1 ? 0 : 1;

    try {
      const res = await updateUser({
        id: u.id,
        user: u.user,
        status: nextStatus,
        expTime: u.expTime,
        flow: u.flow,
        flowMiB: u.flowMiB,
        flowResetTime: u.flowResetTime,
        num: u.num,
      });

      if (res.code !== 0) {
        toast.error(res.msg || t("操作失败"));

        return;
      }
      toast.success(nextStatus === 1 ? t("已启用") : t("已停用并暂停其转发"));
      setSheetUser(null);
      await loadAll();
    } catch {
      toast.error(t("操作失败"));
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      const res = await deleteUser(deleteTarget.id);

      if (res.code !== 0) {
        toast.error(res.msg || t("删除失败"));

        return;
      }
      toast.success(t("用户已删除"));
      setDeleteTarget(null);
      setSheetUser(null);
      await loadAll();
    } catch {
      toast.error(t("删除失败"));
    }
  };

  const tunnelNameById = new Map(tunnels.map((item) => [item.id, item.name]));
  const subNameById = new Map(subs.map((s) => [s.id, s.name]));

  if (loading && users.length === 0) {
    return (
      <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
        <PageLoadingState message="正在加载..." />
      </AnimatedPage>
    );
  }

  return (
    <AnimatedPage className="px-4 py-6 lg:px-10 lg:py-8">
      <PageHeader
        description={t("为其他用户分配中转隧道额度与分享订阅")}
        primary={{
          key: "create-user",
          label: t("新建用户"),
          icon: <Plus className="w-4 h-4" />,
          onPress: openCreate,
        }}
        title={t("用户")}
      />

      {users.length === 0 ? (
        <EmptyHint
          action={{
            key: "create-user",
            label: t("新建用户"),
            onPress: openCreate,
          }}
          text={t("还没有普通用户，点击右上角新建用户并分配隧道或订阅。")}
        />
      ) : (
        <ListGroup count={users.length} title={t("普通用户")}>
          {users.map((u) => {
            const used = u.inFlow + u.outFlow;
            const flowLabel =
              u.flowLimitBytes > 0
                ? `${formatTraffic(used)} / ${formatTraffic(u.flowLimitBytes)}`
                : `${formatTraffic(used)} / ${t("不限")}`;
            const ruleLabel =
              u.num > 0
                ? t("{count} / {max} 条规则", {
                    count: u.forwardCount,
                    max: u.num,
                  })
                : t("{count} 条规则", { count: u.forwardCount });
            const expLabel =
              u.expTime > 0
                ? t("到期 {date}", { date: formatDate(u.expTime) })
                : t("永不过期");

            return (
              <ListRow
                key={u.id}
                badges={
                  <div className="flex flex-wrap items-center gap-1">
                    {u.status === 0 && (
                      <Chip color="default" size="sm" variant="flat">
                        {t("已停用")}
                      </Chip>
                    )}
                    {u.expired && (
                      <Chip color="danger" size="sm" variant="flat">
                        {t("已到期")}
                      </Chip>
                    )}
                    {u.overQuota && (
                      <Chip color="warning" size="sm" variant="flat">
                        {t("中转超额")}
                      </Chip>
                    )}
                  </div>
                }
                meta={
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-default-500">
                    <span>{t("中转 {flow}", { flow: flowLabel })}</span>
                    <span>{ruleLabel}</span>
                    <span>
                      {t("{count} 条隧道", { count: u.tunnelIds.length })}
                    </span>
                    <span>
                      {t("{count} 个订阅", {
                        count: u.subscriptionIds.length,
                      })}
                    </span>
                    <span>{expLabel}</span>
                  </div>
                }
                status={userRowStatus(u)}
                title={u.user}
                onPress={() => setSheetUser(u)}
              />
            );
          })}
        </ListGroup>
      )}

      <ActionSheet
        actions={
          sheetUser
            ? [
                {
                  key: "edit",
                  label: t("编辑用户"),
                  onPress: () => openEdit(sheetUser),
                },
                {
                  key: "reset",
                  label: t("重置流量"),
                  onPress: () => {
                    void handleResetFlow(sheetUser);
                  },
                },
                {
                  key: "toggle",
                  label: sheetUser.status === 1 ? t("停用账号") : t("启用账号"),
                  color: sheetUser.status === 1 ? "warning" : "primary",
                  onPress: () => {
                    void handleToggleStatus(sheetUser);
                  },
                },
                {
                  key: "delete",
                  label: t("删除用户"),
                  color: "danger",
                  onPress: () => setDeleteTarget(sheetUser),
                },
              ]
            : []
        }
        isOpen={Boolean(sheetUser)}
        title={sheetUser?.user ?? ""}
        onClose={() => setSheetUser(null)}
      >
        {sheetUser && (
          <div className="space-y-2 text-sm text-default-600">
            <div className="flex justify-between">
              <span className="text-default-400">{t("中转用量")}</span>
              <span>
                {formatTraffic(sheetUser.inFlow + sheetUser.outFlow)} /{" "}
                {sheetUser.flowLimitBytes > 0
                  ? formatTraffic(sheetUser.flowLimitBytes)
                  : t("不限")}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-default-400">{t("每月重置")}</span>
              <span>
                {sheetUser.flowResetTime > 0
                  ? t("每月 {day} 号", { day: sheetUser.flowResetTime })
                  : t("不重置")}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-default-400">{t("转发规则")}</span>
              <span>
                {sheetUser.forwardCount} /{" "}
                {sheetUser.num > 0 ? sheetUser.num : t("不限")}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-default-400">{t("中转到期")}</span>
              <span>
                {sheetUser.expTime > 0
                  ? formatDate(sheetUser.expTime)
                  : t("永不过期")}
              </span>
            </div>
            <div>
              <div className="text-default-400 mb-1">{t("授权隧道")}</div>
              {sheetUser.tunnelIds.length === 0 ? (
                <span className="text-xs text-default-400">{t("未分配")}</span>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {sheetUser.tunnelIds.map((id) => (
                    <Chip key={id} size="sm" variant="flat">
                      {tunnelNameById.get(id) ?? `#${id}`}
                    </Chip>
                  ))}
                </div>
              )}
            </div>
            <div>
              <div className="text-default-400 mb-1">{t("关联订阅")}</div>
              {sheetUser.subscriptionIds.length === 0 ? (
                <span className="text-xs text-default-400">{t("未分配")}</span>
              ) : (
                <div className="flex flex-wrap gap-1">
                  {sheetUser.subscriptionIds.map((id) => (
                    <Chip key={id} color="primary" size="sm" variant="flat">
                      {subNameById.get(id) ?? `#${id}`}
                    </Chip>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}
      </ActionSheet>

      <Modal
        isOpen={modalOpen}
        scrollBehavior="inside"
        size="lg"
        onOpenChange={setModalOpen}
      >
        <ModalContent>
          <ModalHeader>{form.id ? t("编辑用户") : t("新建用户")}</ModalHeader>
          <ModalBody className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <Input
                isRequired
                label={t("用户名")}
                placeholder={t("输入用户名")}
                value={form.user}
                onChange={(e) => setForm({ ...form, user: e.target.value })}
              />
              <Input
                isRequired={!form.id}
                label={form.id ? t("新密码（留空不改）") : t("密码")}
                placeholder={form.id ? t("留空保持原密码") : t("至少 6 位")}
                type="password"
                value={form.pwd}
                onChange={(e) => setForm({ ...form, pwd: e.target.value })}
              />
            </div>

            <div className="flex items-center justify-between py-1">
              <span className="text-sm font-medium">{t("启用账号")}</span>
              <Switch
                isSelected={form.enabled}
                onValueChange={(v) => setForm({ ...form, enabled: v })}
              />
            </div>

            <div className="border-t border-divider pt-3 space-y-3">
              <div className="text-xs font-medium text-default-500">
                {t("中转配额（与订阅额度分开计算）")}
              </div>
              <TrafficLimitField
                description={t("留空或 0 表示不限制中转流量")}
                label={t("中转流量上限")}
                unit={form.flowUnit}
                value={form.flowInput}
                onChange={(v, u) =>
                  setForm({ ...form, flowInput: v, flowUnit: u })
                }
              />
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <Input
                  description={t("0 表示不重置，1–28 为每月重置日")}
                  label={t("每月重置日")}
                  max={28}
                  min={0}
                  type="number"
                  value={form.flowResetTime}
                  onChange={(e) =>
                    setForm({ ...form, flowResetTime: e.target.value })
                  }
                />
                <Input
                  description={t("0 表示不限制规则数")}
                  label={t("最大规则数")}
                  min={0}
                  type="number"
                  value={form.num}
                  onChange={(e) => setForm({ ...form, num: e.target.value })}
                />
                <Input
                  description={t("留空表示永不过期")}
                  label={t("中转到期日期")}
                  type="date"
                  value={form.expDate}
                  onChange={(e) =>
                    setForm({ ...form, expDate: e.target.value })
                  }
                />
              </div>

              <div>
                <div className="text-xs text-default-500 mb-1.5">
                  {t("授权隧道（勾选后用户可在这些隧道上创建转发）")}
                </div>
                {tunnels.length === 0 ? (
                  <div className="text-xs text-default-400">
                    {t("暂无可用隧道")}
                  </div>
                ) : (
                  <div className="flex flex-wrap gap-1.5">
                    {tunnels.map((item) => {
                      const selected = form.tunnelIds.includes(item.id);

                      return (
                        <button
                          key={item.id}
                          className={`px-2.5 py-1 rounded-lg text-xs border transition-colors ${
                            selected
                              ? "bg-primary/15 border-primary text-primary font-medium"
                              : "border-divider text-default-600 hover:bg-default-100"
                          }`}
                          type="button"
                          onClick={() =>
                            setForm({
                              ...form,
                              tunnelIds: toggleId(form.tunnelIds, item.id),
                            })
                          }
                        >
                          {item.name}
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>

            <div className="border-t border-divider pt-3 space-y-2">
              <div className="text-xs font-medium text-default-500">
                {t("关联订阅（用户登录后可查看链接、用量与重置链接）")}
              </div>
              {subs.length === 0 ? (
                <div className="text-xs text-default-400">
                  {t("暂无订阅，请先在「订阅」页创建")}
                </div>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {subs.map((s) => {
                    const selected = form.subscriptionIds.includes(s.id);
                    const otherOwner =
                      s.userId && s.userId !== form.id && s.userName
                        ? ` (${s.userName})`
                        : "";

                    return (
                      <button
                        key={s.id}
                        className={`px-2.5 py-1 rounded-lg text-xs border transition-colors ${
                          selected
                            ? "bg-primary/15 border-primary text-primary font-medium"
                            : "border-divider text-default-600 hover:bg-default-100"
                        }`}
                        type="button"
                        onClick={() =>
                          setForm({
                            ...form,
                            subscriptionIds: toggleId(
                              form.subscriptionIds,
                              s.id,
                            ),
                          })
                        }
                      >
                        {s.name}
                        {otherOwner}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={() => setModalOpen(false)}>
              {t("取消")}
            </Button>
            <Button
              color="primary"
              isLoading={saving}
              onPress={() => void handleSave()}
            >
              {t("保存")}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      <Modal
        isOpen={Boolean(deleteTarget)}
        size="sm"
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <ModalContent>
          <ModalHeader>{t("删除用户")}</ModalHeader>
          <ModalBody>
            <p className="text-sm text-default-600">
              {t("确定删除用户 ")}
              <strong>{deleteTarget?.user}</strong>
              {t(
                "？该用户的转发规则会被一并删除，关联的订阅会解除归属（不会删除订阅本身）。",
              )}
            </p>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={() => setDeleteTarget(null)}>
              {t("取消")}
            </Button>
            <Button color="danger" onPress={() => void handleDelete()}>
              {t("确认删除")}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}
