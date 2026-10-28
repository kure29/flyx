# curl Examples

Quick reference for common operations using curl.

## Setup

```bash
# Set environment variables
export FLYX_BASE_URL="https://your-panel.example.com"
export FLYX_USERNAME="admin"
export FLYX_PASSWORD="your-password"

# Login and save token
TOKEN=$(curl -s -X POST "${FLYX_BASE_URL}/api/v1/user/login" \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"${FLYX_USERNAME}\",\"password\":\"${FLYX_PASSWORD}\"}" \
  | jq -r '.data.token')

echo "Token: ${TOKEN:0:20}..."
```

## User Operations

```bash
# Get my package info
curl -s -X POST "${FLYX_BASE_URL}/api/v1/user/package" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.'

# List all users (admin)
curl -s -X POST "${FLYX_BASE_URL}/api/v1/user/list" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"page":1,"pageSize":20}' | jq '.'

# Create user (admin)
curl -s -X POST "${FLYX_BASE_URL}/api/v1/user/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "user": "alice",
    "pwd": "SecurePass123!",
    "name": "Alice",
    "flow": 50,
    "num": 10,
    "expTime": 1767225600000
  }' | jq '.'

# Reset user traffic
curl -s -X POST "${FLYX_BASE_URL}/api/v1/user/reset" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":2,"type":"user"}' | jq '.'

# Delete user
curl -s -X POST "${FLYX_BASE_URL}/api/v1/user/delete" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":2}' | jq '.'
```

## Node Operations

```bash
# List nodes with status
curl -s -X POST "${FLYX_BASE_URL}/api/v1/node/list" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.data.list[] | {name, status: (.status == 1), ip: .server_ip}'

# Create node
curl -s -X POST "${FLYX_BASE_URL}/api/v1/node/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"US-Node-1","serverIp":"203.0.113.10"}' | jq '.'

# Get install command
curl -s -X POST "${FLYX_BASE_URL}/api/v1/node/install" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":2}' | jq -r '.data.command'

# Check node status
curl -s -X POST "${FLYX_BASE_URL}/api/v1/node/check-status" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.'

# Upgrade node
curl -s -X POST "${FLYX_BASE_URL}/api/v1/node/upgrade" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":2,"version":"2.1.5"}' | jq '.'

# Delete node
curl -s -X POST "${FLYX_BASE_URL}/api/v1/node/delete" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":2}' | jq '.'
```

## Tunnel Operations

```bash
# List tunnels
curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/list" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.data.list[] | {id, name, status}'

# Create tunnel
curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "HK-US-Tunnel",
    "type": 1,
    "inNodeId": [1],
    "outNodeId": [2]
  }' | jq '.'

# Assign tunnel to user
curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/user/assign" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"userId":2,"tunnelId":1,"flow":30}' | jq '.'

# Get available tunnels (for current user)
curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/user/tunnel" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.'
```

## Forward Operations

```bash
# List forwards with traffic
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/list" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.data.list[] | {
    name,
    tunnel: .tunnel_name,
    port: .in_port,
    target: .remote_addr,
    status: (if .status == 1 then "running" else "paused" end),
    upload_gb: ((.in_flow / 1073741824) | floor),
    download_gb: ((.out_flow / 1073741824) | floor)
  }'

# Create forward
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-web-server",
    "tunnelId": 1,
    "remoteAddr": "192.168.1.100:80",
    "strategy": "fifo"
  }' | jq '.'

# Create forward with load balancing
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "web-cluster",
    "tunnelId": 1,
    "remoteAddr": "10.0.0.1:80,10.0.0.2:80,10.0.0.3:80",
    "strategy": "round"
  }' | jq '.'

# Pause forward
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/pause" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":1}' | jq '.'

# Resume forward
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/resume" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":1}' | jq '.'

# Diagnose forward
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/diagnose" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":1}' | jq '.'

# Delete forward
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/delete" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"id":1}' | jq '.'

# Batch pause forwards
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/batch-pause" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"ids":[1,2,3]}' | jq '.'
```

## Backup Operations

```bash
# Export all data
curl -s -X POST "${FLYX_BASE_URL}/api/v1/backup/export" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' > backup-$(date +%Y%m%d).json

# Export specific types
curl -s -X POST "${FLYX_BASE_URL}/api/v1/backup/export" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"types":["users","tunnels"]}' > partial-backup.json

# Import backup
curl -s -X POST "${FLYX_BASE_URL}/api/v1/backup/import" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d @backup-20260226.json | jq '.'
```

## Helper Functions

```bash
# Add to ~/.bashrc or ~/.zshrc

flyx-login() {
  export FLYX_BASE_URL="${1:-$FLYX_BASE_URL}"
  TOKEN=$(curl -s -X POST "${FLYX_BASE_URL}/api/v1/user/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${FLYX_USERNAME}\",\"password\":\"${FLYX_PASSWORD}\"}" \
    | jq -r '.data.token')
  export FLYX_TOKEN="$TOKEN"
  echo "Logged in. Token: ${TOKEN:0:20}..."
}

flyx-api() {
  local endpoint="$1"
  local data="${2:-{}}"
  curl -s -X POST "${FLYX_BASE_URL}${endpoint}" \
    -H "Authorization: ${FLYX_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$data" | jq '.'
}

# Usage:
# flyx-login
# flyx-api /api/v1/node/list
# flyx-api /api/v1/forward/list '{"keyword":"web"}'
```
