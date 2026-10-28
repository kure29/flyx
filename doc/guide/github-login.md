# GitHub 登录（可选）

开启后，用户可以在个人资料页绑定自己的 GitHub 账号，之后在登录页点 **使用 GitHub 登录** 一键登录。面板用户仍由管理员创建：**没有绑定过的 GitHub 账号无法登录**，也不会自动注册新用户。密码登录照常可用，也可以[禁止管理员使用密码登录](#禁止管理员使用密码登录)。

## 管理员：开启

1. 打开 GitHub → **Settings** → **Developer settings** → **OAuth Apps** → **New OAuth App**：
   - **Homepage URL**：面板网址，例如 `https://panel.example.com`
   - **Authorization callback URL**：面板网址加 `/github-callback`，例如 `https://panel.example.com/github-callback`。只填这一个，登录和绑定都回到这里。填浏览器地址栏里访问面板的地址（前端地址），不是后端 API 地址；面板 **设置 → 安全与登录** 的说明里会直接显示应该填的地址
2. 创建后记下 **Client ID**，再点 **Generate a new client secret** 生成 **Client Secret**
3. 面板 **设置** → **安全与登录**：打开 **启用 GitHub 登录**，填入 Client ID 和 Client Secret，保存

Client Secret 只有管理员能看到，也不会写进 JSON 备份。

## 用户：绑定和登录

1. 先用密码登录，进入 **更多** 页（手机端底部 **更多**，电脑上点左下角 **账户与登录**）
2. 在 **账户** 里点 **GitHub 登录**，输入当前密码，点 **去 GitHub 授权**，授权后自动回到面板，这一行会显示“已绑定 + GitHub 用户名”
3. 以后在登录页点 **使用 GitHub 登录**，GitHub 授权后直接进入面板

- 一个面板用户只能绑定一个 GitHub 账号，一个 GitHub 账号也只能绑定一个面板用户
- 解绑同样在个人资料页，需要输入当前密码
- 账号被停用后，GitHub 登录也会被拒绝；删除用户时会一并删除绑定
- 面板只读取 GitHub 账号的 ID 和用户名，不申请任何仓库或邮箱权限

## 禁止管理员使用密码登录

开启 GitHub 登录后，**设置** → **安全与登录** 里可以打开 **禁止管理员使用密码登录**：管理员账号只能用 GitHub 或通行证密钥登录，即使密码泄露也登录不了管理后台。**普通用户不受影响**，照常用密码登录，配置规则、复制订阅都不变；登录页也照常显示用户名和密码。

- 打开前所有启用中的管理员都要先在个人资料页绑定 GitHub，否则保存会被拒绝并列出未绑定的管理员
- 管理员在登录页输入正确的账号密码时，会提示改用 GitHub 或通行证密钥登录
- 关闭 GitHub 登录，或者 Client ID / Secret 被清空时，管理员的密码登录会自动恢复
- 开关打开期间，管理员不能解绑自己的 GitHub
- 手机 App 里不能用 GitHub 登录，管理员在 App 里只能用通行证密钥登录

GitHub 访问不了、OAuth App 配错等原因导致管理员登录不了时，在面板服务器上执行下面的命令，重新允许管理员使用密码登录（无需重启面板）：

```bash
docker compose exec backend /app/paneld enable-password-login
```

不是用 Docker 部署时，在后端所在机器上，用和面板相同的环境变量（`DB_TYPE`、`DB_PATH` 或 `DATABASE_URL`）运行 `paneld enable-password-login`。

## 注意

- 手机 App（WebView）里不显示 GitHub 登录入口，请在浏览器里使用
- GitHub 提示 “redirect_uri 与此应用程序无关” 时，是 OAuth App 的 callback URL 和面板发出的地址对不上：协议、域名、端口要完全一致，路径是 `/github-callback`。面板换了域名时也要同步修改
- 后端需要能访问 `github.com` 和 `api.github.com`；面板在访问不了 GitHub 的网络里时无法使用此功能
- 授权过程中的临时状态保存在后端内存里，有效期 10 分钟，后端重启后需要重新点一次
- 面板的 JSON 导出不包含 GitHub 绑定，用 JSON 导入迁移后，用户需要重新绑定
