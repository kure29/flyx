import { useEffect, useState } from "react";
import toast from "react-hot-toast";
import { Github } from "lucide-react";

import { Button } from "@/shadcn-bridge/heroui/button";
import { Input } from "@/shadcn-bridge/heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@/shadcn-bridge/heroui/modal";
import { beginGithubBind, unbindGithub, type GithubInfo } from "@/api";
import { githubCallbackUrl, goToGithub } from "@/utils/github-login";

/** Binds or unbinds the GitHub account used for GitHub login. */
export function GithubBindingModal({
  info,
  isOpen,
  onClose,
  onChanged,
}: {
  info: GithubInfo | null;
  isOpen: boolean;
  onClose: () => void;
  onChanged: () => void;
}) {
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const bound = Boolean(info?.bound);

  useEffect(() => {
    if (isOpen) setPassword("");
  }, [isOpen]);

  const submit = async () => {
    if (!password) {
      toast.error("请输入当前密码");

      return;
    }
    setBusy(true);
    try {
      if (!bound) {
        const res = await beginGithubBind(githubCallbackUrl(), password);

        if (res.code !== 0) {
          toast.error(res.msg || "无法绑定 GitHub");
          setBusy(false);

          return;
        }
        // Leaves the page; GitHub comes back to /github-callback.
        goToGithub("bind", res.data.url);

        return;
      }
      const res = await unbindGithub(password);

      if (res.code !== 0) {
        toast.error(res.msg || "解绑失败");

        return;
      }
      toast.success("已解绑 GitHub 账号");
      onChanged();
      onClose();
    } catch {
      toast.error(bound ? "解绑失败" : "无法绑定 GitHub");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      classNames={{
        base: "!w-[calc(100%-32px)] !mx-auto sm:!w-full overflow-hidden",
      }}
      isOpen={isOpen}
      placement="center"
      size="md"
      onClose={onClose}
    >
      <ModalContent>
        <ModalHeader>GitHub 登录</ModalHeader>
        <ModalBody>
          <div className="flex items-center gap-3 rounded-xl bg-default-100 px-3 py-3">
            <Github className="h-5 w-5 shrink-0 text-default-600" />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">
                {bound ? info?.login : "未绑定"}
              </div>
              <div className="text-xs text-default-500">
                {bound
                  ? `绑定于 ${new Date(info?.createdAt || 0).toLocaleDateString()}，可在登录页用 GitHub 登录`
                  : "绑定后可在登录页用 GitHub 账号登录"}
              </div>
            </div>
          </div>
          <Input
            autoComplete="current-password"
            label="当前密码"
            placeholder={bound ? "输入当前密码以解绑" : "输入当前密码以绑定"}
            type="password"
            value={password}
            variant="bordered"
            onChange={(e) => setPassword(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void submit();
            }}
          />
        </ModalBody>
        <ModalFooter>
          <Button variant="light" onPress={onClose}>
            取消
          </Button>
          <Button
            color={bound ? "danger" : "primary"}
            isLoading={busy}
            variant={bound ? "flat" : "solid"}
            onPress={() => void submit()}
          >
            {bound ? "解绑" : "去 GitHub 授权"}
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}
