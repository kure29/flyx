# Federation / Clustering API

Federation allows sharing nodes between FLYX panels. One panel can share nodes, and another panel can use them as remote nodes.

## Share Management (Admin)

### POST /api/v1/federation/share/list

List all peer shares.

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "name": "Share-to-Partner",
      "node_id": 1,
      "node_name": "HK-Node-1",
      "token": "share-token-abc123",
      "max_bandwidth": 107374182400,
      "expiry_time": 1767225600000,
      "port_range_start": 10000,
      "port_range_end": 20000,
      "allowed_domains": "example.com,api.example.com",
      "allowed_ips": "10.0.0.0/8,192.168.0.0/16",
      "status": 1,
      "created_at": 1706659200000
    }
  ]
}
```

### POST /api/v1/federation/share/create

Create a peer share (share a node with another panel).

**Request:**
```json
{
  "name": "Share-to-Partner",
  "nodeId": 1,
  "maxBandwidth": 107374182400,
  "expiryTime": 1767225600000,
  "portRangeStart": 10000,
  "portRangeEnd": 20000,
  "allowedDomains": "example.com,api.example.com",
  "allowedIps": "10.0.0.0/8,192.168.0.0/16"
}
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | Yes | Share name |
| nodeId | number | Yes | Node to share |
| maxBandwidth | number | No | Max traffic in bytes (0 = unlimited) |
| expiryTime | number | No | Expiry timestamp in ms (0 = never) |
| portRangeStart | number | No | Allowed port range start |
| portRangeEnd | number | No | Allowed port range end |
| allowedDomains | string | No | Comma-separated domains |
| allowedIps | string | No | Comma-separated IPs/CIDRs |

**Response:**
```json
{
  "code": 0,
  "data": {
    "id": 1,
    "token": "share-token-abc123"
  }
}
```

The `token` is what the remote panel uses to connect.

### POST /api/v1/federation/share/update

Update a peer share.

**Request:** Same as create, with `id` field required.

### POST /api/v1/federation/share/delete

Delete a peer share.

**Request:**
```json
{"id": 1}
```

### POST /api/v1/federation/share/reset-flow

Reset traffic counter for a share.

**Request:**
```json
{"id": 1}
```

### POST /api/v1/federation/share/remote-usage/list

List remote node usage statistics.

**Request:** `{}`

---

## Federation Runtime (Peer-to-Peer)

These endpoints use **Bearer token authentication** (different from JWT).

### POST /api/v1/federation/connect

Connect to a remote panel and get share info.

**Headers:**
```
Authorization: Bearer <share-token>
```

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": {
    "nodeName": "HK-Node-1",
    "allowedPorts": [10000, 20000],
    "allowedDomains": ["example.com"],
    "allowedIps": ["10.0.0.0/8"]
  }
}
```

### POST /api/v1/federation/tunnel/create

Create a federation tunnel on the remote node.

**Headers:**
```
Authorization: Bearer <share-token>
```

**Request:**
```json
{
  "tunnelId": 1,
  "role": "entry"
}
```

### POST /api/v1/federation/runtime/reserve-port

Reserve a port on the remote node.

**Request:**
```json
{
  "port": 15000,
  "tunnelId": 1
}
```

### POST /api/v1/federation/runtime/apply-role

Apply for a role (entry/chain/exit) on the remote node.

**Request:**
```json
{
  "tunnelId": 1,
  "role": "exit"
}
```

### POST /api/v1/federation/runtime/release-role

Release a role on the remote node.

**Request:**
```json
{
  "tunnelId": 1
}
```

### POST /api/v1/federation/runtime/diagnose

TCP ping diagnostics from remote node to target.

**Request:**
```json
{
  "target": "10.0.0.1:80"
}
```

### POST /api/v1/federation/runtime/command

Execute a command on the remote node.

**Request:**
```json
{
  "command": "status"
}
```

---

## Node Import (Admin)

### POST /api/v1/federation/node/import

Import a remote node from another panel.

**Request:**
```json
{
  "name": "Remote-HK-Node",
  "remoteUrl": "https://other-panel.example.com",
  "remoteToken": "share-token-abc123"
}
```

This creates a node with `is_remote: 1`.

---

## Workflow: Share Node with Another Panel

**On the sharing panel (Panel A):**

```bash
# 1. Create a share
SHARE_RESP=$(curl -s -X POST "${FLYX_BASE_URL}/api/v1/federation/share/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Share-HK-Node",
    "nodeId": 1,
    "portRangeStart": 10000,
    "portRangeEnd": 20000,
    "allowedIps": "0.0.0.0/0"
  }')

SHARE_TOKEN=$(echo "$SHARE_RESP" | jq -r '.data.token')
echo "Share Token: $SHARE_TOKEN"
echo "Panel URL: ${FLYX_BASE_URL}"
```

**On the receiving panel (Panel B):**

```bash
# 2. Import the remote node
curl -s -X POST "${FLYX_BASE_URL}/api/v1/federation/node/import" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Remote-HK-Node",
    "remoteUrl": "https://panel-a.example.com",
    "remoteToken": "share-token-abc123"
  }'

# 3. Use the remote node in tunnels like a local node
curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Federated-Tunnel",
    "type": 1,
    "inNodeId": [1],
    "outNodeId": [2]
  }'
```
