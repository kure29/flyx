<div align="center">

<img src="doc/public/logo.png" width="88" alt="FLYX" />

# FLYX

转发 + 节点搭建面板

在一个面板里管理服务器、节点与中转：一键部署 Xray / sing-box / Snell 节点，对接中转，生成订阅。

[在线演示](https://kure29.github.io/flyx/demo/) · [使用文档](https://kure29.github.io/flyx/) · [快速上手](https://kure29.github.io/flyx/guide/getting-started) · [下载](https://github.com/kure29/flyx/releases) · [Telegram 交流群](https://t.me/flyx_chat)

</div>

## 功能

- **多内核节点**：Xray、sing-box、Snell v4 / v5 按服务器安装；VLESS Reality、Hysteria2（端口跳跃）、TUIC、AnyTLS、NaïveProxy、ShadowTLS 等模板一键创建
- **证书**：自签、服务器上的证书文件，或由 sing-box 自动申请（ACME，HTTP / Cloudflare / 阿里云 DNS 验证）
- **对接中转**：端口转发、隧道转发，或“前置 → IX → 落地”多跳线路；入站白名单、真实客户端 IP
- **导入节点**：粘贴分享链接、Clash / sing-box / Surge 配置或订阅地址，别处搭好的节点也能对接中转
- **订阅**：参考 Sub-Store，节点组统一挑选节点、过滤、改名，多个订阅自由组合、加国旗、按地区分组，可分享给他人（独立凭据、到期自动断开）；按客户端自动返回 Clash / Mihomo、sing-box、Surge 或通用 base64
- **流量转发**：TCP / UDP 端口转发与隧道转发，realm、nftables 引擎，按规则统计上下行流量
- **单用户**：只有管理员一个账号，可以只用 GitHub 或通行证密钥登录
- **状态页**：不用登录的公开页面，显示服务器在线情况、近 30 天可用率、负载与流量，不显示 IP、用户和协议
- **面板内升级**：检查更新、一键升级

## 安装

面板（需要 Docker）：

```bash
curl -L https://github.com/kure29/flyx/releases/latest/download/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```

选 `1. 安装面板`，完成后访问 `http://服务器IP:6366`，默认账号 `admin_user` / `admin_user`，登录后请立即修改密码。

agent：在面板 **服务器** 页新建服务器，复制生成的安装命令到服务器上执行。

更多说明见 [使用文档](https://kure29.github.io/flyx/)：[安装面板](https://kure29.github.io/flyx/guide/install) · [安装 agent](https://kure29.github.io/flyx/guide/agent) · [内核与协议](https://kure29.github.io/flyx/guide/kernels) · [新建入站](https://kure29.github.io/flyx/guide/nodes) · [对接中转](https://kure29.github.io/flyx/guide/relay) · [延迟与回程](https://kure29.github.io/flyx/guide/latency) · [订阅](https://kure29.github.io/flyx/guide/subscription) · [升级](https://kure29.github.io/flyx/guide/upgrade) · [常见问题](https://kure29.github.io/flyx/guide/faq)

## 许可

以 GPLv3 发布，见 [LICENSE](LICENSE)；所含第三方代码的来源与许可见 [NOTICE](NOTICE)。

## 免责声明

本项目仅供个人学习与研究使用，仅限合法、合规用途。使用者须确保其使用行为符合所在国家或地区的法律法规，并自行承担使用本项目带来的一切风险。作者不对因使用本项目导致的任何后果承担责任，亦不提供任何形式的担保。禁止将本项目用于网络攻击、数据窃取、非法访问等任何违法或未经授权的行为。


## ⭐ 喝杯咖啡！（USDT）

| 网络       | 地址                                                                 |
|------------|----------------------------------------------------------------------|
| BNB(BEP20) | `0x736f077bb3717b467d3da1576828dd6cbe089015`                          |
| TRC20      | `TWjh5CRZ2qe1NxzPgHWkwLA9zgMUfjijpH`                                  |
| polygon    |  `0x736f077bb3717b467d3da1576828dd6cbe089015`    |

