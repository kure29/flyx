package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

type contextKey string

const ClaimsContextKey contextKey = "claims"

type AuthOptions struct {
	JWTSecret        string
	GetUserAuthState func(userID int64) (*auth.UserAuthState, error)
}

func JWT(opts AuthOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			token := strings.TrimSpace(r.Header.Get("Authorization"))
			if token == "" {
				if allowsOptionalAuth(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
				return
			}

			claims, ok := auth.ValidateToken(token, opts.JWTSecret)
			if !ok {
				if allowsOptionalAuth(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
				return
			}

			if opts.GetUserAuthState != nil {
				userID, err := strconv.ParseInt(claims.Sub, 10, 64)
				if err != nil {
					if allowsOptionalAuth(r.URL.Path) {
						next.ServeHTTP(w, r)
						return
					}
					response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
					return
				}
				state, err := opts.GetUserAuthState(userID)
				if err != nil || state == nil || state.Status != 1 || state.RoleID != claims.RoleID || claims.IatMs <= state.PasswordChangedAt {
					if allowsOptionalAuth(r.URL.Path) {
						next.ServeHTTP(w, r)
						return
					}
					response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
					return
				}
			}

			// FLYX is single-user: only the admin may use the panel, whatever
			// other accounts an older version left in the database.
			if claims.RoleID != 0 {
				response.WriteJSON(w, response.Err(401, "只有管理员可以登录"))
				return
			}

			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func allowsOptionalAuth(path string) bool {
	return path == "/api/v1/config/get"
}

func shouldSkip(path string) bool {
	switch {
	case strings.HasPrefix(path, "/flow/"):
		return true
	case strings.HasPrefix(path, "/api/v1/open_api/"):
		return true
	case strings.HasPrefix(path, "/api/v1/captcha/"):
		return true
	case path == "/api/v1/config/get":
		return false
	case path == "/api/v1/user/login":
		return true
	case path == "/api/v1/user/passkey/status" || path == "/api/v1/user/passkey/login/begin" || path == "/api/v1/user/passkey/login/finish":
		return true
	case path == "/api/v1/user/login/methods":
		return true
	case path == "/api/v1/user/github/status" || path == "/api/v1/user/github/login/begin" || path == "/api/v1/user/github/login/finish":
		return true
	case path == "/api/v1/public/config/get" || path == "/api/v1/public/status":
		return true
	case path == "/api/v1/public/status/history" || path == "/api/v1/public/status/ping-history" ||
		path == "/api/v1/public/status/theme-config":
		return true
	case path == "/api/v1/federation/connect":
		return true
	case path == "/api/v1/federation/tunnel/create":
		return true
	case path == "/api/v1/federation/runtime/reserve-port":
		return true
	case path == "/api/v1/federation/runtime/apply-role":
		return true
	case path == "/api/v1/federation/runtime/release-role":
		return true
	case path == "/api/v1/federation/runtime/diagnose":
		return true
	case path == "/api/v1/federation/runtime/command":
		return true
	default:
		return false
	}
}
