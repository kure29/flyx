# Group & Permission Management API

Groups organize users and tunnels, with permissions controlling access.

## Tunnel Groups

### POST /api/v1/group/tunnel/list

List all tunnel groups.

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "name": "Premium-Tunnels",
      "status": 1,
      "tunnel_ids": [1, 2, 3],
      "created_at": 1706659200000
    }
  ]
}
```

### POST /api/v1/group/tunnel/create

Create a tunnel group.

**Request:**
```json
{
  "name": "Premium-Tunnels",
  "status": 1
}
```

### POST /api/v1/group/tunnel/update

Update tunnel group.

**Request:**
```json
{
  "id": 1,
  "name": "VIP-Tunnels",
  "status": 1
}
```

### POST /api/v1/group/tunnel/delete

Delete tunnel group.

**Request:**
```json
{"id": 1}
```

### POST /api/v1/group/tunnel/assign

Assign tunnels to a group.

**Request:**
```json
{
  "groupId": 1,
  "tunnelIds": [1, 2, 3]
}
```

---

## User Groups

### POST /api/v1/group/user/list

List all user groups.

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "name": "VIP-Users",
      "status": 1,
      "user_ids": [2, 3, 4],
      "created_at": 1706659200000
    }
  ]
}
```

### POST /api/v1/group/user/create

Create a user group.

**Request:**
```json
{
  "name": "VIP-Users",
  "status": 1
}
```

### POST /api/v1/group/user/update

Update user group.

**Request:**
```json
{
  "id": 1,
  "name": "Premium-Users",
  "status": 1
}
```

### POST /api/v1/group/user/delete

Delete user group.

**Request:**
```json
{"id": 1}
```

### POST /api/v1/group/user/assign

Assign users to a group.

**Request:**
```json
{
  "groupId": 1,
  "userIds": [2, 3, 4]
}
```

---

## Permissions

Permissions link user groups to tunnel groups, allowing users in a user group to access tunnels in a tunnel group.

### POST /api/v1/group/permission/list

List all permissions.

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": [
    {
      "id": 1,
      "user_group_id": 1,
      "user_group_name": "VIP-Users",
      "tunnel_group_id": 1,
      "tunnel_group_name": "Premium-Tunnels",
      "created_at": 1706659200000
    }
  ]
}
```

### POST /api/v1/group/permission/assign

Create a permission (grant user group access to tunnel group).

**Request:**
```json
{
  "userGroupId": 1,
  "tunnelGroupId": 1
}
```

**Response:**
```json
{"code": 0, "msg": "success", "data": {"id": 1}}
```

### POST /api/v1/group/permission/remove

Remove a permission.

**Request:**
```json
{"id": 1}
```

---

## Workflow: Set Up Group-Based Access

```bash
# 1. Create user group
curl -s -X POST "${FLYX_BASE_URL}/api/v1/group/user/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"Standard-Users"}'
# Response: {"data":{"id":1}}

# 2. Create tunnel group
curl -s -X POST "${FLYX_BASE_URL}/api/v1/group/tunnel/create" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name":"Standard-Tunnels"}'
# Response: {"data":{"id":1}}

# 3. Add tunnels to tunnel group
curl -s -X POST "${FLYX_BASE_URL}/api/v1/group/tunnel/assign" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"groupId":1,"tunnelIds":[1,2,3]}'

# 4. Add users to user group
curl -s -X POST "${FLYX_BASE_URL}/api/v1/group/user/assign" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"groupId":1,"userIds":[2,3,4]}'

# 5. Grant permission (user group -> tunnel group)
curl -s -X POST "${FLYX_BASE_URL}/api/v1/group/permission/assign" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"userGroupId":1,"tunnelGroupId":1}'
```

Now users 2, 3, 4 can access tunnels 1, 2, 3.
