import { defineConfig } from "vitepress";

export default defineConfig({
  lang: "zh-CN",
  title: "FLYX",
  description: "转发 + 节点搭建面板",
  base: "/flyx/",
  cleanUrls: true,
  lastUpdated: true,
  head: [["link", { rel: "icon", type: "image/png", href: "/flyx/logo.png" }]],
  themeConfig: {
    logo: "/logo.png",
    nav: [
      { text: "指南", link: "/guide/getting-started" },
      { text: "在线演示", link: "https://kure29.github.io/flyx/demo/" },
      { text: "更新日志", link: "/changelog" },
      { text: "下载", link: "https://github.com/kure29/flyx/releases" },
    ],
    sidebar: [
      {
        text: "开始",
        items: [
          { text: "基本概念", link: "/guide/concepts" },
          { text: "快速上手", link: "/guide/getting-started" },
          { text: "安装面板", link: "/guide/install" },
          { text: "安装 agent", link: "/guide/agent" },
          { text: "服务器设置", link: "/guide/servers" },
          { text: "升级", link: "/guide/upgrade" },
        ],
      },
      {
        text: "节点",
        items: [
          { text: "内核与协议", link: "/guide/kernels" },
          { text: "新建入站", link: "/guide/nodes" },
          { text: "分流与链式出站", link: "/guide/routing" },
          { text: "导入节点", link: "/guide/import" },
          { text: "延迟与回程", link: "/guide/latency" },
        ],
      },
      {
        text: "中转与转发",
        items: [
          { text: "对接中转", link: "/guide/relay" },
          { text: "隧道与转发规则", link: "/guide/forward" },
        ],
      },
      {
        text: "订阅",
        items: [
          { text: "订阅与客户端", link: "/guide/subscription" },
          { text: "节点组与订阅源", link: "/guide/subscription-sources" },
          { text: "筛选、命名与脚本", link: "/guide/subscription-nodes" },
          { text: "分流规则与链式代理", link: "/guide/subscription-rules" },
          { text: "订阅模板", link: "/guide/subscription-templates" },
          { text: "凭据、到期与流量", link: "/guide/subscription-users" },
        ],
      },
      {
        text: "运维",
        items: [
          { text: "通知", link: "/guide/notify" },
          { text: "状态页", link: "/guide/status-page" },
          { text: "状态页主题开发", link: "/guide/status-theme-dev" },
          { text: "备份与恢复", link: "/guide/backup" },
          { text: "PostgreSQL", link: "/guide/postgresql" },
          { text: "通行证密钥登录", link: "/guide/passkey" },
          { text: "GitHub 登录", link: "/guide/github-login" },
          { text: "AI Skill", link: "/ai-skill" },
        ],
      },
      {
        text: "帮助",
        items: [
          { text: "常见问题", link: "/guide/faq" },
          { text: "在线演示", link: "https://kure29.github.io/flyx/demo/" },
      { text: "更新日志", link: "/changelog" },
        ],
      },
    ],
    socialLinks: [
      { icon: "github", link: "https://github.com/kure29/flyx" },
      { icon: "telegram", link: "https://t.me/flyx_chat" },
    ],
    search: { provider: "local" },
    outline: { level: [2, 3], label: "本页目录" },
    docFooter: { prev: "上一页", next: "下一页" },
    lastUpdated: { text: "最后更新" },
    returnToTopLabel: "回到顶部",
    sidebarMenuLabel: "菜单",
    darkModeSwitchLabel: "外观",
    footer: {
      message: "基于 GPLv3 发布 · 仅供学习研究使用",
    },
  },
});
