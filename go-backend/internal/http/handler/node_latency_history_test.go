package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/model"
)

func TestLatencyAlertTracker(t *testing.T) {
	tr := newLatencyAlertTracker()
	round := func(ms int64, loss float64) map[int64]latencyTransition {
		return tr.track(map[int64]reachProbe{
			1: {Points: []latencyPoint{{Name: "电信", OK: true, LatencyMs: ms, LossPct: loss}}},
		}, 20, 300)
	}
	for i := 0; i < latencyAlertStreak-1; i++ {
		if got := round(50, 40); len(got) != 0 {
			t.Fatalf("round %d alerted early: %+v", i+1, got)
		}
	}
	if got := round(50, 40); len(got[1].bad) != 1 {
		t.Fatalf("third bad round = %+v", got)
	}
	if got := round(400, 0); len(got) != 0 {
		t.Fatalf("still bad (slow) must not alert again: %+v", got)
	}
	if got := round(50, 0); len(got[1].recovered) != 1 {
		t.Fatalf("recovery = %+v", got)
	}
	// A good round in between starts the count over.
	round(50, 40)
	round(50, 0)
	round(50, 40)
	if got := round(50, 40); len(got) != 0 {
		t.Fatalf("interrupted streak alerted: %+v", got)
	}
}

func TestRouteChanges(t *testing.T) {
	prev := nodeRoute{Points: []routePoint{
		{Name: "电信", Route: "电信 CN2 GIA"},
		{Name: "联通", Route: "未识别"},
		{Name: "移动", Route: "移动 CMI"},
	}}
	next := nodeRoute{Points: []routePoint{
		{Name: "电信", Route: "电信 163"},
		{Name: "联通", Route: "联通 4837"},
		{Name: "移动", Error: "检测失败"},
	}}
	got := routeChanges(prev, next)
	if len(got) != 1 || got[0] != "电信：电信 CN2 GIA → 电信 163" {
		t.Fatalf("changes = %v", got)
	}
}

func TestBuildTrend(t *testing.T) {
	const min = int64(time.Minute / time.Millisecond)
	since := int64(0)
	samples := []model.NodeLatencySample{
		{Point: "电信", CreatedTime: 0, OK: 1, LatencyMs: 40},
		{Point: "联通", CreatedTime: 0, OK: 0, LossPct: 100},
		{Point: "电信", CreatedTime: 3 * min, OK: 1, LatencyMs: 60, LossPct: 25},
		{Point: "联通", CreatedTime: 3 * min, OK: 1, LatencyMs: 80},
	}
	// A short range keeps one sample per bucket.
	got := buildTrend(samples, since, 10*min)
	if len(got) != 2 || got[0].Name != "电信" || len(got[0].Samples) != 2 {
		t.Fatalf("trend = %+v", got)
	}
	if got[0].AvgMs != 50 || got[0].MaxMs != 60 || got[0].LossPct != 12.5 {
		t.Fatalf("telecom stats = %+v", got[0])
	}
	if got[1].Samples[0].Ms != nil || got[1].Samples[0].LossPct != 100 || got[1].AvgMs != 80 {
		t.Fatalf("unicom = %+v", got[1])
	}
	// A week folds both samples into one bucket per point.
	week := buildTrend(samples, since, 7*24*60*min)
	if len(week[0].Samples) != 1 || *week[0].Samples[0].Ms != 50 || week[0].Samples[0].LossPct != 12.5 {
		t.Fatalf("week trend = %+v", week[0])
	}
}

func TestLatencyRoundsRecordAndAlert(t *testing.T) {
	h, r, sent := setupNotifyHandler(t)
	if err := r.UpsertConfig(latencyTargetsConfigKey, `[{"name":"电信","host":"ct.example.com","port":80,"enabled":true}]`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	node := mustCreateLatencyNode(t, r, "香港", 1, 0)
	loss := 50.0
	stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		return map[string]interface{}{"success": true, "averageTime": 30.0, "packetLoss": loss}, nil
	})
	for i := 0; i < latencyAlertStreak; i++ {
		if _, err := h.probeNodeLatency(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	// A clicked re-probe records a sample but does not count for alerts.
	if _, err := h.probeNodeLatency(context.Background(), []int64{node}); err != nil {
		t.Fatal(err)
	}
	msgs := sent()
	if len(msgs) != 1 || msgs[0].Event != notifyEventLatencyBad || !strings.Contains(msgs[0].Message, "香港") || !strings.Contains(msgs[0].Message, "丢包 50%") {
		t.Fatalf("messages = %+v", msgs)
	}

	w := httptest.NewRecorder()
	h.nodeLatencyHistory(w, httptest.NewRequest(http.MethodPost, "/api/v1/node/latency/history", bytes.NewBufferString(`{"id":`+itoa(node)+`,"hours":24}`)))
	var res struct {
		Data struct {
			Points []trendPoint `json:"points"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Data.Points) != 1 || res.Data.Points[0].LossPct != 50 || res.Data.Points[0].AvgMs != 30 {
		t.Fatalf("history = %s", w.Body.String())
	}
}
