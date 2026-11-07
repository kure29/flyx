import React, { useState, useEffect } from "react";
import { toast } from "react-hot-toast";
import { useNavigate } from "react-router-dom";
import {
  ChevronRight,
  Fingerprint,
  Github,
  KeyRound,
  LogOut,
  Settings,
  Share2,
  type LucideIcon,
} from "lucide-react";

import { Button } from "@/shadcn-bridge/heroui/button";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  useDisclosure,
} from "@/shadcn-bridge/heroui/modal";
import { Input } from "@/shadcn-bridge/heroui/input";
import { isWebViewFunc } from "@/utils/panel";
import { siteConfig } from "@/config/site";
import { VersionFooter } from "@/components/version-footer";
import {
  getGithubInfo,
  getPasskeyStatus,
  listPasskeys,
  updatePassword,
  type GithubInfo,
  type PasskeyItem,
} from "@/api";
import { safeLogout } from "@/utils/logout";
import { getSessionName } from "@/utils/session";
import { PasskeyModal } from "@/components/passkey-manager";
import { GithubBindingModal } from "@/components/github-binding";
import { PageHeader } from "@/components/page-kit";
import { useH5Mode } from "@/hooks/useH5Mode";

interface PasswordForm {
  newUsername: string;
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

interface SecurityItem {
  key: string;
  label: string;
  description: string;
  /** The current state, e.g. "已绑定 kure29". */
  status: string;
  /** Whether the status is a good one (bound, set up). */
  active?: boolean;
  icon: LucideIcon;
  onPress: () => void;
}

interface RowItem {
  key: string;
  label: string;
  description?: string;
  status?: string;
  active?: boolean;
  icon: LucideIcon;
  onPress: () => void;
}

/** A titled group of rows split by hairlines, as on the other pages. */
function RowGroup({ title, items }: { title?: string; items: RowItem[] }) {
  return (
    <section>
      {title && (
        <h2 className="border-b border-divider px-1 pb-1.5 text-xs font-medium text-default-500">
          {title}
        </h2>
      )}
      <div className="divide-y divide-divider border-b border-divider">
        {items.map((item) => (
          <button
            key={item.key}
            className="flex w-full items-center gap-3 px-1 py-3 text-left transition-colors hover:bg-default-50"
            type="button"
            onClick={item.onPress}
          >
            <item.icon
              className="h-4 w-4 shrink-0 text-default-500"
              strokeWidth={1.75}
            />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm">{item.label}</span>
              {item.description && (
                <span className="hidden truncate text-xs text-default-400 sm:block">
                  {item.description}
                </span>
              )}
            </span>
            {item.status && (
              <span
                className={`max-w-[45%] truncate text-xs ${item.active ? "text-default-600" : "text-default-400"}`}
              >
                {item.status}
              </span>
            )}
            <ChevronRight className="h-4 w-4 shrink-0 text-default-300" />
          </button>
        ))}
      </div>
    </section>
  );
}

/**
 * The login rows (passkey, GitHub) only show once their status loads, so
 * the page jumped as they popped in. The last status is remembered per
 * user in this browser and shown at once, then refreshed.
 */
interface LoginCache {
  passkeyEnabled: boolean;
  passkeyCount: number;
  github: GithubInfo | null;
}

const loginCacheKey = () => `flyx_login_rows_${getSessionName() || ""}`;

function readLoginCache(): LoginCache {
  try {
    const raw = JSON.parse(localStorage.getItem(loginCacheKey()) || "null");

    if (raw && typeof raw === "object") return raw as LoginCache;
  } catch {
    // ignore: the rows just load as before
  }

  return { passkeyEnabled: false, passkeyCount: 0, github: null };
}

function writeLoginCache(patch: Partial<LoginCache>) {
  try {
    localStorage.setItem(
      loginCacheKey(),
      JSON.stringify({ ...readLoginCache(), ...patch }),
    );
  } catch {
    // ignore
  }
}

export default function ProfilePage() {
  const navigate = useNavigate();
  const isH5 = useH5Mode();
  const { isOpen, onOpen, onOpenChange } = useDisclosure();
  const [username, setUsername] = useState("");
  const [passwordLoading, setPasswordLoading] = useState(false);
  const [passwordForm, setPasswordForm] = useState<PasswordForm>({
    newUsername: "",
    currentPassword: "",
    newPassword: "",
    confirmPassword: "",
  });

  const [cached] = useState(readLoginCache);
  const [passkeyEnabled, setPasskeyEnabled] = useState(cached.passkeyEnabled);
  const [passkeys, setPasskeys] = useState<PasskeyItem[] | null>(null);
  const passkeyCount = passkeys?.length ?? cached.passkeyCount;
  const [github, setGithub] = useState<GithubInfo | null>(cached.github);
  const [openLogin, setOpenLogin] = useState<"passkey" | "github" | null>(null);
  // The app's WebView can't do passkeys or come back from GitHub.
  const webView = isWebViewFunc();

  const loadPasskeys = () =>
    listPasskeys()
      .then((res) => {
        if (res.code === 0) {
          setPasskeys(res.data || []);
          writeLoginCache({ passkeyCount: (res.data || []).length });
        }
      })
      .catch(() => setPasskeys([]));
  const loadGithub = () =>
    getGithubInfo()
      .then((res) => {
        if (res.code === 0) {
          setGithub(res.data);
          writeLoginCache({ github: res.data });
        }
      })
      .catch(() => setGithub(null));

  useEffect(() => {
    // 获取用户信息
    setUsername(getSessionName() || "Admin");
    void loadGithub();
    if (
      window.isSecureContext &&
      typeof window.PublicKeyCredential !== "undefined"
    ) {
      getPasskeyStatus()
        .then((res) => {
          if (res.code !== 0) return;
          setPasskeyEnabled(res.data.enabled);
          writeLoginCache({ passkeyEnabled: res.data.enabled });
          if (res.data.enabled) void loadPasskeys();
        })
        .catch(() => setPasskeyEnabled(false));
    }
  }, []);

  const go = (path: string) => () => navigate(path);

  // On phones the sidebar is gone; these are its admin pages.
  const shortcuts: RowItem[] = [
    {
      key: "/panel-sharing",
      label: "面板共享",
      description: "与其他面板联邦共享",
      icon: Share2,
      onPress: go("/panel-sharing"),
    },
    {
      key: "/config",
      label: "网站配置",
      description: "站点、登录与备份",
      icon: Settings,
      onPress: go("/config"),
    },
  ];

  // 退出登录
  const handleLogout = () => {
    safeLogout();
  };

  // 密码表单验证
  const validatePasswordForm = (): boolean => {
    if (!passwordForm.newUsername.trim()) {
      toast.error("请输入新用户名");

      return false;
    }
    if (passwordForm.newUsername.length < 3) {
      toast.error("用户名长度至少3位");

      return false;
    }
    if (!passwordForm.currentPassword) {
      toast.error("请输入当前密码");

      return false;
    }
    if (!passwordForm.newPassword) {
      toast.error("请输入新密码");

      return false;
    }
    if (passwordForm.newPassword.length < 6) {
      toast.error("新密码长度不能少于6位");

      return false;
    }
    if (passwordForm.newPassword !== passwordForm.confirmPassword) {
      toast.error("两次输入密码不一致");

      return false;
    }

    return true;
  };

  // 提交密码修改
  const handlePasswordSubmit = async () => {
    if (!validatePasswordForm()) return;

    setPasswordLoading(true);
    try {
      const response = await updatePassword(passwordForm);

      if (response.code === 0) {
        toast.success("密码修改成功，请重新登录");
        onOpenChange();
        handleLogout();
      } else {
        toast.error(response.msg || "密码修改失败");
      }
    } catch {
      toast.error("修改密码时发生错误");
    } finally {
      setPasswordLoading(false);
    }
  };

  // 重置密码表单
  const resetPasswordForm = () => {
    setPasswordForm({
      newUsername: "",
      currentPassword: "",
      newPassword: "",
      confirmPassword: "",
    });
  };

  const securityItems: SecurityItem[] = [
    {
      key: "password",
      label: "用户名与密码",
      description: "修改登录用的用户名和密码，改完需要重新登录",
      status: username,
      icon: KeyRound,
      onPress: onOpen,
    },
    ...(passkeyEnabled && !webView
      ? [
          {
            key: "passkey",
            label: "通行证密钥",
            description: "用指纹、面容或设备密码登录，不用输入密码",
            status: passkeyCount > 0 ? `${passkeyCount} 个` : "未设置",
            active: passkeyCount > 0,
            icon: Fingerprint,
            onPress: () => setOpenLogin("passkey"),
          },
        ]
      : []),
    // Shown once an admin turns GitHub login on, or while still bound so
    // the user can unbind.
    ...(github && !webView && (github.enabled || github.bound)
      ? [
          {
            key: "github",
            label: "GitHub 登录",
            description: "在登录页用 GitHub 账号一键登录",
            status: github.bound ? `已绑定 ${github.login}` : "未绑定",
            active: github.bound,
            icon: Github,
            onPress: () => setOpenLogin("github"),
          },
        ]
      : []),
  ];

  const account = (
    <div className="flex items-baseline justify-between gap-3 border-b border-divider px-1 pb-3">
      <span className="truncate text-sm">{username}</span>
      <span className="shrink-0 text-xs text-default-400">管理员</span>
    </div>
  );
  const logoutRow = (
    <button
      className="flex w-full items-center gap-3 border-b border-divider px-1 py-3 text-left text-sm text-danger transition-colors hover:bg-default-50"
      type="button"
      onClick={handleLogout}
    >
      <LogOut className="h-4 w-4 shrink-0" strokeWidth={1.75} />
      退出登录
    </button>
  );

  return (
    <div className="flex min-h-full flex-col px-4 py-6 lg:px-10 lg:py-8">
      <div className="mx-auto w-full max-w-2xl flex-1">
        <PageHeader
          description={isH5 ? undefined : "你的账户和登录方式"}
          title={isH5 ? "更多" : "账户与登录"}
        />
        <div className="space-y-8">
          {account}
          {isH5 && <RowGroup items={shortcuts} title="管理" />}
          <RowGroup items={securityItems} title="登录与安全" />
          {logoutRow}
        </div>
      </div>

      {/* The desktop sidebar already shows the version. */}
      {isH5 && (
        <VersionFooter
          containerClassName="mt-8 text-center"
          poweredClassName="text-xs text-default-400"
          updateBadgeClassName="ml-1.5 text-foreground"
          version={
            isWebViewFunc() ? siteConfig.app_version : siteConfig.version
          }
          versionClassName="mt-1 text-xs text-default-400"
        />
      )}

      <PasskeyModal
        isOpen={openLogin === "passkey"}
        items={passkeys ?? []}
        onChanged={() => void loadPasskeys()}
        onClose={() => setOpenLogin(null)}
      />
      <GithubBindingModal
        info={github}
        isOpen={openLogin === "github"}
        onChanged={() => void loadGithub()}
        onClose={() => setOpenLogin(null)}
      />

      {/* 修改密码弹窗 */}
      <Modal
        classNames={{
          base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
        }}
        isOpen={isOpen}
        placement="center"
        scrollBehavior="inside"
        size="2xl"
        onOpenChange={() => {
          onOpenChange();
          resetPasswordForm();
        }}
      >
        <ModalContent>
          {(onClose: () => void) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                修改密码
              </ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  <Input
                    label="新用户名"
                    placeholder="请输入新用户名（至少3位）"
                    value={passwordForm.newUsername}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setPasswordForm((prev) => ({
                        ...prev,
                        newUsername: e.target.value,
                      }))
                    }
                  />
                  <Input
                    label="当前密码"
                    placeholder="请输入当前密码"
                    type="password"
                    value={passwordForm.currentPassword}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setPasswordForm((prev) => ({
                        ...prev,
                        currentPassword: e.target.value,
                      }))
                    }
                  />
                  <Input
                    label="新密码"
                    placeholder="请输入新密码（至少6位）"
                    type="password"
                    value={passwordForm.newPassword}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setPasswordForm((prev) => ({
                        ...prev,
                        newPassword: e.target.value,
                      }))
                    }
                  />
                  <Input
                    label="确认密码"
                    placeholder="请再次输入新密码"
                    type="password"
                    value={passwordForm.confirmPassword}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setPasswordForm((prev) => ({
                        ...prev,
                        confirmPassword: e.target.value,
                      }))
                    }
                  />
                </div>
              </ModalBody>
              <ModalFooter>
                <Button color="default" variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color="primary"
                  isLoading={passwordLoading}
                  onPress={handlePasswordSubmit}
                >
                  修改密码
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </div>
  );
}
