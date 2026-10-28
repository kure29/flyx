# Tunnel Management API

Tunnels define the forwarding path: entry node(s) → (chain nodes) → exit node(s).

## POST /api/v1/tunnel/list

List all tunnels.

**Request:**
```json
{
  "page": 1,
  "pageSize": 20,
  "keyword": ""
}
```

**Response:**
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 1,
        "name": "HK-US-Tunnel",
        "type": 1,
        "protocol": "tcp",
        "flow": 1,
        "traffic_ratio": 1,
        "status": 1,
        "ip_preference": "ipv4",
        "in_ip": "",
        "in_node_id": [1],
        "chain_node_id": [],
        "out_node_id": [2],
        "created_at": 1706659200000
      }
    ],
    "total": 1
  }
}
```

## POST /api/v1/tunnel/get

Get a single tunnel by ID.

**Request:**
```json
{"id": 1}
```

## POST /api/v1/tunnel/create

Create a new tunnel.

**Request:**
```json
{
  "name": "JP-SG-Tunnel",
  "type": 1,
  "flow": 1,
  "trafficRatio": 1,
  "status": 1,
  "ipPreference": "ipv4",
  "inIp": "",
  "inNodeId": [3],
  "chainNodeId": [],
  "outNodeId": [4]
}
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | Yes | Tunnel name |
| type | number | Yes | 1=port forward, 2=tunnel forward |
| flow | number | No | Traffic multiplier (default: 1) |
| trafficRatio | number | No | Traffic ratio (default: 1) |
| status | number | No | 1=active, 0=disabled (default: 1) |
| ipPreference | string | No | "ipv4", "ipv6", or "" (both) |
| inIp | string | No | Custom entry IP |
| inNodeId | number[] | Yes | Entry node IDs |
| chainNodeId | number[] | No | Chain/relay node IDs |
| outNodeId | number[] | Yes | Exit node IDs |

**Tunnel Types:**
- `1` = Port Forward: Simple port-to-port forwarding
- `2` = Tunnel Forward: Multi-hop tunnel forwarding

**Response:**
```json
{"code": 0, "msg": "success", "data": {"id": 2}}
```

## POST /api/v1/tunnel/update

Update tunnel configuration.

**Request:** Same as create, with `id` field required.

## POST /api/v1/tunnel/delete

Delete a tunnel.

**Request:**
```json
{"id": 2}
```

## POST /api/v1/tunnel/batch-delete

Delete multiple tunnels.

**Request:**
```json
{"ids": [2, 3]}
```

## POST /api/v1/tunnel/diagnose

Diagnose tunnel connectivity.

**Request:**
```json
{"id": 1}
```

**Response:**
```json
{
  "code": 0,
  "data": {
    "reachable": true,
    "latency_ms": 25,
    "path": ["entry-node", "exit-node"],
    "error": ""
  }
}
```

## POST /api/v1/tunnel/update-order

Reorder tunnels.

**Request:**
```json
{
  "orders": [
    {"id": 1, "order": 0},
    {"id": 2, "order": 1}
  ]
}
```

## POST /api/v1/tunnel/batch-redeploy

Redeploy multiple tunnels (recreate forwarding services).

**Request:**
```json
{"ids": [1, 2, 3]}
```

---

## User-Tunnel Assignment

These endpoints manage which users can use which tunnels.

### POST /api/v1/tunnel/user/tunnel

List tunnels visible to the current user (or all tunnels for admin).

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "name": "HK-US-Tunnel",
      "type": 1,
      "status": 1,
      "in_node_name": "HK-Node-1",
      "out_node_name": "US-Node-1"
    }
  ]
}
```

### POST /api/v1/tunnel/user/list

List user-tunnel assignments (admin only).

**Request:**
```json
{
  "page": 1,
  "pageSize": 20,
  "userId": 2
}
```

**Response:**
```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 1,
        "user_id": 2,
        "tunnel_id": 1,
        "tunnel_name": "HK-US-Tunnel",
        "flow": 50,
        "in_flow": 1073741824,
        "out_flow": 2147483648,
        "exp_time": 0,
        "speed_id": 0
      }
    ],
    "total": 1
  }
}
```

### POST /api/v1/tunnel/user/assign

Assign a tunnel to a user.

**Request:**
```json
{
  "userId": 2,
  "tunnelId": 1,
  "flow": 50,
  "expTime": 0,
  "speedId": 0
}
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| userId | number | Yes | User ID |
| tunnelId | number | Yes | Tunnel ID |
| flow | number | No | Traffic quota for this tunnel in GB |
| expTime | number | No | Expiry for this assignment (ms, 0=never) |
| speedId | number | No | Speed limit rule ID |

### POST /api/v1/tunnel/user/batch-assign

Batch assign tunnels to a user.

**Request:**
```json
{
  "userId": 2,
  "tunnelIds": [1, 2, 3],
  "flow": 50,
  "expTime": 0
}
```

### POST /api/v1/tunnel/user/remove

Remove a tunnel from a user.

**Request:**
```json
{
  "userId": 2,
  "tunnelId": 1
}
```

### POST /api/v1/tunnel/user/update

Update user-tunnel assignment settings.

**Request:**
```json
{
  "id": 1,
  "flow": 100,
  "expTime": 1767225600000,
  "speedId": 2
}
```

## Example: Assign Tunnel to User

```bash
# 1. Create tunnel
TUNNEL_RESP=$(curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test-Tunnel","type":1,"inNodeId":[1],"outNodeId":[2]}')

TUNNEL_ID=$(echo "$TUNNEL_RESP" | jq -r '.data.id')

# 2. Assign to user with 30GB quota
curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/user/assign" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "{\"userId\":2,\"tunnelId\":${TUNNEL_ID},\"flow\":30}"
```
