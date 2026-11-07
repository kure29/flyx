package handler

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
)

// The status page's history, without a login: a node's metrics and latency
// over the last hour to week, averaged into at most ~360 points. Nodes are
// named by their position on the page (the id /public/status gives them),
// never by database id, and a hidden node answers exactly as a missing one.

type statusHistoryRange struct {
	span   time.Duration
	bucket time.Duration
	// How long one answer is reused.
	cache time.Duration
}

var statusHistoryRanges = map[string]statusHistoryRange{
	"1h":  {time.Hour, 10 * time.Second, 10 * time.Second},
	"6h":  {6 * time.Hour, time.Minute, time.Minute},
	"24h": {24 * time.Hour, 4 * time.Minute, time.Minute},
	"7d":  {7 * 24 * time.Hour, 28 * time.Minute, time.Minute},
}

// At most this many reference points per node, as on the page.
const statusHistoryMaxPoints = 8

// statusGate is what the page shows, re-read at most every statusCacheMaxAge.
type statusGate struct {
	at          time.Time
	enabled     bool
	showTraffic bool
	// Public id (position on the page) → database id.
	nodes map[int64]int64
}

type statusHistoryCache struct {
	mu      sync.Mutex
	gate    statusGate
	entries map[string]statusHistoryEntry
}

type statusHistoryEntry struct {
	at  time.Time
	ttl time.Duration
	raw []byte
}

func (h *Handler) statusHistoryGate(now time.Time) (statusGate, error) {
	c := h.statusHistory
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.gate.at.IsZero() && now.Sub(c.gate.at) >= 0 && now.Sub(c.gate.at) < statusCacheMaxAge {
		return c.gate, nil
	}
	cfg, err := h.repo.GetConfigsByNames(statusPageConfigKeys)
	if err != nil {
		return statusGate{}, err
	}
	g := statusGate{
		at:          now,
		enabled:     strings.TrimSpace(cfg[statusPageEnabledKey]) == "true",
		showTraffic: strings.TrimSpace(cfg[statusPageShowTrafficKey]) != "false",
		nodes:       map[int64]int64{},
	}
	if g.enabled {
		nodes, err := h.repo.ListStatusPageNodes()
		if err != nil {
			return statusGate{}, err
		}
		hidden := parseStatusHiddenNodes(cfg[statusPageHiddenNodesKey])
		pos := int64(0)
		for _, n := range nodes {
			if !hidden[n.ID] {
				pos++
				g.nodes[pos] = n.ID
			}
		}
	}
	c.gate = g
	return g, nil
}

type statusMetricPoint struct {
	T      int64    `json:"t"`
	CPU    float64  `json:"cpu"`
	Mem    float64  `json:"mem"`
	Swap   float64  `json:"swap"`
	Disk   float64  `json:"disk"`
	Load   float64  `json:"load"`
	TCP    float64  `json:"tcp"`
	UDP    float64  `json:"udp"`
	Procs  float64  `json:"procs"`
	NetIn  *float64 `json:"netIn,omitempty"`
	NetOut *float64 `json:"netOut,omitempty"`
}

type statusMetricHistory struct {
	Range     string              `json:"range"`
	BucketSec int64               `json:"bucketSec"`
	Points    []statusMetricPoint `json:"points"`
}

type statusPingSample struct {
	T    int64    `json:"t"`
	Ms   *float64 `json:"ms"`
	Loss float64  `json:"loss"`
}

type statusPingSeries struct {
	Name    string             `json:"name"`
	Samples []statusPingSample `json:"samples"`
}

type statusPingHistory struct {
	Range     string             `json:"range"`
	BucketSec int64              `json:"bucketSec"`
	Points    []statusPingSeries `json:"points"`
}

func (h *Handler) publicStatusHistory(w http.ResponseWriter, r *http.Request) {
	h.serveStatusHistory(w, r, "metric")
}

func (h *Handler) publicStatusPingHistory(w http.ResponseWriter, r *http.Request) {
	h.serveStatusHistory(w, r, "ping")
}

func (h *Handler) serveStatusHistory(w http.ResponseWriter, r *http.Request, kind string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	q := r.URL.Query()
	rangeName := strings.TrimSpace(q.Get("range"))
	if rangeName == "" {
		rangeName = "1h"
	}
	rg, okRange := statusHistoryRanges[rangeName]
	pubID, err := strconv.ParseInt(strings.TrimSpace(q.Get("node")), 10, 64)
	if !okRange || err != nil || pubID <= 0 {
		response.WriteJSON(w, response.Err(400, "参数错误：node 为状态页里的服务器 id，range 为 1h、6h、24h 或 7d"))
		return
	}

	now := time.Now()
	gate, err := h.statusHistoryGate(now)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "状态数据暂时不可用"))
		return
	}
	if !gate.enabled {
		response.WriteJSON(w, response.Err(404, "状态页未开启"))
		return
	}
	nodeID, found := gate.nodes[pubID]
	if !found {
		response.WriteJSON(w, response.Err(404, "没有这台服务器"))
		return
	}

	// Keyed by the database id and what the answer depends on, so a node
	// changing position or the traffic switch flipping is not served stale.
	key := kind + "|" + rangeName + "|" + strconv.FormatInt(nodeID, 10) + "|" + strconv.FormatBool(gate.showTraffic)
	raw, err := h.statusHistoryAnswer(key, rg.cache, now, func() (interface{}, error) {
		if kind == "ping" {
			return h.buildStatusPingHistory(nodeID, rangeName, rg, now)
		}
		return h.buildStatusMetricHistory(nodeID, rangeName, rg, gate.showTraffic, now)
	})
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "状态数据暂时不可用"))
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json; charset=utf-8")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Referrer-Policy", "no-referrer")
	hdr.Set("Cache-Control", "public, max-age="+strconv.Itoa(int(rg.cache/time.Second)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(raw)
}

func (h *Handler) statusHistoryAnswer(key string, ttl time.Duration, now time.Time, build func() (interface{}, error)) ([]byte, error) {
	c := h.statusHistory
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && now.Sub(e.at) >= 0 && now.Sub(e.at) < e.ttl {
		c.mu.Unlock()
		return e.raw, nil
	}
	c.mu.Unlock()

	body, err := build()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(response.OK(body))
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]statusHistoryEntry{}
	}
	// Drop what has expired; entries are bounded by nodes × ranges × kinds.
	for k, e := range c.entries {
		if now.Sub(e.at) >= e.ttl {
			delete(c.entries, k)
		}
	}
	c.entries[key] = statusHistoryEntry{at: now, ttl: ttl, raw: raw}
	return raw, nil
}

func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*100) / 100
}

func (h *Handler) buildStatusMetricHistory(nodeID int64, rangeName string, rg statusHistoryRange, showTraffic bool, now time.Time) (*statusMetricHistory, error) {
	end := now.UnixMilli()
	rows, err := h.repo.NodeMetricBuckets(nodeID, end-rg.span.Milliseconds(), end, rg.bucket.Milliseconds())
	if err != nil {
		return nil, err
	}
	out := &statusMetricHistory{Range: rangeName, BucketSec: int64(rg.bucket / time.Second), Points: make([]statusMetricPoint, 0, len(rows))}
	for _, b := range rows {
		p := statusMetricPoint{
			T: b.T, CPU: round2(b.CPU), Mem: round2(b.Mem), Swap: round2(b.Swap), Disk: round2(b.Disk),
			Load: round2(b.Load), TCP: math.Round(b.TCP), UDP: math.Round(b.UDP), Procs: math.Round(b.Procs),
		}
		if showTraffic {
			in, outv := math.Round(b.NetIn), math.Round(b.NetOut)
			p.NetIn, p.NetOut = &in, &outv
		}
		out.Points = append(out.Points, p)
	}
	return out, nil
}

func (h *Handler) buildStatusPingHistory(nodeID int64, rangeName string, rg statusHistoryRange, now time.Time) (*statusPingHistory, error) {
	end := now.UnixMilli()
	rows, err := h.repo.NodeLatencyBuckets(nodeID, end-rg.span.Milliseconds(), end, rg.bucket.Milliseconds())
	if err != nil {
		return nil, err
	}
	series := map[string]*statusPingSeries{}
	var names []string
	for _, b := range rows {
		s := series[b.Point]
		if s == nil {
			s = &statusPingSeries{Name: b.Point}
			series[b.Point] = s
			names = append(names, b.Point)
		}
		sample := statusPingSample{T: b.T, Loss: round2(b.Loss)}
		if b.Ms.Valid {
			ms := math.Round(b.Ms.Float64)
			sample.Ms = &ms
		}
		s.Samples = append(s.Samples, sample)
	}
	orderStatusPingNames(names, h.statusPinnedPoints(nodeID))
	if len(names) > statusHistoryMaxPoints {
		names = names[:statusHistoryMaxPoints]
	}
	out := &statusPingHistory{Range: rangeName, BucketSec: int64(rg.bucket / time.Second), Points: make([]statusPingSeries, 0, len(names))}
	for _, n := range names {
		out.Points = append(out.Points, *series[n])
	}
	return out, nil
}

// orderStatusPingNames puts the pinned points first, in their order, then
// the rest by name, as the page does.
func orderStatusPingNames(names, pinned []string) {
	rank := map[string]int{}
	for i, p := range pinned {
		if _, seen := rank[p]; !seen {
			rank[p] = i
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		ri, pi := rank[names[i]]
		rj, pj := rank[names[j]]
		switch {
		case pi && pj:
			return ri < rj
		case pi != pj:
			return pi
		default:
			return names[i] < names[j]
		}
	})
}
