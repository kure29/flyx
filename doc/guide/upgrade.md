# 升级

## FLYX 命名兼容

新安装使用 `flyx-panel-backend`、`flyx-panel-frontend`、`flyx-panel-postgres` 容器，以及 `flyx-sqlite-data`、`flyx-postgres-data` 数据卷和 `flyx-network` 网络。安装脚本会把这些名称写入 `.env`。

旧部署升级时继续使用原容器、数据卷和数据库账号，不搬动已有数据。Compose 中保留的旧默认值用于兼容旧版面板直接下载新版配置的升级方式。手动部署如需使用新名称，应显式配置 `PANEL_BACKEND_CONTAINER`、`PANEL_FRONTEND_CONTAINER`、`PANEL_POSTGRES_CONTAINER`、`FLYX_SQLITE_VOLUME`、`FLYX_POSTGRES_VOLUME` 和 `FLYX_NETWORK`；已有部署请填写实际使用的名称，不能将数据卷改成一个空的新卷。自定义部署应在升级前核对这些参数。

版本变量统一为 `FLYX_VERSION`，升级脚本同时更新旧的 `FLUX_VERSION`，供旧模板或回退使用。其他配置优先读取 `FLYX_*`，未设置时兼容对应的 `FLUX_*` / `FLVX_*` 旧名称。通行证密钥的公开 origin 保持原值，浏览器的主题和列表偏好自动兼容旧键。

节点新安装使用 `flyx-agent` 服务及 `/etc/flyx-agent`。旧节点继续在 `/etc/flux_agent` 中运行 `flux_agent`，保留配置、内核状态、待上报流量及调优备份。新版程序按自身安装位置执行升级和回退。Docker 重装命令会识别已有容器的数据挂载，镜像拉取成功后才替换容器；发现新旧容器同时存在或挂载不明确时停止。

已有 nftables 表和计数 epoch 继续沿用，新建状态使用 FLYX 名称，避免仅因项目改名重置统计。内部的用户凭据、数字标记及 API 数据字段保持兼容。旧镜像名作为过渡别名继续发布。

回退前保留部署目录中的 `.env`、`docker-compose.yml` 及备份，使用该安装对应的原配置恢复；新安装回到不识别新资源参数的历史版本时，需要显式保留原数据卷挂载。API Skill 的迁移见 [AI Skill](../ai-skill.md)。

## 面板内升级

**设置** → **系统与升级** → **面板自升级** → **检查更新** → **立即升级**。

升级会拉取新镜像并重建前后端容器，约 1 分钟后刷新页面即可。页面没有变化时，通常是浏览器或 CDN 缓存，刷新或清理缓存即可。

## 脚本升级

在面板部署目录（存放 `docker-compose.yml` 和 `.env` 的目录）重新运行安装脚本，选 `2. 更新面板`：

```bash
curl -L https://github.com/kure29/flyx/releases/latest/download/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```

数据会保留，更新前会自动备份到部署目录的 `backups/`。

## 指定版本

到 [Releases](https://github.com/kure29/flyx/releases) 复制对应版本的命令，例如：

```bash
curl -L https://github.com/kure29/flyx/releases/download/3.0.7/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```

## agent 升级

**服务器** 页选择服务器 → **升级**，留空版本即使用最新版；点 **多选** 可以批量升级。也可以在服务器上重新运行服务器安装脚本。

面板升级后建议把 agent 也升级到同一版本：新功能（例如回程检测、订阅流量统计）需要新 agent，旧 agent 能照常转发，只是用不了这些功能。内核（Xray / sing-box / Snell）的版本在 **落地 → 内核** 里单独更换，见 [内核与协议](./kernels.md)。

### 流量可靠上报

先升级面板，再升级 agent。新版 agent 将已采集、待确认的转发流量保存在工作目录的 `traffic-*.json` 中，网络失败或重启后继续重试；面板按批次去重，账户、配额、共享用量和统计记录一起提交。请保留该目录及文件，容器部署需要保留 agent 数据卷。

旧 agent 仍可向新版面板上报，但不能获得跨重试去重和本地待发送缓冲的保障。新版 agent 连接不支持 `/flow/upload/v2` 的旧面板时会保留待上报数据，升级面板后继续发送。缓冲保护已落盘的采样；进程突然退出前尚未采集或落盘的字节无法补回。延迟补报按面板实际入账时间归入配额周期，不回填过去的日、月。

面板游标因旧备份恢复或数据库重建而丢失时，节点会保留当前待确认批次，重新建立上报流并继续发送。完整流量数据备份现在同时保存去重游标；导入旧格式完整备份会清理所恢复节点的旧游标，让节点自动重新建立上报流。部分数据导入不替换游标。已被节点确认并删除、但不在备份中的历史用量不能凭空恢复。

明确被面板拒绝的批次会拆分重试，正常条目继续入账。仍无效的单条记录或批次身份冲突会保存在同目录的 `traffic-*.json.rejected-*.json`，并打印日志；这些记录不自动重新计费，需要检查配置和账目后人工处理。留档或队列保存失败时保留原批次。普通网络错误、代理返回的错误页和无法识别的响应不会触发丢弃。

## 修复面板内升级

面板内升级提示“部署docker-compose.yml不可用”时：3.0.6 及更早版本的面板内升级会把后端容器的部署目录挂载弄丢，之后面板内升级会一直提示这个错误。数据不受影响，修复一次即可：

1. 在服务器上进入存放 `docker-compose.yml` 和 `.env` 的目录（常见为 `/root`，或当初运行安装脚本的目录），用 `ls` 确认这两个文件都在
2. 执行：

   ```bash
   docker compose up -d --force-recreate backend frontend
   ```

也可以在该目录运行 3.0.7 及以后的安装脚本，选“更新面板”，会顺带修复。
