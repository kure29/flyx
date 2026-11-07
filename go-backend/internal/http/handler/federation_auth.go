package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

func (h *Handler) authPeer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			response.WriteJSON(w, response.Err(401, "Missing Authorization header"))
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.WriteJSON(w, response.Err(401, "Invalid Authorization format"))
			return
		}

		token := parts[1]
		share, err := h.repo.GetPeerShareByToken(token)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if share == nil {
			response.WriteJSON(w, response.Err(401, "Invalid token"))
			return
		}

		if strings.TrimSpace(share.AllowedIPs) != "" {
			clientIP := resolvePeerClientIP(r)
			if clientIP == nil {
				response.WriteJSON(w, response.Err(403, "Unable to determine client IP"))
				return
			}
			if !isPeerIPAllowed(clientIP, share.AllowedIPs) {
				response.WriteJSON(w, response.Err(403, "IP not allowed"))
				return
			}
		}

		if share.AllowedDomains != "" {
			clientDomain := r.Header.Get("X-Panel-Domain")
			if clientDomain == "" {
				response.WriteJSON(w, response.Err(403, "Domain verification required"))
				return
			}
			allowed := false
			domains := strings.Split(share.AllowedDomains, ",")
			for _, d := range domains {
				if strings.TrimSpace(d) == clientDomain {
					allowed = true
					break
				}
			}
			if !allowed {
				response.WriteJSON(w, response.Err(403, "Domain not allowed"))
				return
			}
		}

		if share.IsActive != 1 || (share.ExpiryTime > 0 && share.ExpiryTime <= time.Now().UnixMilli()) || isPeerShareFlowExceeded(share) {
			if !isFederationCleanupRequest(r) {
				response.WriteJSON(w, response.Err(403, "Share is inactive, expired, or over quota"))
				return
			}
		}

		next(w, r)
	}
}

// Revoked allocation privileges must not revoke cleanup privileges. Inspect
// only supported destructive commands, preserving the body for the handler.
func isFederationCleanupRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/federation/runtime/release-role":
		return true
	case "/api/v1/federation/runtime/command":
		if r.Body == nil {
			return false
		}
		var copied bytes.Buffer
		original := r.Body
		var request federationRuntimeCommandRequest
		err := json.NewDecoder(io.TeeReader(io.LimitReader(original, 1<<20), &copied)).Decode(&request)
		r.Body = struct {
			io.Reader
			io.Closer
		}{Reader: io.MultiReader(&copied, original), Closer: original}
		if err != nil || !isFederationRuntimeCommandAllowed(request.CommandType) {
			return false
		}
		_, action := federationResourceCommandKind(request.CommandType)
		return action == "delete"
	default:
		return false
	}
}

func (h *Handler) federationConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	token := extractBearerToken(r)
	share, err := h.repo.GetPeerShareByToken(token)
	if err != nil || share == nil {
		response.WriteJSON(w, response.Err(401, "Unauthorized"))
		return
	}

	nodeInfo, err := h.repo.GetNodeBasicInfo(share.NodeID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "Node not found"))
		return
	}

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"shareId":        share.ID,
		"shareName":      share.Name,
		"nodeId":         share.NodeID,
		"nodeName":       nodeInfo.Name,
		"serverIp":       nodeInfo.ServerIP,
		"status":         nodeInfo.Status,
		"maxBandwidth":   share.MaxBandwidth,
		"currentFlow":    share.CurrentFlow,
		"expiryTime":     share.ExpiryTime,
		"portRangeStart": share.PortRangeStart,
		"portRangeEnd":   share.PortRangeEnd,
	}))
}

func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	if len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	return ""
}

func isPeerShareFlowExceeded(share *repo.PeerShare) bool {
	if share == nil {
		return false
	}
	if share.MaxBandwidth <= 0 {
		return false
	}
	return share.CurrentFlow >= share.MaxBandwidth
}

func normalizePeerShareAllowedIPs(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	parts := strings.Split(raw, ",")
	normalized := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}

		if strings.Contains(item, "/") {
			_, network, err := net.ParseCIDR(item)
			if err != nil {
				return "", fmt.Errorf("Invalid allowed IP or CIDR: %s", item)
			}
			item = network.String()
		} else {
			ip := parseIPLiteral(item)
			if ip == nil {
				return "", fmt.Errorf("Invalid allowed IP or CIDR: %s", item)
			}
			item = ip.String()
		}

		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}

	return strings.Join(normalized, ","), nil
}

func resolvePeerClientIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}

	remoteIP := parseIPLiteral(r.RemoteAddr)
	if isTrustedProxyIP(remoteIP) {
		if ip := parseForwardedFor(r.Header.Get("X-Forwarded-For")); ip != nil {
			return ip
		}
		if ip := parseIPLiteral(r.Header.Get("X-Real-IP")); ip != nil {
			return ip
		}
	}

	return remoteIP
}

func parseForwardedFor(raw string) net.IP {
	for _, part := range strings.Split(raw, ",") {
		if ip := parseIPLiteral(part); ip != nil {
			return ip
		}
	}
	return nil
}

func parseIPLiteral(raw string) net.IP {
	value := strings.Trim(strings.TrimSpace(raw), "\"")
	if value == "" {
		return nil
	}

	if ip := net.ParseIP(value); ip != nil {
		return normalizeIPAddress(ip)
	}

	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return nil
	}

	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil
	}
	return normalizeIPAddress(net.ParseIP(host))
}

func normalizeIPAddress(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip.To16()
}

func isTrustedProxyIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func isPeerIPAllowed(clientIP net.IP, whitelist string) bool {
	if clientIP == nil {
		return false
	}

	for _, part := range strings.Split(whitelist, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}

		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if network.Contains(clientIP) {
				return true
			}
			continue
		}

		allowedIP := parseIPLiteral(entry)
		if allowedIP != nil && allowedIP.Equal(clientIP) {
			return true
		}
	}

	return false
}
