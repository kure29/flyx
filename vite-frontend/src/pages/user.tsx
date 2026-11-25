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
      toast.error("加载用户列表失败");
    } finally {
      setLoading(false);
    }
  }, []);

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
      toast.error("请填写用户名");

      return;
    }
    if (!form.id && form.pwd.length < 6) {
      toast.error("密码长度至少为 6 位");

      return;
    }
    if (form.id && form.pwd && form.pwd.length < 6) {
      toast.error("新密码长度至少为 6 位");

      return;
    }
    const resetDay = Math.max(0, Math.min(28, Number(form.flowResetTime) || 0));
    const num = Math.max(0, Math.floor(Number(form.num) || 0));
    let flowMiB = 0;

    if (form.flowInput.trim() !== "") {
      const parsed = parseTrafficInput(form.flowInput, form.flowUnit);

      if (parsed === null || parsed < 0) {
        toast.error("流量上限格式不正确");

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
        toast.error(res.msg || "保存失败");

        return;
      }
      toast.success(form.id ? "已保存" : "用户已创建");
      setModalOpen(false);
      await loadAll();
    } catch {
      toast.error("保存失败");
    } finally {
      setSaving(false);
    }
  };

  const handleResetFlow = async (u: UserApiItem) => {
    try {
      const res = await resetUserFlow({ id: u.id });

      if (res.code !== 0) {
        toast.error(res.msg || "重置流量失败");

        return;
      }
      toast.success("已重置中转流量，符合条件的转发已自动恢复");
      setSheetUser(null);
      await loadAll();
    } catch {
      toast.error("重置流量失败");
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
        toast.error(res.msg || "操作失败");

        return;
      }
      toast.success(nextStatus === 1 ? "已启用" : "已停用并暂停其转发");
      setSheetUser(null);
      await loadAll();
    } catch {
      toast.error("操作失败");
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      const res = await deleteUser(deleteTarget.id);

      if (res.code !== 0) {
        toast.error(res.msg || "删除失败");

        return;
      }
      toast.success("用户已删除");
      setDeleteTarget(null);
      setSheetUser(null);
      await loadAll();
    } catch {
      toast.error("删除失败");
    }
  };

  const tunnelNameById = new Map(tunnels.map((t) => [t.id, t.name]));
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
        description="为其他用户分配中转隧道额度与分享订阅"
        primary={{
          key: "create-user",
          label: "新建用户",
          icon: <Plus className="w-4 h-4" />,
          onPress: openCreate,
        }}
        title="用户"
      />

      {users.length === 0 ? (
        <EmptyHint
          action={{
            key: "create-user",
            label: "新建用户",
            onPress: openCreate,
          }}
          text="还没有普通用户，点击右上角新建用户并分配隧道或订阅。"
        />
      ) : (
        <ListGroup count={users.length} title="普通用户">
          {users.map((u) => {
            const used = u.inFlow + u.outFlow;
            const flowLabel =
              u.flowLimitBytes > 0
                ? `${formatTraffic(used)} / ${formatTraffic(u.flowLimitBytes)}`
                : `${formatTraffic(used)} / 不限`;
            const ruleLabel =
              u.num > 0
                ? `${u.forwardCount} / ${u.num} 条规则`
                : `${u.forwardCount} 条规则`;
            const expLabel =
              u.expTime > 0 ? `到期 ${formatDate(u.expTime)}` : "永不过期";

            return (
              <ListRow
                key={u.id}
                badges={
                  <div className="flex flex-wrap items-center gap-1">
                    {u.status === 0 && (
                      <Chip color="default" size="sm" variant="flat">
                        已停用
                      </Chip>
                    )}
                    {u.expired && (
                      <Chip color="danger" size="sm" variant="flat">
                        已到期
                      </Chip>
                    )}
                    {u.overQuota && (
                      <Chip color="warning" size="sm" variant="flat">
                        中转超额
                      </Chip>
                    )}
                  </div>
                }
                meta={
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-default-500">
                    <span>中转 {flowLabel}</span>
                    <span>{ruleLabel}</span>
                    <span>{u.tunnelIds.length} 条隧道</span>
                    <span>{u.subscriptionIds.length} 个订阅</span>
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
                  label: "编辑用户与配额",
                  onPress: () => openEdit(sheetUser),
                },
                {
                  key: "reset",
                  label: "重置中转流量",
                  onPress: () => {
                    void handleResetFlow(sheetUser);
                  },
                },
                {
                  key: "toggle",
                  label: sheetUser.status === 1 ? "停用账号" : "启用账号",
                  color: sheetUser.status === 1 ? "warning" : "primary",
                  onPress: () => {
                    void handleToggleStatus(sheetUser);
                  },
                },
                {
                  key: "delete",
                  label: "删除用户",
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
              <span className="text-default-400">中转用量</span>
              <span>
                {formatTraffic(sheetUser.inFlow + sheetUser.outFlow)} /{" "}
                {sheetUser.flowLimitBytes > 0
                  ? formatTraffic(sheetUser.flowLimitBytes)
                  : "不限"}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-default-400">每月重置</span>
              <span>
                {sheetUser.flowResetTime > 0
                  ? `每月 ${sheetUser.flowResetTime} 号`
                  : "不重置"}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-default-400">转发规则</span>
              <span>
                {sheetUser.forwardCount} /{" "}
                {sheetUser.num > 0 ? sheetUser.num : "不限"}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-default-400">中转到期</span>
              <span>
                {sheetUser.expTime > 0
                  ? formatDate(sheetUser.expTime)
                  : "永不过期"}
              </span>
            </div>
            <div>
              <div className="text-default-400 mb-1">授权隧道</div>
              {sheetUser.tunnelIds.length === 0 ? (
                <span className="text-xs text-default-400">未分配</span>
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
              <div className="text-default-400 mb-1">关联订阅</div>
              {sheetUser.subscriptionIds.length === 0 ? (
                <span className="text-xs text-default-400">未分配</span>
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
          <ModalHeader>{form.id ? "编辑用户" : "新建用户"}</ModalHeader>
          <ModalBody className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <Input
                isRequired
                label="用户名"
                placeholder="输入用户名"
                value={form.user}
                onChange={(e) => setForm({ ...form, user: e.target.value })}
              />
              <Input
                isRequired={!form.id}
                label={form.id ? "新密码（留空不改）" : "密码"}
                placeholder={form.id ? "留空保持原密码" : "至少 6 位"}
                type="password"
                value={form.pwd}
                onChange={(e) => setForm({ ...form, pwd: e.target.value })}
              />
            </div>

            <div className="flex items-center justify-between py-1">
              <span className="text-sm font-medium">启用账号</span>
              <Switch
                isSelected={form.enabled}
                onValueChange={(v) => setForm({ ...form, enabled: v })}
              />
            </div>

            <div className="border-t border-divider pt-3 space-y-3">
              <div className="text-xs font-medium text-default-500">
                中转配额（与订阅额度分开计算）
              </div>
              <TrafficLimitField
                description="留空或 0 表示不限制中转流量"
                label="中转流量上限"
                unit={form.flowUnit}
                value={form.flowInput}
                onChange={(v, u) =>
                  setForm({ ...form, flowInput: v, flowUnit: u })
                }
              />
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                <Input
                  description="0 表示不重置，1–28 为每月重置日"
                  label="每月重置日"
                  max={28}
                  min={0}
                  type="number"
                  value={form.flowResetTime}
                  onChange={(e) =>
                    setForm({ ...form, flowResetTime: e.target.value })
                  }
                />
                <Input
                  description="0 表示不限制规则数"
                  label="最大规则数"
                  min={0}
                  type="number"
                  value={form.num}
                  onChange={(e) => setForm({ ...form, num: e.target.value })}
                />
                <Input
                  description="留空表示永不过期"
                  label="中转到期日期"
                  type="date"
                  value={form.expDate}
                  onChange={(e) =>
                    setForm({ ...form, expDate: e.target.value })
                  }
                />
              </div>

              <div>
                <div className="text-xs text-default-500 mb-1.5">
                  授权隧道（勾选后用户可在这些隧道上创建转发）
                </div>
                {tunnels.length === 0 ? (
                  <div className="text-xs text-default-400">暂无可用隧道</div>
                ) : (
                  <div className="flex flex-wrap gap-1.5">
                    {tunnels.map((t) => {
                      const selected = form.tunnelIds.includes(t.id);

                      return (
                        <button
                          key={t.id}
                          className={`px-2.5 py-1 rounded-lg text-xs border transition-colors ${
                            selected
                              ? "bg-primary/15 border-primary text-primary font-medium"
                              : "border-divider text-default-600 hover:bg-default-100"
                          }`}
                          type="button"
                          onClick={() =>
                            setForm({
                              ...form,
                              tunnelIds: toggleId(form.tunnelIds, t.id),
                            })
                          }
                        >
                          {t.name}
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>

            <div className="border-t border-divider pt-3 space-y-2">
              <div className="text-xs font-medium text-default-500">
                关联订阅（用户登录后可查看链接、用量与重置链接）
              </div>
              {subs.length === 0 ? (
                <div className="text-xs text-default-400">
                  暂无订阅，请先在「订阅」页创建
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
              取消
            </Button>
            <Button
              color="primary"
              isLoading={saving}
              onPress={() => void handleSave()}
            >
              保存
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
          <ModalHeader>删除用户</ModalHeader>
          <ModalBody>
            <p className="text-sm text-default-600">
              确定删除用户 <strong>{deleteTarget?.user}</strong>
              ？该用户的转发规则会被一并删除，关联的订阅会解除归属（不会删除订阅本身）。
            </p>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={() => setDeleteTarget(null)}>
              取消
            </Button>
            <Button color="danger" onPress={() => void handleDelete()}>
              确认删除
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}
