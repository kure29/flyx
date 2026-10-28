# User Management API

All user management endpoints require admin privileges (role_id: 0).

## POST /api/v1/user/list

List all users with pagination and filtering.

**Request:**
```json
{
  "page": 1,
  "pageSize": 20,
  "keyword": "search-term"
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
        "user": "admin",
        "name": "Administrator",
        "role_id": 0,
        "status": 1,
        "flow": 1000,
        "in_flow": 10737418240,
        "out_flow": 21474836480,
        "exp_time": 1767225600000,
        "flow_reset_time": 1,
        "created_at": 1706659200000,
        "updated_at": 1706659200000
      }
    ],
    "total": 1
  }
}
```

## POST /api/v1/user/create

Create a new user.

**Request:**
```json
{
  "user": "username",
  "pwd": "password",
  "name": "Display Name",
  "status": 1,
  "flow": 100,
  "num": 10,
  "expTime": 1767225600000,
  "flowResetTime": 1,
  "groupIds": [1, 2]
}
```

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| user | string | Yes | Username (unique) |
| pwd | string | Yes | Password |
| name | string | No | Display name |
| status | number | No | 1=active, 0=disabled (default: 1) |
| flow | number | No | Traffic quota in GB (default: 0) |
| num | number | No | Max forwards allowed (default: 0 = unlimited) |
| expTime | number | No | Expiry timestamp in ms (0 = never) |
| flowResetTime | number | No | Monthly reset day 1-28 (0 = no reset) |
| groupIds | number[] | No | User group IDs to assign |

**Response:**
```json
{"code": 0, "msg": "success", "data": {"id": 2}}
```

## POST /api/v1/user/update

Update user details.

**Request:**
```json
{
  "id": 2,
  "user": "new-username",
  "pwd": "new-password",
  "name": "New Name",
  "status": 1,
  "flow": 200,
  "num": 20,
  "expTime": 1767225600000,
  "flowResetTime": 15,
  "groupIds": [1]
}
```

Note: `pwd` is optional for updates. If omitted, password remains unchanged.

## POST /api/v1/user/delete

Delete a user (cascades to forwards and tunnel assignments).

**Request:**
```json
{"id": 2}
```

**Response:**
```json
{"code": 0, "msg": "success"}
```

## POST /api/v1/user/reset

Reset user traffic at user or tunnel level.

**Request (User level):**
```json
{
  "id": 2,
  "type": "user"
}
```

**Request (Tunnel level):**
```json
{
  "id": 2,
  "type": "tunnel",
  "tunnelId": 1
}
```

**Response:**
```json
{"code": 0, "msg": "success"}
```

## POST /api/v1/user/groups

Get groups a user belongs to.

**Request:**
```json
{"id": 2}
```

**Response:**
```json
{
  "code": 0,
  "data": [
    {"id": 1, "name": "VIP Users"}
  ]
}
```

## Traffic Units

| Field | Unit | Conversion |
|-------|------|------------|
| flow | GB | Gigabytes |
| in_flow | Bytes | Divide by 1,073,741,824 for GB |
| out_flow | Bytes | Divide by 1,073,741,824 for GB |

## Example: Create User with 50GB Quota

```bash
curl -X POST "${FLYX_BASE_URL}/api/v1/user/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "user": "alice",
    "pwd": "SecurePass123!",
    "name": "Alice",
    "status": 1,
    "flow": 50,
    "num": 5,
    "expTime": 1735689600000,
    "flowResetTime": 1
  }'
```
