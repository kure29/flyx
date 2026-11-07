import { useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import toast from "react-hot-toast";

import { finishGithubBind, finishGithubLogin } from "@/api";
import { PageLoadingState } from "@/components/page-state";
import { writeLoginSession } from "@/utils/session";
import { takeGithubCallback } from "@/utils/github-login";

/** Where GitHub sends the browser back to, for both login and binding. */
export default function GithubCallbackPage() {
  const navigate = useNavigate();
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;

    const callback = takeGithubCallback();
    const back = callback.flow === "bind" ? "/profile" : "/";

    if ("error" in callback) {
      toast.error(callback.error);
      navigate(back, { replace: true });

      return;
    }

    if (callback.flow === "bind") {
      finishGithubBind(callback.code, callback.state)
        .then((res) => {
          if (res.code !== 0)
            toast.error(res.msg || "绑定失败", { duration: 6000 });
          else toast.success(`已绑定 GitHub 账号 ${res.data.login}`);
        })
        .catch(() => toast.error("绑定失败"))
        .finally(() => navigate(back, { replace: true }));

      return;
    }

    finishGithubLogin(callback.code, callback.state)
      .then((res) => {
        if (res.code !== 0) {
          toast.error(res.msg || "GitHub 登录失败", { duration: 6000 });
          navigate(back, { replace: true });

          return;
        }
        writeLoginSession(res.data);
        toast.success("登录成功");
        navigate(
          res.data.requirePasswordChange ? "/change-password" : "/dashboard",
          { replace: true },
        );
      })
      .catch(() => {
        toast.error("GitHub 登录失败");
        navigate(back, { replace: true });
      });
  }, [navigate]);

  return <PageLoadingState message="正在完成 GitHub 授权..." />;
}
