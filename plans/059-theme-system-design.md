# 059 - 主题系统设计（v2 — 完整可扩展架构）

## 概述

设计一个高度可扩展的主题包架构，允许第三方作者通过代码提交的方式创建主题，覆盖前端所有元素——从 CSS 变量到组件实现、布局结构、甚至整个页面。

## 架构

```
src/themes/
├── types.ts                  # ThemePackage 接口定义
├── registry.ts               # 主题注册表 + CSS 注入引擎
├── context.tsx                # React Context + Provider + Hooks
├── index.ts                  # 公共 API barrel
├── loader.ts                 # 主题加载器（注册所有内置主题）
├── README.md                 # 主题开发指南
│
├── default/                  # 默认主题（参考实现）
│   └── index.ts
│
├── example-cyberpunk/        # 示例主题（赛博朋克）
│   ├── index.ts
│   └── components/
│       └── button.tsx        # 组件覆盖示范
│
└── <your-theme>/             # 第三方主题
    ├── index.ts
    ├── components/
    ├── layouts/
    ├── pages/
    └── assets/
```

## 覆盖层级

| 层级 | 字段 | 说明 |
|------|------|------|
| CSS 变量 | `tokens.light` / `tokens.dark` | 80+ 个设计 token（颜色、字体、圆角） |
| 原始 CSS | `css` | 注入自定义 CSS（动画、字体、阴影等） |
| 组件替换 | `components` | 替换任意 UI 组件（30+ 个可替换组件键） |
| 布局替换 | `layouts` | 替换 4 种布局（Admin / H5 / H5Simple / Default） |
| 页面替换 | `pages` | 替换 14 个页面路由实现 |
| 生命周期 | `onActivate` / `onDeactivate` | 主题启用/停用回调 |

## 任务清单

- [x] **T1**: 创建 `src/themes/types.ts` — ThemePackage 接口 + 所有可覆盖键定义
- [x] **T2**: 创建 `src/themes/registry.ts` — 主题注册/激活/停用/CSS 注入引擎
- [x] **T3**: 创建 `src/themes/context.tsx` — React Context + ThemeProvider + hooks
- [x] **T4**: 创建 `src/themes/index.ts` — 公共 API barrel
- [x] **T5**: 创建 `src/themes/loader.ts` — 自动加载所有内置主题
- [x] **T6**: 创建 `src/themes/default/` — 默认主题参考实现
- [x] **T7**: 创建 `src/themes/example-cyberpunk/` — 完整示例主题（含组件覆盖 + CSS + 生命周期）
- [x] **T8**: 重构 `use-theme.tsx` — 向后兼容包装
- [x] **T9**: 重构 `theme-provider.tsx` — 集成新主题系统
- [x] **T10**: 编写 `README.md` — 主题开发完整指南
- [x] **T11**: TypeScript 编译验证通过
- [ ] **T12**: (后续) 设置页面集成主题选择器 UI
- [ ] **T13**: (后续) 将现有组件导入逐步迁移到 `useThemedComponent` 模式
