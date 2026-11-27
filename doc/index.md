---
layout: home

hero:
  name: FLYX
  text: 转发 + 多内核节点面板
  tagline: 在一个面板里统一管理服务器、多内核节点、中转线路、订阅分发与轻量多用户，支持中英文实时切换
  image:
    src: /screenshots/overview.png
    alt: FLYX 控制台概览
  actions:
    - theme: brand
      text: 快速上手
      link: /guide/getting-started
    - theme: alt
      text: 在线演示
      link: https://kure29.github.io/flyx/demo/
    - theme: alt
      text: 基本概念
      link: /guide/concepts
    - theme: alt
      text: GitHub
      link: https://github.com/kure29/flyx
    - theme: alt
      text: Telegram 交流群
      link: https://t.me/flyx_chat

features:
  - title: 多内核节点与 443 端口复用
    details: 按服务器安装 Xray、sing-box、Snell v4 / v5 与 Mieru，一键创建 VLESS Reality、Hysteria2（端口跳跃 / Salamander 混淆）、TUIC、AnyTLS、NaïveProxy、ShadowTLS、Shadowsocks / SS2022 等入站；内置 Agent 原生 TCP SNI 443 端口复用，可与面板 HTTPS 共用 443。
  - title: 灵活中转与转发
    details: 支持 TCP / UDP 端口转发、隧道转发与多跳链路，内置 GOST / realm / nftables 引擎；中转线路自动进入订阅，支持入站白名单与真实客户端 IP。
  - title: Sub-Store 风格订阅
    details: 节点组支持独立勾选落地直连与中转线路，统一筛选、改名与脚本加工；多订阅支持独立订阅分组、标签分类、置顶、跨节点在线 IP 限制、拉取记录审计与批量管理，自动适配主流客户端。
  - title: 轻量多用户
    details: 管理员可创建普通用户并分别分配中转隧道配额与分享订阅（两者分开计算）；普通用户登录后仅见精简门户，超额或到期自动暂停转发，重置后自动恢复。
  - title: 延迟监控与三网回程
    details: 服务器定时测到北京 / 上海 / 广东三网的延迟与丢包，自动识别 CN2 GIA、9929、CMIN2 等回程线路，配套趋势图、公开状态页与异常告警。
  - title: 现代体验与中英双语
    details: 高密度电脑端布局与移动端 H5 自适应界面，柔化护眼深浅色主题与简体中文 / English 实时切换；支持 Passkey / GitHub 登录、Gist 加密备份与面板内一键升级。
---
