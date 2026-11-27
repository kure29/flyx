package kernel

import (
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// onlineIPWindow keeps an observed (user, clientIP) active for 2 minutes
	// so periodic panel polls (60s) catch every active device without flickering.
	onlineIPWindow = 120 * time.Second
	// singboxConnTTL retains connection-id -> client-ip mappings long enough
	// for sing-box's follow-up authenticated line (and multiplexed streams).
	singboxConnTTL = 30 * time.Second

	maxPendingSingboxConns = 4096
	maxTrackedIPsPerUser   = 256
	maxTrackedUsers        = 4096
)

type trackedIP struct {
	ip       string
	lastSeen int64 // unix ms
}

type pendingConn struct {
	ip     string
	key    string
	seenAt time.Time
}

// onlineIPTracker observes Xray and sing-box stdout/stderr access lines to
// maintain a bounded sliding window of active client IPs per subscription user.
type onlineIPTracker struct {
	mu      sync.Mutex
	users   map[int64]map[string]trackedIP // userID -> ipKey -> trackedIP
	pending map[string]pendingConn         // sing-box connID -> client IP
}

func newOnlineIPTracker() *onlineIPTracker {
	return &onlineIPTracker{
		users:   make(map[int64]map[string]trackedIP),
		pending: make(map[string]pendingConn),
	}
}

// ObserveLine inspects one kernel output line, recording any authenticated
// (userID, clientIP) activity. It returns true when the line is a routine
// connection/access log that should be omitted from the UI error logRing.
func (t *onlineIPTracker) ObserveLine(kernelType, rawLine string, now time.Time) bool {
	if t == nil || rawLine == "" {
		return false
	}
	line := ansiEscape.ReplaceAllString(rawLine, "")
	switch kernelType {
	case "xray":
		return t.observeXray(line, now)
	case "singbox":
		return t.observeSingbox(line, now)
	default:
		return false
	}
}

// observeXray parses Xray-core access lines such as:
//
//	2026/01/02 15:04:05.123456 from 1.2.3.4:54321 accepted tcp:example.com:443 [in-1 -> user-5] email: u5@flyx
//	2026/01/02 15:04:05 from tcp:[2001:db8::1]:54321 accepted tcp:example.com:443 [in-1 -> direct] email: u5
func (t *onlineIPTracker) observeXray(line string, now time.Time) bool {
	fromIdx := strings.Index(line, " from ")
	if fromIdx < 0 {
		return false
	}
	rest := line[fromIdx+len(" from "):]
	accIdx := strings.Index(rest, " accepted ")
	if accIdx < 0 {
		return false
	}
	addr := strings.TrimSpace(rest[:accIdx])
	if ip, key, ok := normalizeClientIP(addr); ok {
		if uid := parseXrayEmailUserID(rest[accIdx:]); uid > 0 {
			t.record(uid, ip, key, now)
		}
	}
	// Routine accepted connection log: keep out of the 200-line error logRing.
	return true
}

func parseXrayEmailUserID(s string) int64 {
	idx := strings.LastIndex(s, "email:")
	if idx < 0 {
		return 0
	}
	tok := strings.TrimSpace(s[idx+len("email:"):])
	if sp := strings.IndexAny(tok, " \t\r\n"); sp >= 0 {
		tok = tok[:sp]
	}
	if at := strings.IndexByte(tok, '@'); at >= 0 {
		tok = tok[:at]
	}
	return parseUserTag(tok)
}

// observeSingbox parses sing-box INFO connection lines (1.8–1.14+):
//
//	+0800 2026-01-02 15:04:05 INFO [123456789 0ms] inbound/vless[in-1]: inbound connection from 1.2.3.4:54321
//	+0800 2026-01-02 15:04:05 INFO [123456789 1ms] inbound/vless[in-1]: [u5] inbound connection to example.com:443
//	+0800 2026-01-02 15:04:05 INFO [123456789 0ms] inbound/hysteria2[in-2]: [u5] inbound packet connection from [2001:db8::1]:54321
func (t *onlineIPTracker) observeSingbox(line string, now time.Time) bool {
	if !strings.Contains(line, "INFO") {
		return false
	}
	inIdx := strings.Index(line, "inbound/")
	if inIdx < 0 {
		return false
	}
	connID := parseSingboxConnID(line[:inIdx])
	body := line[inIdx:]
	colon := strings.Index(body, ":")
	if colon < 0 {
		return false
	}
	msg := strings.TrimSpace(body[colon+1:])

	var uid int64
	if strings.HasPrefix(msg, "[") {
		if end := strings.IndexByte(msg, ']'); end > 1 {
			if parsed := parseUserTag(msg[1:end]); parsed > 0 {
				uid = parsed
				msg = strings.TrimSpace(msg[end+1:])
			}
		}
	}

	if fromAddr, ok := cutAfterConnectionFrom(msg); ok {
		if ip, key, valid := normalizeClientIP(fromAddr); valid {
			if uid > 0 {
				t.record(uid, ip, key, now)
			} else if connID != "" {
				t.rememberConn(connID, ip, key, now)
			}
		}
		return true
	}

	if isSingboxConnectionTo(msg) {
		if uid > 0 && connID != "" {
			if ip, key, ok := t.lookupConn(connID, now); ok {
				t.record(uid, ip, key, now)
			}
		}
		return true
	}

	return false
}

func cutAfterConnectionFrom(msg string) (string, bool) {
	for _, prefix := range []string{
		"inbound connection from ",
		"inbound packet connection from ",
	} {
		if strings.HasPrefix(msg, prefix) {
			addr := strings.TrimSpace(msg[len(prefix):])
			if sp := strings.IndexAny(addr, " \t\r\n"); sp >= 0 {
				addr = addr[:sp]
			}
			return addr, true
		}
	}
	return "", false
}

func isSingboxConnectionTo(msg string) bool {
	return strings.HasPrefix(msg, "inbound connection to ") ||
		strings.HasPrefix(msg, "inbound packet connection to ") ||
		strings.HasPrefix(msg, "inbound multiplexed connection")
}

// parseSingboxConnID extracts the numeric connection id from "[123456 0ms]".
func parseSingboxConnID(prefix string) string {
	end := strings.LastIndexByte(prefix, ']')
	if end < 0 {
		return ""
	}
	start := strings.LastIndexByte(prefix[:end], '[')
	if start < 0 || start+1 >= end {
		return ""
	}
	inner := strings.TrimSpace(prefix[start+1 : end])
	sp := strings.IndexByte(inner, ' ')
	if sp <= 0 {
		return ""
	}
	id := inner[:sp]
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return ""
		}
	}
	return id
}

// parseUserTag parses FLYX's subscription user tag "u<id>" (id > 0).
func parseUserTag(tok string) int64 {
	if len(tok) < 2 || tok[0] != 'u' {
		return 0
	}
	id, err := strconv.ParseInt(tok[1:], 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// normalizeClientIP parses a raw address token (with optional tcp:/udp: prefix
// and :port suffix) into a display IP and a dedup key (exact IPv4 or IPv6 /64).
func normalizeClientIP(raw string) (ipStr string, key string, ok bool) {
	s := strings.TrimSpace(raw)
	for _, p := range []string{"tcp:", "udp:", "unix:"} {
		if strings.HasPrefix(strings.ToLower(s), p) {
			s = s[len(p):]
			break
		}
	}
	if s == "" {
		return "", "", false
	}
	host := s
	if h, _, err := net.SplitHostPort(s); err == nil {
		host = h
	} else {
		host = strings.Trim(s, "[]")
	}
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return "", "", false
	}
	if ip4 := ip.To4(); ip4 != nil {
		str := ip4.String()
		return str, str, true
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return "", "", false
	}
	prefix := ip16.Mask(net.CIDRMask(64, 128)).String() + "/64"
	return ip16.String(), prefix, true
}

func (t *onlineIPTracker) rememberConn(connID, ip, key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) >= maxPendingSingboxConns {
		cutoff := now.Add(-singboxConnTTL)
		for k, v := range t.pending {
			if v.seenAt.Before(cutoff) {
				delete(t.pending, k)
			}
		}
		if len(t.pending) >= maxPendingSingboxConns {
			return
		}
	}
	t.pending[connID] = pendingConn{ip: ip, key: key, seenAt: now}
}

func (t *onlineIPTracker) lookupConn(connID string, now time.Time) (string, string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.pending[connID]
	if !ok {
		return "", "", false
	}
	if now.Sub(p.seenAt) > singboxConnTTL {
		delete(t.pending, connID)
		return "", "", false
	}
	p.seenAt = now
	t.pending[connID] = p
	return p.ip, p.key, true
}

func (t *onlineIPTracker) record(uid int64, ip, key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	nowMs := now.UnixMilli()
	bucket := t.users[uid]
	if bucket == nil {
		if len(t.users) >= maxTrackedUsers {
			t.pruneLocked(now)
			if len(t.users) >= maxTrackedUsers {
				return
			}
		}
		bucket = make(map[string]trackedIP)
		t.users[uid] = bucket
	}
	if _, exists := bucket[key]; !exists && len(bucket) >= maxTrackedIPsPerUser {
		cutoffMs := now.Add(-onlineIPWindow).UnixMilli()
		for k, v := range bucket {
			if v.lastSeen < cutoffMs {
				delete(bucket, k)
			}
		}
		if len(bucket) >= maxTrackedIPsPerUser {
			return
		}
	}
	bucket[key] = trackedIP{ip: ip, lastSeen: nowMs}
}

func (t *onlineIPTracker) pruneLocked(now time.Time) {
	cutoffMs := now.Add(-onlineIPWindow).UnixMilli()
	for uid, bucket := range t.users {
		for k, v := range bucket {
			if v.lastSeen < cutoffMs {
				delete(bucket, k)
			}
		}
		if len(bucket) == 0 {
			delete(t.users, uid)
		}
	}
	connCutoff := now.Add(-singboxConnTTL)
	for k, v := range t.pending {
		if v.seenAt.Before(connCutoff) {
			delete(t.pending, k)
		}
	}
}

// Snapshot prunes expired entries and returns all currently active (user, IP) pairs.
func (t *onlineIPTracker) Snapshot(now time.Time) []UserOnlineIP {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pruneLocked(now)
	var out []UserOnlineIP
	for uid, bucket := range t.users {
		for _, v := range bucket {
			out = append(out, UserOnlineIP{
				ID:       uid,
				IP:       v.ip,
				LastSeen: v.lastSeen,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].IP < out[j].IP
	})
	return out
}
