# Error Codes & Handling

## Response Code Field

| code | Meaning | Action |
|------|---------|--------|
| `0` | Success | Use `data` field |
| `-1` | Business error | Show `msg` to user |
| `-2` | Server/DB error | Retry or report bug |
| `401` | Unauthorized | Token expired/invalid, re-login |
| `403` | Forbidden | Need admin privileges |

## Common Error Messages (Chinese)

| msg | Cause | Solution |
|-----|-------|----------|
| 用户名或密码错误 | Wrong credentials | Check username/password |
| Token已过期 | Token expired | Re-login |
| 权限不足 | Need admin | Use admin account (role_id: 0) |
| 端口已被占用 | Port in use | Choose different port or delete conflicting forward |
| 流量不足 | Out of traffic | Contact admin or upgrade plan |
| 节点离线 | Node offline | Check node status, run install command |
| 隧道不可用 | Tunnel disabled | Enable tunnel first |
| 用户已存在 | Username taken | Choose different username |
| 参数错误 | Invalid request | Check request body format |
| 转发数量已达上限 | Forward limit reached | Delete unused forwards or contact admin |
| 该隧道未分配给当前用户 | No tunnel access | Contact admin to get tunnel assigned |

## Error Handling Pattern

### JavaScript/TypeScript

```typescript
async function callApi<T>(endpoint: string, data: object): Promise<T> {
  const res = await fetch(`${BASE_URL}${endpoint}`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Authorization": TOKEN,
    },
    body: JSON.stringify(data),
  });

  const result = await res.json();

  if (result.code === 0) {
    return result.data;
  }

  switch (result.code) {
    case 401:
      // Token expired - clear and retry
      TOKEN = null;
      throw new Error("登录已过期，请重新登录");
    case 403:
      throw new Error("权限不足，需要管理员权限");
    case -2:
      throw new Error("服务器错误，请稍后重试");
    default:
      throw new Error(result.msg || "操作失败");
  }
}
```

### Python

```python
def call_api(endpoint: str, data: dict = None) -> dict:
    global TOKEN
    
    headers = {"Content-Type": "application/json"}
    if TOKEN:
        headers["Authorization"] = TOKEN
    
    resp = requests.post(f"{BASE_URL}{endpoint}", headers=headers, json=data or {})
    result = resp.json()
    
    if result["code"] == 0:
        return result.get("data")
    
    if result["code"] == 401:
        TOKEN = None
        raise Exception("登录已过期，请重新登录")
    elif result["code"] == 403:
        raise Exception("权限不足，需要管理员权限")
    elif result["code"] == -2:
        raise Exception("服务器错误，请稍后重试")
    else:
        raise Exception(result["msg"] or "操作失败")
```

### Bash

```bash
call_api() {
  local endpoint="$1"
  local data="$2"
  
  local response
  response=$(curl -s -X POST "${FLYX_BASE_URL}${endpoint}" \
    -H "Authorization: ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$data")
  
  local code
  code=$(echo "$response" | jq -r '.code')
  
  if [ "$code" == "0" ]; then
    echo "$response" | jq '.data'
    return 0
  fi
  
  local msg
  msg=$(echo "$response" | jq -r '.msg')
  
  case "$code" in
    401) echo "Error: 登录已过期" >&2 ;;
    403) echo "Error: 权限不足" >&2 ;;
    -2)  echo "Error: 服务器错误" >&2 ;;
    *)   echo "Error: $msg" >&2 ;;
  esac
  
  return 1
}
```

## Retry Logic with Auto Re-login

```typescript
async function callApiWithRetry<T>(
  endpoint: string, 
  data: object,
  maxRetries = 1
): Promise<T> {
  let lastError: Error;
  
  for (let i = 0; i <= maxRetries; i++) {
    try {
      if (!TOKEN) {
        await login();
      }
      return await callApi<T>(endpoint, data);
    } catch (error) {
      lastError = error;
      if (error.message.includes("过期") || error.message.includes("expired")) {
        TOKEN = null;  // Force re-login on next attempt
        continue;
      }
      throw error;
    }
  }
  
  throw lastError!;
}
```

## Validation Errors

When request validation fails, the API returns code -1 with specific messages:

| Scenario | Error Message |
|----------|--------------|
| Missing required field | `参数错误` or field-specific message |
| Invalid port range | `端口范围无效` |
| Invalid IP format | `IP地址格式错误` |
| Invalid date | `时间格式错误` |
| Username too short | `用户名长度不能少于3个字符` |
| Password too weak | `密码长度不能少于6个字符` |
