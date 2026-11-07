# Backup & Restore API

Export and import system data for backup, migration, or disaster recovery.

## POST /api/v1/backup/export

Export system data.

**Request:**
```json
{
  "types": ["users", "nodes", "tunnels", "forwards", "speed_limits", "groups"]
}
```

If `types` is empty or omitted, exports all data.

**Available types:**
- `users` - User accounts
- `nodes` - Node configurations
- `tunnels` - Tunnel configurations
- `forwards` - Forward rules
- `speed_limits` - Speed limit rules
- `groups` - User/tunnel groups and permissions
- `configs` - System configurations

**Response:**
```json
{
  "code": 0,
  "data": {
    "version": "2.1.5",
    "exportedAt": 1706659200000,
    "types": ["users", "nodes", "tunnels"],
    "users": [...],
    "nodes": [...],
    "tunnels": [...],
    "forwards": [...],
    "speedLimits": [...],
    "tunnelGroups": [...],
    "userGroups": [...],
    "groupPermissions": [...],
    "configs": {...}
  }
}
```

## POST /api/v1/backup/import

Import system data from a backup.

**Request:**
```json
{
  "version": "2.1.5",
  "exportedAt": 1706659200000,
  "types": ["users", "nodes"],
  "users": [...],
  "nodes": [...]
}
```

**Import Behavior:**
- Existing records are updated if IDs match
- New records are created for non-existent IDs
- Related entities must be included (e.g., forwards require tunnels)

**Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "imported": {
      "users": 5,
      "nodes": 3,
      "tunnels": 10
    },
    "skipped": {
      "forwards": 2
    }
  }
}
```

## POST /api/v1/backup/restore

Alias for `/api/v1/backup/import`.

---

## Workflow: Full System Backup

```bash
# Export all data
curl -s -X POST "${FLYX_BASE_URL}/api/v1/backup/export" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' > backup-$(date +%Y%m%d).json

echo "Backup saved to backup-$(date +%Y%m%d).json"
```

## Workflow: Partial Export

```bash
# Export only users and tunnels
curl -s -X POST "${FLYX_BASE_URL}/api/v1/backup/export" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"types":["users","tunnels"]}' > partial-backup.json
```

## Workflow: Restore from Backup

```bash
# Import from backup file
curl -s -X POST "${FLYX_BASE_URL}/api/v1/backup/import" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d @backup-20260226.json | jq '.'
```

## Workflow: Migrate to New Panel

```bash
# On source panel
curl -s -X POST "${SOURCE_URL}/api/v1/backup/export" \
  -H "Authorization: ${SOURCE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}' > migration.json

# On target panel
curl -s -X POST "${TARGET_URL}/api/v1/backup/import" \
  -H "Authorization: ${TARGET_TOKEN}" \
  -H "Content-Type: application/json" \
  -d @migration.json
```

**Note:** After migration, you may need to:
1. Reinstall node agents with new panel URL
2. Update node secrets if they differ
3. Reassign federation tokens
