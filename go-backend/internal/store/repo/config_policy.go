package repo

import "strings"

type ConfigAccessPolicy string

const (
	ConfigAccessPublic    ConfigAccessPolicy = "public"
	ConfigAccessSensitive ConfigAccessPolicy = "sensitive"
)

var publicConfigKeys = map[string]struct{}{
	"app_name":            {},
	"app_logo":            {},
	"app_favicon":         {},
	"app_bg_image":        {},
	"app_bg_image_light":  {},
	"app_bg_image_dark":   {},
	"cloudflare_site_key": {},
	"is_commercial":       {},
	"hide_footer_brand":   {},
	"status_page_enabled": {},
}

var sensitiveConfigKeys = map[string]struct{}{
	"jwt_secret":            {},
	"license_key":           {},
	"license_expiry":        {},
	"license_machine_id":    {},
	"machine_fingerprint":   {},
	"cloudflare_secret_key": {},
	"node_sub_token":        {},
	"github_oauth_secret":   {},
	// Notification credentials: hidden from non-admins and kept out of
	// backups, like the other secrets.
	"notify_telegram_bot_token": {},
	"notify_webhook_url":        {},
	// The Gist backup's credentials.
	"gist_token":    {},
	"gist_password": {},
	// Reads a private release repository (github_token.go).
	"github_token": {},
}

var systemManagedConfigKeys = map[string]struct{}{
	"license_key":         {},
	"license_expiry":      {},
	"license_machine_id":  {},
	"is_commercial":       {},
	"machine_fingerprint": {},
	"node_sub_token":      {},
	// Gist backup settings and state: set through their own endpoints and
	// left out of backups, so restoring one keeps the current settings.
	"gist_token":          {},
	"gist_password":       {},
	"gist_id":             {},
	"gist_interval_hours": {},
	"gist_last_at":        {},
	"gist_last_upload":    {},
	"gist_last_error":     {},
	"gist_last_hash":      {},
	// Set through its own endpoint, never sent back nor backed up.
	"github_token": {},
}

func PolicyForConfig(name string) ConfigAccessPolicy {
	if IsPublicConfigKey(name) {
		return ConfigAccessPublic
	}
	if IsSensitiveConfigKey(name) {
		return ConfigAccessSensitive
	}
	return ConfigAccessSensitive
}

func IsPublicConfigKey(name string) bool {
	_, ok := publicConfigKeys[normalizeConfigKey(name)]
	return ok
}

func IsSensitiveConfigKey(name string) bool {
	_, ok := sensitiveConfigKeys[normalizeConfigKey(name)]
	return ok
}

func IsSystemManagedConfigKey(name string) bool {
	_, ok := systemManagedConfigKeys[normalizeConfigKey(name)]
	return ok
}

func FilterSensitiveConfigs(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for name, value := range in {
		if IsSensitiveConfigKey(name) {
			continue
		}
		out[name] = value
	}
	return out
}

func FilterBackupConfigs(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for name, value := range in {
		if IsSensitiveConfigKey(name) || IsSystemManagedConfigKey(name) {
			continue
		}
		out[name] = value
	}
	return out
}

func normalizeConfigKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
