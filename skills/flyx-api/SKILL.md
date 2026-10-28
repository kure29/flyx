---
name: flyx-api
description: Operate FLYX traffic forwarding management system via REST API. Supports user/node/tunnel/forward management, federation clustering, and traffic monitoring. Use when user wants to manage FLYX panel programmatically or via natural language.
metadata:
  author: FLVX Team
  version: "2.1.5"
  requires_env:
    - FLYX_BASE_URL
    - FLYX_USERNAME
    - FLYX_PASSWORD
---

# FLYX API Operations

For existing installations, read `FLYX_BASE_URL`, `FLYX_USERNAME` and `FLYX_PASSWORD` first; when a variable is unset, accept its corresponding `FLVX_` legacy alias. An explicitly supplied new value takes precedence.

Operate FLYX panel through REST API. All endpoints use POST method and return JSON with `{code, msg, data, ts}` envelope.

## Supported AI Tools

| Tool | Installation | Notes |
|------|--------------|-------|
| **OpenCode** | `npm i -g @flyx/skill-api` or `ln -s . ~/.agents/skills/flyx-api` | Auto-loads from `~/.agents/skills/` |
| **OpenClaw** | Same as OpenCode | Compatible skill format |
| **Claude Code** | Copy SKILL.md to CLAUDE.md or `~/.claude/CLAUDE.md` | Uses context file instead of skills |

## Prerequisites

Set environment variables before starting:

```bash
export FLYX_BASE_URL="https://your-panel.example.com"
export FLYX_USERNAME="admin"
export FLYX_PASSWORD="your-password"
```

**Security tip:** Add to `~/.flyx/.env` and source on demand:
```bash
mkdir -p ~/.flyx && cat > ~/.flyx/.env << 'EOF'
export FLYX_BASE_URL="https://panel.example.com"
export FLYX_USERNAME="admin"
export FLYX_PASSWORD="your-password"
EOF
chmod 600 ~/.flyx/.env
source ~/.flyx/.env
```

## Authentication Flow

### Session Token Cache

- Token is cached **only for the current conversation**
- New conversation = fresh login required
- Token is NOT written to disk (security)

### Auto-Login Pattern

```
Before ANY API call:
1. Check if TOKEN is cached in current session
   ├─ Yes → Use cached token, proceed
   └─ No → 
       1. Read FLYX_USERNAME and FLYX_PASSWORD from environment
       2. POST /api/v1/user/login with credentials
       3. Cache response.data.token in session memory
       4. Proceed with original request
```

### Login Request

```bash
curl -X POST "${FLYX_BASE_URL}/api/v1/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${FLYX_USERNAME}\",\"password\":\"${FLYX_PASSWORD}\"}"
```

**Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "name": "Administrator",
    "role_id": 0,
    "requirePasswordChange": false
  },
  "ts": 1706659200000
}
```

## Authentication Rules

| Header | Value | Critical |
|--------|-------|----------|
| `Authorization` | `<jwt_token>` | ⚠️ NO "Bearer" prefix! |
| `Content-Type` | `application/json` | All requests use JSON |

## Quick Start Workflow

```
User request → Check env vars → Auto-login if needed → Call API → Return result
```

## Intent → API Mapping

| User Intent | API Endpoint | Reference |
|-------------|--------------|-----------|
| "登录" / "查看我的信息" | `/api/v1/user/package` | [auth](references/auth.md) |
| "创建用户" / "添加用户" | `/api/v1/user/create` | [users](references/users.md) |
| "查看用户列表" / "所有用户" | `/api/v1/user/list` | [users](references/users.md) |
| "重置流量" | `/api/v1/user/reset` | [users](references/users.md) |
| "添加节点" / "新建节点" | `/api/v1/node/create` | [nodes](references/nodes.md) |
| "查看节点" / "节点状态" | `/api/v1/node/list` | [nodes](references/nodes.md) |
| "安装命令" / "部署节点" | `/api/v1/node/install` | [nodes](references/nodes.md) |
| "升级节点" | `/api/v1/node/upgrade` | [nodes](references/nodes.md) |
| "创建隧道" / "新建隧道" | `/api/v1/tunnel/create` | [tunnels](references/tunnels.md) |
| "分配隧道给用户" | `/api/v1/tunnel/user/assign` | [tunnels](references/tunnels.md) |
| "创建转发" / "新建转发" / "添加转发" | `/api/v1/forward/create` | [forwards](references/forwards.md) |
| "暂停转发" | `/api/v1/forward/pause` | [forwards](references/forwards.md) |
| "恢复转发" | `/api/v1/forward/resume` | [forwards](references/forwards.md) |
| "删除转发" | `/api/v1/forward/delete` | [forwards](references/forwards.md) |
| "查看我的转发" / "转发列表" | `/api/v1/forward/list` | [forwards](references/forwards.md) |
| "查看流量" / "流量统计" | `/api/v1/forward/list` or `/api/v1/user/package` | [forwards](references/forwards.md) |
| "诊断转发" / "测试连通性" | `/api/v1/forward/diagnose` | [forwards](references/forwards.md) |
| "创建限速规则" | `/api/v1/speed-limit/create` | [speed-limits](references/speed-limits.md) |
| "联邦共享" / "节点共享" | `/api/v1/federation/share/create` | [federation](references/federation.md) |
| "导出备份" | `/api/v1/backup/export` | [backup](references/backup.md) |
| "导入备份" | `/api/v1/backup/import` | [backup](references/backup.md) |

## HTTP Request Template

### Bash/curl (with auto-login)

```bash
#!/bin/bash
BASE_URL="${FLYX_BASE_URL}"
USERNAME="${FLYX_USERNAME}"
PASSWORD="${FLYX_PASSWORD}"

# Login and get token
TOKEN=$(curl -s -X POST "${BASE_URL}/api/v1/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${USERNAME}\",\"password\":\"${PASSWORD}\"}" | jq -r '.data.token')

if [ "$TOKEN" == "null" ] || [ -z "$TOKEN" ]; then
  echo "Login failed"
  exit 1
fi

# Use token for API calls - NOTE: NO "Bearer" prefix!
curl -s -X POST "${BASE_URL}/api/v1/node/list" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.'
```

### Python (requests)

```python
import os
import requests

BASE_URL = os.environ.get("FLYX_BASE_URL")
USERNAME = os.environ.get("FLYX_USERNAME")
PASSWORD = os.environ.get("FLYX_PASSWORD")

# Login
resp = requests.post(f"{BASE_URL}/api/v1/user/login", 
    headers={"Content-Type": "application/json"},
    json={"username": USERNAME, "password": PASSWORD})
result = resp.json()
if result["code"] != 0:
    raise Exception(f"Login failed: {result['msg']}")

TOKEN = result["data"]["token"]

# Authenticated request - NO "Bearer" prefix!
headers = {
    "Content-Type": "application/json",
    "Authorization": TOKEN
}
resp = requests.post(f"{BASE_URL}/api/v1/node/list", headers=headers, json={})
print(resp.json())
```

### Node.js (fetch)

```javascript
const BASE_URL = process.env.FLYX_BASE_URL;
const USERNAME = process.env.FLYX_USERNAME;
const PASSWORD = process.env.FLYX_PASSWORD;

// Login
const loginRes = await fetch(`${BASE_URL}/api/v1/user/login`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username: USERNAME, password: PASSWORD })
});
const loginData = await loginRes.json();
if (loginData.code !== 0) throw new Error(loginData.msg);
const TOKEN = loginData.data.token;

// Authenticated request - NO "Bearer" prefix!
const res = await fetch(`${BASE_URL}/api/v1/node/list`, {
  method: 'POST',
  headers: { 
    'Content-Type': 'application/json',
    'Authorization': TOKEN
  },
  body: JSON.stringify({})
});
console.log(await res.json());
```

## Response Handling

**Success:**
```json
{"code": 0, "msg": "success", "data": {...}, "ts": 1706659200000}
```

**Error:**
```json
{"code": -1, "msg": "用户名或密码错误", "ts": 1706659200000}
```

**Pattern:**
```
1. Parse JSON response
2. If code === 0 → return data
3. If code === 401 → token expired, re-login and retry
4. If code === 403 → permission denied, need admin
5. Else → show msg to user as error message
```

## Permission Model

| role_id | Type | Access |
|---------|------|--------|
| 0 | Admin | All endpoints |
| 1 | Regular | Forward CRUD, own profile, assigned tunnels only |

Non-admin users can only see/modify their own resources.

## Module Reference

| Module | Endpoints | Reference |
|--------|-----------|-----------|
| Auth | login, captcha | [auth.md](references/auth.md) |
| Users | CRUD, reset, password | [users.md](references/users.md) |
| Nodes | CRUD, install, upgrade, status | [nodes.md](references/nodes.md) |
| Tunnels | CRUD, user assignment | [tunnels.md](references/tunnels.md) |
| Forwards | CRUD, pause/resume, diagnose | [forwards.md](references/forwards.md) |
| Groups | User/tunnel groups, permissions | [groups.md](references/groups.md) |
| Speed Limits | CRUD | [speed-limits.md](references/speed-limits.md) |
| Federation | Share, remote nodes | [federation.md](references/federation.md) |
| Backup | Export/import | [backup.md](references/backup.md) |
| Config | System settings | [config.md](references/config.md) |
| Types | TypeScript interfaces | [types.md](references/types.md) |
| Errors | Error codes | [errors.md](references/errors.md) |
| Examples | Code samples | [examples/](references/examples/) |

## Critical Rules

1. ⚠️ **NO "Bearer" prefix** - `Authorization: <token>`, NOT `Authorization: Bearer <token>`
2. **All endpoints use POST** - Including list/get operations
3. **code === 0 means success** - Any other value is an error
4. **Traffic units**: User.flow is GB, in_flow/out_flow are bytes
5. **Timestamps**: All timestamps are milliseconds since epoch
6. **Token is session-scoped**: Cache in memory only, not on disk

## Common Workflows

### Workflow 1: New User Onboarding (Admin)
```
1. POST /api/v1/user/create → Create user with traffic quota
2. POST /api/v1/tunnel/user/assign → Assign tunnels to user
3. Tell user their username/password
4. User logs in and creates forwards
```

### Workflow 2: Add New Node (Admin)
```
1. POST /api/v1/node/create → Register node in panel
2. POST /api/v1/node/install → Get install command
3. Run install command on target server
4. POST /api/v1/node/check-status → Verify node is online
```

### Workflow 3: Create Forward (Any User)
```
1. POST /api/v1/tunnel/user/tunnel → List available tunnels
2. POST /api/v1/forward/create → Create forward on chosen tunnel
3. POST /api/v1/forward/diagnose → Verify connectivity
```

### Workflow 4: Node Maintenance (Admin)
```
1. POST /api/v1/node/list → Check node statuses
2. POST /api/v1/node/releases → Check available versions
3. POST /api/v1/node/upgrade or /batch-upgrade → Upgrade nodes
4. POST /api/v1/node/rollback → Rollback if needed
```
