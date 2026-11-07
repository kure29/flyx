package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
)

// Return routes: each node traces the route to every reference point and the
// backbone networks on it name the line (CN2 GIA, 163, 9929, CMI ...).
const (
	routeRefreshInterval = 24 * time.Hour
	// Failed traces (old agent, no raw socket) are retried sooner.
	routeRetryInterval  = 6 * time.Hour
	routeCommandTimeout = 20 * time.Second
	routeParallel       = 4
)

// Backbone prefixes seen on return routes into China.
var routeBackbones = []struct {
	prefix string
	net    string
}{
	{"59.43.", "CN2"},
	{"202.97.", "163"},
	{"219.158.", "4837"},
	{"218.105.", "9929"},
	{"210.51.", "9929"},
	{"223.118.", "CMI"},
	{"223.119.", "CMI"},
	{"223.120.", "CMI"},
	{"223.121.", "CMI"},
}

// routeHopNet names the backbone an address belongs to, or "".
func routeHopNet(ip string) string {
	for _, b := range routeBackbones {
		if strings.HasPrefix(ip, b.prefix) {
			return b.net
		}
	}
	return ""
}

// IPv6 has no backbone-only prefixes, so IPv6 hops are named by origin AS.
// AS4134 carries both the 163 backbone and provincial ChinaNet: it is "CT",
// and classifyRoute tells CN2 GIA from GT by whether CT comes before CN2.
var routeASNs = map[string]string{
	"4809":  "CN2",
	"4134":  "CT",
	"9929":  "9929",
	"4837":  "4837",
	"58453": "CMI",
	"58807": "CMIN2",
}

// hopNet names the network a hop belongs to, or "".
func hopNet(ip string) string {
	addr := net.ParseIP(ip)
	switch {
	case addr == nil:
		return ""
	case addr.To4() != nil:
		return refineHopNet(ip, routeHopNet(ip))
	case !addr.IsGlobalUnicast() || addr.IsPrivate():
		return ""
	}
	return routeASNs[hopOriginASN(ip)]
}

// asnQueryName is the Team Cymru origin query for an address:
// d.c.b.a.origin.asn.cymru.com, or the reversed nibbles of an IPv6 address
// under origin6.asn.cymru.com.
func asnQueryName(ip string) string {
	addr := net.ParseIP(ip)
	if addr == nil {
		return ""
	}
	if v4 := addr.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", v4[3], v4[2], v4[1], v4[0])
	}
	var b strings.Builder
	v6 := addr.To16()
	for i := len(v6) - 1; i >= 0; i-- {
		fmt.Fprintf(&b, "%x.%x.", v6[i]&0x0f, v6[i]>>4)
	}
	return b.String() + "origin6.asn.cymru.com"
}

// China Mobile's international networks share address space, so CMI hops
// are told apart by origin AS: AS58807 is CMIN2, AS58453 CMI. Team Cymru's
// DNS service answers "58807 | 223.x.x.0/24 | HK | apnic | ..." for
// d.c.b.a.origin.asn.cymru.com.
var (
	hopASNMu    sync.Mutex
	hopASNCache = make(map[string]string)
)

// hopASNLookup returns the TXT records for an origin AS query; tests
// replace it.
var hopASNLookup = func(name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func hopOriginASN(ip string) string {
	hopASNMu.Lock()
	asn, ok := hopASNCache[ip]
	hopASNMu.Unlock()
	if ok {
		return asn
	}
	name := asnQueryName(ip)
	if name == "" {
		return ""
	}
	txt, err := hopASNLookup(name)
	if err != nil {
		return "" // not cached, so a later trace asks again
	}
	if len(txt) > 0 {
		// A prefix announced by several ASes lists them space-separated.
		if f := strings.Fields(strings.SplitN(txt[0], "|", 2)[0]); len(f) > 0 {
			asn = f[0]
		}
	}
	hopASNMu.Lock()
	hopASNCache[ip] = asn
	hopASNMu.Unlock()
	return asn
}

// refineHopNet splits CMI hops into CMI and CMIN2 by origin AS; others
// keep their prefix match.
func refineHopNet(ip, prefixNet string) string {
	if prefixNet != "CMI" {
		return prefixNet
	}
	if hopOriginASN(ip) == "58807" {
		return "CMIN2"
	}
	return "CMI"
}

// classifyRoute names a return route from the backbones its hops cross.
func classifyRoute(hops []routeHop) string {
	seen := make(map[string]bool)
	ctBeforeCN2 := false
	for _, h := range hops {
		if h.Net == "CT" && !seen["CN2"] {
			ctBeforeCN2 = true
		}
		if h.Net != "" {
			seen[h.Net] = true
		}
	}
	switch {
	case seen["CN2"] && !seen["163"] && !ctBeforeCN2:
		return "电信 CN2 GIA"
	case seen["CN2"]:
		return "电信 CN2 GT"
	case seen["163"] || seen["CT"]:
		return "电信 163"
	case seen["9929"]:
		return "联通 9929"
	case seen["4837"]:
		return "联通 4837"
	case seen["CMIN2"]:
		return "移动 CMIN2"
	case seen["CMI"]:
		return "移动 CMI"
	}
	return "未识别"
}

type routeHop struct {
	TTL   int     `json:"ttl"`
	IP    string  `json:"ip,omitempty"`
	RttMs float64 `json:"rttMs,omitempty"`
	Net   string  `json:"net,omitempty"`
}

type routePoint struct {
	Name    string `json:"name"`
	Route   string `json:"route,omitempty"`
	Reached bool   `json:"reached"`
	// Family is "IPv6" when the route was traced over IPv6.
	Family string     `json:"family,omitempty"`
	Hops   []routeHop `json:"hops,omitempty"`
	// IPv6 is a dual-stack node's IPv6 route to the same point, traced
	// next to the IPv4 one.
	IPv6  *routePoint `json:"ipv6,omitempty"`
	Error string      `json:"error,omitempty"`
	// host is the address traced, so an edited point is traced again.
	host string
}

type nodeRoute struct {
	CheckedAt int64        `json:"checkedAt"`
	Points    []routePoint `json:"points"`
	failed    bool
	// agent is the node's agent version when traced, so an upgraded agent
	// (one that gained Traceroute, say) is traced again right away.
	agent string
}

type nodeRouteStore struct {
	mu      sync.RWMutex
	results map[int64]nodeRoute
}

func newNodeRouteStore() *nodeRouteStore {
	return &nodeRouteStore{results: make(map[int64]nodeRoute)}
}

func (s *nodeRouteStore) get(nodeID int64) (nodeRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.results[nodeID]
	return r, ok
}

func (s *nodeRouteStore) set(nodeID int64, r nodeRoute) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[nodeID] = r
}

// nodeTraceroute runs one Traceroute from a node; tests replace it.
// family 4 / 6 asks for that IP version only, 0 lets the node pick.
var nodeTraceroute = func(h *Handler, nodeID int64, host string, family int) (map[string]interface{}, error) {
	payload := map[string]interface{}{"host": host}
	if family != 0 {
		payload["family"] = family
	}
	res, err := h.sendNodeCommandWithTimeout(nodeID, "Traceroute", payload, routeCommandTimeout, false, false)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

func routePointFrom(t latencyTarget, data map[string]interface{}, err error) routePoint {
	p := routePoint{Name: t.Name, host: t.Host}
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "未知命令"):
			p.Error = "服务器 agent 版本过旧，升级后可检测回程"
		case strings.Contains(msg, "NET_RAW"):
			p.Error = "服务器没有 root / NET_RAW 权限"
		case strings.Contains(msg, "没有 IPv6 地址"):
			p.Error = "服务器只有 IPv6，测试点没有 IPv6 地址"
		case strings.Contains(msg, "没有 IPv4 地址"):
			p.Error = "测试点只有 IPv6 地址，服务器没有 IPv6"
		case strings.Contains(msg, "路由"):
			p.Error = "服务器到测试点没有路由"
		case strings.Contains(msg, "DNS"):
			p.Error = "域名解析失败"
		default:
			p.Error = "检测失败"
		}
		return p
	}
	p.Reached = asBool(data["reached"], false)
	if addr := net.ParseIP(asString(data["ip"])); addr != nil && addr.To4() == nil {
		p.Family = "IPv6"
	}
	rawHops, _ := data["hops"].([]interface{})
	warmHopASNs(rawHops)
	for _, raw := range rawHops {
		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		ip := asString(m["ip"])
		if ip != "" && net.ParseIP(ip) == nil {
			ip = ""
		}
		p.Hops = append(p.Hops, routeHop{
			TTL:   int(asInt64(m["ttl"], 0)),
			IP:    ip,
			RttMs: asFloat(m["rttMs"], 0),
			Net:   hopNet(ip),
		})
	}
	p.Route = classifyRoute(p.Hops)
	return p
}

// warmHopASNs looks up the origin AS of a trace's IPv6 hops in parallel;
// hopNet then reads them from the cache.
func warmHopASNs(rawHops []interface{}) {
	var wg sync.WaitGroup
	for _, raw := range rawHops {
		m, _ := raw.(map[string]interface{})
		ip := asString(m["ip"])
		if addr := net.ParseIP(ip); addr == nil || addr.To4() != nil || !addr.IsGlobalUnicast() || addr.IsPrivate() {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			hopOriginASN(ip)
		}()
	}
	wg.Wait()
}

// traceIPv6Leg traces a point again over IPv6 after an IPv4 trace. Nodes or
// points without IPv6 have no IPv6 leg; an old agent that ignores the
// family answers over IPv4 again, which is dropped.
func traceIPv6Leg(h *Handler, nodeID int64, t latencyTarget) *routePoint {
	data, err := nodeTraceroute(h, nodeID, t.Host, 6)
	if err != nil {
		return nil
	}
	leg := routePointFrom(t, data, nil)
	if leg.Family != "IPv6" {
		return nil
	}
	return &leg
}

// traceNodeRoutes traces the given local online nodes (all when ids is
// empty) to every enabled reference point. With force false it skips nodes
// whose last trace is fresh and covers the current points.
func (h *Handler) traceNodeRoutes(ctx context.Context, ids []int64, force bool) (map[int64]nodeRoute, error) {
	nodes, err := h.repo.ListMonitorNodes()
	if err != nil {
		return nil, err
	}
	pool := h.latencyPool()
	if len(pool) == 0 {
		return map[int64]nodeRoute{}, nil
	}
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	now := time.Now()
	type job struct {
		targets []latencyTarget
		agent   string
		name    string
	}
	todo := make(map[int64]job)
	for _, n := range nodes {
		if n.Status != 1 || (len(ids) > 0 && !want[n.ID]) {
			continue
		}
		targets := targetsForNode(pool, n.LatencyTargets)
		if len(targets) == 0 {
			continue
		}
		agent := n.Version.String
		if !force {
			if r, ok := h.nodeRoutes.get(n.ID); ok && routeFresh(r, targets, agent, now) {
				continue
			}
		}
		todo[n.ID] = job{targets: targets, agent: agent, name: n.Name}
	}

	results := make(map[int64]nodeRoute, len(todo))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, routeParallel)
	for id, j := range todo {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(nodeID int64, targets []latencyTarget, agent, name string) {
			defer wg.Done()
			defer func() { <-sem }()
			r := nodeRoute{Points: make([]routePoint, len(targets)), agent: agent}
			// One trace at a time per node keeps the agent's raw socket quiet.
			for i, t := range targets {
				data, err := nodeTraceroute(h, nodeID, t.Host, 0)
				r.Points[i] = routePointFrom(t, data, err)
				if r.Points[i].Error == "" && r.Points[i].Family == "" {
					r.Points[i].IPv6 = traceIPv6Leg(h, nodeID, t)
				}
				if r.Points[i].Error != "" {
					r.failed = true
				}
			}
			r.CheckedAt = time.Now().UnixMilli()
			if prev, ok := h.nodeRoutes.get(nodeID); ok {
				if changes := routeChanges(prev, r); len(changes) > 0 {
					h.notifyForNode(nodeID, notifyEventRouteChange, "回程线路变化",
						fmt.Sprintf("服务器：%s\n%s", name, strings.Join(changes, "\n")))
				}
			}
			h.nodeRoutes.set(nodeID, r)
			mu.Lock()
			results[nodeID] = r
			mu.Unlock()
		}(id, j.targets, j.agent, j.name)
	}
	wg.Wait()
	return results, nil
}

// routeFresh reports whether a stored trace is recent and was made by the
// node's current agent to the points it measures now.
func routeFresh(r nodeRoute, targets []latencyTarget, agent string, now time.Time) bool {
	if r.agent != agent || len(r.Points) != len(targets) {
		return false
	}
	for i, t := range targets {
		if r.Points[i].Name != t.Name || r.Points[i].host != t.Host {
			return false
		}
	}
	every := routeRefreshInterval
	if r.failed {
		every = routeRetryInterval
	}
	return now.Sub(time.UnixMilli(r.CheckedAt)) < every
}

// withRoutes adds each point's return route name to a latency result.
func (h *Handler) withRoutes(nodeID int64, p reachProbe) reachProbe {
	r, ok := h.nodeRoutes.get(nodeID)
	if !ok || len(p.Points) == 0 {
		return p
	}
	byName := make(map[string]routePoint, len(r.Points))
	for _, rp := range r.Points {
		byName[rp.Name] = rp
	}
	points := make([]latencyPoint, len(p.Points))
	for i, pt := range p.Points {
		rp := byName[pt.Name]
		pt.Route, pt.RouteV6 = rp.Route, ""
		if rp.IPv6 != nil && rp.IPv6.Error == "" {
			pt.RouteV6 = rp.IPv6.Route
		}
		points[i] = pt
	}
	p.Points = points
	return p
}

// nodeRouteGet returns a node's last traces with every hop.
func (h *Handler) nodeRouteGet(w http.ResponseWriter, r *http.Request) {
	h.nodeRouteRespond(w, r, false)
}

// nodeRouteProbe traces a node's return routes now.
func (h *Handler) nodeRouteProbe(w http.ResponseWriter, r *http.Request) {
	h.nodeRouteRespond(w, r, true)
}

func (h *Handler) nodeRouteRespond(w http.ResponseWriter, r *http.Request, probe bool) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID int64 `json:"id"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if probe {
		if len(h.latencyPool()) == 0 {
			response.WriteJSON(w, response.ErrDefault("还没有设置延迟测试点，请到网站配置添加"))
			return
		}
		if _, err := h.traceNodeRoutes(r.Context(), []int64{req.ID}, true); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	route, ok := h.nodeRoutes.get(req.ID)
	if !ok {
		response.WriteJSON(w, response.OK(nil))
		return
	}
	response.WriteJSON(w, response.OK(route))
}
