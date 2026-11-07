# System Configuration API

Manage system-wide settings and configurations.

## POST /api/v1/config/get

Get a single configuration by name. This endpoint is public (no auth required).

**Request:**
```json
{"name": "site_name"}
```

**Response:**
```json
{
  "code": 0,
  "data": {
    "name": "site_name",
    "value": "My FLYX Panel",
    "time": 1706659200000
  }
}
```

## POST /api/v1/config/list

List all configurations (requires authentication).

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": {
    "site_name": "My FLYX Panel",
    "site_logo": "https://example.com/logo.png",
    "site_announcement": "System maintenance scheduled",
    "captcha_enabled": "true",
    "captcha_type": "turnstile",
    "turnstile_site_key": "...",
    "default_user_flow": "100",
    "default_user_exp_days": "30"
  }
}
```

## POST /api/v1/config/update

Batch update multiple configurations (admin only).

**Request:**
```json
{
  "site_name": "New Panel Name",
  "site_announcement": "Welcome to the new panel!",
  "default_user_flow": "50"
}
```

Only include the keys you want to update.

**Response:**
```json
{"code": 0, "msg": "success"}
```

## POST /api/v1/config/update-single

Update a single configuration (admin only).

**Request:**
```json
{
  "name": "site_name",
  "value": "My Awesome Panel"
}
```

## POST /api/v1/announcement/get

Get the site announcement (public endpoint).

**Method:** GET

**Response:**
```json
{
  "code": 0,
  "data": {
    "content": "System maintenance scheduled for tonight"
  }
}
```

## POST /api/v1/announcement/update

Update the site announcement (admin only).

**Request:**
```json
{"content": "New announcement message"}
```

---

## Common Configuration Keys

| Key | Description | Example |
|-----|-------------|---------|
| `site_name` | Panel display name | `"My FLYX Panel"` |
| `site_logo` | Logo URL | `"https://example.com/logo.png"` |
| `site_announcement` | Announcement HTML | `"<p>Notice...</p>"` |
| `captcha_enabled` | Enable captcha | `"true"` or `"false"` |
| `captcha_type` | Captcha provider | `"turnstile"` or `"local"` |
| `turnstile_site_key` | Cloudflare Turnstile site key | `"0x4..."` |
| `turnstile_secret_key` | Cloudflare Turnstile secret | `"0x4..."` |
| `default_user_flow` | Default user traffic (GB) | `"100"` |
| `default_user_exp_days` | Default user expiry days | `"30"` |
| `default_user_num` | Default max forwards | `"10"` |

---

## Example: Update Panel Name and Announcement

```bash
curl -s -X POST "${FLYX_BASE_URL}/api/v1/config/update" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "site_name": "Awesome Traffic Panel",
    "site_announcement": "<strong>Welcome!</strong> New nodes added."
  }'
```

## Example: Enable Cloudflare Turnstile Captcha

```bash
curl -s -X POST "${FLYX_BASE_URL}/api/v1/config/update" \
  -H "Authorization: ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "captcha_enabled": "true",
    "captcha_type": "turnstile",
    "turnstile_site_key": "0x4AAAAAAAAjq0JN9YQg",
    "turnstile_secret_key": "0x4AAAAAAAAjq0JN9YQg_secret"
  }'
```
