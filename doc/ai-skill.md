# AI Skill 使用指南

让大模型直接操作 FLYX 面板的技能包。支持 OpenCode、OpenClaw、Claude Code 等工具。

## 安装

### 方式 1: npm (推荐)

```bash
npm install -g @flyx/skill-api
```

postinstall 脚本会自动链接到 `~/.agents/skills/flyx-api/`。

### 方式 2: 手动链接

```bash
# 从 FLYX 源码
cd /path/to/flyx
mkdir -p ~/.agents/skills
ln -sf $(pwd)/skills/flyx-api ~/.agents/skills/

# 或从 GitHub
git clone https://github.com/kure29/flyx.git
cd flyx
ln -sf $(pwd)/skills/flyx-api ~/.agents/skills/
```

## 配置

设置环境变量：

```bash
export FLYX_BASE_URL="https://your-panel.example.com"
export FLYX_USERNAME="admin"
export FLYX_PASSWORD="your-password"
```

或使用凭证文件：

```bash
mkdir -p ~/.flyx
cat > ~/.flyx/.env << 'EOF'
export FLYX_BASE_URL="https://panel.example.com"
export FLYX_USERNAME="admin"
export FLYX_PASSWORD="your-password"
EOF
chmod 600 ~/.flyx/.env
source ~/.flyx/.env
```

---

## 工具接入方法

### OpenCode

OpenCode 是命令行 AI 编程助手，支持通过 skills 扩展能力。

**安装 skill:**
```bash
npm install -g @flyx/skill-api
```

**使用:**
```bash
export FLYX_BASE_URL="https://panel.example.com"
export FLYX_USERNAME="admin"
export FLYX_PASSWORD="your-password"

opencode
```

**示例对话:**
```
你: 查看我的转发列表
你: 创建一个转发到 192.168.1.100:80 使用隧道 1
你: 检查服务器状态
你: 查看流量使用情况
```

---

### OpenClaw

OpenClaw 同样支持 skills 机制。

**安装 skill:**
```bash
npm install -g @flyx/skill-api

# 或手动链接
mkdir -p ~/.openclaw/skills
ln -sf /path/to/flyx/skills/flyx-api ~/.openclaw/skills/flyx-api
```

**使用:**
```bash
openclaw

>>> 查看所有服务器状态
>>> 给用户 alice 分配 50GB 流量
>>> 导出系统备份
```

---

### Claude Code

Claude Code 是 Anthropic 官方的命令行工具，支持通过 CLAUDE.md 扩展。

#### 方式 1: 项目级 CLAUDE.md

在项目根目录创建 `CLAUDE.md`：

```markdown
# FLYX API Skill

你可以通过 REST API 操作 FLYX 面板。

## 环境变量
- FLYX_BASE_URL: 面板地址
- FLYX_USERNAME: 用户名
- FLYX_PASSWORD: 密码

## 认证规则
- Authorization 头使用原始 JWT token，不加 "Bearer " 前缀
- 所有 API 使用 POST 方法

## 常用 API

### 登录获取 token
POST /api/v1/user/login
{"username": "...", "password": "..."}

### 查看转发列表
POST /api/v1/forward/list
Authorization: <token>
{}

### 创建转发
POST /api/v1/forward/create
{"name": "xxx", "tunnelId": 1, "remoteAddr": "1.2.3.4:80"}

### 查看服务器
POST /api/v1/node/list
{}
```

**使用:**
```bash
cd /path/to/your/project
claude
```

#### 方式 2: 全局 CLAUDE.md

```bash
mkdir -p ~/.claude
cat > ~/.claude/CLAUDE.md << 'EOF'
# FLYX Panel Operations

使用 FLYX REST API 操作流量转发面板。

环境变量: FLYX_BASE_URL, FLYX_USERNAME, FLYX_PASSWORD
调用方式: curl -X POST "$FLYX_BASE_URL/api/v1/..." -H "Authorization: $TOKEN"
注意: Authorization 不要加 Bearer 前缀
EOF
```

#### 方式 3: 复制 SKILL.md

```bash
cat ~/.agents/skills/flyx-api/SKILL.md >> ~/.claude/CLAUDE.md
```

**示例对话:**
```
>>> 帮我查看 FLYX 面板上有哪些服务器
>>> 创建一个名为 test 的转发，目标地址 10.0.0.1:80
>>> 查看我的流量使用情况
```

---

## API 覆盖

| 模块 | 操作 |
|------|------|
| 认证 | 登录、Token 管理 |
| 用户 | 增删改查、流量重置、密码 |
| 服务器 | 增删改查、安装、升级、状态 |
| 隧道 | 增删改查、用户分配 |
| 转发 | 增删改查、暂停/恢复、诊断 |
| 分组 | 用户/隧道分组、权限 |
| 限速 | 增删改查 |
| 联邦 | 服务器共享、远程服务器 |
| 备份 | 导出/导入 |

## 安全提示

- ⚠️ 环境变量在进程列表中可见
- 使用 `~/.flyx/.env` 文件并设置 `chmod 600`
- 添加 `export HISTIGNORE="*FLYX_PASSWORD*"` 防止密码进入历史记录
- Token 仅在会话内存中缓存，不写入磁盘

## 发布

维护者可通过以下方式发布新版本：

```bash
# 方式 1: 推送 tag
git tag skill-v2.1.6
git push --tags

# 方式 2: GitHub Actions 手动触发
# 在 Actions 页面运行 publish-skill workflow
```

需要在 GitHub 仓库设置 `NPM_TOKEN` secret。

## 从旧命名迁移

新包为 `@flyx/skill-api`，安装目录为 `~/.agents/skills/flyx-api`。旧仓库路径 `skills/flvx-api` 保留为兼容链接；新包安装程序只清理能确认属于本项目的旧链接，保留用户创建的目录和其他 Skill。

配置建议改为 `FLYX_BASE_URL`、`FLYX_USERNAME`、`FLYX_PASSWORD`；Skill 仍接受对应的 `FLVX_*` 变量作为回退。已有 `~/.flvx/.env` 可以继续使用，不会被安装程序移动或删除。

发布新包前，维护者需确认 npm 账号有 `@flyx` scope 的发布权限，并在旧包页面提供迁移说明。
