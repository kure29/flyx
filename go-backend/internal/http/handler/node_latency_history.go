package handler

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
)

// Latency history (trend chart) and the alerts built on it: a reference point
// that stays bad for a few rounds, and a return route that changes line.
const (
	notifyEventLatencyBad  = "latency_bad"
	notifyEventRouteChange = "route_change"

	notifyConfigLatencyLossPct = "notify_latency_loss_pct"
	notifyConfigLatencyMs      = "notify_latency_ms"

	defaultLatencyAlertLossPct = 20
	// latencyAlertStreak bad rounds in a row (about nine minutes) alert.
	latencyAlertStreak = 3

	// maxTrendPoints caps the samples per point a trend returns.
	maxTrendPoints = 400
)

func (h *Handler) recordLatencySamples(results map[int64]reachProbe) {
	var rows []model.NodeLatencySample
	for nodeID, p := range results {
		for _, pt := range p.Points {
			if pt.Name == "" {
				continue
			}
			row := model.NodeLatencySample{NodeID: nodeID, CreatedTime: p.CheckedAt, Point: pt.Name, LossPct: 100}
			if pt.OK {
				row.OK, row.LatencyMs, row.LossPct = 1, pt.LatencyMs, pt.LossPct
			}
			rows = append(rows, row)
		}
	}
	if err := h.repo.InsertNodeLatencySamples(rows); err != nil {
		log.Printf("store latency samples: %v", err)
	}
}

// latencyThresholds returns the alert loss (percent) and latency (ms, 0 off)
// limits; unset or invalid values fall back to the defaults.
func (h *Handler) latencyThresholds() (lossPct float64, latencyMs int64) {
	lossPct = defaultLatencyAlertLossPct
	cfg, err := h.repo.GetConfigsByNames([]string{notifyConfigLatencyLossPct, notifyConfigLatencyMs})
	if err != nil {
		return lossPct, 0
	}
	if v, err := strconv.Atoi(strings.TrimSpace(cfg[notifyConfigLatencyLossPct])); err == nil && v >= 1 && v <= 100 {
		lossPct = float64(v)
	}
	if v, err := strconv.Atoi(strings.TrimSpace(cfg[notifyConfigLatencyMs])); err == nil && v > 0 {
		latencyMs = int64(v)
	}
	return lossPct, latencyMs
}

func latencyPointBad(p latencyPoint, lossPct float64, latencyMs int64) bool {
	if !p.OK || p.LossPct >= lossPct {
		return true
	}
	return latencyMs > 0 && p.LatencyMs >= latencyMs
}

func describeLatencyPoint(p latencyPoint) string {
	if !p.OK {
		return p.Name + " " + firstNonEmpty(p.Error, "连不上")
	}
	s := fmt.Sprintf("%s %d ms", p.Name, p.LatencyMs)
	if p.LossPct > 0 {
		s += fmt.Sprintf(" 丢包 %.0f%%", p.LossPct)
	}
	return s
}

// latencyAlertTracker counts bad rounds per node and point, and remembers
// which ones were reported, so each outage alerts once and once on recovery.
type latencyAlertTracker struct {
	mu     sync.Mutex
	streak map[string]int
	down   map[string]bool
}

func newLatencyAlertTracker() *latencyAlertTracker {
	return &latencyAlertTracker{streak: make(map[string]int), down: make(map[string]bool)}
}

type latencyTransition struct {
	bad, recovered []latencyPoint
}

// track records one full round and returns, per node, the points that just
// went bad or came back.
func (t *latencyAlertTracker) track(results map[int64]reachProbe, lossPct float64, latencyMs int64) map[int64]latencyTransition {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[int64]latencyTransition)
	seen := make(map[string]bool)
	for nodeID, p := range results {
		for _, pt := range p.Points {
			key := strconv.FormatInt(nodeID, 10) + "\x00" + pt.Name
			seen[key] = true
			tr := out[nodeID]
			if latencyPointBad(pt, lossPct, latencyMs) {
				t.streak[key]++
				if t.streak[key] >= latencyAlertStreak && !t.down[key] {
					t.down[key] = true
					tr.bad = append(tr.bad, pt)
				}
			} else {
				delete(t.streak, key)
				if t.down[key] {
					delete(t.down, key)
					tr.recovered = append(tr.recovered, pt)
				}
			}
			if len(tr.bad)+len(tr.recovered) > 0 {
				out[nodeID] = tr
			}
		}
	}
	// Points no longer measured (removed, node offline) start over.
	for key := range t.streak {
		if !seen[key] {
			delete(t.streak, key)
		}
	}
	for key := range t.down {
		if !seen[key] {
			delete(t.down, key)
		}
	}
	return out
}

func (h *Handler) alertLatency(results map[int64]reachProbe, names map[int64]string) {
	lossPct, latencyMs := h.latencyThresholds()
	limit := fmt.Sprintf("丢包 ≥ %.0f%%", lossPct)
	if latencyMs > 0 {
		limit += fmt.Sprintf(" 或延迟 ≥ %d ms", latencyMs)
	}
	for nodeID, tr := range h.latencyAlerts.track(results, lossPct, latencyMs) {
		name := firstNonEmpty(names[nodeID], fmt.Sprintf("#%d", nodeID))
		if len(tr.bad) > 0 {
			lines := make([]string, len(tr.bad))
			for i, p := range tr.bad {
				lines[i] = describeLatencyPoint(p)
			}
			h.notifyIncident(nodeID, notifyEventLatencyBad, latencyIncidentKeys(nodeID, tr.bad), "延迟 / 丢包异常",
				fmt.Sprintf("服务器：%s\n%s\n（连续 %d 次 %s）", name, strings.Join(lines, "\n"), latencyAlertStreak, limit))
		}
		// Recoveries of points whose alert was muted stay quiet.
		var lines []string
		for _, p := range tr.recovered {
			if h.incidentAlerted(notifyEventLatencyBad, latencyIncidentKey(nodeID, p)) {
				lines = append(lines, describeLatencyPoint(p))
			}
		}
		if len(lines) > 0 {
			h.notifyForNode(nodeID, notifyEventLatencyBad, "延迟已恢复",
				fmt.Sprintf("服务器：%s\n%s", name, strings.Join(lines, "\n")))
		}
	}
}

func latencyIncidentKey(nodeID int64, p latencyPoint) string {
	return strconv.FormatInt(nodeID, 10) + "\x00" + p.Name
}

func latencyIncidentKeys(nodeID int64, points []latencyPoint) []string {
	keys := make([]string, len(points))
	for i, p := range points {
		keys[i] = latencyIncidentKey(nodeID, p)
	}
	return keys
}

// routeChanges lists the points whose identified line changed between two
// traces; unidentified or failed traces are not a change.
func routeChanges(prev, next nodeRoute) []string {
	known := func(p *routePoint) bool { return p != nil && p.Error == "" && p.Route != "" && p.Route != "未识别" }
	before := make(map[string]string, len(prev.Points)*2)
	for _, p := range prev.Points {
		if known(&p) {
			before[p.Name] = p.Route
		}
		if known(p.IPv6) {
			before[p.Name+"/v6"] = p.IPv6.Route
		}
	}
	var out []string
	for _, p := range next.Points {
		if was, ok := before[p.Name]; ok && known(&p) && p.Route != was {
			out = append(out, fmt.Sprintf("%s：%s → %s", p.Name, was, p.Route))
		}
		if was, ok := before[p.Name+"/v6"]; ok && known(p.IPv6) && p.IPv6.Route != was {
			out = append(out, fmt.Sprintf("%s（IPv6）：%s → %s", p.Name, was, p.IPv6.Route))
		}
	}
	return out
}

type trendSample struct {
	T       int64    `json:"t"`
	Ms      *float64 `json:"ms"`
	LossPct float64  `json:"lossPct"`
}

type trendPoint struct {
	Name    string        `json:"name"`
	Samples []trendSample `json:"samples"`
	AvgMs   float64       `json:"avgMs"`
	MaxMs   int64         `json:"maxMs"`
	LossPct float64       `json:"lossPct"`
}

// buildTrend groups samples per point (in first-seen order) and averages
// them into buckets so a week fits in maxTrendPoints; a bucket where every
// sample failed has no latency.
func buildTrend(samples []model.NodeLatencySample, sinceMs, nowMs int64) []trendPoint {
	bucket := (nowMs - sinceMs) / maxTrendPoints
	if bucket < int64(reachProbeInterval/time.Millisecond) {
		bucket = 1 // one sample per bucket
	}
	type acc struct {
		sumMs, n, lossSum, count float64
	}
	var order []string
	byPoint := make(map[string]map[int64]*acc)
	stats := make(map[string]*trendPoint)
	for _, s := range samples {
		m, ok := byPoint[s.Point]
		if !ok {
			m = make(map[int64]*acc)
			byPoint[s.Point] = m
			order = append(order, s.Point)
			stats[s.Point] = &trendPoint{Name: s.Point}
		}
		key := s.CreatedTime / bucket * bucket
		a, ok := m[key]
		if !ok {
			a = &acc{}
			m[key] = a
		}
		a.count++
		a.lossSum += s.LossPct
		st := stats[s.Point]
		if s.OK == 1 {
			a.sumMs += float64(s.LatencyMs)
			a.n++
			st.AvgMs += float64(s.LatencyMs)
			if s.LatencyMs > st.MaxMs {
				st.MaxMs = s.LatencyMs
			}
		}
		st.LossPct += s.LossPct
	}
	out := make([]trendPoint, 0, len(order))
	for _, name := range order {
		m := byPoint[name]
		keys := make([]int64, 0, len(m))
		var okCount, all float64
		for k, a := range m {
			keys = append(keys, k)
			okCount += a.n
			all += a.count
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		st := stats[name]
		for _, k := range keys {
			a := m[k]
			s := trendSample{T: k, LossPct: round1(a.lossSum / a.count)}
			if a.n > 0 {
				ms := round1(a.sumMs / a.n)
				s.Ms = &ms
			}
			st.Samples = append(st.Samples, s)
		}
		if okCount > 0 {
			st.AvgMs = round1(st.AvgMs / okCount)
		}
		st.LossPct = round1(st.LossPct / all)
		out = append(out, *st)
	}
	return out
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

// nodeLatencyHistory returns a node's latency to each reference point over
// the last 24 hours (or the hours asked, up to the monitoring retention).
func (h *Handler) nodeLatencyHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID    int64 `json:"id"`
		Hours int   `json:"hours"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.Hours <= 0 || req.Hours > 24*31 {
		req.Hours = 24
	}
	now := time.Now().UnixMilli()
	since := now - int64(req.Hours)*int64(time.Hour/time.Millisecond)
	samples, err := h.repo.ListNodeLatencySamples(req.ID, since)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"since":  since,
		"until":  now,
		"points": buildTrend(samples, since, now),
	}))
}
