import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import toast from "react-hot-toast";
import { Turnstile } from "@marsidev/react-turnstile";
import { Eye, EyeOff } from "lucide-react";

import { Card, CardBody, CardHeader } from "@/shadcn-bridge/heroui/card";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Button } from "@/shadcn-bridge/heroui/button";
import { siteConfig } from "@/config/site";
import { VersionFooter } from "@/components/version-footer";
import { BrandLogo } from "@/components/brand-logo";
import { LanguageToggle } from "@/components/language-toggle";
import { useI18n } from "@/i18n";
import {
  login,
  LoginData,
  checkCaptcha,
  getPublicConfigByName,
  getPasskeyStatus,
  beginPasskeyLogin,
  finishPasskeyLogin,
  getGithubStatus,
  getLoginMethods,
  beginGithubLogin,
} from "@/api";
import { writeLoginSession } from "@/utils/session";
import { getPasskey } from "@/utils/passkey";
import { githubCallbackUrl, goToGithub } from "@/utils/github-login";
import { useWebViewMode } from "@/hooks/useWebViewMode";

interface LoginForm {
  username: string;
  password: string;
  captchaId: string;
}

export default function IndexPage() {
  const { t } = useI18n();
  const [form, setForm] = useState<LoginForm>({
    username: "",
    password: "",
    captchaId: "",
  });
  const [loading, setLoading] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [errors, setErrors] = useState<Partial<LoginForm>>({});
  const [showCaptcha, setShowCaptcha] = useState(false);
  const [siteKey, setSiteKey] = useState("");
  const [passkeyEnabled, setPasskeyEnabled] = useState(false);
  const [githubEnabled, setGithubEnabled] = useState(false);
  const [statusPageEnabled, setStatusPageEnabled] = useState(false);
  // null until known, so the password form does not flash in and out.
  const [passwordAllowed, setPasswordAllowed] = useState<boolean | null>(null);
  const [userPasswordAllowed, setUserPasswordAllowed] = useState(false);
  const [showUserPasswordForm, setShowUserPasswordForm] = useState(false);
  const navigate = useNavigate();
  const isWebView = useWebViewMode();

  // The app's WebView can't come back from GitHub, so no button there.
  useEffect(() => {
    if (isWebView) return;
    getGithubStatus()
      .then((res) => setGithubEnabled(res.code === 0 && res.data.enabled))
      .catch(() => setGithubEnabled(false));
  }, [isWebView]);

  useEffect(() => {
    getLoginMethods()
      .then((res) => {
        if (res.code !== 0) {
          setPasswordAllowed(true);

          return;
        }
        setPasswordAllowed(res.data.password);
        setUserPasswordAllowed(Boolean(res.data.userPassword));
      })
      .catch(() => setPasswordAllowed(true));
  }, []);

  useEffect(() => {
    getPublicConfigByName("status_page_enabled")
      .then((res) =>
        setStatusPageEnabled(res.code === 0 && res.data?.value === "true"),
      )
      .catch(() => setStatusPageEnabled(false));
  }, []);

  const handleGithubLogin = async () => {
    setLoading(true);
    try {
      const res = await beginGithubLogin(githubCallbackUrl());

      if (res.code !== 0) {
        toast.error(res.msg || t("无法使用 GitHub 登录"));
        setLoading(false);

        return;
      }
      goToGithub("login", res.data.url);
    } catch {
      toast.error(t("无法使用 GitHub 登录"));
      setLoading(false);
    }
  };

  useEffect(() => {
    if (
      window.isSecureContext &&
      typeof window.PublicKeyCredential !== "undefined"
    ) {
      getPasskeyStatus()
        .then((res) => setPasskeyEnabled(res.code === 0 && res.data.enabled))
        .catch(() => setPasskeyEnabled(false));
    }
  }, []);

  const handlePasskeyLogin = async () => {
    setLoading(true);
    try {
      const begin = await beginPasskeyLogin();

      if (begin.code !== 0) {
        toast.error(begin.msg || t("无法使用通行证密钥登录"));

        return;
      }
      const credential = await getPasskey(begin.data.options);
      const result = await finishPasskeyLogin(begin.data.sessionId, credential);

      if (result.code !== 0) {
        toast.error(result.msg || t("通行证密钥登录失败"));

        return;
      }
      writeLoginSession(result.data);
      toast.success(t("登录成功"));
      navigate(
        result.data.requirePasswordChange ? "/change-password" : "/dashboard",
      );
    } catch {
      toast.error(t("通行证密钥登录已取消或失败"));
    } finally {
      setLoading(false);
    }
  };

  // 验证表单
  const validateForm = (): boolean => {
    const newErrors: Partial<LoginForm> = {};

    if (!form.username.trim()) {
      newErrors.username = t("请输入用户名");
    }

    if (!form.password.trim()) {
      newErrors.password = t("请输入密码");
    } else if (form.password.length < 6) {
      newErrors.password = t("密码长度至少6位");
    }

    setErrors(newErrors);

    return Object.keys(newErrors).length === 0;
  };

  // 处理输入变化
  const handleInputChange = (field: keyof LoginForm, value: string) => {
    setForm((prev) => ({ ...prev, [field]: value }));
    // 清除该字段的错误
    if (errors[field]) {
      setErrors((prev) => ({ ...prev, [field]: undefined }));
    }
  };

  // 执行登录请求
  const performLogin = async (captchaToken?: string) => {
    try {
      const finalCaptchaId =
        typeof captchaToken === "string" && captchaToken.trim()
          ? captchaToken
          : form.captchaId;

      const loginData: LoginData = {
        username: form.username.trim(),
        password: form.password,
        captchaId: finalCaptchaId,
      };

      const response = await login(loginData);

      if (response.code !== 0) {
        toast.error(response.msg || t("登录失败"));
        if (showCaptcha) {
          setForm((prev) => ({ ...prev, captchaId: "" }));
        }

        return;
      }

      // 检查是否需要强制修改密码
      if (response.data.requirePasswordChange) {
        writeLoginSession(response.data);
        toast.success(t("检测到默认密码，即将跳转到修改密码页面"));
        navigate("/change-password");

        return;
      }

      // 保存登录信息
      writeLoginSession(response.data);

      // 登录成功
      toast.success(t("登录成功"));
      navigate(response.data.role_id === 0 ? "/dashboard" : "/forward");
    } catch {
      toast.error(t("网络错误，请稍后重试"));
    } finally {
      setLoading(false);
    }
  };

  const handleLogin = async () => {
    if (!validateForm()) return;

    setLoading(true);

    try {
      // 先检查是否需要验证码
      const checkResponse = await checkCaptcha();

      if (checkResponse.code !== 0) {
        toast.error(t("检查验证码状态失败，请重试") + checkResponse.msg);
        setLoading(false);

        return;
      }

      // 根据返回值决定是否显示验证码
      if (checkResponse.data === 0) {
        await performLogin();
      } else {
        const configResp = await getPublicConfigByName("cloudflare_site_key");

        if (configResp.code === 0 && configResp.data && configResp.data.value) {
          setSiteKey(configResp.data.value);
          setShowCaptcha(true);
        } else {
          toast.error(t("未配置Cloudflare Site Key，请联系管理员"));
          setLoading(false);
        }
      }
    } catch (error) {
      toast.error(t("网络错误，请稍后重试") + error);
      setLoading(false);
    }
  };

  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !loading) {
      handleLogin();
    }
  };

  return (
    <div className="relative flex flex-col min-h-screen bg-mesh-gradient overflow-hidden">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_at_top,rgba(120,120,120,0.08),transparent_60%)] dark:bg-[radial-gradient(ellipse_at_top,rgba(255,255,255,0.05),transparent_55%)]"
      />
      <div className="fixed right-4 top-4 z-20 safe-top">
        <LanguageToggle variant="pill" />
      </div>
      <section className="flex flex-col items-center justify-center flex-1 w-full p-4 relative z-10">
        <div className="ui-fade-in w-full max-w-[392px] px-2 sm:px-0">
          <Card className="w-full rounded-2xl border border-divider bg-content1/90 p-1 shadow-[0_12px_40px_-12px_rgba(0,0,0,0.1)] backdrop-blur-md dark:bg-default-50/90 dark:shadow-[0_12px_40px_-12px_rgba(0,0,0,0.45)]">
            <CardHeader className="pb-1 pt-7 px-6 flex-col items-center">
              <BrandLogo className="w-10 h-10 mb-3.5" size={40} />
              <h1 className="text-2xl font-bold tracking-tight text-foreground">
                {siteConfig.name}
              </h1>
              <p className="text-sm text-default-500 mt-1.5 text-center">
                {t("登录以管理你的网络与订阅")}
              </p>
            </CardHeader>
            <CardBody className="px-6 py-6">
              <div className="flex flex-col gap-4">
                {(passwordAllowed || showUserPasswordForm) && (
                  <>
                    <Input
                      autoComplete="username"
                      classNames={{
                        inputWrapper:
                          "bg-background border border-input h-10 rounded-lg",
                        input: "text-sm font-medium",
                      }}
                      errorMessage={errors.username}
                      isDisabled={loading}
                      isInvalid={!!errors.username}
                      label={t("用户名")}
                      placeholder={t("请输入用户名")}
                      value={form.username}
                      variant="bordered"
                      onChange={(e) =>
                        handleInputChange("username", e.target.value)
                      }
                      onKeyDown={handleKeyPress}
                    />

                    <Input
                      autoComplete="current-password"
                      classNames={{
                        inputWrapper:
                          "bg-background border border-input h-10 rounded-lg",
                        input: "text-sm font-medium",
                      }}
                      endContent={
                        <button
                          aria-label={
                            showPassword ? t("隐藏密码") : t("显示密码")
                          }
                          className="text-default-400 transition-colors hover:text-foreground"
                          tabIndex={-1}
                          type="button"
                          onClick={() => setShowPassword((v) => !v)}
                        >
                          {showPassword ? (
                            <EyeOff className="h-4 w-4" />
                          ) : (
                            <Eye className="h-4 w-4" />
                          )}
                        </button>
                      }
                      errorMessage={errors.password}
                      isDisabled={loading}
                      isInvalid={!!errors.password}
                      label={t("密码")}
                      placeholder={t("请输入密码")}
                      type={showPassword ? "text" : "password"}
                      value={form.password}
                      variant="bordered"
                      onChange={(e) =>
                        handleInputChange("password", e.target.value)
                      }
                      onKeyDown={handleKeyPress}
                    />

                    <Button
                      className="mt-1 h-10 rounded-lg bg-primary text-primary-foreground font-medium"
                      disabled={loading}
                      isLoading={loading}
                      onPress={handleLogin}
                    >
                      {loading
                        ? showCaptcha
                          ? t("验证中...")
                          : t("登录中...")
                        : t("登录")}
                    </Button>
                  </>
                )}
                {passkeyEnabled && (
                  <Button
                    className="h-10 rounded-lg"
                    disabled={loading}
                    variant="bordered"
                    onPress={() => void handlePasskeyLogin()}
                  >
                    {t("选择通行证密钥登录")}
                  </Button>
                )}
                {githubEnabled && (
                  <Button
                    className={`h-10 rounded-lg ${
                      passwordAllowed === false
                        ? "bg-primary text-primary-foreground font-medium"
                        : ""
                    }`}
                    disabled={loading}
                    variant={passwordAllowed === false ? "solid" : "bordered"}
                    onPress={() => void handleGithubLogin()}
                  >
                    {t("使用 GitHub 登录")}
                  </Button>
                )}
                {passwordAllowed === false &&
                  userPasswordAllowed &&
                  !showUserPasswordForm && (
                    <button
                      className="text-center text-sm text-default-500 transition-colors hover:text-foreground"
                      type="button"
                      onClick={() => setShowUserPasswordForm(true)}
                    >
                      {t("普通用户账号密码登录")}
                    </button>
                  )}
                {statusPageEnabled && (
                  // The status page is a separate app (a theme) the panel
                  // serves at /status/, not a route of this one.
                  <a
                    className="text-center text-sm text-default-500 transition-colors hover:text-foreground"
                    href="/status/"
                  >
                    {t("查看服务器状态 →")}
                  </a>
                )}
              </div>
            </CardBody>
          </Card>
        </div>

        {/* 版权信息 - 文档流底部，避免矮屏遮挡登录卡片 */}
        <VersionFooter
          containerClassName="relative z-10 w-full text-center py-4 safe-bottom"
          poweredClassName="text-xs text-default-400"
          updateBadgeClassName="ml-1.5 text-foreground"
          version={isWebView ? siteConfig.app_version : siteConfig.version}
          versionClassName="text-xs text-default-400 mt-1"
        />

        {/* 验证码弹层 */}
        {showCaptcha && siteKey && (
          <div className="fixed inset-0 z-50 flex items-center justify-center">
            {/* 背景遮罩层 - 模糊效果，暗黑模式下更深 */}
            <button
              className="absolute inset-0 bg-black/60 dark:bg-black/80 backdrop-blur-sm captcha-backdrop-enter"
              type="button"
              onClick={() => {
                setShowCaptcha(false);
                setLoading(false);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  setShowCaptcha(false);
                  setLoading(false);
                }
              }}
            />
            {/* 验证码容器 */}
            <div className="mb-4 relative z-50 rounded-xl border border-divider bg-content1 p-6 shadow-xl">
              <div className="mb-4 text-center text-sm font-medium text-foreground">
                {t("请完成安全验证")}
              </div>
              <div className="flex justify-center">
                <Turnstile
                  options={{
                    theme: (document.documentElement.classList.contains(
                      "dark",
                    ) ||
                    document.documentElement.getAttribute("data-theme") ===
                      "dark" ||
                    window.matchMedia("(prefers-color-scheme: dark)").matches
                      ? "dark"
                      : "light") as "light" | "dark" | "auto",
                  }}
                  siteKey={siteKey}
                  onError={() => {
                    toast.error(t("验证失败，请刷新重试"));
                    setLoading(false);
                  }}
                  onExpire={() => {
                    setForm((prev) => ({ ...prev, captchaId: "" }));
                  }}
                  onSuccess={(token) => {
                    setForm((prev) => ({ ...prev, captchaId: token }));
                    void performLogin(token);
                  }}
                />
              </div>
            </div>
          </div>
        )}
      </section>
    </div>
  );
}
