# Authentication API

## POST /api/v1/user/login

Authenticate and obtain JWT token.

**Request:**
```json
{
  "username": "admin",
  "password": "secret",
  "captchaId": "optional-captcha-id"
}
```

**Response:**
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "name": "Administrator",
    "role_id": 0,
    "requirePasswordChange": false
  },
  "ts": 1706659200000
}
```

**Response Fields:**

| Field | Type | Description |
|-------|------|-------------|
| token | string | JWT token for subsequent requests |
| name | string | User's display name |
| role_id | number | Always 0: the panel has only the admin |
| requirePasswordChange | boolean | Whether password change is required |

## JWT Token Details

**Algorithm:** HMAC-SHA256
**Lifetime:** 90 days

**Token Claims:**
```json
{
  "sub": "1",
  "user": "admin",
  "name": "Administrator",
  "role_id": 0,
  "iat": 1706659200,
  "exp": 1738195200
}
```

## POST /api/v1/captcha/check

Check if captcha verification is required.

**Request:** `{}`

**Response:**
```json
{
  "code": 0,
  "data": {
    "enabled": true,
    "type": "turnstile"
  }
}
```

## POST /api/v1/captcha/verify

Verify captcha response (Cloudflare Turnstile or local captcha).

**Request:**
```json
{
  "captchaId": "captcha-session-id",
  "captchaValue": "user-captcha-response"
}
```

## Token Usage

Include the token in all authenticated requests:

```bash
curl -X POST "${FLYX_BASE_URL}/api/v1/node/list" \
  -H "Authorization: eyJhbGciOiJIUzI1NiIs..." \
  -H "Content-Type: application/json" \
  -d '{}'
```

⚠️ **CRITICAL: Do NOT add "Bearer " prefix!**

```
✅ Correct:   Authorization: eyJhbGciOiJIUzI1NiIs...
❌ Incorrect: Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

## POST /api/v1/user/updatePassword

Change current user's password.

**Request:**
```json
{
  "oldPassword": "current-password",
  "newPassword": "new-password"
}
```

**Response:**
```json
{"code": 0, "msg": "success"}
```
