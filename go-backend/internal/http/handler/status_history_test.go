package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

type historyAnswer struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func historyRequest(t *testing.T, h *Handler, path string) (historyAnswer, http.Header) {
	t.Helper()
	w := httptest.NewRecorder()
	if strings.Contains(path, "ping-history") {
		h.publicStatusPingHistory(w, httptest.NewRequest(http.MethodGet, path, nil))
	} else {
		h.publicStatusHistory(w, httptest.NewRequest(http.MethodGet, path, nil))
	}
	var a historyAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return a, w.Header()
}

// Three nodes, the first hidden: the page shows B as 1 and C as 2.
func seedStatusHistory(t *testing.T, r *repo.Repository) (hidden, b, c model.Node) {
	t.Helper()
	now := time.Now()
	nodes := []model.Node{
		{Name: "A-hidden", Secret: "sa", ServerIP: "192.0.2.1", Port: "1", Status: 1, CreatedTime: now.UnixMilli(), Inx: 1},
		{Name: "B", Secret: "sb", ServerIP: "192.0.2.2", Port: "1", Status: 1, CreatedTime: now.UnixMilli(), Inx: 2},
		{Name: "C", Secret: "sc", ServerIP: "192.0.2.3", Port: "1", Status: 1, CreatedTime: now.UnixMilli(), Inx: 3},
	}
	for i := range nodes {
		if err := r.DB().Create(&nodes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	_ = r.UpsertConfig(statusPageEnabledKey, "true", now.UnixMilli())
	_ = r.UpsertConfig(statusPageHiddenNodesKey, strconv.FormatInt(nodes[0].ID, 10), now.UnixMilli())

	ms := now.Add(-2 * time.Minute).UnixMilli()
	metrics := []*model.NodeMetric{
		{NodeID: nodes[0].ID, Timestamp: ms, CPUUsage: 99},
		{NodeID: nodes[1].ID, Timestamp: ms, CPUUsage: 12.345, MemUsage: 40, SwapUsage: 3, NetInSpeed: 100, NetOutSpeed: 200, Procs: 90},
		{NodeID: nodes[2].ID, Timestamp: ms, CPUUsage: 70},
	}
	if err := r.InsertNodeMetricBatch(metrics); err != nil {
		t.Fatal(err)
	}
	samples := []model.NodeLatencySample{
		{NodeID: nodes[1].ID, CreatedTime: ms, Point: "联通", OK: 1, LatencyMs: 80},
		{NodeID: nodes[1].ID, CreatedTime: ms, Point: "电信", OK: 0, LossPct: 100},
	}
	if err := r.InsertNodeLatencySamples(samples); err != nil {
		t.Fatal(err)
	}
	return nodes[0], nodes[1], nodes[2]
}

func TestPublicStatusHistoryUsesPagePositions(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	hidden, _, _ := seedStatusHistory(t, r)

	a, hdr := historyRequest(t, h, "/api/v1/public/status/history?node=1&range=1h")
	if a.Code != 0 {
		t.Fatalf("history: %d %s", a.Code, a.Msg)
	}
	var hist statusMetricHistory
	_ = json.Unmarshal(a.Data, &hist)
	if hist.Range != "1h" || hist.BucketSec != 10 || len(hist.Points) != 1 {
		t.Fatalf("history = %s", a.Data)
	}
	p := hist.Points[0]
	if p.CPU != 12.35 || p.Mem != 40 || p.Swap != 3 || p.Procs != 90 || p.NetIn == nil || *p.NetIn != 100 {
		t.Fatalf("node 1 must be B: %+v", p)
	}
	if !strings.Contains(hdr.Get("Cache-Control"), "max-age=10") {
		t.Fatalf("cache header %q", hdr.Get("Cache-Control"))
	}

	a, _ = historyRequest(t, h, "/api/v1/public/status/history?node=2&range=24h")
	_ = json.Unmarshal(a.Data, &hist)
	if a.Code != 0 || len(hist.Points) != 1 || hist.Points[0].CPU != 70 || hist.BucketSec != 240 {
		t.Fatalf("node 2 must be C: %s", a.Data)
	}

	// The hidden node has no position; asking past the end, or by its
	// database id, gives the same answer as a node that does not exist.
	missing, _ := historyRequest(t, h, "/api/v1/public/status/history?node=999&range=1h")
	for _, id := range []int64{3, hidden.ID + 100} {
		got, _ := historyRequest(t, h, "/api/v1/public/status/history?node="+strconv.FormatInt(id, 10)+"&range=1h")
		if got.Code != 404 || got.Msg != missing.Msg {
			t.Fatalf("node %d: %d %q, want %d %q", id, got.Code, got.Msg, missing.Code, missing.Msg)
		}
	}
}

func TestPublicStatusPingHistory(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	seedStatusHistory(t, r)

	a, _ := historyRequest(t, h, "/api/v1/public/status/ping-history?node=1&range=6h")
	var hist statusPingHistory
	_ = json.Unmarshal(a.Data, &hist)
	if a.Code != 0 || hist.BucketSec != 60 || len(hist.Points) != 2 {
		t.Fatalf("ping history = %d %s", a.Code, a.Data)
	}
	// Without pinned points the order is by name; check each series by name.
	byName := map[string]statusPingSeries{}
	for _, s := range hist.Points {
		byName[s.Name] = s
	}
	if s := byName["联通"]; len(s.Samples) != 1 || s.Samples[0].Ms == nil || *s.Samples[0].Ms != 80 {
		t.Fatalf("联通 = %+v", s)
	}
	if s := byName["电信"]; len(s.Samples) != 1 || s.Samples[0].Ms != nil || s.Samples[0].Loss != 100 {
		t.Fatalf("a failed round has no latency: %+v", s)
	}
	if strings.Contains(string(a.Data), "192.0.2") {
		t.Fatalf("ping history leaks an address: %s", a.Data)
	}
}

func TestPublicStatusHistoryRefusals(t *testing.T) {
	h, r := setupStatusPageHandler(t)

	if a, _ := historyRequest(t, h, "/api/v1/public/status/history?node=1&range=1h"); a.Code != 404 || !strings.Contains(a.Msg, "未开启") {
		t.Fatalf("page off: %d %s", a.Code, a.Msg)
	}
	seedStatusHistory(t, r)
	h.statusHistory = &statusHistoryCache{} // forget the gate read while the page was off
	for _, q := range []string{"node=1&range=30d", "node=abc", "node=0", "range=1h"} {
		if a, _ := historyRequest(t, h, "/api/v1/public/status/history?"+q); a.Code != 400 {
			t.Fatalf("%s: %d %s", q, a.Code, a.Msg)
		}
	}

	w := httptest.NewRecorder()
	h.publicStatusHistory(w, httptest.NewRequest(http.MethodPost, "/api/v1/public/status/history?node=1", nil))
	if !strings.Contains(w.Body.String(), "请求失败") {
		t.Fatalf("POST: %s", w.Body.String())
	}
}

func TestPublicStatusHistoryHidesTrafficWhenOff(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	seedStatusHistory(t, r)
	_ = r.UpsertConfig(statusPageShowTrafficKey, "false", time.Now().UnixMilli())

	a, _ := historyRequest(t, h, "/api/v1/public/status/history?node=1&range=1h")
	if a.Code != 0 || strings.Contains(string(a.Data), "netIn") || strings.Contains(string(a.Data), "netOut") {
		t.Fatalf("traffic off: %s", a.Data)
	}
}

func TestPublicStatusShowsSwapAndHostOnlyWhenAllowed(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	now := time.Now()
	node := model.Node{Name: "SG", Secret: "s", ServerIP: "192.0.2.5", Port: "1", Status: 1, CreatedTime: now.UnixMilli()}
	if err := r.DB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.SetNodeHostFacts(node.ID, repo.NodeHostFacts{OS: "Debian GNU/Linux 12 (bookworm)", Kernel: "6.1.0-27-amd64", Arch: "x86_64", CPUName: "AMD EPYC 7B13", Virtualization: "kvm"}); err != nil {
		t.Fatal(err)
	}
	_ = r.UpsertConfig(statusPageEnabledKey, "true", now.UnixMilli())
	h.statusLive.set(node.ID, statusLiveSample{cpu: 1, swapUsed: 1 << 29, swapTotal: 1 << 30, procs: 120})

	_, raw, data := statusPageRequest(t, h)
	n := data.Nodes[0]
	if n.Swap == nil || *n.Swap != 50 || n.SwapTotal != 1<<30 || n.Procs == nil || *n.Procs != 120 {
		t.Fatalf("swap and procs: %+v", n)
	}
	if n.OS != "" || strings.Contains(raw, "Debian") || strings.Contains(raw, "EPYC") {
		t.Fatalf("system information must stay off by default: %s", raw)
	}

	_ = r.UpsertConfig(statusPageShowHostKey, "true", now.UnixMilli())
	h.statusCache = &statusPageCache{}
	_, raw, data = statusPageRequest(t, h)
	n = data.Nodes[0]
	if n.OS != "Debian GNU/Linux 12 (bookworm)" || n.Arch != "x86_64" || n.Virtualization != "kvm" || n.CPUName != "AMD EPYC 7B13" {
		t.Fatalf("system information when allowed: %+v", n)
	}
	if strings.Contains(raw, "6.1.0-27") {
		t.Fatalf("the kernel version is never public: %s", raw)
	}
}

func TestOrderStatusPingNames(t *testing.T) {
	names := []string{"c", "a", "b", "d"}
	orderStatusPingNames(names, []string{"d", "b"})
	if strings.Join(names, ",") != "d,b,a,c" {
		t.Fatalf("order = %v", names)
	}
}
