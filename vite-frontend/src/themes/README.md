# FLYX 主题开发指南

## 概述

FLYX 主题系统允许你完全自定义前端的外观和行为。一个主题可以覆盖：

| 覆盖层级 | 说明 | 难度 |
|----------|------|------|
| **CSS 变量** | 修改颜色、字体、圆角等设计 token | ⭐ 简单 |
| **原始 CSS** | 注入自定义 CSS（动画、字体、阴影等） | ⭐⭐ 中等 |
| **组件替换** | 替换任意 UI 组件（按钮、卡片、输入框等） | ⭐⭐⭐ 高级 |
| **布局替换** | 替换整个页面布局结构 | ⭐⭐⭐ 高级 |
| **页面替换** | 替换整个页面实现 | ⭐⭐⭐⭐ 专家 |

## 快速开始

### 1. 创建主题文件夹

```
src/themes/my-theme/
├── index.ts           ← 必须：主题入口，导出 ThemePackage
├── components/        ← 可选：组件覆盖
│   ├── index.ts
│   └── button.tsx
├── layouts/           ← 可选：布局覆盖
│   └── admin.tsx
├── pages/             ← 可选：页面覆盖
│   └── login.tsx
├── assets/            ← 可选：图片、字体等资源
└── styles.css         ← 可选：额外样式文件
```

### 2. 编写主题入口 `index.ts`

```typescript
import type { ThemePackage } from "../types";

const myTheme: ThemePackage = {
  id: "my-theme",           // 唯一标识（kebab-case）
  name: "我的主题",          // 显示名称
  author: "Your Name",      // 作者
  version: "1.0.0",         // 版本号
  description: "一个自定义主题",

  // CSS 变量覆盖
  tokens: {
    light: {
      "--primary": "#ff6600",
      "--primary-foreground": "#ffffff",
      "--background": "#fafafa",
    },
    dark: {
      "--primary": "#ff8833",
      "--background": "#1a1a2e",
    },
  },
};

export default myTheme;
```

### 3. 注册主题

打开 `src/themes/loader.ts`，添加两行：

```typescript
import myTheme from "./my-theme";
registerTheme(myTheme);
```

完成！主题已可用。

---

## 详细指南

### CSS 变量覆盖

所有可用的 CSS 变量定义在 `src/themes/types.ts` 的 `ThemeTokens` 接口中。常用的：

```typescript
tokens: {
  light: {
    // 基础色
    "--background": "#ffffff",    // 页面背景
    "--foreground": "#000000",    // 文字颜色
    "--border": "#e5e7eb",        // 边框颜色
    "--content1": "#ffffff",      // 卡片背景

    // 品牌色
    "--primary": "#2563eb",       // 主色
    "--primary-foreground": "#fff", // 主色上的文字
    "--secondary": "#6366f1",     // 辅色

    // 状态色
    "--danger": "#dc2626",
    "--success": "#16a34a",
    "--warning": "#d97706",

    // 每种品牌色都有 50-900 共 10 级色阶
    "--primary-50": "#eff6ff",    // 最浅
    "--primary-500": "#3b82f6",   // 中间
    "--primary-900": "#1e3a8a",   // 最深

    // 字体
    "--font-sans": '"Inter", sans-serif',
    "--font-mono": '"Fira Code", monospace',

    // 圆角
    "--radius": "0.5rem",
  },
  dark: {
    // 暗色模式下的覆盖...
  },
}
```

> **提示**: 你不需要定义所有变量，只定义你想修改的，其余沿用默认值。

### 原始 CSS 注入

`css` 字段可以注入任意 CSS。主题激活时会插入一个 `<style>` 标签，停用时自动移除。

```typescript
const theme: ThemePackage = {
  // ...
  css: `
    /* 自定义字体 */
    @import url('https://fonts.googleapis.com/css2?family=Noto+Sans+SC&display=swap');

    body {
      font-family: 'Noto Sans SC', sans-serif;
    }

    /* 自定义动画 */
    @keyframes my-fade-in {
      from { opacity: 0; transform: translateY(10px); }
      to { opacity: 1; transform: translateY(0); }
    }

    /* 给所有卡片加阴影 */
    .rounded-xl, .rounded-lg {
      box-shadow: 0 4px 24px rgba(0, 0, 0, 0.08);
    }

    /* 自定义滚动条 */
    ::-webkit-scrollbar { width: 8px; }
    ::-webkit-scrollbar-thumb {
      background: var(--primary);
      border-radius: 4px;
    }
  `,
};
```

### 组件替换

可以替换任意 UI 组件。替换组件**必须接受与原组件相同的 props**。

#### 可替换的组件列表

| 组件键名 | 原始位置 | 说明 |
|----------|----------|------|
| `Button` | `shadcn-bridge/heroui/button` | 按钮 |
| `Card`, `CardHeader`, `CardBody`, `CardFooter` | `shadcn-bridge/heroui/card` | 卡片 |
| `Input` | `shadcn-bridge/heroui/input` | 输入框 |
| `Select`, `SelectItem` | `shadcn-bridge/heroui/select` | 下拉选择 |
| `Switch` | `shadcn-bridge/heroui/switch` | 开关 |
| `Checkbox` | `shadcn-bridge/heroui/checkbox` | 复选框 |
| `Chip` | `shadcn-bridge/heroui/chip` | 标签/芯片 |
| `Modal`, `ModalContent`, `ModalHeader`, `ModalBody`, `ModalFooter` | `shadcn-bridge/heroui/modal` | 模态框 |
| `Table`, `TableHeader`, `TableBody`, `TableRow`, `TableCell`, `TableColumn` | `shadcn-bridge/heroui/table` | 表格 |
| `Tabs`, `Tab` | `shadcn-bridge/heroui/tabs` | 标签页 |
| `Progress` | `shadcn-bridge/heroui/progress` | 进度条 |
| `Spinner` | `shadcn-bridge/heroui/spinner` | 加载指示器 |
| `Divider` | `shadcn-bridge/heroui/divider` | 分割线 |
| `Link` | `shadcn-bridge/heroui/link` | 链接 |
| `Dropdown`, `DropdownTrigger`, `DropdownMenu`, `DropdownItem` | `shadcn-bridge/heroui/dropdown` | 下拉菜单 |
| `Navbar`, `NavbarContent`, `NavbarItem` | `shadcn-bridge/heroui/navbar` | 导航栏 |
| `Radio`, `RadioGroup` | `shadcn-bridge/heroui/radio` | 单选框 |
| `Accordion`, `AccordionItem` | `shadcn-bridge/heroui/accordion` | 手风琴 |
| `DatePicker` | `shadcn-bridge/heroui/date-picker` | 日期选择器 |
| `Alert` | `shadcn-bridge/heroui/alert` | 警告提示 |
| `SearchBar` | `components/search-bar` | 搜索栏 |
| `BrandLogo` | `components/brand-logo` | 品牌 Logo |
| `VersionFooter` | `components/version-footer` | 版本页脚 |

#### 组件替换示例

**方式一：包装原组件**（推荐，保证兼容性）

```typescript
// src/themes/my-theme/components/button.tsx
import React from "react";
import type { ButtonProps } from "@/shadcn-bridge/heroui/button";
import { Button as OriginalButton } from "@/shadcn-bridge/heroui/button";

export const MyButton: React.FC<ButtonProps> = (props) => {
  return (
    <OriginalButton
      {...props}
      className={`${props.className || ""} my-custom-class`}
      style={{
        ...props.style,
        borderRadius: "9999px",  // 全圆角
      }}
    />
  );
};
```

**方式二：完全重写组件**

```typescript
// src/themes/my-theme/components/button.tsx
import React from "react";
import type { ButtonProps } from "@/shadcn-bridge/heroui/button";

export const MyButton: React.FC<ButtonProps> = ({
  children,
  color = "default",
  variant = "solid",
  size = "md",
  isLoading,
  isDisabled,
  onPress,
  className,
  ...rest
}) => {
  return (
    <button
      className={`my-totally-custom-button ${className || ""}`}
      disabled={isDisabled || isLoading}
      onClick={() => onPress?.()}
      {...rest}
    >
      {isLoading && <span className="spinner" />}
      {children}
    </button>
  );
};
```

然后在主题入口中注册：

```typescript
// src/themes/my-theme/index.ts
import { MyButton } from "./components/button";

const theme: ThemePackage = {
  // ...
  components: {
    Button: MyButton,
  },
};
```

### 布局替换

可以替换 4 种布局：

| 布局键名 | 说明 |
|----------|------|
| `AdminLayout` | 管理后台主布局（侧边栏 + 顶栏） |
| `H5Layout` | 移动端布局（底部导航） |
| `H5SimpleLayout` | 移动端简洁布局（无底部导航） |
| `DefaultLayout` | 默认布局（登录页等） |

```typescript
// src/themes/my-theme/layouts/admin.tsx
import React from "react";

const MyAdminLayout: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  return (
    <div className="my-admin-layout">
      <header className="my-header">
        {/* 自定义顶栏 */}
      </header>
      <aside className="my-sidebar">
        {/* 自定义侧边栏 */}
      </aside>
      <main className="my-content">
        {children}
      </main>
    </div>
  );
};

export default MyAdminLayout;
```

### 页面替换

可以替换任意页面路由的实现：

| 页面键名 | 路由 |
|----------|------|
| `LoginPage` | `/` |
| `DashboardPage` | `/dashboard` |
| `MonitorPage` | `/monitor` |
| `ForwardPage` | `/forward` |
| `TunnelPage` | `/tunnel` |
| `NodePage` | `/node` |
| `UserPage` | `/user` |
| `GroupPage` | `/group` |
| `ProfilePage` | `/profile` |
| `LimitPage` | `/limit` |
| `ConfigPage` | `/config` |
| `PanelSharingPage` | `/panel-sharing` |
| `SettingsPage` | `/settings` |

### 生命周期钩子

```typescript
const theme: ThemePackage = {
  // ...
  onActivate: () => {
    // 主题被激活时执行
    // 例如：加载外部字体、注入全局属性
    console.log("Theme activated!");
  },
  onDeactivate: () => {
    // 主题被停用时执行
    // 例如：清理全局属性
    console.log("Theme deactivated!");
  },
};
```

---

## 主题提交流程

1. Fork 本仓库
2. 在 `src/themes/` 下创建你的主题文件夹
3. 在 `src/themes/loader.ts` 中注册
4. 提交 Pull Request

### 命名规范

- 文件夹名：`kebab-case`（如 `my-awesome-theme`）
- 主题 `id`：与文件夹名一致
- 主题 `name`：简短中文名

### 代码规范

- TypeScript 严格模式
- 组件替换必须保证 props 兼容性
- 不得修改 `src/themes/types.ts`（影响其他主题）
- 不得修改 `src/themes/registry.ts`（影响核心逻辑）
- 仅修改你自己的主题文件夹 + `loader.ts` 中的注册

---

## 文件结构参考

```
src/themes/
├── types.ts                    # 主题接口定义 (勿改)
├── registry.ts                 # 主题注册表   (勿改)
├── context.tsx                 # React Context (勿改)
├── index.ts                    # 公共 API     (勿改)
├── loader.ts                   # 主题加载器   (仅在此添加注册)
│
├── default/                    # 默认主题 (参考实现)
│   └── index.ts
│
├── example-cyberpunk/          # 示例主题 (可复制修改)
│   ├── index.ts
│   └── components/
│       └── button.tsx
│
└── your-theme/                 # 你的主题
    ├── index.ts
    ├── components/
    │   ├── button.tsx
    │   └── card.tsx
    ├── layouts/
    │   └── admin.tsx
    └── assets/
        └── logo.svg
```
