# Forward Management API

Forwards are port forwarding rules created by users on their assigned tunnels.

## POST /api/v1/forward/list

List forwards. Non-admin users see only their own forwards.

**Request:**
```json
{
  "page": 1,
  "pageSize": 20,
  "keyword": "",
  "status": -1
}
```

**status filter:**
- `-1` = All
- `0` = Paused
- `1` = Running

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
        "name": "my-web-server",
        "in_port": 10001,
        "remote_addr": "192.168.1.100:80",
        "strategy": "fifo",
        "status": 1,
        "speed_id": 0,
        "speed_name": "",
        "in_flow": 1073741824,
        "out_flow": 2147483648,
        "created_at": 1706659200000,
        "updated_at": 1706659200000
      }
    ],
    "total": 1
  }
}
```

## POST /api/v1/forward/create

Create a new forward.

**Request:**
```json
{
  "name": "my-web-server",
  "tunnelId": 1,
  "remoteAddr": "192.168.1.100:80",
  "strategy": "fifo",
  "inPort": 0,
  "speedId": 0
}
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | Yes | Forward name |
| tunnelId | number | Yes | Tunnel to use |
| remoteAddr | string | Yes | Target address(es), comma-separated for load balancing |
| strategy | string | No | "fifo" or "round" (default: "fifo") |
| inPort | number | No | Entry port (0 = auto-assign) |
| speedId | number | No | Speed limit rule ID (0 = no limit) |

**Strategy:**
- `fifo` = First target only
- `round` = Round-robin load balancing across targets

**Remote Address Format:**
- Single: `192.168.1.100:80`
- Multiple: `192.168.1.100:80,192.168.1.101:80,192.168.1.102:80`

**Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "id": 1,
    "in_port": 10001
  }
}
```

## POST /api/v1/forward/update

Update forward settings.

**Request:** Same as create, with `id` field required.

```json
{
  "id": 1,
  "name": "my-web-server-updated",
  "remoteAddr": "192.168.1.100:8080",
  "strategy": "round",
  "speedId": 2
}
```

## POST /api/v1/forward/delete

Delete a forward.

**Request:**
```json
{"id": 1}
```

## POST /api/v1/forward/force-delete

Force delete a forward (even if in use).

**Request:**
```json
{"id": 1}
```

## POST /api/v1/forward/pause

Pause a forward (stops traffic but keeps configuration).

**Request:**
```json
{"id": 1}
```

**Response:**
```json
{"code": 0, "msg": "success"}
```

## POST /api/v1/forward/resume

Resume a paused forward.

**Request:**
```json
{"id": 1}
```

## POST /api/v1/forward/diagnose

Diagnose forward connectivity (TCP ping to target).

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
    "latency_ms": 15,
    "error": ""
  }
}
```

## POST /api/v1/forward/update-order

Reorder forwards.

**Request:**
```json
{
  "orders": [
    {"id": 1, "order": 0},
    {"id": 2, "order": 1}
  ]
}
```

## Batch Operations

### POST /api/v1/forward/batch-delete

```json
{"ids": [1, 2, 3]}
```

### POST /api/v1/forward/batch-pause

```json
{"ids": [1, 2, 3]}
```

### POST /api/v1/forward/batch-resume

```json
{"ids": [1, 2, 3]}
```

### POST /api/v1/forward/batch-redeploy

Recreate forwarding services on nodes.

```json
{"ids": [1, 2, 3]}
```

### POST /api/v1/forward/batch-change-tunnel

Move forwards to a different tunnel.

```json
{
  "ids": [1, 2, 3],
  "tunnelId": 5
}
```

## Traffic Units

| Field | Unit | Notes |
|-------|------|-------|
| in_flow | Bytes | Upload traffic |
| out_flow | Bytes | Download traffic |

Convert to GB: `in_flow / 1073741824`

## Example: Create Forward with Load Balancing

```bash
# Create forward with 3 backend servers
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "web-cluster",
    "tunnelId": 1,
    "remoteAddr": "10.0.0.1:80,10.0.0.2:80,10.0.0.3:80",
    "strategy": "round"
  }'
```

## Example: Check Forward Status and Traffic

```bash
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/list" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.data.list[] | {
    name,
    tunnel: .tunnel_name,
    entry_port: .in_port,
    target: .remote_addr,
    status: (if .status == 1 then "running" else "paused" end),
    upload_gb: (.in_flow / 1073741824 | floor),
    download_gb: (.out_flow / 1073741824 | floor)
  }'
```
