import type { GithubTokenStatus } from "@/api";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";

import { getGithubTokenStatus, saveGithubToken } from "@/api";
import { Field } from "@/pages/config/section";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";

/**
 * 服务器与监控 → GitHub 令牌: lets the panel read a private release
 * repository. Nodes download through the panel's mirror, so the token never
 * leaves the panel.
 */
export function GithubToken({
  usingPanelMirror,
}: {
  usingPanelMirror: boolean;
}) {
  const [status, setStatus] = useState<GithubTokenStatus | null>(null);
  const [token, setToken] = useState("");
  const [saving, setSaving] = useState(false);

  const load = async () => {
    const res = await getGithubTokenStatus();

    if (res.code === 0 && res.data) setStatus(res.data);
  };

  useEffect(() => {
    void load();
  }, []);

  const save = async (clear: boolean) => {
    if (!clear && !token.trim()) {
      toast.error("请填写令牌");

      return;
    }
    setSaving(true);
    try {
      const res = await saveGithubToken(
        clear ? { clear: true } : { token: token.trim() },
      );

      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");

        return;
      }
      toast.success(clear ? "已清除令牌" : "令牌已保存");
      setToken("");
      await load();
    } finally {
      setSaving(false);
    }
  };

  const active = !!status && (status.set || status.fromEnv);

  return (
    <Field
      help={
        <>
          发布仓库{status ? ` ${status.repo} ` : ""}
          是私有仓库时填写：面板用它检查更新、升级面板，并替服务器下载 agent
          和安装脚本。需要 repo 权限；面板一键升级拉取镜像还需要 read:packages
          权限。令牌只保存在面板，不会下发到服务器，保存后也不会再显示
        </>
      }
      label="GitHub 令牌"
    >
      <div className="flex flex-wrap items-center gap-2">
        <Input
          autoComplete="off"
          className="min-w-0 flex-1"
          placeholder={
            status?.set
              ? "已设置，填写新令牌可替换"
              : status?.fromEnv
                ? "正在使用环境变量 GITHUB_TOKEN"
                : "ghp_… 或 github_pat_…"
          }
          type="password"
          value={token}
          variant="bordered"
          onChange={(e) => setToken(e.target.value)}
        />
        <Button
          color="primary"
          isLoading={saving}
          size="sm"
          onPress={() => void save(false)}
        >
          保存
        </Button>
        {status?.set && (
          <Button
            isDisabled={saving}
            size="sm"
            variant="bordered"
            onPress={() => void save(true)}
          >
            清除
          </Button>
        )}
      </div>
      {active && !usingPanelMirror && (
        <p className="text-xs text-warning">
          私有仓库的文件服务器无法直接下载：请打开上方 GitHub
          加速并选择「面板中转」，服务器才能经面板安装和升级 agent
        </p>
      )}
    </Field>
  );
}
