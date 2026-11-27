package handler

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
)

const (
	subIPActionAlert   = "alert"
	subIPActionSuspend = "suspend"
	subIPActionDisable = "disable"

	defaultIPSuspendMinutes = 10
	maxOnlineIPsLimit       = 1000
	maxPullIPsLimit         = 1000
	maxIPSuspendMinutes     = 10080 // 7 days

	subPullWindow    = 24 * time.Hour
	subPullLogKeep   = 200
	subPullLogMaxAge = 7 * 24 * time.Hour
)

// Tunable in unit tests.
var (
	subOnlineIPTTL            = 3 * time.Minute
	subIPBreachStreakRequired = 2
	subIPBreachMinGap         = 45 * time.Second
	subIPAlertCooldown        = 30 * time.Minute
)

// agentUserOnlineIP mirrors go-gost/x/kernel.UserOnlineIP.
type agentUserOnlineIP struct {
	ID       int64  `json:"id"`
	IP       string `json:"ip"`
	LastSeen int64  `json:"lastSeen"`
}

type subscriptionOnlineIPView struct {
	IP        string   `json:"ip"`
	Country   string   `json:"country,omitempty"`
	NodeIDs   []int64  `json:"nodeIds"`
	NodeNames []string `json:"nodeNames"`
	LastSeen  int64    `json:"lastSeen"`
}

type subscriptionPullLogView struct {
	ID          int64  `json:"id"`
	IP          string `json:"ip"`
	Country     string `json:"country,omitempty"`
	UserAgent   string `json:"userAgent"`
	Format      string `json:"format"`
	Blocked     bool   `json:"blocked"`
	CreatedTime int64  `json:"createdTime"`
}

type nodeUserIPEntry struct {
	displayIP string
	lastSeen  int64
}

type aggregatedOnlineIP struct {
	ip       string
	nodeIDs  []int64
	lastSeen int64
}

type breachState struct {
	streak int
	lastAt time.Time
}

// subOnlineIPStore holds the latest per-node online client IP snapshots for
// subscriptions and tracks consecutive breach windows and alert cooldowns.
type subOnlineIPStore struct {
	mu           sync.RWMutex
	byNode       map[int64]map[int64]map[string]nodeUserIPEntry
	nodeReportAt map[int64]time.Time
	breaches     map[int64]breachState
	lastAlertAt  map[string]time.Time

	enforceMu sync.Mutex
	pullMu    sync.Mutex
}

func newSubOnlineIPStore() *subOnlineIPStore {
	return &subOnlineIPStore{
		byNode:       make(map[int64]map[int64]map[string]nodeUserIPEntry),
		nodeReportAt: make(map[int64]time.Time),
		breaches:     make(map[int64]breachState),
		lastAlertAt:  make(map[string]time.Time),
	}
}

func normalizeIPAction(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", subIPActionAlert:
		return subIPActionAlert, nil
	case subIPActionSuspend:
		return subIPActionSuspend, nil
	case subIPActionDisable:
		return subIPActionDisable, nil
	default:
		return "", errText("不支持的超限处理动作")
	}
}

func effectiveIPAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case subIPActionSuspend:
		return subIPActionSuspend
	case subIPActionDisable:
		return subIPActionDisable
	default:
		return subIPActionAlert
	}
}

func effectiveIPSuspendMinutes(mins int) int {
	if mins <= 0 {
		return defaultIPSuspendMinutes
	}
	return mins
}

func subIPSuspended(s *model.Subscription, now time.Time) bool {
	return s != nil && s.IPSuspendedUntil > now.UnixMilli()
}

// canonicalSubscriptionIP normalizes raw IP text and returns (display, dedupKey).
// IPv6 addresses are deduplicated by their /64 prefix so privacy-extension
// rotations on the same client do not inflate online or pull IP counts.
func canonicalSubscriptionIP(raw string) (string, string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	if addr, err := netip.ParseAddr(s); err == nil {
		addr = addr.Unmap()
		display := addr.String()
		if addr.Is6() {
			return display, netip.PrefixFrom(addr, 64).Masked().String()
		}
		return display, display
	}
	return s, s
}

func countDistinctSubscriptionIPs(ips []string) int {
	seen := make(map[string]struct{}, len(ips))
	for _, raw := range ips {
		if _, key := canonicalSubscriptionIP(raw); key != "" {
			seen[key] = struct{}{}
		}
	}
	return len(seen)
}

func (s *subOnlineIPStore) updateNode(nodeID int64, items []agentUserOnlineIP, now time.Time) {
	if s == nil || nodeID <= 0 {
		return
	}
	nowMs := now.UnixMilli()
	cutoffMs := now.Add(-subOnlineIPTTL).UnixMilli()
	maxFutureMs := nowMs + 60_000

	snap := make(map[int64]map[string]nodeUserIPEntry)
	for _, item := range items {
		if item.ID <= 0 {
			continue
		}
		display, key := canonicalSubscriptionIP(item.IP)
		if key == "" {
			continue
		}
		seen := item.LastSeen
		if seen <= 0 {
			seen = nowMs
		} else if seen > maxFutureMs {
			seen = nowMs
		}
		if seen < cutoffMs {
			continue
		}
		perSub := snap[item.ID]
		if perSub == nil {
			perSub = make(map[string]nodeUserIPEntry)
			snap[item.ID] = perSub
		}
		if existing, ok := perSub[key]; !ok || seen > existing.lastSeen {
			perSub[key] = nodeUserIPEntry{displayIP: display, lastSeen: seen}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(snap) == 0 {
		delete(s.byNode, nodeID)
	} else {
		s.byNode[nodeID] = snap
	}
	s.nodeReportAt[nodeID] = now
}

func (s *subOnlineIPStore) onlineIPsForSub(subID int64, now time.Time) []aggregatedOnlineIP {
	if s == nil || subID <= 0 {
		return nil
	}
	cutoffTime := now.Add(-subOnlineIPTTL)
	cutoffMs := cutoffTime.UnixMilli()

	type acc struct {
		displayIP string
		lastSeen  int64
		nodes     map[int64]struct{}
	}
	merged := make(map[string]*acc)

	s.mu.RLock()
	for nodeID, perSub := range s.byNode {
		if repAt, ok := s.nodeReportAt[nodeID]; !ok || repAt.Before(cutoffTime) {
			continue
		}
		entries := perSub[subID]
		for key, e := range entries {
			if e.lastSeen < cutoffMs {
				continue
			}
			a := merged[key]
			if a == nil {
				a = &acc{
					displayIP: e.displayIP,
					lastSeen:  e.lastSeen,
					nodes:     map[int64]struct{}{nodeID: {}},
				}
				merged[key] = a
				continue
			}
			a.nodes[nodeID] = struct{}{}
			if e.lastSeen > a.lastSeen {
				a.lastSeen = e.lastSeen
				a.displayIP = e.displayIP
			}
		}
	}
	s.mu.RUnlock()

	out := make([]aggregatedOnlineIP, 0, len(merged))
	for _, a := range merged {
		nodeIDs := make([]int64, 0, len(a.nodes))
		for id := range a.nodes {
			nodeIDs = append(nodeIDs, id)
		}
		sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
		out = append(out, aggregatedOnlineIP{
			ip:       a.displayIP,
			nodeIDs:  nodeIDs,
			lastSeen: a.lastSeen,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].lastSeen != out[j].lastSeen {
			return out[i].lastSeen > out[j].lastSeen
		}
		return out[i].ip < out[j].ip
	})
	return out
}

func (s *subOnlineIPStore) onlineIPCounts(now time.Time) map[int64]int {
	out := make(map[int64]int)
	if s == nil {
		return out
	}
	cutoffTime := now.Add(-subOnlineIPTTL)
	cutoffMs := cutoffTime.UnixMilli()

	perSubKeys := make(map[int64]map[string]struct{})
	s.mu.RLock()
	for nodeID, perSub := range s.byNode {
		if repAt, ok := s.nodeReportAt[nodeID]; !ok || repAt.Before(cutoffTime) {
			continue
		}
		for subID, entries := range perSub {
			for key, e := range entries {
				if e.lastSeen < cutoffMs {
					continue
				}
				set := perSubKeys[subID]
				if set == nil {
					set = make(map[string]struct{})
					perSubKeys[subID] = set
				}
				set[key] = struct{}{}
			}
		}
	}
	s.mu.RUnlock()

	for subID, set := range perSubKeys {
		out[subID] = len(set)
	}
	return out
}

// recordBreach advances the consecutive over-limit counter when enough time has
// elapsed since the last counted observation so parallel node polls within the
// same tick do not double-count.
func (s *subOnlineIPStore) recordBreach(subID int64, now time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.breaches[subID]
	if st.streak == 0 {
		st = breachState{streak: 1, lastAt: now}
	} else if subIPBreachMinGap <= 0 || now.Sub(st.lastAt) >= subIPBreachMinGap {
		st.streak++
		st.lastAt = now
	}
	s.breaches[subID] = st
	return st.streak >= subIPBreachStreakRequired
}

func (s *subOnlineIPStore) clearBreach(subID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.breaches, subID)
	s.mu.Unlock()
}

func (s *subOnlineIPStore) allowAlert(key string, now time.Time, cooldown time.Duration) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.lastAlertAt[key]; ok && now.Sub(prev) < cooldown {
		return false
	}
	s.lastAlertAt[key] = now
	return true
}

func (h *Handler) onlineIPStore() *subOnlineIPStore {
	if h == nil {
		return nil
	}
	return h.subOnlineIPs
}

func (h *Handler) geoipCountry(ip string) string {
	if h == nil || h.geoip == nil || ip == "" {
		return ""
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	return h.geoip.Lookup(parsed)
}

func (h *Handler) formatOnlineIPList(ips []aggregatedOnlineIP, limit int) string {
	if limit <= 0 || limit > len(ips) {
		limit = len(ips)
	}
	parts := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		ip := ips[i].ip
		if country := h.geoipCountry(ip); country != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", ip, country))
		} else {
			parts = append(parts, ip)
		}
	}
	if len(ips) > limit {
		parts = append(parts, fmt.Sprintf("等共 %d 个", len(ips)))
	}
	return strings.Join(parts, "、")
}

// applyNodeOnlineIPs updates the cross-node online IP store with one node's
// latest snapshot and enforces per-subscription online IP limits.
func (h *Handler) applyNodeOnlineIPs(nodeID int64, items []agentUserOnlineIP, now time.Time) {
	store := h.onlineIPStore()
	if store == nil {
		return
	}
	store.updateNode(nodeID, items, now)
	h.enforceSubscriptionIPLimits(now)
}

func (h *Handler) enforceSubscriptionIPLimits(now time.Time) {
	store := h.onlineIPStore()
	if store == nil || h.repo == nil {
		return
	}
	store.enforceMu.Lock()
	defer store.enforceMu.Unlock()

	subs, err := h.repo.ListSubscriptions()
	if err != nil {
		return
	}
	needSync := false
	nowMs := now.UnixMilli()

	for i := range subs {
		s := &subs[i]
		if !isShare(s) || s.MaxOnlineIPs <= 0 || !subActive(s, now) {
			store.clearBreach(s.ID)
			continue
		}
		ips := store.onlineIPsForSub(s.ID, now)
		if len(ips) <= s.MaxOnlineIPs {
			store.clearBreach(s.ID)
			continue
		}
		if !store.recordBreach(s.ID, now) {
			continue
		}

		action := effectiveIPAction(s.IPAction)
		ipSummary := h.formatOnlineIPList(ips, 6)
		switch action {
		case subIPActionSuspend:
			mins := effectiveIPSuspendMinutes(s.IPSuspendMinutes)
			until := now.Add(time.Duration(mins) * time.Minute)
			if err := h.repo.SetSubscriptionIPSuspendedUntil(s.ID, until.UnixMilli(), nowMs); err != nil {
				log.Printf("subscription %d ip suspend failed: %v", s.ID, err)
				continue
			}
			store.clearBreach(s.ID)
			needSync = true
			if store.allowAlert(fmt.Sprintf("online-suspend:%d", s.ID), now, time.Minute) {
				h.notify(notifyEventUserQuota, "订阅在线 IP 超限，已临时暂停",
					fmt.Sprintf("订阅：%s\n当前在线：%d / 上限 %d\n暂停时长：%d 分钟（预计 %s 恢复）\n在线 IP：%s",
						s.Name, len(ips), s.MaxOnlineIPs, mins, until.Format("15:04"), ipSummary))
			}
		case subIPActionDisable:
			s.Enabled = 0
			s.IPSuspendedUntil = 0
			s.UpdatedTime = nowMs
			if err := h.repo.SaveSubscription(s); err != nil {
				log.Printf("subscription %d ip disable failed: %v", s.ID, err)
				continue
			}
			store.clearBreach(s.ID)
			needSync = true
			if store.allowAlert(fmt.Sprintf("online-disable:%d", s.ID), now, time.Minute) {
				h.notify(notifyEventUserQuota, "订阅在线 IP 超限，已自动停用",
					fmt.Sprintf("订阅：%s\n当前在线：%d / 上限 %d\n在线 IP：%s",
						s.Name, len(ips), s.MaxOnlineIPs, ipSummary))
			}
		default: // alert
			if store.allowAlert(fmt.Sprintf("online-alert:%d", s.ID), now, subIPAlertCooldown) {
				h.notify(notifyEventUserQuota, "订阅在线 IP 超限告警",
					fmt.Sprintf("订阅：%s\n当前在线：%d / 上限 %d\n在线 IP：%s",
						s.Name, len(ips), s.MaxOnlineIPs, ipSummary))
			}
		}
	}

	if needSync {
		h.syncSubscriptions(now)
	}
}

// resolveSubscriptionClientIP extracts the client IP for a subscription HTTP
// request, trusting forwarded headers only when the peer is a trusted proxy.
func resolveSubscriptionClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	remoteIP := parseIPLiteral(r.RemoteAddr)
	if isTrustedProxyIP(remoteIP) {
		if ip := parseIPLiteral(r.Header.Get("CF-Connecting-IP")); ip != nil {
			return ip.String()
		}
		if ip := parseForwardedFor(r.Header.Get("X-Forwarded-For")); ip != nil {
			return ip.String()
		}
		if ip := parseIPLiteral(r.Header.Get("X-Real-IP")); ip != nil {
			return ip.String()
		}
	}
	if remoteIP != nil {
		return remoteIP.String()
	}
	return ""
}

// checkAndRecordSubscriptionPull records a GET pull of /open_api/sub/{token}
// and returns true if the request exceeded the subscription's 24h MaxPullIPs cap.
func (h *Handler) checkAndRecordSubscriptionPull(sub *model.Subscription, r *http.Request, format string, now time.Time) bool {
	if h == nil || h.repo == nil || sub == nil || r == nil {
		return false
	}
	if store := h.onlineIPStore(); store != nil {
		store.pullMu.Lock()
		defer store.pullMu.Unlock()
	}

	rawIP := resolveSubscriptionClientIP(r)
	displayIP, ipKey := canonicalSubscriptionIP(rawIP)
	country := h.geoipCountry(displayIP)
	ua := truncate(strings.TrimSpace(r.UserAgent()), 200)
	nowMs := now.UnixMilli()

	blocked := false
	if sub.MaxPullIPs > 0 && ipKey != "" {
		recentIPs, err := h.repo.ListRecentSubscriptionPullIPs(sub.ID, now.Add(-subPullWindow).UnixMilli())
		if err == nil {
			seen := make(map[string]struct{}, len(recentIPs))
			for _, ip := range recentIPs {
				if _, k := canonicalSubscriptionIP(ip); k != "" {
					seen[k] = struct{}{}
				}
			}
			if _, ok := seen[ipKey]; !ok && len(seen) >= sub.MaxPullIPs {
				blocked = true
			}
		}
	}

	entry := &model.SubscriptionPullLog{
		SubscriptionID: sub.ID,
		IP:             displayIP,
		Country:        country,
		UserAgent:      ua,
		Format:         truncate(strings.TrimSpace(format), 24),
		Blocked:        boolToInt(blocked),
		CreatedTime:    nowMs,
	}
	if err := h.repo.RecordSubscriptionPull(entry, subPullLogKeep, int64(subPullLogMaxAge/time.Millisecond)); err != nil {
		log.Printf("record subscription pull failed sub_id=%d err=%v", sub.ID, err)
	}

	if blocked {
		store := h.onlineIPStore()
		if store == nil || store.allowAlert(fmt.Sprintf("pull-block:%d", sub.ID), now, subIPAlertCooldown) {
			loc := ""
			if country != "" {
				loc = " (" + country + ")"
			}
			h.notify(notifyEventUserQuota, "订阅拉取 IP 超限已拦截",
				fmt.Sprintf("订阅：%s\n24 小时拉取 IP 上限：%d\n本次拦截 IP：%s%s\n客户端：%s",
					sub.Name, sub.MaxPullIPs, displayIP, loc, ua))
		}
	}
	return blocked
}

func (h *Handler) onlineIPViewsForSub(subID int64, now time.Time) []subscriptionOnlineIPView {
	store := h.onlineIPStore()
	if store == nil {
		return []subscriptionOnlineIPView{}
	}
	raw := store.onlineIPsForSub(subID, now)
	nodeLabels := make(map[int64]string)
	out := make([]subscriptionOnlineIPView, 0, len(raw))
	for _, item := range raw {
		names := make([]string, 0, len(item.nodeIDs))
		for _, nid := range item.nodeIDs {
			name, ok := nodeLabels[nid]
			if !ok {
				name = h.nodeLabel(nid)
				nodeLabels[nid] = name
			}
			names = append(names, name)
		}
		out = append(out, subscriptionOnlineIPView{
			IP:        item.ip,
			Country:   h.geoipCountry(item.ip),
			NodeIDs:   item.nodeIDs,
			NodeNames: names,
			LastSeen:  item.lastSeen,
		})
	}
	return out
}

// subscriptionIPActivity returns live online IPs and recent subscription pull
// logs for a single subscription (admin only).
func (h *Handler) subscriptionIPActivity(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	s, err := h.repo.GetSubscription(id)
	if err != nil || s == nil {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	now := time.Now()
	onlineIPs := h.onlineIPViewsForSub(s.ID, now)

	pullRows, err := h.repo.ListSubscriptionPullLogs(s.ID, 100)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	pullLogs := make([]subscriptionPullLogView, 0, len(pullRows))
	for _, row := range pullRows {
		country := row.Country
		if country == "" {
			country = h.geoipCountry(row.IP)
		}
		pullLogs = append(pullLogs, subscriptionPullLogView{
			ID:          row.ID,
			IP:          row.IP,
			Country:     country,
			UserAgent:   row.UserAgent,
			Format:      row.Format,
			Blocked:     row.Blocked == 1,
			CreatedTime: row.CreatedTime,
		})
	}
	recentIPs, _ := h.repo.ListRecentSubscriptionPullIPs(s.ID, now.Add(-subPullWindow).UnixMilli())

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"onlineIps":        onlineIPs,
		"pullLogs":         pullLogs,
		"pullIpCount24h":   countDistinctSubscriptionIPs(recentIPs),
		"maxOnlineIps":     s.MaxOnlineIPs,
		"ipAction":         effectiveIPAction(s.IPAction),
		"ipSuspendMinutes": effectiveIPSuspendMinutes(s.IPSuspendMinutes),
		"ipSuspendedUntil": s.IPSuspendedUntil,
		"maxPullIps":       s.MaxPullIPs,
	}))
}
