import type { StatusTheme } from "@/api";

import { useCallback, useEffect, useRef, useState } from "react";
import toast from "react-hot-toast";

import {
  deleteStatusTheme,
  fetchStatusThemePreview,
  installStatusTheme,
  listStatusThemes,
  selectStatusTheme,
  updateStatusTheme,
  uploadStatusTheme,
} from "@/api";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Chip } from "@/shadcn-bridge/heroui/chip";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  hasThemeSettings,
  StatusThemeSettings,
} from "@/pages/config/status-theme-settings";

const isGithub = (url: string) =>
  /^https:\/\/github\.com\/[^/]+\/[^/]+/.test(url);

/** A theme's thumbnail; the endpoint needs the token, so it is fetched. */
function Preview({ theme }: { theme: StatusTheme }) {
  const [src, setSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!theme.preview) return;
    let url: string | null = null;
    let live = true;

    void fetchStatusThemePreview(theme.short).then((u) => {
      url = u;
      if (live) setSrc(u);
      else if (u) URL.revokeObjectURL(u);
    });

    return () => {
      live = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [theme.short, theme.version, theme.preview]);

  return (
    <div className="aspect-[16/9] w-full overflow-hidden rounded-lg border border-divider bg-default-100">
      {src ? (
        <img
          alt={`${theme.name} 预览`}
          className="h-full w-full object-cover object-top"
          src={src}
        />
      ) : (
        <div className="flex h-full items-center justify-center text-xs text-default-400">
          {theme.name}
        </div>
      )}
    </div>
  );
}

/**
 * 网站配置 → 状态页 → 主题: the themes the panel can serve at /status/.
 * Every action takes effect at once (not with the page's 保存).
 */
export function StatusThemeManager() {
  const [themes, setThemes] = useState<StatusTheme[]>([]);
  const [busy, setBusy] = useState("");
  const [repoUrl, setRepoUrl] = useState("");
  const [settingsFor, setSettingsFor] = useState<StatusTheme | null>(null);
  const closeSettings = useCallback(() => setSettingsFor(null), []);
  const fileRef = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    const res = await listStatusThemes();

    if (res.code === 0 && res.data) setThemes(res.data.themes);
    else toast.error(res.msg || "读取主题失败");
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const run = async (
    key: string,
    action: () => Promise<{ code: number; msg: string }>,
    done: string | (() => string),
  ) => {
    setBusy(key);
    try {
      const res = await action();

      if (res.code === 0) {
        toast.success(typeof done === "string" ? done : done());
        await load();
      } else {
        toast.error(res.msg || "操作失败");
      }
    } finally {
      setBusy("");
    }
  };

  const use = (t: StatusTheme) =>
    run(
      `use:${t.short}`,
      () => selectStatusTheme(t.short),
      `已切换到「${t.name}」`,
    );

  const remove = (t: StatusTheme) => {
    if (!window.confirm(`删除主题「${t.name}」？正在使用时会换回内置主题。`))
      return;
    void run(`del:${t.short}`, () => deleteStatusTheme(t.short), "已删除");
  };

  const update = async (t: StatusTheme) => {
    setBusy(`upd:${t.short}`);
    try {
      const res = await updateStatusTheme(t.short);

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "检查更新失败");
      } else if (res.data.updated) {
        toast.success(`已更新到 ${res.data.version}`);
        await load();
      } else {
        toast.success(`已是最新版本 ${res.data.version}`);
      }
    } finally {
      setBusy("");
    }
  };

  const install = () => {
    const url = repoUrl.trim();

    if (!url) {
      toast.error("填写主题的 GitHub 仓库地址");

      return;
    }
    void run(
      "install",
      async () => {
        const res = await installStatusTheme(url);

        if (res.code === 0) setRepoUrl("");

        return res;
      },
      "已安装，点「使用」切换到它",
    );
  };

  const upload = (file: File | undefined) => {
    if (!file) return;
    void run(
      "upload",
      () => uploadStatusTheme(file),
      "已安装，点「使用」切换到它",
    );
    if (fileRef.current) fileRef.current.value = "";
  };

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {themes.map((t) => (
          <div
            key={t.short}
            className={`flex flex-col gap-2 rounded-xl border p-3 ${
              t.selected ? "border-primary" : "border-divider"
            }`}
          >
            <Preview theme={t} />
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-sm font-medium">{t.name}</span>
              {t.selected && (
                <Chip color="primary" size="sm" variant="flat">
                  使用中
                </Chip>
              )}
              {t.builtin && (
                <Chip size="sm" variant="flat">
                  内置
                </Chip>
              )}
            </div>
            <p className="text-xs text-default-400">
              {[t.version && `v${t.version}`, t.author]
                .filter(Boolean)
                .join(" · ")}
              {isGithub(t.url) && (
                <>
                  {" · "}
                  <a
                    className="text-primary hover:underline"
                    href={t.url}
                    rel="noreferrer"
                    target="_blank"
                  >
                    源码
                  </a>
                </>
              )}
            </p>
            {t.description && (
              <p className="line-clamp-2 text-xs text-default-500">
                {t.description}
              </p>
            )}
            <div className="mt-auto flex flex-wrap gap-2 pt-1">
              {!t.selected && (
                <Button
                  color="primary"
                  isDisabled={!!busy}
                  isLoading={busy === `use:${t.short}`}
                  size="sm"
                  onPress={() => void use(t)}
                >
                  使用
                </Button>
              )}
              {hasThemeSettings(t) && (
                <Button
                  isDisabled={!!busy}
                  size="sm"
                  variant="bordered"
                  onPress={() => setSettingsFor(t)}
                >
                  设置
                </Button>
              )}
              {!t.builtin && isGithub(t.url) && (
                <Button
                  isDisabled={!!busy}
                  isLoading={busy === `upd:${t.short}`}
                  size="sm"
                  variant="bordered"
                  onPress={() => void update(t)}
                >
                  检查更新
                </Button>
              )}
              {!t.builtin && (
                <Button
                  color="danger"
                  isDisabled={!!busy}
                  isLoading={busy === `del:${t.short}`}
                  size="sm"
                  variant="light"
                  onPress={() => remove(t)}
                >
                  删除
                </Button>
              )}
            </div>
          </div>
        ))}
      </div>

      <div className="flex flex-col gap-2 sm:flex-row">
        <Input
          classNames={{ input: "text-sm" }}
          placeholder="https://github.com/作者/仓库"
          size="sm"
          value={repoUrl}
          variant="bordered"
          onChange={(e) => setRepoUrl(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") install();
          }}
        />
        <div className="flex shrink-0 gap-2">
          <Button
            isDisabled={!!busy}
            isLoading={busy === "install"}
            size="sm"
            variant="bordered"
            onPress={install}
          >
            从 GitHub 安装
          </Button>
          <Button
            isDisabled={!!busy}
            isLoading={busy === "upload"}
            size="sm"
            variant="bordered"
            onPress={() => fileRef.current?.click()}
          >
            上传 theme.tar.gz
          </Button>
        </div>
        <input
          ref={fileRef}
          accept=".tar.gz,.tgz,application/gzip"
          className="hidden"
          type="file"
          onChange={(e) => upload(e.target.files?.[0])}
        />
      </div>
      <p className="text-xs text-default-400">
        从 GitHub 安装会取仓库最新 release 里的
        theme.tar.gz。主题是运行在面板域名下的网页，
        能读到这个浏览器里的面板登录状态，只安装你信任的主题。
      </p>
      {settingsFor && (
        <StatusThemeSettings theme={settingsFor} onClose={closeSettings} />
      )}
    </div>
  );
}
