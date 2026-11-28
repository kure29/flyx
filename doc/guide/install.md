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
1. 安装面板（末尾可选交互式配置域名 HTTPS）
2. 更新面板
3. 卸载面板
4. 迁移到 PostgreSQL（见 [PostgreSQL](./postgresql.md)）
5. 配置 / 管理 HTTPS（自动申请 Let's Encrypt 证书、自定义证书、查看状态或关闭回滚）
6. 退出

## 下一步

1. **设置 → 基本** 里填写“面板后端地址”（`ip:port` 或 `域名:port`），服务器用它连接面板，可以套 CDN / HTTPS
2. [安装 agent](./agent.md)

## 配置 HTTPS（推荐）

### 方式一：通过 `panel_install.sh` 一键配置（推荐）

安装面板时可以直接选择开启域名 HTTPS，或随时再次运行 `./panel_install.sh` 选择 **`5. 配置 / 管理 HTTPS`**：

- **自动申请证书（Let's Encrypt）**：输入已解析到本机的域名（及可选邮箱），脚本会自动生成 `Caddyfile` 与 `docker-compose.override.yml`，以 Docker 容器方式运行 Caddy 并自动续期，支持 WebSocket 与 SSE 诊断流。
- **使用自定义证书**：支持填写自有 `.pem`/`.crt` 与 `.key` 路径以及自定义监听端口（例如 `8443` 或 `127.0.0.1:8443`，便于与 443 TCP SNI 复用共存）。
- **Passkey 联动**：开启 HTTPS 后会自动提示写入 `FLYX_WEBAUTHN_ORIGIN` 并重启后端，使 Passkey 登录开箱即用。
- **平滑升级与回滚**：配置保存在 `docker-compose.override.yml` 中，面板脚本更新与后台自更新均会自动保留；不需要时可在菜单中选择「关闭并回滚 HTTPS」恢复纯 HTTP 访问。

### 方式二：手动在宿主机安装 Caddy

如果你希望自行管理宿主机上的 Caddy，也可以按以下步骤配置：

#### 安装 Caddy

```bash
# Debian / Ubuntu
sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudflare.com/content/v1/e2qwFJ2fRP2b2q/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudflare.com/content/v1/e2qwFJ2fRP2b2q/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
sudo apt update
sudo apt install caddy
```

其他系统请参考 [Caddy 官方安装文档](https://caddyserver.com/docs/install)。

#### 配置 Caddyfile

编辑 Caddy 配置文件：

```bash
sudo nano /etc/caddy/Caddyfile
```

将 `panel.example.com` 替换为你自己的域名：

```txt
panel.example.com {
    reverse_proxy localhost:6366
}
```

Caddy 会自动为域名申请和续期 HTTPS 证书，无需额外配置。

#### 重启 Caddy

```bash
sudo systemctl restart caddy
```

### 注意事项

- 确保域名已正确解析到服务器 IP。
- 使用自动申请证书时，确保服务器防火墙放行了 **80** 和 **443** 端口。
- 使用反向代理后，可以在 `.env` 中将前端端口改为仅监听本地，避免直接暴露：
  ```
  FRONTEND_PORT=127.0.0.1:6366
  ```

升级见 [升级](./upgrade.md)。

