package handler

import (
	"database/sql"
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

func setupStatusPageHandler(t *testing.T) (*Handler, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir() + "/status.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return New(r, "unit-test-secret"), r
}

func statusPageRequest(t *testing.T, h *Handler) (int, string, statusPagePayload) {
	t.Helper()
	w := httptest.NewRecorder()
	h.publicStatus(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/status", nil))
	var res struct {
		Code int               `json:"code"`
		Data statusPagePayload `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return res.Code, w.Body.String(), res.Data
}

func TestPublicStatusOffByDefault(t *testing.T) {
	h, _ := setupStatusPageHandler(t)
	if code, _, _ := statusPageRequest(t, h); code != 404 {
		t.Fatalf("code = %d, want 404 while the status page is off", code)
	}
}

func TestPublicStatusShowsNodesWithoutSecrets(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	now := time.Now()
	nodes := []model.Node{
		{Name: "HK-01", Secret: "node-secret-abc", ServerIP: "203.0.113.7", Port: "1000-2000", Status: 1, CreatedTime: now.UnixMilli(),
			ExpiryTime: sql.NullInt64{Int64: time.Date(2030, 1, 2, 12, 0, 0, 0, time.Local).UnixMilli(), Valid: true}},
		{Name: "JP-01", Secret: "node-secret-def", ServerIP: "198.51.100.9", Port: "3000", Status: 0, CreatedTime: now.UnixMilli()},
		{Name: "hidden", Secret: "node-secret-ghi", ServerIP: "192.0.2.1", Port: "4000", Status: 1, CreatedTime: now.UnixMilli()},
	}
	for i := range nodes {
		if err := r.DB().Create(&nodes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for k, v := range map[string]string{
		statusPageEnabledKey:     "true",
		statusPageTitleKey:       "我的状态",
		statusPageHiddenNodesKey: strconv.FormatInt(nodes[2].ID, 10),
		statusPageShowExpiryKey:  "true",
	} {
		if err := r.UpsertConfig(k, v, now.UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	// HK-01 up for 9 of 10 samples, JP-01 down for all.
	bucket := now.Unix()/uptimeBucketSeconds - 1
	for i := 0; i < 10; i++ {
		if err := r.RecordNodeUptime(bucket, map[int64]bool{nodes[0].ID: i != 0, nodes[1].ID: false}); err != nil {
			t.Fatal(err)
		}
	}
	h.statusLive.set(nodes[0].ID, statusLiveSample{cpu: 12.5, mem: 40, up: 1000, down: 2000, cores: 4, memUsed: 1 << 30, memTotal: 4 << 30, diskUsed: 10 << 30, diskTotal: 40 << 30})

	code, raw, data := statusPageRequest(t, h)
	if code != 0 {
		t.Fatalf("code = %d: %s", code, raw)
	}
	for _, secret := range []string{"203.0.113.7", "198.51.100.9", "node-secret", "1000-2000", "hidden"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("payload leaks %q: %s", secret, raw)
		}
	}
	if data.Title != "我的状态" || len(data.Days) != statusPageDays || len(data.Nodes) != 2 {
		t.Fatalf("payload = %+v", data)
	}
	hk, jp := data.Nodes[0], data.Nodes[1]
	if !hk.Online || hk.CPU == nil || *hk.CPU != 12.5 || hk.SpeedUp == nil || *hk.SpeedUp != 1000 {
		t.Fatalf("HK-01 = %+v", hk)
	}
	if hk.Cores != 4 || hk.MemUsed != 1<<30 || hk.MemTotal != 4<<30 || hk.DiskUsed != 10<<30 || hk.DiskTotal != 40<<30 {
		t.Fatalf("HK-01 capacity = %+v", hk)
	}
	if hk.Expiry != "2030-01-02" {
		t.Fatalf("HK-01 expiry = %q", hk.Expiry)
	}
	if hk.Availability.H24 == nil || *hk.Availability.H24 != 90 {
		t.Fatalf("HK-01 availability = %+v", hk.Availability)
	}
	if jp.Online || jp.CPU != nil || jp.Availability.D30 == nil || *jp.Availability.D30 != 0 {
		t.Fatalf("JP-01 = %+v", jp)
	}
	if data.Totals.Nodes != 2 || data.Totals.Online != 1 || data.Totals.SpeedDown != 2000 {
		t.Fatalf("totals = %+v", data.Totals)
	}
}

func TestPublicStatusCanHideTraffic(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	now := time.Now()
	node := model.Node{Name: "SG", Secret: "s", ServerIP: "192.0.2.5", Port: "1", Status: 1, CreatedTime: now.UnixMilli()}
	if err := r.DB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	_ = r.UpsertConfig(statusPageEnabledKey, "true", now.UnixMilli())
	_ = r.UpsertConfig(statusPageShowTrafficKey, "false", now.UnixMilli())
	h.statusLive.set(node.ID, statusLiveSample{cpu: 1, up: 5, down: 6})

	_, raw, data := statusPageRequest(t, h)
	if data.ShowTraffic || len(data.Nodes) != 1 {
		t.Fatalf("payload = %s", raw)
	}
	n := data.Nodes[0]
	if n.SpeedUp != nil || n.MonthUp != nil || n.Expiry != "" || n.CPU == nil {
		t.Fatalf("node = %+v", n)
	}
}

func TestComputeStatusAvailabilityPerDay(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, loc)
	days := []string{"2026-03-08", "2026-03-09", "2026-03-10"}
	b := func(tm time.Time) int64 { return tm.Unix() / uptimeBucketSeconds }
	rows := []model.NodeUptime{
		{NodeID: 1, Bucket: b(time.Date(2026, 3, 9, 1, 0, 0, 0, loc)), Samples: 10, Up: 5},
		{NodeID: 1, Bucket: b(time.Date(2026, 3, 10, 11, 0, 0, 0, loc)), Samples: 10, Up: 10},
		{NodeID: 1, Bucket: b(time.Date(2026, 1, 1, 0, 0, 0, 0, loc)), Samples: 10, Up: 0}, // outside the days
	}
	got := computeStatusAvailability(rows, days, now, loc)[1]
	if got == nil {
		t.Fatal("no availability")
	}
	if got.Days[0] != nil || got.Down[0] != nil {
		t.Fatalf("day without data = %v / %v", got.Days[0], got.Down[0])
	}
	if *got.Days[1] != 50 || *got.Down[1] != 5 || *got.Days[2] != 100 || *got.Down[2] != 0 {
		t.Fatalf("days = %v %v, down = %v %v", *got.Days[1], *got.Days[2], *got.Down[1], *got.Down[2])
	}
	if *got.D30 != 75 || *got.H24 != 100 {
		t.Fatalf("d30 = %v, h24 = %v", *got.D30, *got.H24)
	}
}

func TestPublicStatusMethodsAndCache(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	w := httptest.NewRecorder()
	h.publicStatus(w, httptest.NewRequest(http.MethodPost, "/api/v1/public/status", strings.NewReader(`{}`)))
	var res struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Code == 0 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST answered: %s (%v)", w.Body.String(), err)
	}

	now := time.Now()
	if _, err := h.statusPageResponse(now); err != nil {
		t.Fatal(err)
	}
	// Turning the page on is seen once the cached answer is old, not before.
	if err := r.UpsertConfig(statusPageEnabledKey, "true", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	raw, _ := h.statusPageResponse(now.Add(time.Second))
	if !strings.Contains(string(raw), `"code":404`) {
		t.Fatalf("cached answer not reused: %s", raw)
	}
	raw, _ = h.statusPageResponse(now.Add(statusCacheMaxAge + time.Second))
	if !strings.Contains(string(raw), `"code":0`) {
		t.Fatalf("answer not rebuilt: %s", raw)
	}

	w = httptest.NewRecorder()
	h.publicStatus(w, httptest.NewRequest(http.MethodHead, "/api/v1/public/status", nil))
	if w.Body.Len() != 0 || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Header().Get("Cache-Control"), "max-age=5") {
		t.Fatalf("HEAD = %q, headers %v", w.Body.String(), w.Header())
	}
}

func TestStatusPingRows(t *testing.T) {
	var rows []model.NodeLatencySample
	for i := 0; i < 25; i++ {
		rows = append(rows,
			model.NodeLatencySample{Point: "上海电信", OK: 1, LatencyMs: int64(30 + i), CreatedTime: int64(i)},
			model.NodeLatencySample{Point: "北京联通", OK: 1, LatencyMs: 50, LossPct: 10, CreatedTime: int64(i)},
			model.NodeLatencySample{Point: "广州移动", OK: 0, LossPct: 100, CreatedTime: int64(i)},
			model.NodeLatencySample{Point: "成都电信", OK: 1, LatencyMs: 40, CreatedTime: int64(i)},
		)
	}
	got := statusPingRows(rows, nil)
	if len(got) != statusPingPoints || got[0].Name != "上海电信" || got[1].Name != "北京联通" || got[2].Name != "广州移动" {
		t.Fatalf("points = %+v", got)
	}
	sh := got[0]
	if len(sh.Samples) != statusPingSamples || sh.Samples[0] != 35 || sh.Ms == nil || *sh.Ms != 54 {
		t.Fatalf("上海电信 = %+v", sh)
	}
	if got[1].Loss != 10 || got[2].Ms != nil || got[2].Samples[0] != -1 {
		t.Fatalf("rows = %+v", got)
	}
	pinned := statusPingRows(rows, []string{"成都电信", "不存在"})
	if len(pinned) != 1 || pinned[0].Name != "成都电信" {
		t.Fatalf("pinned = %+v", pinned)
	}
}

func TestStatusPingRoutes(t *testing.T) {
	rows := []statusPagePing{{Name: "上海电信"}, {Name: "北京联通"}, {Name: "广州移动"}, {Name: "成都电信"}}
	statusPingRoutes(rows, nodeRoute{Points: []routePoint{
		{Name: "上海电信", Route: "CN2 GIA", Hops: []routeHop{{TTL: 1, IP: "10.0.0.1"}}, IPv6: &routePoint{Route: "CN2 GT"}},
		{Name: "北京联通", Route: "4837", IPv6: &routePoint{Route: "4837"}},
		{Name: "广州移动", Route: "CMI", Error: "trace failed"},
	}})
	if rows[0].Route != "CN2 GIA" || rows[0].RouteV6 != "CN2 GT" {
		t.Fatalf("上海电信 = %+v", rows[0])
	}
	// The same line over IPv6 is not repeated; a failed trace shows nothing.
	if rows[1].Route != "4837" || rows[1].RouteV6 != "" || rows[2].Route != "" || rows[3].Route != "" {
		t.Fatalf("rows = %+v", rows)
	}
}
