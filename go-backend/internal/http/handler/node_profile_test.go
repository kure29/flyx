package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestTrafficPeriodStart(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 12, 0, 0, 0, loc) }
	for _, c := range []struct {
		reset int
		now   time.Time
		want  string
	}{
		{0, day(2026, 11, 8), "2026-11-01"},
		{1, day(2026, 11, 1), "2026-11-01"},
		{15, day(2026, 11, 8), "2026-10-15"},
		{15, day(2026, 11, 20), "2026-11-15"},
		// A short month resets on its last day.
		{31, day(2026, 2, 28), "2026-02-28"},
		{31, day(2026, 3, 5), "2026-02-28"},
		{30, day(2026, 1, 10), "2025-12-30"},
	} {
		if got := trafficPeriodStart(c.reset, c.now).Format("2006-01-02"); got != c.want {
			t.Errorf("reset %d at %s: %s, want %s", c.reset, c.now.Format("2006-01-02"), got, c.want)
		}
	}
}

func TestRenewalCycles(t *testing.T) {
	for in, want := range map[string]string{
		"month": "month", "quarter": "quarter", "year": "year",
		"1m": "month", "3m": "quarter", "12m": "year", "1y": "year",
		"6m": "6m", "2y": "2y", "24m": "2y", "0m": "", "121m": "", "x": "", "": "", "11y": "",
	} {
		if got := normalizeNodeRenewalCycle(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if model.RenewalCycleMonths("2y") != 24 || model.RenewalCycleMonths("6m") != 6 {
		t.Fatal("cycle months")
	}
}

func TestNodeProfile(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/profile.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	id := seedRelayTargetNode(t, r, "hk", "198.51.100.9")

	// Bad values are refused before anything is saved.
	for _, req := range []map[string]interface{}{
		{"price": -1}, {"currency": "元"}, {"trafficQuotaGb": -5}, {"trafficCalc": "both"},
		{"trafficResetDay": 32}, {"group": strings.Repeat("组", 51)},
	} {
		if _, err := nodeProfileUpdates(req); err == nil {
			t.Errorf("%v must be refused", req)
		}
	}

	req := map[string]interface{}{
		"group": " 香港 ", "publicNote": "CN2 GIA", "price": 151.2, "currency": "cny",
		"trafficQuotaGb": 1024, "trafficCalc": "max", "trafficResetDay": 15, "statusVisible": false,
	}
	if err := h.applyNodeProfileRequest(id, req); err != nil {
		t.Fatal(err)
	}
	n, _ := r.GetNodeByID(id)
	if n.GroupName != "香港" || n.PublicNote != "CN2 GIA" || n.Price != 151.2 || n.Currency != "CNY" ||
		n.TrafficQuotaGB != 1024 || n.TrafficCalc != "max" || n.TrafficResetDay != 15 {
		t.Fatalf("profile not stored: %+v", n)
	}
	cfg, _ := r.GetConfigsByNames([]string{statusPageHiddenNodesKey})
	if !parseStatusHiddenNodes(cfg[statusPageHiddenNodesKey])[id] {
		t.Fatal("hidden from the status page")
	}
	// A request without the fields keeps them.
	if err := h.applyNodeProfileRequest(id, map[string]interface{}{"statusVisible": true}); err != nil {
		t.Fatal(err)
	}
	n, _ = r.GetNodeByID(id)
	cfg, _ = r.GetConfigsByNames([]string{statusPageHiddenNodesKey})
	if n.GroupName != "香港" || parseStatusHiddenNodes(cfg[statusPageHiddenNodesKey])[id] {
		t.Fatalf("absent fields kept, shown again: %+v %q", n, cfg[statusPageHiddenNodesKey])
	}

	// Traffic correction: the panel counted 29 TB this period, the provider says 300 GB.
	now := time.Now()
	start := trafficPeriodStart(15, now.In(time.Local))
	const gb = int64(1) << 30
	if err := r.AddNodeTraffic([]model.NodeTraffic{{NodeID: id, Hour: start.UnixMilli() + 3600_000, BucketMinutes: 15, InBytes: 29000 * gb, OutBytes: 100 * gb}}); err != nil {
		t.Fatal(err)
	}
	if err := h.applyNodeProfileRequest(id, map[string]interface{}{"trafficCorrection": map[string]interface{}{"downGb": 300}}); err != nil {
		t.Fatal(err)
	}
	plans, _ := r.ListNodeTrafficPlans()
	traffic, err := h.periodTraffic(plans, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := traffic[id]; got.Down != 300*gb || got.Up != 100*gb {
		t.Fatalf("corrected traffic %+v, want down 300 GB, up 100 GB", got)
	}
	// The correction lapses with the next period.
	if later, _ := h.periodTraffic(plans, start.AddDate(0, 1, 1)); later[id].Down != 0 {
		t.Fatalf("correction applied to the next period: %+v", later[id])
	}

	// The quick 价格与到期 dialog changes only its fields.
	body := `{"id":` + jsonID(id) + `,"price":"9.9","currency":"USD","renewalCycle":"6m","expiryTime":1893456000000}`
	rec := httptest.NewRecorder()
	h.nodeBilling(rec, httptest.NewRequest(http.MethodPost, "/api/v1/node/billing", strings.NewReader(body)))
	var out response.R
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Code != 0 {
		t.Fatalf("billing: %+v", out)
	}
	n, _ = r.GetNodeByID(id)
	if n.Price != 9.9 || n.Currency != "USD" || n.RenewalCycle.String != "6m" || n.ExpiryTime.Int64 != 1893456000000 || n.GroupName != "香港" {
		t.Fatalf("billing saved: %+v", n)
	}
}
