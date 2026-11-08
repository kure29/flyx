package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
	"go-backend/internal/ws"
)

// The public status page: anyone may see which servers are up, how
// available they have been, their load and traffic. The payload never
// contains addresses, ports, users, protocols or prices, and it is served
// only while the admin has turned the page on.

const (
	statusPageEnabledKey     = "status_page_enabled"
	statusPageTitleKey       = "status_page_title"
	statusPageHiddenNodesKey = "status_page_hidden_nodes"
	statusPageShowExpiryKey  = "status_page_show_expiry"
	statusPageShowTrafficKey = "status_page_show_traffic"
	// Off by default: the system, architecture, virtualization and CPU model.
	statusPageShowHostKey = "status_page_show_host"

	// uptimeBucketSeconds is the size of a node_uptime bucket; a sample is
	// taken every minute, so a full bucket holds 10 samples.
	uptimeBucketSeconds = 600
	statusPageDays      = 30
	// A node's live figures older than this are not shown.
	statusLiveMaxAge  = 3 * time.Minute
	statusCacheMaxAge = 5 * time.Second
	// Latency shown per node: this many reference points, this many rounds.
	statusPingPoints  = 3
	statusPingSamples = 20
	statusPingWindow  = time.Hour
)

var statusPageConfigKeys = []string{
	"app_name",
	statusPageEnabledKey,
	statusPageTitleKey,
	statusPageHiddenNodesKey,
	statusPageShowExpiryKey,
	statusPageShowTrafficKey,
	statusPageShowHostKey,
}

// statusLiveStore keeps the latest system info each node reported.
type statusLiveStore struct {
	mu    sync.RWMutex
	nodes map[int64]statusLiveSample
}

type statusLiveSample struct {
	at      time.Time
	cpu     float64
	mem     float64
	disk    float64
	load    [3]float64
	up      int64
	down    int64
	uptime  uint64
	tcp     int64
	udp     int64
	hasInfo bool
	// Capacity; zero when the agent predates reporting it.
	cores               int
	memUsed, memTotal   uint64
	diskUsed, diskTotal uint64
	swapUsed, swapTotal uint64
	procs               int64
}

func newStatusLiveStore() *statusLiveStore {
	return &statusLiveStore{nodes: map[int64]statusLiveSample{}}
}

func (s *statusLiveStore) set(nodeID int64, v statusLiveSample) {
	if s == nil {
		return
	}
	v.at = time.Now()
	v.hasInfo = true
	s.mu.Lock()
	s.nodes[nodeID] = v
	s.mu.Unlock()
}

func (s *statusLiveStore) get(nodeID int64) (statusLiveSample, bool) {
	if s == nil {
		return statusLiveSample{}, false
	}
	s.mu.RLock()
	v, ok := s.nodes[nodeID]
	s.mu.RUnlock()
	if !ok || time.Since(v.at) > statusLiveMaxAge {
		return statusLiveSample{}, false
	}
	return v, true
}

// statusPageCache holds the encoded response everyone gets for a few
// seconds. The page is public, so a flood of requests must cost no more than
// serving a static file: the response is encoded once, and the lock lets one
// request rebuild it while the others wait for that result instead of all
// querying the database.
type statusPageCache struct {
	mu   sync.Mutex
	at   time.Time
	body statusBody
}

// ---------------------------------------------------------------- sampling

func (h *Handler) runNodeUptimeLoop(ctx context.Context) {
	defer h.jobsWG.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			h.sampleNodeUptime(now)
		}
	}
}

// sampleNodeUptime records which nodes are online now and drops buckets
// older than the page shows.
func (h *Handler) sampleNodeUptime(now time.Time) {
	nodes, err := h.repo.ListStatusPageNodes()
	if err != nil {
		return
	}
	online := make(map[int64]bool, len(nodes))
	for _, n := range nodes {
		online[n.ID] = n.Status == 1
	}
	bucket := now.Unix() / uptimeBucketSeconds
	_ = h.repo.RecordNodeUptime(bucket, online)
	if now.Minute()%30 == 0 {
		_ = h.repo.PruneNodeUptime(bucket - (statusPageDays+1)*24*3600/uptimeBucketSeconds)
	}
}

// ---------------------------------------------------------------- payload

type statusPageAvailability struct {
	H24  *float64   `json:"h24"`
	D30  *float64   `json:"d30"`
	Days []*float64 `json:"days"`
	// Down is the minutes down per day (a sample a minute), null = no data.
	Down []*int64 `json:"down"`
}

type statusPageNode struct {
	ID           int64                   `json:"id"`
	Name         string                  `json:"name"`
	Online       bool                    `json:"online"`
	Availability *statusPageAvailability `json:"availability"`
	CPU          *float64                `json:"cpu,omitempty"`
	Mem          *float64                `json:"mem,omitempty"`
	Disk         *float64                `json:"disk,omitempty"`
	Load         *[3]float64             `json:"load,omitempty"`
	SpeedUp      *int64                  `json:"speedUp,omitempty"`
	SpeedDown    *int64                  `json:"speedDown,omitempty"`
	Uptime       *uint64                 `json:"uptime,omitempty"`
	TCP          *int64                  `json:"tcp,omitempty"`
	UDP          *int64                  `json:"udp,omitempty"`
	// Capacity, from agents that report it: logical CPUs and bytes.
	Cores     int    `json:"cores,omitempty"`
	MemUsed   uint64 `json:"memUsed,omitempty"`
	MemTotal  uint64 `json:"memTotal,omitempty"`
	DiskUsed  uint64 `json:"diskUsed,omitempty"`
	DiskTotal uint64 `json:"diskTotal,omitempty"`
	// From agents of 3.9.0 on.
	Swap      *float64 `json:"swap,omitempty"`
	SwapUsed  uint64   `json:"swapUsed,omitempty"`
	SwapTotal uint64   `json:"swapTotal,omitempty"`
	Procs     *int64   `json:"procs,omitempty"`
	// Only while the admin shows system information; never the kernel.
	OS             string `json:"os,omitempty"`
	Arch           string `json:"arch,omitempty"`
	Virtualization string `json:"virtualization,omitempty"`
	CPUName        string `json:"cpuName,omitempty"`
	// MonthUp / MonthDown are the bytes since the 1st of this month.
	MonthUp   *int64 `json:"monthUp,omitempty"`
	MonthDown *int64 `json:"monthDown,omitempty"`
	Expiry    string `json:"expiry,omitempty"`
	// Region is the node's country, ISO 3166-1 alpha-2 (HK, JP, US …): set by
	// the admin, else detected from its address. Never the address itself.
	Region string `json:"region,omitempty"`
	// Ping is the latency to the node's reference points over the last
	// hour: names the admin gave them only, never their addresses or errors.
	Ping []statusPagePing `json:"ping,omitempty"`
}

type statusPagePing struct {
	Name string `json:"name"`
	// Ms is the latest round's latency, null when it failed.
	Ms *int64 `json:"ms"`
	// Loss is the average loss over the hour, percent.
	Loss float64 `json:"loss"`
	// Samples are the hour's rounds, oldest first: latency in ms, -1 failed.
	Samples []int64 `json:"samples"`
	// Route is the line the point's return route takes (CN2 GIA, 9929, ...),
	// from the daily trace; RouteV6 the IPv6 one on a dual-stack node when
	// it differs. Line names only, never the hops.
	Route   string `json:"route,omitempty"`
	RouteV6 string `json:"routeV6,omitempty"`
}

type statusPageTotals struct {
	Nodes     int   `json:"nodes"`
	Online    int   `json:"online"`
	SpeedUp   int64 `json:"speedUp"`
	SpeedDown int64 `json:"speedDown"`
	MonthUp   int64 `json:"monthUp"`
	MonthDown int64 `json:"monthDown"`
	// D30 is the average 30-day availability of the nodes with data.
	D30 *float64 `json:"d30"`
}

type statusPagePayload struct {
	Title       string           `json:"title"`
	Timezone    string           `json:"timezone"`
	GeneratedAt int64            `json:"generatedAt"`
	Days        []string         `json:"days"`
	ShowTraffic bool             `json:"showTraffic"`
	Nodes       []statusPageNode `json:"nodes"`
	Totals      statusPageTotals `json:"totals"`
}

// publicStatus serves the status page's data, without a login.
func (h *Handler) publicStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	body, err := h.statusPageBody(time.Now())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "状态数据暂时不可用"))
		return
	}
	writeStatusBody(w, r, body, int(statusCacheMaxAge/time.Second))
}

// statusPageResponse returns the encoded (uncompressed) response.
func (h *Handler) statusPageResponse(now time.Time) ([]byte, error) {
	b, err := h.statusPageBody(now)
	return b.raw, err
}

// statusPageBody returns the encoded response, rebuilt (and compressed) at
// most once per statusCacheMaxAge however many requests arrive. While the
// page is off the response says so and holds no data.
func (h *Handler) statusPageBody(now time.Time) (statusBody, error) {
	c := h.statusCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.body.raw != nil && now.Sub(c.at) >= 0 && now.Sub(c.at) < statusCacheMaxAge {
		return c.body, nil
	}
	cfg, err := h.repo.GetConfigsByNames(statusPageConfigKeys)
	if err != nil {
		return statusBody{}, err
	}
	var payload response.R
	if strings.TrimSpace(cfg[statusPageEnabledKey]) != "true" {
		payload = response.Err(404, "状态页未开启")
	} else {
		page, err := h.buildStatusPage(cfg, now)
		if err != nil {
			return statusBody{}, err
		}
		payload = response.OK(page)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return statusBody{}, err
	}
	c.body, c.at = newStatusBody(raw), now
	return c.body, nil
}

func (h *Handler) buildStatusPage(cfg map[string]string, now time.Time) (*statusPagePayload, error) {
	loc := time.Local
	title := strings.TrimSpace(cfg[statusPageTitleKey])
	if title == "" {
		title = strings.TrimSpace(cfg["app_name"])
	}
	showTraffic := strings.TrimSpace(cfg[statusPageShowTrafficKey]) != "false"
	showExpiry := strings.TrimSpace(cfg[statusPageShowExpiryKey]) == "true"
	showHost := strings.TrimSpace(cfg[statusPageShowHostKey]) == "true"
	hidden := parseStatusHiddenNodes(cfg[statusPageHiddenNodesKey])

	out := &statusPagePayload{
		Title:       title,
		Timezone:    statusZoneLabel(now.In(loc)),
		GeneratedAt: now.UnixMilli(),
		ShowTraffic: showTraffic,
		Nodes:       []statusPageNode{},
	}
	local := now.In(loc)
	for i := statusPageDays - 1; i >= 0; i-- {
		out.Days = append(out.Days, local.AddDate(0, 0, -i).Format("2006-01-02"))
	}

	nodes, err := h.repo.ListStatusPageNodes()
	if err != nil {
		return nil, err
	}
	shown := make([]model.Node, 0, len(nodes))
	for _, n := range nodes {
		if !hidden[n.ID] {
			shown = append(shown, n)
		}
	}

	uptime, err := h.repo.NodeUptimeSince(now.Unix()/uptimeBucketSeconds - int64(statusPageDays*24*3600/uptimeBucketSeconds))
	if err != nil {
		return nil, err
	}
	avail := computeStatusAvailability(uptime, out.Days, now, loc)

	var month map[int64][2]int64
	if showTraffic {
		start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
		if month, err = h.repo.NodeTrafficSince(start.UnixMilli()); err != nil {
			return nil, err
		}
	}

	samples, err := h.repo.ListLatencySamplesSince(now.Add(-statusPingWindow).UnixMilli())
	if err != nil {
		return nil, err
	}
	byNode := map[int64][]model.NodeLatencySample{}
	for _, row := range samples {
		byNode[row.NodeID] = append(byNode[row.NodeID], row)
	}

	var d30Sum float64
	var d30N int
	for i, n := range shown {
		// The position, not the database id: nothing on the page needs it.
		v := statusPageNode{ID: int64(i + 1), Name: n.Name, Online: n.Status == 1, Availability: avail[n.ID]}
		if v.Availability == nil {
			v.Availability = emptyStatusAvailability(len(out.Days))
		}
		if v.Online {
			out.Totals.Online++
			if s, ok := h.statusLive.get(n.ID); ok {
				cpu, mem, disk, load := s.cpu, s.mem, s.disk, s.load
				v.CPU, v.Mem, v.Disk, v.Load = &cpu, &mem, &disk, &load
				up, down, upt, tcp, udp := s.up, s.down, s.uptime, s.tcp, s.udp
				v.Uptime, v.TCP, v.UDP = &upt, &tcp, &udp
				v.Cores = s.cores
				if s.memTotal > 0 {
					v.MemUsed, v.MemTotal = s.memUsed, s.memTotal
				}
				if s.diskTotal > 0 {
					v.DiskUsed, v.DiskTotal = s.diskUsed, s.diskTotal
				}
				if s.swapTotal > 0 {
					swap := swapUsage(s.swapUsed, s.swapTotal)
					v.Swap, v.SwapUsed, v.SwapTotal = &swap, s.swapUsed, s.swapTotal
				}
				if s.procs > 0 {
					procs := s.procs
					v.Procs = &procs
				}
				if showTraffic {
					v.SpeedUp, v.SpeedDown = &up, &down
					out.Totals.SpeedUp += up
					out.Totals.SpeedDown += down
				}
			}
		}
		if showTraffic {
			t := month[n.ID]
			// node_traffic in = received, out = sent
			down, up := t[0], t[1]
			v.MonthUp, v.MonthDown = &up, &down
			out.Totals.MonthUp += up
			out.Totals.MonthDown += down
		}
		if v.Availability.D30 != nil {
			d30Sum += *v.Availability.D30
			d30N++
		}
		v.Ping = statusPingRows(byNode[n.ID], h.statusPinnedPoints(n.ID))
		if r, ok := h.nodeRoutes.get(n.ID); ok {
			statusPingRoutes(v.Ping, r)
		}
		if showHost {
			v.OS, v.Arch, v.Virtualization, v.CPUName = n.HostOS, n.HostArch, n.HostVirtualization, n.HostCPUName
		}
		if showExpiry && n.ExpiryTime.Valid && n.ExpiryTime.Int64 > 0 {
			v.Expiry = time.UnixMilli(n.ExpiryTime.Int64).In(loc).Format("2006-01-02")
		}
		v.Region = n.EffectiveRegion()
		out.Nodes = append(out.Nodes, v)
	}
	out.Totals.Nodes = len(out.Nodes)
	if d30N > 0 {
		avg := d30Sum / float64(d30N)
		out.Totals.D30 = &avg
	}
	return out, nil
}

// statusZoneLabel names the panel's time zone for visitors: the IANA name
// when the process has one (TZ=Asia/Shanghai), else the UTC offset.
func statusZoneLabel(t time.Time) string {
	if name := t.Location().String(); name != "" && name != "Local" {
		return name
	}
	return "UTC" + t.Format("-07:00")
}

func parseStatusHiddenNodes(raw string) map[int64]bool {
	out := map[int64]bool{}
	for _, part := range strings.Split(raw, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
			out[id] = true
		}
	}
	return out
}

func emptyStatusAvailability(days int) *statusPageAvailability {
	return &statusPageAvailability{Days: make([]*float64, days), Down: make([]*int64, days)}
}

// computeStatusAvailability turns uptime buckets into 24-hour, 30-day and
// per-day availability per node; days are local dates, oldest first.
func computeStatusAvailability(rows []model.NodeUptime, days []string, now time.Time, loc *time.Location) map[int64]*statusPageAvailability {
	type acc struct{ n, up int64 }
	add := func(m map[int64]*acc, id int64, n, up int) {
		a := m[id]
		if a == nil {
			a = &acc{}
			m[id] = a
		}
		a.n += int64(n)
		a.up += int64(up)
	}
	dayIndex := make(map[string]int, len(days))
	for i, d := range days {
		dayIndex[d] = i
	}
	cut24 := now.Unix()/uptimeBucketSeconds - 24*3600/uptimeBucketSeconds
	h24 := map[int64]*acc{}
	d30 := map[int64]*acc{}
	perDay := map[int64][]acc{}
	for _, row := range rows {
		if row.Samples <= 0 {
			continue
		}
		i, ok := dayIndex[time.Unix(row.Bucket*uptimeBucketSeconds, 0).In(loc).Format("2006-01-02")]
		if !ok {
			continue
		}
		add(d30, row.NodeID, row.Samples, row.Up)
		if row.Bucket > cut24 {
			add(h24, row.NodeID, row.Samples, row.Up)
		}
		if perDay[row.NodeID] == nil {
			perDay[row.NodeID] = make([]acc, len(days))
		}
		perDay[row.NodeID][i].n += int64(row.Samples)
		perDay[row.NodeID][i].up += int64(row.Up)
	}
	pct := func(a *acc) *float64 {
		if a == nil || a.n == 0 {
			return nil
		}
		v := float64(a.up) * 100 / float64(a.n)
		return &v
	}
	out := make(map[int64]*statusPageAvailability, len(d30))
	for id := range d30 {
		av := emptyStatusAvailability(len(days))
		av.H24 = pct(h24[id])
		av.D30 = pct(d30[id])
		for i := range days {
			a := perDay[id][i]
			if a.n == 0 {
				continue
			}
			av.Days[i] = pct(&a)
			down := a.n - a.up
			av.Down[i] = &down
		}
		out[id] = av
	}
	return out
}

// statusPinnedPoints names the reference points the node pins in its latest
// round, in their order; nil when none are pinned.
func (h *Handler) statusPinnedPoints(nodeID int64) []string {
	if h.nodeLatency == nil {
		return nil
	}
	p, ok := h.nodeLatency.get(nodeID)
	if !ok {
		return nil
	}
	var names []string
	for _, pt := range p.Points {
		if pt.Pinned && pt.Name != "" {
			names = append(names, pt.Name)
		}
	}
	return names
}

// statusPingRows summarizes a node's latency samples (oldest first) for the
// page: the pinned points, else the points in name order, at most
// statusPingPoints of them.
func statusPingRows(rows []model.NodeLatencySample, pinned []string) []statusPagePing {
	if len(rows) == 0 {
		return nil
	}
	byPoint := map[string][]model.NodeLatencySample{}
	for _, row := range rows {
		byPoint[row.Point] = append(byPoint[row.Point], row)
	}
	var names []string
	for _, name := range pinned {
		if len(byPoint[name]) > 0 {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		for name := range byPoint {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	if len(names) > statusPingPoints {
		names = names[:statusPingPoints]
	}
	out := make([]statusPagePing, 0, len(names))
	for _, name := range names {
		list := byPoint[name]
		if len(list) > statusPingSamples {
			list = list[len(list)-statusPingSamples:]
		}
		row := statusPagePing{Name: name, Samples: make([]int64, 0, len(list))}
		var loss float64
		for _, s := range list {
			ms := int64(-1)
			if s.OK == 1 {
				ms = s.LatencyMs
			}
			row.Samples = append(row.Samples, ms)
			loss += s.LossPct
		}
		row.Loss = loss / float64(len(list))
		if last := list[len(list)-1]; last.OK == 1 {
			ms := last.LatencyMs
			row.Ms = &ms
		}
		out = append(out, row)
	}
	return out
}

// statusPingRoutes adds each point's return route line, by point name.
func statusPingRoutes(rows []statusPagePing, r nodeRoute) {
	byName := make(map[string]routePoint, len(r.Points))
	for _, rp := range r.Points {
		byName[rp.Name] = rp
	}
	for i := range rows {
		rp, ok := byName[rows[i].Name]
		if !ok || rp.Error != "" {
			continue
		}
		rows[i].Route = rp.Route
		if rp.IPv6 != nil && rp.IPv6.Error == "" && rp.IPv6.Route != rp.Route {
			rows[i].RouteV6 = rp.IPv6.Route
		}
	}
}

// swapUsage is the share of swap in use, 0 without swap.
func swapUsage(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// storeNodeHostFacts keeps what a node's agent says about its machine.
func (h *Handler) storeNodeHostFacts(nodeID int64, f *ws.HostFacts) {
	err := h.repo.SetNodeHostFacts(nodeID, repo.NodeHostFacts{
		OS: truncateRunes(f.OS, 200), Kernel: truncateRunes(f.Kernel, 200), Arch: truncateRunes(f.Arch, 50),
		CPUName: truncateRunes(f.CPUName, 200), Virtualization: truncateRunes(f.Virtualization, 50),
	})
	if err != nil {
		log.Printf("store host facts of node %d: %v", nodeID, err)
	}
}
