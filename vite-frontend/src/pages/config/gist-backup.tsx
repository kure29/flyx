import type { GistBackupStatus, GistBackupVersion } from "@/api";

import { useEffect, useState } from "react";
import toast from "react-hot-toast";

import {
  getGistBackupStatus,
  getGistBackupVersions,
  restoreGistBackup,
  saveGistBackup,
  uploadGistBackup,
} from "@/api";
import { Field, SubHeading } from "@/pages/config/section";
import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";

const INTERVALS: Array<[number, string]> = [
  [0, "关闭"],
  [6, "每 6 小时"],
  [12, "每 12 小时"],
  [24, "每天"],
  [168, "每周"],
];

const SELECT_CLASS =
  "select-chevron h-10 w-full rounded-md border border-divider bg-background pl-3 text-sm";

function formatTime(ms: number): string {
  return new Date(ms).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** 备份与恢复 → 上传到 GitHub Gist. */
export function GistBackup() {
  const [status, setStatus] = useState<GistBackupStatus | null>(null);
  const [token, setToken] = useState("");
  const [password, setPassword] = useState("");
  const [intervalHours, setIntervalHours] = useState(0);
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [restoreOpen, setRestoreOpen] = useState(false);

  const apply = (s: GistBackupStatus) => {
    setStatus(s);
    setIntervalHours(s.intervalHours);
    setToken("");
    setPassword("");
  };

  useEffect(() => {
    void getGistBackupStatus().then((res) => {
      if (res.code === 0 && res.data) apply(res.data);
    });
  }, []);

  if (!status) return null;

  const dirty =
    token.trim() !== "" ||
    password !== "" ||
    intervalHours !== status.intervalHours;

  const save = async (patch: {
    clearToken?: boolean;
    clearPassword?: boolean;
  }): Promise<boolean> => {
    setSaving(true);
    try {
      const res = await saveGistBackup({
        token: token.trim(),
        password,
        intervalHours,
        ...patch,
      });

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "保存失败");

        return false;
      }
      apply(res.data);

      return true;
    } finally {
      setSaving(false);
    }
  };

  const upload = async () => {
    // Unsaved changes go first, so the upload uses them.
    if (dirty && !(await save({}))) return;
    setUploading(true);
    try {
      const res = await uploadGistBackup();

      if (res.code !== 0 || !res.data) {
        toast.error(res.msg || "上传失败");
        void getGistBackupStatus().then((s) => {
          if (s.code === 0 && s.data) setStatus(s.data);
        });

        return;
      }
      setStatus(res.data.status);
      if (res.data.result.uploaded) toast.success("已上传到 Gist");
      else toast(res.data.result.message);
    } finally {
      setUploading(false);
    }
  };

  const failed = status.lastError !== "";

  return (
    <>
      <SubHeading>上传到 GitHub Gist</SubHeading>
      {status.configured && status.lastAt > 0 && (
        <p className="flex flex-wrap items-center gap-x-1 text-xs text-default-500">
          <span
            className={`mr-1 inline-block h-2 w-2 rounded-full ${failed ? "bg-danger" : "bg-success"}`}
          />
          {failed
            ? `上次上传失败（${formatTime(status.lastAt)}）：${status.lastError}`
            : `上次上传 ${formatTime(status.lastUpload || status.lastAt)} 成功`}
          {status.gistUrl && (
            <>
              <span>·</span>
              <a
                className="text-primary"
                href={status.gistUrl}
                rel="noreferrer"
                target="_blank"
              >
                查看 Gist
              </a>
            </>
          )}
        </p>
      )}

      <Field
        help={
          status.configured ? (
            <>
              已保存，留空不修改 ·{" "}
              <button
                className="text-danger"
                type="button"
                onClick={() => void save({ clearToken: true })}
              >
                清除令牌
              </button>
            </>
          ) : (
            "只需 gist 权限的令牌，保存后不再显示"
          )
        }
        label="GitHub 令牌"
      >
        <Input
          aria-label="GitHub 令牌"
          autoComplete="off"
          placeholder={status.configured ? status.tokenHint : "ghp_..."}
          value={token}
          variant="bordered"
          onChange={(e) => setToken(e.target.value)}
        />
      </Field>

      <Field
        help={
          status.encrypted ? (
            <>
              已设置，留空不修改；恢复时要输入 ·{" "}
              <button
                className="text-danger"
                type="button"
                onClick={() => void save({ clearPassword: true })}
              >
                不再加密
              </button>
            </>
          ) : (
            <>
              填了就先加密再上传，恢复时要输入。
              <span className="text-warning-600">
                不加密时，拿到 Gist 链接的人能看到节点密码等信息
              </span>
            </>
          )
        }
        label="加密密码（可选）"
      >
        <Input
          aria-label="加密密码"
          autoComplete="new-password"
          placeholder={status.encrypted ? "已设置" : "留空则不加密"}
          type="password"
          value={password}
          variant="bordered"
          onChange={(e) => setPassword(e.target.value)}
        />
      </Field>

      <Field label="自动上传">
        <select
          aria-label="自动上传"
          className={SELECT_CLASS}
          value={intervalHours}
          onChange={(e) => setIntervalHours(Number(e.target.value))}
        >
          {INTERVALS.map(([h, label]) => (
            <option key={h} value={h}>
              {label}
            </option>
          ))}
        </select>
      </Field>

      <div className="flex flex-wrap gap-2">
        <Button
          color="primary"
          isDisabled={!status.configured && token.trim() === ""}
          isLoading={uploading}
          size="sm"
          onPress={upload}
        >
          立即上传
        </Button>
        {dirty && (
          <Button
            isLoading={saving}
            size="sm"
            variant="flat"
            onPress={() => void save({})}
          >
            保存
          </Button>
        )}
        {status.gistUrl && (
          <Button size="sm" variant="flat" onPress={() => setRestoreOpen(true)}>
            从 Gist 恢复
          </Button>
        )}
      </div>

      {restoreOpen && (
        <GistRestoreModal
          encrypted={status.encrypted}
          onClose={() => setRestoreOpen(false)}
        />
      )}
    </>
  );
}

function GistRestoreModal({
  encrypted,
  onClose,
}: {
  encrypted: boolean;
  onClose: () => void;
}) {
  const [versions, setVersions] = useState<GistBackupVersion[] | null>(null);
  const [version, setVersion] = useState("");
  const [password, setPassword] = useState("");
  const [restoring, setRestoring] = useState(false);

  useEffect(() => {
    void getGistBackupVersions().then((res) => {
      if (res.code !== 0) {
        toast.error(res.msg || "获取版本失败");
        onClose();

        return;
      }
      const list = res.data ?? [];

      setVersions(list);
      setVersion(list[0]?.version ?? "");
    });
  }, []);

  const restore = async () => {
    setRestoring(true);
    try {
      const res = await restoreGistBackup({ version, password });

      if (res.code !== 0) {
        toast.error(res.msg || "恢复失败");

        return;
      }
      toast.success("恢复完成，正在刷新页面");
      // Restoring users may sign this session out; reload either way.
      window.setTimeout(() => window.location.reload(), 1500);
      onClose();
    } finally {
      setRestoring(false);
    }
  };

  return (
    <Modal
      isOpen
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      onOpenChange={(open) => !open && onClose()}
    >
      <ModalContent>
        <ModalHeader>从 Gist 恢复</ModalHeader>
        <ModalBody className="space-y-4">
          <Field label="版本">
            <select
              aria-label="版本"
              className={SELECT_CLASS}
              disabled={!versions}
              value={version}
              onChange={(e) => setVersion(e.target.value)}
            >
              {!versions && <option>正在加载...</option>}
              {versions?.map((v, i) => (
                <option key={v.version} value={v.version}>
                  {formatTime(v.committedAt)}
                  {i === 0 ? "（最新）" : ""}
                </option>
              ))}
            </select>
          </Field>
          <Field
            help={
              encrypted
                ? "这个版本上传时用的密码"
                : "没有加密的版本留空；加密的版本填上传时用的密码"
            }
            label="加密密码"
          >
            <Input
              aria-label="加密密码"
              autoComplete="off"
              type="password"
              value={password}
              variant="bordered"
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
          <p className="text-xs text-default-500">
            恢复会覆盖面板里的全部数据（Gist 设置除外），之后可能需要重新登录
          </p>
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={onClose}>
            取消
          </Button>
          <Button
            color="primary"
            isDisabled={!version}
            isLoading={restoring}
            onPress={restore}
          >
            恢复
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
