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
