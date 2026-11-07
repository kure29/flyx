# 安装面板

面板管理所有服务器、入站、隧道和订阅，用 Docker 部署在一台服务器上。服务器端（agent）的安装见 [安装 agent](./agent.md)。

## 环境要求

- Linux（推荐 Debian 10+ / Ubuntu 20.04+）
- 已安装 Docker 和 Docker Compose（没装可以用 `curl -fsSL https://get.docker.com | bash`）

## 一键安装

使用以下命令即可快速安装面板：

```bash
curl -L https://github.com/kure29/flyx/releases/latest/download/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```

**安装过程中会提示输入以下信息：**
- **前端端口**: 默认为 `6366`
- **后端端口**: 默认为 `6365`

端口已被占用（例如同一台服务器上装了 flux-panel 或 FLVX）时，脚本会建议下一个空闲端口。面板文件默认放在 `/opt/flyx-panel`，可用 `PANEL_DEPLOY_DIR=/path ./panel_install.sh` 指定。和其他面板共存见 [和 flux-panel / FLVX 装在同一台服务器](./upgrade.md#和-flux-panel-flvx-装在同一台服务器)。

脚本会自动检测系统是否支持 IPv6，并自动配置 Docker 的 IPv6 支持。

## 访问面板

安装完成后，访问：
`http://<服务器IP>:<前端端口>` (默认: `http://<服务器IP>:6366`)

**默认管理员账号：**
- 用户名: `admin_user`
- 密码: `admin_user`

::: warning
首次登录后请立即在“我的 / 个人资料”里修改默认密码。
:::

## 维护菜单

再次运行 `./panel_install.sh` 脚本可以看到管理菜单：
1. 安装面板
2. 更新面板
3. 卸载面板
4. 迁移到 PostgreSQL（见 [PostgreSQL](./postgresql.md)）
5. 退出

## 下一步

1. **设置 → 基本** 里填写“面板后端地址”（`ip:port` 或 `域名:port`），服务器用它连接面板，可以套 CDN / HTTPS
2. [安装 agent](./agent.md)

## 反向代理（可选）

如果需要通过域名访问面板并自动获取 HTTPS 证书，可以使用 Caddy 作为反向代理。

### 安装 Caddy

```bash
# Debian / Ubuntu
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudflare.com/content/v1/e2qwFJ2fRP2b2q/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudflare.com/content/v1/e2qwFJ2fRP2b2q/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install caddy
```

其他系统请参考 [Caddy 官方安装文档](https://caddyserver.com/docs/install)。

### 配置 Caddyfile

编辑 Caddy 配置文件：

```bash
sudo nano /etc/caddy/Caddyfile
```

#### 面板域名配置

将 `panel.example.com` 替换为你自己的域名：

```txt
panel.example.com {
    reverse_proxy localhost:6366
}
```

Caddy 会自动为域名申请和续期 HTTPS 证书，无需额外配置。

### 重启 Caddy

```bash
sudo systemctl restart caddy
```

### 注意事项

- 确保域名已正确解析到服务器 IP。
- 确保服务器防火墙放行了 **80** 和 **443** 端口（Caddy 自动申请证书需要）。
- 使用 Caddy 反向代理后，可以在 `.env` 中将前端端口改为仅监听本地，避免直接暴露：
  ```
  FRONTEND_PORT=127.0.0.1:6366
  ```

升级见 [升级](./upgrade.md)。

