import type {
  SystemUpgradeCheckApiData,
  SystemUpgradeReleaseApiItem,
  SystemUpgradeVersionApiData,
} from "@/api/types";

import { useState, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import toast from "react-hot-toast";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import { Textarea } from "@/shadcn-bridge/heroui/input";
import { Spinner } from "@/shadcn-bridge/heroui/spinner";
import { Switch } from "@/shadcn-bridge/heroui/switch";
import { Select, SelectItem } from "@/shadcn-bridge/heroui/select";
import { Checkbox } from "@/shadcn-bridge/heroui/checkbox";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import {
  updateConfigs,
  exportBackup,
  importBackup,
  getAnnouncement,
  updateAnnouncement,
  getStorageSummary,
  getSystemUpgradeVersion,
  checkSystemUpgrade,
  runSystemUpgrade,
  type AnnouncementData,
} from "@/api";
import { AnimatedPage } from "@/components/animated-page";
import { PageHeader } from "@/components/page-kit";
import { ThemeSettings } from "@/components/theme-settings";
import { useH5Mode } from "@/hooks/useH5Mode";
import { NotifySettings, notifySummary } from "@/pages/config/notify-card";
import {
  LATENCY_TARGETS_KEY,
  LatencySettings,
  latencySummary,
} from "@/pages/config/latency-card";
import { ConfigSection, Field, SubHeading } from "@/pages/config/section";
import { GistBackup } from "@/pages/config/gist-backup";
import { isAdmin } from "@/utils/auth";
import { getCachedConfigs, configCache, updateSiteConfig } from "@/config/site";
import { githubCallbackUrl } from "@/utils/github-login";
import {
  DEFAULT_TUNNEL_QUALITY_INTERVAL_SEC,
  MAX_TUNNEL_QUALITY_INTERVAL_SEC,
  MIN_TUNNEL_QUALITY_INTERVAL_SEC,
  parseTunnelQualityIntervalSeconds,
  TUNNEL_QUALITY_INTERVAL_CONFIG_KEY,
  validateTunnelQualityInterval,
} from "@/config/tunnel-quality";
import {
  type UpdateReleaseChannel,
  getUpdateReleaseChannel,
} from "@/utils/version-update";
import {
  convertBrandAssetToPngDataURL,
  isPngDataURL,
  type BrandAssetKind,
} from "@/utils/brand-asset";

interface ConfigItem {
  key: string;
  label: string;
  placeholder?: string;
  description?: string;
  type: "input" | "switch" | "select" | "bg_image";
  options?: { label: string; value: string; description?: string }[];
  dependsOn?: string; // 依赖的配置项key
  dependsValue?: string; // 依赖的配置项值
}

type BgImageKey = "app_bg_image" | "app_bg_image_light" | "app_bg_image_dark";

const BRAND_PREVIEW_KEYS = ["app_logo", "app_favicon"] as const;

type BrandPreviewKey = (typeof BRAND_PREVIEW_KEYS)[number];

const isBrandPreviewKey = (key: string): key is BrandPreviewKey =>
  BRAND_PREVIEW_KEYS.includes(key as BrandPreviewKey);

const BRAND_FILE_ACCEPT = "image/png,image/jpeg,image/webp,image/svg+xml";

const toBrandAssetKind = (key: BrandPreviewKey): BrandAssetKind => {
  return key === "app_logo" ? "logo" : "favicon";
};

// 网站配置项定义
const CONFIG_ITEMS: ConfigItem[] = [
  {
    key: "app_bg_image",
    label: "背景壁纸",
    description: "亮色、暗色可各设一张，没设的用默认背景；保存后随外观模式切换",
    type: "bg_image",
  },
  {
    key: "ip",
    label: "面板后端地址",
    placeholder: "请输入面板后端IP:PORT",
    description:
      "服务器连接面板用，格式 ip:port 或 域名:port；可套 CDN / HTTPS，通讯已加密",
    type: "input",
  },
  {
    key: "panel_domain",
    label: "面板域名",
    placeholder: "请输入面板域名",
    description: "与其他面板联邦共享时用来验证身份",
    type: "input",
  },
  {
    key: "app_name",
    label: "应用名称",
    placeholder: "请输入应用名称",
    description: "显示在浏览器标签页和导航栏",
    type: "input",
  },
  {
    key: "app_logo",
    label: "导航栏图标",
    description: "页面左上角的图标，建议方形图片，自动转为 96×96 PNG",
    type: "input",
  },
  {
    key: "app_favicon",
    label: "浏览器标签图标",
    description: "浏览器标签页上的小图标，建议方形图片，自动转为 64×64 PNG",
    type: "input",
  },
  {
    key: "hide_footer_brand",
    label: "隐藏底部 FLYX 版权",
    description: "开启后页面底部不再显示 Powered by FLYX",
    type: "switch",
  },
  {
    key: "forward_compact_mode",
    label: "规则页面精简模式",
    description: "规则列表使用 2.1.6-alpha8 的样式，对所有用户生效",
    type: "switch",
  },
  {
    key: "monitor_tunnel_quality_enabled",
    label: "实时隧道质量检测",
    description: "关闭后前端停止自动刷新，后端停止探测，对所有用户生效",
    type: "switch",
  },
  {
    key: TUNNEL_QUALITY_INTERVAL_CONFIG_KEY,
    label: "隧道质量探测间隔",
    placeholder: String(DEFAULT_TUNNEL_QUALITY_INTERVAL_SEC),
    description: "多久探测一次，1–3600 秒，默认 1 秒",
    type: "input",
    dependsOn: "monitor_tunnel_quality_enabled",
    dependsValue: "true",
  },
  {
    key: "monitor_retention_days",
    label: "监控数据保留天数",
    placeholder: "7",
    description: "服务器指标、隧道流量、服务监控和隧道质量历史，默认 7 天",
    type: "input",
  },
  {
    key: "captcha_enabled",
    label: "启用验证码",
    description: "登录时需要通过 Cloudflare Turnstile 验证",
    type: "switch",
  },
  {
    key: "cloudflare_site_key",
    label: "Turnstile 站点密钥（Site Key）",
    placeholder: "请输入站点密钥",
    description: "在 Cloudflare Turnstile 后台获取",
    type: "input",
    dependsOn: "captcha_enabled",
    dependsValue: "true",
  },
  {
    key: "cloudflare_secret_key",
    label: "Turnstile 私钥（Secret Key）",
    placeholder: "请输入私钥",
    description: "与站点密钥成对使用",
    type: "input",
    dependsOn: "captcha_enabled",
    dependsValue: "true",
  },
  {
    key: "github_login_enabled",
    label: "启用 GitHub 登录",
    description:
      "用户在个人中心绑定 GitHub 账号后，可在登录页用 GitHub 登录；未绑定的账号无法登录",
    type: "switch",
  },
  {
    key: "github_oauth_client_id",
    label: "GitHub OAuth App Client ID",
    placeholder: "Ov23li...",
    description: `在 GitHub → Settings → Developer settings → OAuth Apps 新建，Authorization callback URL 只填这一个：${githubCallbackUrl()}`,
    type: "input",
    dependsOn: "github_login_enabled",
    dependsValue: "true",
  },
  {
    key: "github_oauth_secret",
    label: "GitHub OAuth App Client Secret",
    placeholder: "请输入 Client Secret",
    description: "与 Client ID 成对使用，只有管理员可见",
    type: "input",
    dependsOn: "github_login_enabled",
    dependsValue: "true",
  },
  {
    key: "admin_password_login_disabled",
    label: "禁止管理员使用密码登录",
    description:
      "管理员账号只能用 GitHub 或通行证密钥登录，普通用户照常用密码登录。需所有管理员先绑定 GitHub。被锁在外面时，在服务器执行 docker compose exec backend /app/paneld enable-password-login 恢复",
    type: "switch",
    dependsOn: "github_login_enabled",
    dependsValue: "true",
  },
  {
    key: "github_proxy_enabled",
    label: "开启 GitHub 加速",
    description: "服务器安装、升级和内核下载经代理，适合 GitHub 访问受限的地区",
    type: "switch",
  },
  {
    key: "github_proxy_url",
    label: "加速地址",
    placeholder: "https://gcode.hostcentral.cc",
    description:
      "GitHub 下载加速代理地址。服务器在国内、面板在海外时选「面板中转」：服务器经面板下载，面板会缓存文件",
    type: "input",
    dependsOn: "github_proxy_enabled",
    dependsValue: "true",
  },
  {
    key: "allow_local_remote_addr",
    label: "允许转发到本地地址",
    description:
      "普通用户的规则可指向 127.0.0.1、10.x、172.16–31.x、192.168.x 等内网地址；默认关闭，避免成为开放代理",
    type: "switch",
  },
  {
    key: "fetch_allow_local",
    label: "拉取订阅可访问内网地址",
    description:
      "订阅源、网址模板和图标库可从 127.0.0.1、10.x、192.168.x 等内网地址下载，例如同机或局域网里的 Sub-Store；默认关闭，避免面板被用来读取内网服务",
    type: "switch",
  },
];

const BACKUP_TYPE_OPTIONS = [
  { value: "users", label: "用户" },
  { value: "nodes", label: "服务器" },
  { value: "tunnels", label: "隧道" },
  { value: "forwards", label: "规则" },
  { value: "userTunnels", label: "用户隧道权限" },
  { value: "speedLimits", label: "限速规则" },
  { value: "tunnelGroups", label: "隧道分组" },
  { value: "userGroups", label: "用户分组" },
  { value: "permissions", label: "分组权限" },
  { value: "configs", label: "系统配置" },
  { value: "inbounds", label: "内核与入站" },
] as const;

const BACKUP_TYPE_VALUES = BACKUP_TYPE_OPTIONS.map((option) => option.value);

// 初始化时从缓存读取配置，避免闪烁
const getInitialConfigs = (): Record<string, string> => {
  if (typeof window === "undefined") return {};

  const configKeys = [
    "app_name",
    "captcha_enabled",
    "cloudflare_site_key",
    "cloudflare_secret_key",
    "github_login_enabled",
    "github_oauth_client_id",
    "forward_compact_mode",
    "monitor_tunnel_quality_enabled",
    TUNNEL_QUALITY_INTERVAL_CONFIG_KEY,
    "monitor_retention_days",
    "ip",
    "panel_domain",
    "app_logo",
    "app_favicon",
    "app_bg_image",
    "app_bg_image_light",
    "app_bg_image_dark",
    "github_proxy_enabled",
    "github_proxy_url",
    "allow_local_remote_addr",
    "fetch_allow_local",
    "hide_footer_brand",
  ];
  const initialConfigs: Record<string, string> = {};

  try {
    configKeys.forEach((key) => {
      const cachedValue = localStorage.getItem("vite_config_" + key);

      if (cachedValue) {
        initialConfigs[key] = cachedValue;
      }
    });
  } catch {}

  return initialConfigs;
};

type SectionKey =
  | "basic"
  | "appearance"
  | "security"
  | "network"
  | "latency"
  | "notify"
  | "announcement"
  | "backup"
  | "system";

// 每个分组包含的配置项，用于判断分组是否有未保存的修改
const SECTION_CONFIG_KEYS: Partial<Record<SectionKey, string[]>> = {
  latency: [LATENCY_TARGETS_KEY],
  basic: [
    "app_name",
    "ip",
    "panel_domain",
    "app_logo",
    "app_favicon",
    "hide_footer_brand",
  ],
  appearance: [
    "app_bg_image",
    "app_bg_image_light",
    "app_bg_image_dark",
    "forward_compact_mode",
  ],
  security: [
    "captcha_enabled",
    "cloudflare_site_key",
    "cloudflare_secret_key",
    "github_login_enabled",
    "github_oauth_client_id",
    "github_oauth_secret",
    "admin_password_login_disabled",
    "allow_local_remote_addr",
    "fetch_allow_local",
  ],
  network: [
    "monitor_tunnel_quality_enabled",
    TUNNEL_QUALITY_INTERVAL_CONFIG_KEY,
    "monitor_retention_days",
    "github_proxy_enabled",
    "github_proxy_url",
  ],
};

const describeBackground = (value?: string) => {
  if (!value) return "默认纯色背景";
  if (value === "wallpaper") return "内置壁纸";
  if (value === "theme") return "自适应纯色";
  if (/^(https?:|data:|\/|blob:)/.test(value)) return "自定义图片";

  return `纯色 ${value}`;
};

const DEFAULT_GITHUB_PROXY = "https://gcode.hostcentral.cc";

/**
 * The panel's GitHub mirror as nodes reach it: 面板后端地址 (what nodes connect
 * to) plus /api/v1/open_api/gh. Without a scheme, port 443 or a page opened
 * over HTTPS on the same address means https.
 */
function panelMirrorURL(ip: string): string {
  const raw = ip.trim().replace(/\/+$/, "");

  if (!raw) return "";
  if (/^https?:\/\//i.test(raw)) return `${raw}/api/v1/open_api/gh`;
  const port = raw.match(/:(\d+)$/)?.[1];
  const pageHTTPS = window.location.protocol === "https:";
  const https =
    port === "443" ||
    (port !== "80" && pageHTTPS && raw === window.location.host) ||
    (!port && pageHTTPS);

  return `${https ? "https" : "http"}://${raw}/api/v1/open_api/gh`;
}

export default function ConfigPage() {
  const navigate = useNavigate();
  const isH5 = useH5Mode();
  const [openSections, setOpenSections] = useState<Set<SectionKey>>(
    () => new Set(),
  );

  const toggleSection = (key: SectionKey) => {
    const opening = !openSections.has(key);

    setOpenSections((prev) => {
      if (isH5) return opening ? new Set([key]) : new Set();
      const next = new Set(prev);

      if (opening) next.add(key);
      else next.delete(key);

      return next;
    });

    // 手机上只展开一个分组，上方分组收起后把当前分组滚回视野
    if (opening && isH5) {
      requestAnimationFrame(() =>
        document
          .getElementById(`config-${key}`)
          ?.scrollIntoView({ block: "start", behavior: "smooth" }),
      );
    }
  };
  const initialConfigs = getInitialConfigs();
  const [configs, setConfigs] =
    useState<Record<string, string>>(initialConfigs);
  const [loading, setLoading] = useState(
    Object.keys(initialConfigs).length === 0,
  );
  const [saving, setSaving] = useState(false);
  const [hasChanges, setHasChanges] = useState(false);
  const [originalConfigs, setOriginalConfigs] =
    useState<Record<string, string>>(initialConfigs);

  const [exportTypes, setExportTypes] = useState<string[]>([]);
  const [importTypes, setImportTypes] = useState<string[]>([]);
  const [exporting, setExporting] = useState(false);
  const [importing, setImporting] = useState(false);
  const [exportSelectorOpen, setExportSelectorOpen] = useState(false);
  const [importSelectorOpen, setImportSelectorOpen] = useState(false);
  const [importFileName, setImportFileName] = useState("");
  const backupFileInputRef = useRef<HTMLInputElement>(null);

  const logoFileInputRef = useRef<HTMLInputElement>(null);
  const faviconFileInputRef = useRef<HTMLInputElement>(null);
  const bgImageFileInputRefs = useRef<
    Partial<Record<BgImageKey, HTMLInputElement>>
  >({});
  const [bgImageUploading, setBgImageUploading] = useState<BgImageKey | null>(
    null,
  );

  const [announcement, setAnnouncement] = useState<AnnouncementData>({
    content: "",
    enabled: 0,
  });
  const [announcementLoading, setAnnouncementLoading] = useState(true);
  const [announcementSaving, setAnnouncementSaving] = useState(false);
  const updateChannel: UpdateReleaseChannel = getUpdateReleaseChannel();
  const [systemUpgradeInfo, setSystemUpgradeInfo] =
    useState<SystemUpgradeVersionApiData | null>(null);
  const [systemUpgradeChecking, setSystemUpgradeChecking] = useState(false);
  const [systemUpgradeExecuting, setSystemUpgradeExecuting] = useState(false);
  const [systemUpgradeLoading, setSystemUpgradeLoading] = useState(true);
  const [systemUpgradeModalOpen, setSystemUpgradeModalOpen] = useState(false);
  const [systemUpgradeReleases, setSystemUpgradeReleases] = useState<
    SystemUpgradeReleaseApiItem[]
  >([]);
  const [systemUpgradeCheckedChannel, setSystemUpgradeCheckedChannel] =
    useState<UpdateReleaseChannel | null>(null);
  const [systemUpgradeSelectedVersion, setSystemUpgradeSelectedVersion] =
    useState("");
  const [previewLoadFailed, setPreviewLoadFailed] = useState<
    Partial<Record<BrandPreviewKey, boolean>>
  >({});
  const [brandUploading, setBrandUploading] = useState<
    Partial<Record<BrandPreviewKey, boolean>>
  >({});
  const [storageSummary, setStorageSummary] = useState("加载中...");
  const systemUpgradeReleasesMatchChannel =
    systemUpgradeCheckedChannel === updateChannel;
  const systemUpgradeHasConfirmedUpdate = Boolean(
    systemUpgradeInfo?.hasUpdate &&
      systemUpgradeReleasesMatchChannel &&
      systemUpgradeReleases.length > 0,
  );
  const canTriggerSystemUpgrade = Boolean(
    !systemUpgradeLoading &&
      !systemUpgradeChecking &&
      !systemUpgradeExecuting &&
      systemUpgradeInfo?.capability.capable !== false,
  );
  const canOpenSystemUpgradeModal = Boolean(
    systemUpgradeInfo?.capability.capable &&
      systemUpgradeHasConfirmedUpdate &&
      !systemUpgradeLoading &&
      !systemUpgradeChecking &&
      !systemUpgradeExecuting,
  );

  // 权限检查
  useEffect(() => {
    if (!isAdmin()) {
      toast.error("权限不足，只有管理员可以访问此页面");
      navigate("/dashboard", { replace: true });

      return;
    }
  }, [navigate]);

  // 加载配置数据（优先从缓存）
  const loadConfigs = async (currentConfigs?: Record<string, string>) => {
    const configsToCompare = currentConfigs || configs;
    const hasInitialData = Object.keys(configsToCompare).length > 0;

    // 如果已有缓存数据，不显示loading，静默更新
    if (!hasInitialData) {
      setLoading(true);
    }

    try {
      const configData = await getCachedConfigs();

      // 只有在数据有变化时才更新
      const hasDataChanged =
        JSON.stringify(configData) !== JSON.stringify(configsToCompare);

      if (hasDataChanged) {
        setConfigs(configData);
        setOriginalConfigs({ ...configData });
        setHasChanges(false);
      } else {
      }
    } catch {
      // 只有在没有缓存数据时才显示错误
      if (!hasInitialData) {
        toast.error("加载配置出错，请重试");
      }
    } finally {
      setLoading(false);
    }
  };

  const loadStorageSummary = async () => {
    try {
      const response = await getStorageSummary();

      if (response.code === 0 && response.data?.databaseSizeText) {
        setStorageSummary(response.data.databaseSizeText);

        return;
      }
      setStorageSummary("获取失败");
    } catch {
      setStorageSummary("获取失败");
    }
  };

  const loadSystemUpgradeInfo = async (channel = updateChannel) => {
    setSystemUpgradeLoading(true);
    try {
      const response = await getSystemUpgradeVersion();

      if (response.code === 0 && response.data) {
        setSystemUpgradeInfo({
          ...response.data,
          channel,
          hasUpdate:
            response.data.channel === channel ? response.data.hasUpdate : false,
          latestVersion:
            response.data.channel === channel
              ? response.data.latestVersion
              : "",
        });
      } else {
        setSystemUpgradeInfo(null);
      }
    } catch {
      setSystemUpgradeInfo(null);
    } finally {
      setSystemUpgradeLoading(false);
    }
  };

  useEffect(() => {
    const timer = setTimeout(() => {
      loadConfigs(initialConfigs);
      loadAnnouncement();
      loadStorageSummary();
      void loadSystemUpgradeInfo();
    }, 100);

    return () => clearTimeout(timer);
  }, []);

  const loadAnnouncement = async () => {
    setAnnouncementLoading(true);
    try {
      const res = await getAnnouncement();

      if (res.code === 0 && res.data) {
        setAnnouncement(res.data);
      }
    } catch {
    } finally {
      setAnnouncementLoading(false);
    }
  };

  const saveAnnouncement = async () => {
    setAnnouncementSaving(true);
    try {
      const res = await updateAnnouncement(announcement);

      if (res.code === 0) {
        toast.success("公告保存成功");
      } else {
        toast.error(res.msg || "保存失败");
      }
    } catch {
      toast.error("保存公告失败，请重试");
    } finally {
      setAnnouncementSaving(false);
    }
  };

  const handleCheckSystemUpgrade = async () => {
    const channel = updateChannel;

    setSystemUpgradeChecking(true);
    try {
      const response = await checkSystemUpgrade(channel);

      if (response.code === 0 && response.data) {
        const data = response.data as SystemUpgradeCheckApiData;

        setSystemUpgradeInfo(data);
        setSystemUpgradeReleases(data.releases || []);
        setSystemUpgradeCheckedChannel(channel);
        setSystemUpgradeSelectedVersion("");
        if (data.latestVersion && !data.hasUpdate) {
          toast.success("当前已是最新版本");

          return false;
        }
        toast.success(
          data.latestVersion
            ? `已检查到最新版本 ${data.latestVersion}`
            : "未获取到可用版本",
        );

        return Boolean(
          data.capability.capable && data.hasUpdate && data.releases?.length,
        );
      } else {
        setSystemUpgradeReleases([]);
        setSystemUpgradeCheckedChannel(null);
        toast.error(response.msg || "检查更新失败");
      }
    } catch {
      setSystemUpgradeReleases([]);
      setSystemUpgradeCheckedChannel(null);
      toast.error("检查更新失败，请重试");
    } finally {
      setSystemUpgradeChecking(false);
    }

    return false;
  };

  const handleOpenSystemUpgradeModal = async () => {
    if (!canOpenSystemUpgradeModal) {
      const checked = await handleCheckSystemUpgrade();

      if (!checked) {
        return;
      }
    }
    setSystemUpgradeModalOpen(true);
  };

  const handleConfirmSystemUpgrade = async () => {
    setSystemUpgradeExecuting(true);
    try {
      const response = await runSystemUpgrade(
        systemUpgradeSelectedVersion || undefined,
        updateChannel,
      );

      if (response.code === 0 && response.data) {
        setSystemUpgradeModalOpen(false);
        setSystemUpgradeSelectedVersion("");
        toast.success("升级已开始，约 1 分钟后刷新页面", { duration: 8000 });
      } else {
        toast.error(response.msg || "面板升级失败");
      }
    } catch {
      toast.error("面板升级失败，请重试");
    } finally {
      setSystemUpgradeExecuting(false);
    }
  };

  const handleConfigChange = (key: string, value: string) => {
    const newConfigs = { ...configs, [key]: value };

    setConfigs(newConfigs);

    if (isBrandPreviewKey(key)) {
      setPreviewLoadFailed((prev) => ({ ...prev, [key]: false }));
    }

    const hasChangesNow =
      Object.keys(newConfigs).some(
        (k) => newConfigs[k] !== originalConfigs[k],
      ) ||
      Object.keys(originalConfigs).some(
        (k) => originalConfigs[k] !== newConfigs[k],
      );

    setHasChanges(hasChangesNow);
  };

  // 保存配置
  const handleSave = async () => {
    const intervalValue = configs[TUNNEL_QUALITY_INTERVAL_CONFIG_KEY];
    const intervalChanged =
      intervalValue !== originalConfigs[TUNNEL_QUALITY_INTERVAL_CONFIG_KEY];
    const intervalError = intervalChanged
      ? validateTunnelQualityInterval(intervalValue || "")
      : null;

    if (intervalError) {
      toast.error(intervalError);
      setOpenSections((prev) =>
        isH5 ? new Set(["network"]) : new Set([...prev, "network"]),
      );

      return;
    }

    setSaving(true);
    try {
      const changedKeys = Object.keys(configs).filter(
        (key) => configs[key] !== originalConfigs[key],
      );

      if (changedKeys.length === 0) {
        setHasChanges(false);

        return;
      }

      const changedPayload: Record<string, string> = {};

      changedKeys.forEach((key) => {
        changedPayload[key] = configs[key] || "";
      });

      const response = await updateConfigs(changedPayload);

      if (response.code === 0) {
        toast.success("配置保存成功");

        Object.entries(configs).forEach(([key, value]) => {
          configCache.set(key, value);
        });

        setOriginalConfigs({ ...configs });
        setHasChanges(false);

        if (
          changedKeys.some((key) =>
            [
              "app_name",
              "app_logo",
              "app_favicon",
              "app_bg_image",
              "app_bg_image_light",
              "app_bg_image_dark",
            ].includes(key),
          )
        ) {
          await updateSiteConfig(configs);
        }

        // 触发配置更新事件，通知其他组件
        window.dispatchEvent(
          new CustomEvent("configUpdated", {
            detail: { changedKeys },
          }),
        );

        // 如果隧道质量检测配置变更，通知 tunnel-monitor-view
        if (
          changedKeys.some((key) =>
            [
              "monitor_tunnel_quality_enabled",
              TUNNEL_QUALITY_INTERVAL_CONFIG_KEY,
            ].includes(key),
          )
        ) {
          window.dispatchEvent(
            new CustomEvent("monitorTunnelQualityEnabledChanged", {
              detail: {
                enabled: configs["monitor_tunnel_quality_enabled"] === "true",
                intervalSec: parseTunnelQualityIntervalSeconds(
                  configs[TUNNEL_QUALITY_INTERVAL_CONFIG_KEY],
                ),
              },
            }),
          );
        }
      } else {
        toast.error("保存配置失败: " + response.msg);
      }
    } catch {
      toast.error("保存配置出错，请重试");
    } finally {
      setSaving(false);
    }
  };

  // 检查配置项是否应该显示（依赖检查）
  const shouldShowItem = (item: ConfigItem): boolean => {
    if (!item.dependsOn || !item.dependsValue) {
      return true;
    }

    return configs[item.dependsOn] === item.dependsValue;
  };

  const getBrandInputRef = (key: BrandPreviewKey) => {
    return key === "app_logo" ? logoFileInputRef : faviconFileInputRef;
  };

  const triggerBrandFilePicker = (key: BrandPreviewKey) => {
    if (brandUploading[key]) {
      return;
    }

    getBrandInputRef(key).current?.click();
  };

  const clearBrandAsset = (key: BrandPreviewKey) => {
    handleConfigChange(key, "");
    setPreviewLoadFailed((prev) => ({ ...prev, [key]: false }));
  };

  const handleBrandFileChange = async (
    key: BrandPreviewKey,
    event: React.ChangeEvent<HTMLInputElement>,
  ) => {
    const file = event.target.files?.[0];

    if (!file) {
      return;
    }

    setBrandUploading((prev) => ({ ...prev, [key]: true }));

    try {
      const pngDataURL = await convertBrandAssetToPngDataURL(
        file,
        toBrandAssetKind(key),
      );

      handleConfigChange(key, pngDataURL);
      toast.success(
        key === "app_logo" ? "导航栏图标上传成功" : "浏览器标签图标上传成功",
      );
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "图片处理失败，请重试";

      toast.error(message);
    } finally {
      setBrandUploading((prev) => ({ ...prev, [key]: false }));
      event.target.value = "";
    }
  };

  const handleBgImageUpload = async (
    e: React.ChangeEvent<HTMLInputElement>,
    key: BgImageKey,
  ) => {
    const file = e.target.files?.[0];

    if (!file) return;

    if (!file.type.startsWith("image/")) {
      toast.error("只能上传图片文件");

      return;
    }

    setBgImageUploading(key);
    try {
      const compressedImage = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();

        reader.onload = (event) => {
          const img = new Image();

          img.onload = () => {
            const canvas = document.createElement("canvas");
            let width = img.width;
            let height = img.height;
            const MAX_WIDTH = 1920;
            const MAX_HEIGHT = 1080;

            if (width > height) {
              if (width > MAX_WIDTH) {
                height = Math.round((height * MAX_WIDTH) / width);
                width = MAX_WIDTH;
              }
            } else {
              if (height > MAX_HEIGHT) {
                width = Math.round((width * MAX_HEIGHT) / height);
                height = MAX_HEIGHT;
              }
            }

            canvas.width = width;
            canvas.height = height;
            const ctx = canvas.getContext("2d");

            if (!ctx) {
              reject(new Error("Canvas context is null"));

              return;
            }
            ctx.drawImage(img, 0, 0, width, height);

            // Output as webp for better compression
            const dataUrl = canvas.toDataURL("image/webp", 0.8);

            resolve(dataUrl);
          };
          img.onerror = () => reject(new Error("图片加载失败"));
          img.src = event.target?.result as string;
        };
        reader.onerror = () => reject(new Error("文件读取失败"));
        reader.readAsDataURL(file);
      });

      handleConfigChange(key, compressedImage);
      toast.success("壁纸上传成功，保存配置后生效");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "图片处理失败");
    } finally {
      setBgImageUploading(null);
      e.target.value = "";
    }
  };

  const renderBgImageUploader = (key: BgImageKey, label: string) => {
    const bgImage = configs[key] || "";
    const isDefault = key === "app_bg_image";
    const isImage =
      bgImage.startsWith("http") ||
      bgImage.startsWith("data:") ||
      bgImage.startsWith("/") ||
      bgImage.startsWith("blob:");
    const isTheme = bgImage === "theme";
    const isWallpaper = bgImage === "wallpaper";
    const isSolidColor = bgImage && !isImage && !isTheme && !isWallpaper;
    const isChanged = configs[key] !== originalConfigs[key];

    return (
      <div
        className={`flex flex-col gap-2 rounded-lg border p-3 ${
          isChanged ? "border-warning-300" : "border-divider"
        }`}
      >
        <div className="flex items-baseline justify-between gap-2">
          <p className="text-sm font-medium">{label}</p>
          <p className="truncate text-xs text-default-500">
            {!isDefault && !bgImage
              ? "未单独设置，使用默认背景"
              : describeBackground(bgImage)}
          </p>
        </div>

        {bgImage && isImage && (
          <img
            alt={`${label}预览`}
            className="h-20 w-full rounded-md border border-divider object-cover"
            src={bgImage}
          />
        )}

        {bgImage && isSolidColor && (
          <div
            className="h-10 rounded-md border border-divider"
            style={{ backgroundColor: bgImage }}
          />
        )}

        <div className="flex flex-wrap items-center gap-2">
          <input
            ref={(node) => {
              if (node) bgImageFileInputRefs.current[key] = node;
            }}
            accept="image/*"
            className="hidden"
            type="file"
            onChange={(event) => void handleBgImageUpload(event, key)}
          />
          <Button
            isDisabled={bgImageUploading !== null && bgImageUploading !== key}
            isLoading={bgImageUploading === key}
            size="sm"
            variant="flat"
            onPress={() => bgImageFileInputRefs.current[key]?.click()}
          >
            上传图片
          </Button>
          {isDefault && (
            <Button
              isDisabled={bgImageUploading !== null || isWallpaper}
              size="sm"
              variant="flat"
              onPress={() => handleConfigChange(key, "wallpaper")}
            >
              内置壁纸
            </Button>
          )}
          {isDefault && (
            <Button
              isDisabled={bgImageUploading !== null || bgImage === "#ffffff"}
              size="sm"
              variant="flat"
              onPress={() => handleConfigChange(key, "#ffffff")}
            >
              白色纯色
            </Button>
          )}
          {bgImage && (
            <Button
              color="danger"
              isDisabled={bgImageUploading !== null}
              size="sm"
              variant="flat"
              onPress={() => handleConfigChange(key, "")}
            >
              {isDefault ? "恢复默认纯色" : "清除"}
            </Button>
          )}
        </div>
      </div>
    );
  };

  const renderBrandPreview = (key: BrandPreviewKey) => {
    const previewUrl = (configs[key] || "").trim();
    const appNamePreview = (configs.app_name || "").trim() || "应用名称";
    const failed = previewLoadFailed[key] === true;
    const showImage = previewUrl.length > 0 && !failed;

    return (
      <div className="flex min-w-0 flex-1 items-center rounded-md border border-divider bg-background px-2 py-1.5">
        {key === "app_logo" ? (
          <div className="flex min-w-0 items-center gap-2">
            {showImage ? (
              <img
                alt="导航栏图标预览"
                className="h-6 w-6 shrink-0 rounded-sm border border-divider object-cover"
                src={previewUrl}
                onError={() =>
                  setPreviewLoadFailed((prev) => ({ ...prev, [key]: true }))
                }
                onLoad={() =>
                  setPreviewLoadFailed((prev) => ({ ...prev, [key]: false }))
                }
              />
            ) : (
              <div className="h-6 w-6 shrink-0 rounded-sm bg-default-200" />
            )}
            <span className="truncate text-sm font-semibold">
              {appNamePreview}
            </span>
          </div>
        ) : (
          <div className="flex min-w-0 items-center gap-2 rounded bg-default-100 px-2 py-0.5">
            {showImage ? (
              <img
                alt="浏览器标签图标预览"
                className="h-4 w-4 shrink-0 rounded-sm object-contain"
                src={previewUrl}
                onError={() =>
                  setPreviewLoadFailed((prev) => ({ ...prev, [key]: true }))
                }
                onLoad={() =>
                  setPreviewLoadFailed((prev) => ({ ...prev, [key]: false }))
                }
              />
            ) : (
              <div className="h-4 w-4 shrink-0 rounded-sm bg-default-300" />
            )}
            <span className="truncate text-xs text-default-600">
              {appNamePreview}
            </span>
          </div>
        )}
      </div>
    );
  };

  const renderBrandAssetUploader = (
    key: BrandPreviewKey,
    isChanged: boolean,
  ) => {
    const value = (configs[key] || "").trim();
    const uploading = brandUploading[key] === true;
    const failed = previewLoadFailed[key] === true;

    return (
      <div
        className={`space-y-1.5 rounded-lg border p-2 ${
          isChanged ? "border-warning-300" : "border-divider"
        }`}
      >
        <input
          ref={getBrandInputRef(key)}
          accept={BRAND_FILE_ACCEPT}
          className="hidden"
          disabled={uploading}
          type="file"
          onChange={(event) => {
            void handleBrandFileChange(key, event);
          }}
        />

        <div className="flex items-center gap-2">
          {renderBrandPreview(key)}
          <Button
            className="shrink-0"
            isLoading={uploading}
            size="sm"
            variant="flat"
            onPress={() => triggerBrandFilePicker(key)}
          >
            {value.length > 0 ? "替换" : "上传"}
          </Button>
          <Button
            className="shrink-0"
            color="danger"
            isDisabled={value.length === 0 || uploading}
            size="sm"
            variant="flat"
            onPress={() => clearBrandAsset(key)}
          >
            清除
          </Button>
        </div>

        {value.length > 0 && failed ? (
          <p className="text-xs text-danger">图片加载失败，请重新上传</p>
        ) : null}

        {value.length > 0 && !isPngDataURL(value) ? (
          <p className="text-xs text-warning-600 dark:text-warning-400">
            当前是旧版 URL 配置，建议重新上传图片以启用无闪烁加载
          </p>
        ) : null}
      </div>
    );
  };

  // 渲染不同类型的配置项
  const renderConfigItem = (item: ConfigItem) => {
    const isChanged =
      hasChanges && configs[item.key] !== originalConfigs[item.key];

    switch (item.type) {
      case "bg_image":
        return (
          <div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
            <div className="lg:col-span-2">
              {renderBgImageUploader("app_bg_image", "默认背景")}
            </div>
            {renderBgImageUploader("app_bg_image_light", "亮色模式壁纸")}
            {renderBgImageUploader("app_bg_image_dark", "暗色模式壁纸")}
          </div>
        );

      case "input": {
        if (isBrandPreviewKey(item.key)) {
          return renderBrandAssetUploader(item.key, isChanged);
        }

        const isTunnelQualityInterval =
          item.key === TUNNEL_QUALITY_INTERVAL_CONFIG_KEY;
        const intervalValue = isTunnelQualityInterval
          ? (configs[item.key] ?? String(DEFAULT_TUNNEL_QUALITY_INTERVAL_SEC))
          : (configs[item.key] ?? "");
        const intervalError =
          isTunnelQualityInterval && configs[item.key] !== undefined
            ? validateTunnelQualityInterval(intervalValue)
            : null;

        return (
          <Input
            aria-label={item.label}
            classNames={{
              input: "text-sm",
              inputWrapper: isChanged
                ? "border-warning-300 data-[hover=true]:border-warning-400"
                : "",
            }}
            endContent={isTunnelQualityInterval ? "秒" : undefined}
            errorMessage={intervalError || undefined}
            isInvalid={Boolean(intervalError)}
            max={
              isTunnelQualityInterval
                ? MAX_TUNNEL_QUALITY_INTERVAL_SEC
                : undefined
            }
            min={
              isTunnelQualityInterval
                ? MIN_TUNNEL_QUALITY_INTERVAL_SEC
                : undefined
            }
            placeholder={item.placeholder}
            size="sm"
            step={isTunnelQualityInterval ? 1 : undefined}
            type={isTunnelQualityInterval ? "number" : "text"}
            value={intervalValue}
            variant="bordered"
            onChange={(e) => handleConfigChange(item.key, e.target.value)}
          />
        );
      }

      case "switch":
        return (
          <Switch
            aria-label={item.label}
            className={isChanged ? "ring-2 ring-warning-300" : undefined}
            isSelected={configs[item.key] === "true"}
            size="sm"
            onValueChange={(checked) =>
              handleConfigChange(item.key, checked ? "true" : "false")
            }
          />
        );

      case "select":
        return (
          <Select
            aria-label={item.label}
            classNames={{
              trigger: isChanged
                ? "border-warning-300 data-[hover=true]:border-warning-400"
                : "",
            }}
            placeholder="请选择验证码类型"
            selectedKeys={configs[item.key] ? [configs[item.key]] : []}
            size="sm"
            variant="bordered"
            onSelectionChange={(keys) => {
              const selectedKey = Array.from(keys)[0] as string;

              if (selectedKey) {
                handleConfigChange(item.key, selectedKey);
              }
            }}
          >
            {item.options?.map((option) => (
              <SelectItem key={option.value} description={option.description}>
                {option.label}
              </SelectItem>
            )) || []}
          </Select>
        );

      default:
        return null;
    }
  };

  // 一个配置项：标题、控件、简短说明；开关放在右侧
  const renderItem = (key: string) => {
    const item = CONFIG_ITEMS.find((i) => i.key === key);

    if (!item || !shouldShowItem(item)) return null;

    return (
      <Field
        key={item.key}
        help={item.description}
        inline={item.type === "switch"}
        label={item.label}
      >
        {renderConfigItem(item)}
        {item.key === "github_proxy_url" && renderGithubProxyPresets()}
      </Field>
    );
  };

  // Quick picks for 加速地址: the panel's own mirror (built from 面板后端地址)
  // or the public proxy.
  const renderGithubProxyPresets = () => {
    const mirror = panelMirrorURL(configs.ip || "");
    const current = (configs.github_proxy_url || "").replace(/\/+$/, "");

    return (
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <Button
          color={current === mirror && mirror ? "primary" : "default"}
          size="sm"
          variant={current === mirror && mirror ? "solid" : "bordered"}
          onPress={() => {
            if (!mirror) {
              toast.error("请先填写面板后端地址");

              return;
            }
            handleConfigChange("github_proxy_url", mirror);
          }}
        >
          面板中转
        </Button>
        <Button
          size="sm"
          variant="bordered"
          onPress={() =>
            handleConfigChange("github_proxy_url", DEFAULT_GITHUB_PROXY)
          }
        >
          公共加速站
        </Button>
        {mirror && (
          <span className="break-all text-xs text-default-500">
            面板中转地址：{mirror}
          </span>
        )}
      </div>
    );
  };

  const handleExport = async () => {
    if (exportTypes.length === 0) {
      toast.error("请至少选择一种数据类型");

      return;
    }
    setExporting(true);
    try {
      await exportBackup(exportTypes);
      toast.success("导出成功");
      setExportSelectorOpen(false);
    } catch {
      toast.error("导出失败，请重试");
    } finally {
      setExporting(false);
    }
  };

  const triggerImportFilePicker = () => {
    if (importTypes.length === 0) {
      toast.error("请先选择要导入的数据类型");

      return;
    }

    setImportSelectorOpen(false);
    requestAnimationFrame(() => backupFileInputRef.current?.click());
  };

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];

    if (!file) return;

    if (importTypes.length === 0) {
      toast.error("请先选择要导入的数据类型");

      return;
    }

    setImportFileName(file.name);
    setImporting(true);

    try {
      const text = await file.text();
      const data = JSON.parse(text);

      const response = await importBackup({
        types: importTypes,
        ...data,
      });

      if (response.code === 0) {
        toast.success(`导入成功: ${JSON.stringify(response.data)}`);
        setImportTypes([]);
        setImportFileName("");
      } else {
        toast.error("导入失败: " + response.msg);
      }
    } catch {
      toast.error("导入失败，请检查文件格式");
    } finally {
      setImporting(false);
      if (backupFileInputRef.current) {
        backupFileInputRef.current.value = "";
      }
    }
  };

  const toggleTypeSelection = (
    type: string,
    setTypes: React.Dispatch<React.SetStateAction<string[]>>,
  ) => {
    setTypes((prev) =>
      prev.includes(type)
        ? prev.filter((item) => item !== type)
        : [...prev, type],
    );
  };

  const isAllTypesSelected = (types: string[]) =>
    BACKUP_TYPE_VALUES.every((type) => types.includes(type));

  const renderTypeSelection = (
    label: string,
    selectedTypes: string[],
    setTypes: React.Dispatch<React.SetStateAction<string[]>>,
  ) => {
    const allSelected = isAllTypesSelected(selectedTypes);

    return (
      <div className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-sm font-medium text-default-700 dark:text-default-300">
            {label}
          </span>
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="flat"
              onPress={() =>
                setTypes(allSelected ? [] : [...BACKUP_TYPE_VALUES])
              }
            >
              {allSelected ? "取消全选" : "全选"}
            </Button>
            <Button size="sm" variant="light" onPress={() => setTypes([])}>
              清空
            </Button>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          {BACKUP_TYPE_OPTIONS.map((option) => {
            const isSelected = selectedTypes.includes(option.value);

            return (
              <button
                key={option.value}
                aria-pressed={isSelected}
                className={`w-full px-3 py-2 rounded-lg border transition-all duration-200 cursor-pointer text-left ${
                  isSelected
                    ? "bg-primary-50 dark:bg-primary-900/20 border-primary-300 dark:border-primary-500/50 shadow-sm"
                    : "bg-white dark:bg-default-50 border-default-200 dark:border-default-100/30 hover:border-primary-200 dark:hover:border-primary-500/30 hover:shadow-sm"
                }`}
                type="button"
                onClick={() => toggleTypeSelection(option.value, setTypes)}
              >
                <div className="flex items-center gap-3">
                  <Checkbox
                    classNames={{
                      base: "pointer-events-none",
                    }}
                    color="primary"
                    isSelected={isSelected}
                    size="md"
                  />
                  <span
                    className={`font-medium ${
                      isSelected
                        ? "text-default-900 dark:text-default-100"
                        : "text-default-700 dark:text-default-500"
                    }`}
                  >
                    {option.label}
                  </span>
                </div>
              </button>
            );
          })}
        </div>
      </div>
    );
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <Spinner label="加载配置中..." size="lg" />
      </div>
    );
  }

  const sectionChanged = (key: SectionKey) =>
    (key === "notify"
      ? Object.keys({ ...configs, ...originalConfigs }).filter((k) =>
          k.startsWith("notify_"),
        )
      : (SECTION_CONFIG_KEYS[key] ?? [])
    ).some((k) => configs[k] !== originalConfigs[k]);

  const sectionProps = (key: SectionKey) => ({
    id: key,
    open: openSections.has(key),
    changed: sectionChanged(key),
    onToggle: () => toggleSection(key),
  });

  const onOff = (key: string, on = "开", off = "关") =>
    configs[key] === "true" ? on : off;

  const tunnelQualitySummary =
    configs.monitor_tunnel_quality_enabled === "true"
      ? `隧道质量每 ${
          configs[TUNNEL_QUALITY_INTERVAL_CONFIG_KEY] ||
          DEFAULT_TUNNEL_QUALITY_INTERVAL_SEC
        } 秒检测`
      : "隧道质量检测关";

  const systemSummary = [
    systemUpgradeInfo?.currentVersion
      ? `当前 ${systemUpgradeInfo.currentVersion}`
      : systemUpgradeLoading
        ? "加载版本中"
        : "未获取到版本",
    systemUpgradeInfo?.hasUpdate && systemUpgradeInfo.latestVersion
      ? `可升级到 ${systemUpgradeInfo.latestVersion}`
      : "",
    `数据库 ${storageSummary}`,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <AnimatedPage className="mx-auto max-w-3xl px-3 py-6 lg:px-6">
      <PageHeader
        description="面板名称、登录安全、通知、备份与升级"
        title="网站配置"
      />

      <div className="space-y-2">
        <ConfigSection
          {...sectionProps("basic")}
          summary={`${configs.app_name || "未设置名称"} · ${
            configs.ip || "未填后端地址"
          }`}
          title="基本"
        >
          {renderItem("app_name")}
          {renderItem("ip")}
          {renderItem("panel_domain")}
          {renderItem("app_logo")}
          {renderItem("app_favicon")}
          {renderItem("hide_footer_brand")}
        </ConfigSection>

        <ConfigSection
          {...sectionProps("appearance")}
          summary={`${describeBackground(configs.app_bg_image)} · 规则精简模式${onOff(
            "forward_compact_mode",
          )}`}
          title="外观"
        >
          {renderItem("app_bg_image")}
          {renderItem("forward_compact_mode")}
          <SubHeading>主题（点选即生效，不需要保存）</SubHeading>
          <div className="[&_[data-slot=card-content]]:p-0! [&_h2]:hidden [&>[data-slot=card]]:rounded-none! [&>[data-slot=card]]:bg-none! [&>[data-slot=card]]:shadow-none! [&>[data-slot=card]]:backdrop-blur-none!">
            <ThemeSettings />
          </div>
        </ConfigSection>

        <ConfigSection
          {...sectionProps("security")}
          summary={`登录验证码${onOff("captcha_enabled")} · GitHub 登录${onOff(
            "github_login_enabled",
          )} · 转发到内网地址${onOff(
            "allow_local_remote_addr",
            "允许",
            "禁止",
          )} · 拉取内网订阅${onOff("fetch_allow_local", "允许", "禁止")}`}
          title="安全与登录"
        >
          {renderItem("captcha_enabled")}
          {renderItem("cloudflare_site_key")}
          {renderItem("cloudflare_secret_key")}
          {renderItem("github_login_enabled")}
          {renderItem("github_oauth_client_id")}
          {renderItem("github_oauth_secret")}
          {renderItem("admin_password_login_disabled")}
          {renderItem("allow_local_remote_addr")}
          {renderItem("fetch_allow_local")}
        </ConfigSection>

        <ConfigSection
          {...sectionProps("network")}
          summary={`${tunnelQualitySummary} · 监控保留 ${
            configs.monitor_retention_days || 7
          } 天 · GitHub 加速${onOff("github_proxy_enabled")}`}
          title="服务器与监控"
        >
          {renderItem("monitor_tunnel_quality_enabled")}
          {renderItem(TUNNEL_QUALITY_INTERVAL_CONFIG_KEY)}
          {renderItem("monitor_retention_days")}
          {renderItem("github_proxy_enabled")}
          {renderItem("github_proxy_url")}
        </ConfigSection>

        <ConfigSection
          {...sectionProps("latency")}
          summary={latencySummary(configs)}
          title="延迟测试点"
        >
          <LatencySettings configs={configs} onChange={handleConfigChange} />
        </ConfigSection>

        <ConfigSection
          {...sectionProps("notify")}
          summary={notifySummary(configs)}
          title="通知"
        >
          <NotifySettings
            configs={configs}
            originalConfigs={originalConfigs}
            onChange={handleConfigChange}
          />
        </ConfigSection>

        <ConfigSection
          {...sectionProps("announcement")}
          summary={
            announcementLoading
              ? "加载中..."
              : announcement.enabled === 1
                ? "首页显示中（单独保存）"
                : "未启用（单独保存）"
          }
          title="公告"
        >
          {announcementLoading ? (
            <div className="flex justify-center py-4">
              <Spinner size="sm" />
            </div>
          ) : (
            <>
              <Field inline help="启用后在首页顶部显示" label="显示公告">
                <Switch
                  aria-label="显示公告"
                  isSelected={announcement.enabled === 1}
                  size="sm"
                  onValueChange={(checked) =>
                    setAnnouncement({
                      ...announcement,
                      enabled: checked ? 1 : 0,
                    })
                  }
                />
              </Field>

              <Field help="支持 Markdown，链接在新标签页打开" label="公告内容">
                <Textarea
                  aria-label="公告内容"
                  minRows={4}
                  placeholder="例如：**加粗**、[链接](https://example.com)、- 列表"
                  value={announcement.content}
                  variant="bordered"
                  onChange={(e) =>
                    setAnnouncement({
                      ...announcement,
                      content: e.target.value,
                    })
                  }
                />
              </Field>

              <div className="flex items-center justify-between gap-3 border-t border-divider pt-3">
                <span className="text-xs text-default-500">
                  公告单独保存，不随下方“保存”提交
                </span>
                <Button
                  className="shrink-0"
                  color="primary"
                  isLoading={announcementSaving}
                  size="sm"
                  onPress={saveAnnouncement}
                >
                  保存公告
                </Button>
              </div>
            </>
          )}
        </ConfigSection>

        <ConfigSection
          {...sectionProps("backup")}
          summary={
            importing
              ? "正在导入..."
              : exporting
                ? "正在导出..."
                : "导出或导入 JSON 备份"
          }
          title="备份与恢复"
        >
          <Field
            inline
            help={`选择数据类型，导出为 JSON 文件；已选 ${exportTypes.length} / ${BACKUP_TYPE_VALUES.length}`}
            label="导出数据"
          >
            <Button
              isLoading={exporting}
              size="sm"
              variant="flat"
              onPress={() => setExportSelectorOpen(true)}
            >
              {exporting ? "导出中..." : "选择并导出"}
            </Button>
          </Field>

          <input
            ref={backupFileInputRef}
            accept=".json"
            className="hidden"
            type="file"
            onChange={handleFileChange}
          />
          <Field
            inline
            help={
              importFileName
                ? `已选择：${importFileName}`
                : `从备份文件恢复所选类型；已选 ${importTypes.length} / ${BACKUP_TYPE_VALUES.length}`
            }
            label="导入数据"
          >
            <Button
              isLoading={importing}
              size="sm"
              variant="flat"
              onPress={() => setImportSelectorOpen(true)}
            >
              {importing ? "导入中..." : "选择并导入"}
            </Button>
          </Field>

          <GistBackup />
        </ConfigSection>

        <ConfigSection
          {...sectionProps("system")}
          summary={systemSummary}
          title="系统与升级"
        >
          <Field inline help="后端数据库占用空间，仅供参考" label="数据库占用">
            <span className="text-sm font-medium">{storageSummary}</span>
          </Field>

          <SubHeading>面板自升级</SubHeading>
          {systemUpgradeLoading ? (
            <div className="flex items-center gap-2 text-sm text-default-500">
              <Spinner size="sm" />
              正在加载升级状态...
            </div>
          ) : (
            <>
              <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-sm">
                <dt className="text-default-500">当前版本</dt>
                <dd>
                  {systemUpgradeInfo?.currentVersion || "未获取到版本信息"}
                </dd>
                <dt className="text-default-500">最新版本</dt>
                <dd>
                  {systemUpgradeInfo?.latestVersion || "未获取到可用版本"}
                  {!systemUpgradeInfo?.latestVersion &&
                    systemUpgradeInfo?.reason && (
                      <p className="break-all text-xs text-default-500">
                        {systemUpgradeInfo.reason}
                      </p>
                    )}
                </dd>
                <dt className="text-default-500">升级能力</dt>
                <dd>
                  {systemUpgradeInfo?.capability.capable
                    ? "可升级"
                    : "当前不可升级"}
                </dd>
                <dt className="text-default-500">部署目录</dt>
                <dd className="break-all">
                  {systemUpgradeInfo?.capability.deployDir ||
                    "未获取到部署目录"}
                </dd>
                <dt className="text-default-500">后端容器</dt>
                <dd className="break-all">
                  {systemUpgradeInfo?.capability.backendContainer ||
                    "未获取到容器信息"}
                </dd>
              </dl>

              {!systemUpgradeInfo?.capability.capable && (
                <div className="rounded-lg border border-warning-200 bg-warning-50 px-3 py-2 text-warning-800 dark:border-warning-900/40 dark:bg-warning-950/30 dark:text-warning-200">
                  <p className="text-xs font-medium">当前无法升级</p>
                  <ul className="mt-1 list-disc space-y-1 pl-4 text-xs">
                    {(systemUpgradeInfo?.capability.reasons?.length
                      ? systemUpgradeInfo.capability.reasons
                      : ["暂未获取到不可升级原因"]
                    ).map((reason) => (
                      <li key={reason}>{reason}</li>
                    ))}
                  </ul>
                </div>
              )}

              <Field help="留空则升级到当前通道的最新版本" label="目标版本">
                <Select
                  aria-label="目标版本"
                  isDisabled={
                    !systemUpgradeReleasesMatchChannel ||
                    systemUpgradeReleases.length === 0 ||
                    systemUpgradeExecuting
                  }
                  placeholder={
                    systemUpgradeReleasesMatchChannel &&
                    systemUpgradeReleases.length > 0
                      ? "留空时自动选择最新版本"
                      : "请先检查当前通道更新"
                  }
                  selectedKeys={
                    systemUpgradeSelectedVersion
                      ? [systemUpgradeSelectedVersion]
                      : []
                  }
                  size="sm"
                  variant="bordered"
                  onSelectionChange={(keys) => {
                    const selected = Array.from(keys)[0] as string | undefined;

                    setSystemUpgradeSelectedVersion(selected || "");
                  }}
                >
                  {(systemUpgradeReleasesMatchChannel
                    ? systemUpgradeReleases
                    : []
                  ).map((release) => (
                    <SelectItem
                      key={release.version}
                      description={release.publishedAt || "暂无发布时间"}
                    >
                      {release.name || release.version}
                    </SelectItem>
                  ))}
                </Select>
              </Field>
            </>
          )}

          <div className="flex justify-end gap-2 border-t border-divider pt-3">
            <Button
              isLoading={systemUpgradeChecking}
              size="sm"
              variant="flat"
              onPress={handleCheckSystemUpgrade}
            >
              检查更新
            </Button>
            <Button
              color="primary"
              isDisabled={!canTriggerSystemUpgrade}
              isLoading={systemUpgradeExecuting}
              size="sm"
              onPress={handleOpenSystemUpgradeModal}
            >
              立即升级
            </Button>
          </div>
        </ConfigSection>
      </div>

      {/* 配置统一保存（公告除外），有修改时出现 */}
      {hasChanges && (
        <div
          className={`sticky z-20 mt-4 flex items-center gap-3 rounded-xl border border-warning-300 bg-warning-50/95 px-3 py-2 shadow-md backdrop-blur dark:bg-warning-900/60 ${
            isH5 ? "bottom-[calc(4.5rem+var(--safe-area-bottom))]" : "bottom-4"
          }`}
        >
          <span className="min-w-0 flex-1 truncate text-xs text-warning-700 dark:text-warning-300">
            有修改尚未保存
          </span>
          <Button
            className="shrink-0"
            color="primary"
            isLoading={saving}
            size="sm"
            onPress={handleSave}
          >
            {saving ? "保存中..." : "保存"}
          </Button>
        </div>
      )}

      <Modal
        backdrop="blur"
        classNames={{
          base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
        }}
        isOpen={exportSelectorOpen}
        onOpenChange={setExportSelectorOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>选择导出内容</ModalHeader>
              <ModalBody>
                {renderTypeSelection("导出内容", exportTypes, setExportTypes)}
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color="primary"
                  isLoading={exporting}
                  onPress={handleExport}
                >
                  {exporting ? "导出中..." : "导出"}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      <Modal
        backdrop="blur"
        classNames={{
          base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
        }}
        isOpen={importSelectorOpen}
        onOpenChange={setImportSelectorOpen}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>选择导入内容</ModalHeader>
              <ModalBody>
                {renderTypeSelection("导入内容", importTypes, setImportTypes)}
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color="primary"
                  isDisabled={importTypes.length === 0}
                  onPress={triggerImportFilePicker}
                >
                  下一步选择文件
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      <Modal
        backdrop="blur"
        classNames={{
          base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full rounded-2xl overflow-hidden",
        }}
        isOpen={systemUpgradeModalOpen}
        onOpenChange={(open) => {
          if (!systemUpgradeExecuting) {
            setSystemUpgradeModalOpen(open);
          }
        }}
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader>确认面板升级</ModalHeader>
              <ModalBody>
                <div className="space-y-3 text-sm text-default-700 dark:text-default-300">
                  <p>
                    升级过程需要访问 Docker
                    Socket，并会在短时间内中断当前面板服务。
                  </p>
                  <p>
                    请确认已经允许面板管理容器与宿主机 Docker
                    交互，并且可以接受升级期间的临时不可用。
                  </p>
                  <div className="space-y-2 rounded-lg border border-warning-200 bg-warning-50 px-4 py-3 text-warning-800 dark:border-warning-900/40 dark:bg-warning-950/30 dark:text-warning-200">
                    <p className="text-xs font-medium">升级前请确认</p>
                    <ul className="list-disc space-y-1 pl-4 text-xs">
                      <li>Docker Socket 可用且挂载权限正常。</li>
                      <li>当前面板允许短暂停止和重启。</li>
                      <li>已选择正确的更新通道与目标版本。</li>
                    </ul>
                  </div>
                </div>
              </ModalBody>
              <ModalFooter>
                <Button
                  isDisabled={systemUpgradeExecuting}
                  variant="light"
                  onPress={onClose}
                >
                  取消
                </Button>
                <Button
                  color="primary"
                  isDisabled={systemUpgradeExecuting}
                  isLoading={systemUpgradeExecuting}
                  onPress={handleConfirmSystemUpgrade}
                >
                  升级
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </AnimatedPage>
  );
}
