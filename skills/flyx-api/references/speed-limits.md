# Speed Limit Management API

Speed limits define bandwidth restrictions that can be applied to forwards or user-tunnel assignments.

## POST /api/v1/speed-limit/list

List all speed limit rules.

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "name": "10Mbps",
      "speed": 10,
      "status": 1,
      "created_at": 1706659200000
    },
    {
      "id": 2,
      "name": "100Mbps",
      "speed": 100,
      "status": 1,
      "created_at": 1706659200000
    }
  ]
}
```

## POST /api/v1/speed-limit/create

Create a speed limit rule.

**Request:**
```json
{
  "name": "50Mbps",
  "speed": 50,
  "status": 1
}
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | Yes | Rule name |
| speed | number | Yes | Speed limit in Mbps |
| status | number | No | 1=active, 0=disabled (default: 1) |

**Response:**
```json
{"code": 0, "msg": "success", "data": {"id": 3}}
```

## POST /api/v1/speed-limit/update

Update a speed limit rule.

**Request:**
```json
{
  "id": 3,
  "name": "50Mbps-Premium",
  "speed": 50,
  "status": 1
}
```

## POST /api/v1/speed-limit/delete

Delete a speed limit rule.

**Request:**
```json
{"id": 3}
```

## Applying Speed Limits

Speed limits can be applied at two levels:

### 1. Forward Level

Set `speedId` when creating or updating a forward:

```bash
curl -s -X POST "${FLYX_BASE_URL}/api/v1/forward/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "limited-forward",
    "tunnelId": 1,
    "remoteAddr": "10.0.0.1:80",
    "speedId": 1
  }'
```

### 2. User-Tunnel Assignment Level

Set `speedId` when assigning a tunnel to a user:

```bash
curl -s -X POST "${FLYX_BASE_URL}/api/v1/tunnel/user/assign" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "userId": 2,
    "tunnelId": 1,
    "flow": 50,
    "speedId": 2
  }'
```

## Example: Create Tiered Speed Limits

```bash
# Create speed limit tiers
curl -s -X POST "${FLYX_BASE_URL}/api/v1/speed-limit/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"Basic-10Mbps","speed":10}'

curl -s -X POST "${FLYX_BASE_URL}/api/v1/speed-limit/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"Standard-50Mbps","speed":50}'

curl -s -X POST "${FLYX_BASE_URL}/api/v1/speed-limit/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"Premium-Unlimited","speed":1000}'

# List all rules
curl -s -X POST "${FLYX_BASE_URL}/api/v1/speed-limit/list" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' | jq '.data'
```
