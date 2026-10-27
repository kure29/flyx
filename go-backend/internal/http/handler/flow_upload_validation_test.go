package handler

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFlowUploadRejectsInvalidBatchWithoutSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items string
		ratio float64
		mode  int
	}{
		{"negative_input", `{"n":"SERVICE","u":100,"d":-50}`, 1, 1},
		{"negative_output", `{"n":"SERVICE","u":-50,"d":100}`, 1, 1},
		{"integer_range", `{"n":"SERVICE","u":9223372036854775808,"d":0}`, 1, 1},
		{"raw_batch_sum", `{"n":"SERVICE","u":9223372036854775800,"d":0}`, 1, 1},
		{"ratio_overflow", `{"n":"SERVICE","u":0,"d":4611686018427387904}`, 2, 1},
		{"billing_multiplier_overflow", `{"n":"SERVICE","u":0,"d":4611686018427387904}`, 1, 2},
		{"billing_batch_sum", `{"n":"SERVICE","u":2305843009213693952,"d":2305843009213693952}`, 2, 1},
		{"float_conversion_boundary", `{"n":"SERVICE","u":0,"d":9223372036854775707}`, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupNftablesCollectionFixture(t)
			h := f.handler
			t.Cleanup(func() { _ = h.repo.Close() })
			if err := h.repo.DB().Exec("UPDATE tunnel SET traffic_ratio=?, flow=?", tc.ratio, tc.mode).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := h.repo.AddUserQuotaUsage(1, 0, time.Now()); err != nil {
				t.Fatal(err)
			}
			service := fmt.Sprintf("%d_1_%d", f.forwardID, f.userTunnelID)
			// A valid item before the invalid item must not be partially applied.
			body := `[{"n":"SERVICE","u":0,"d":100},` + tc.items + `]`
			rec := httptest.NewRecorder()
			h.flowUpload(rec, httptest.NewRequest("POST", "/flow/upload?secret=secret", strings.NewReader(strings.ReplaceAll(body, "SERVICE", service))))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid upload returned %d: %s", rec.Code, rec.Body.String())
			}
			for _, query := range []string{
				"SELECT in_flow+out_flow FROM forward",
				"SELECT in_flow+out_flow FROM user WHERE id=1",
				"SELECT in_flow+out_flow FROM user_tunnel",
				"SELECT daily_used_bytes+monthly_used_bytes FROM user_quota WHERE user_id=1",
				"SELECT COUNT(*) FROM tunnel_metric",
			} {
				if got := mustHandlerCount(t, h, query); got != 0 {
					t.Errorf("invalid batch changed %s: %d", query, got)
				}
			}
			// Rejection does not poison subsequent valid uploads.
			rec = httptest.NewRecorder()
			h.flowUpload(rec, httptest.NewRequest("POST", "/flow/upload?secret=secret", strings.NewReader(fmt.Sprintf(`[{"n":%q,"u":0,"d":100}]`, service))))
			if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
				t.Fatalf("valid upload rejected: %d %s", rec.Code, rec.Body.String())
			}
			if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward"); got <= 0 {
				t.Fatalf("valid upload not accounted: %d", got)
			}
		})
	}
}

func TestFlowUploadRejectsInvalidBillingConfiguration(t *testing.T) {
	f := setupNftablesCollectionFixture(t)
	t.Cleanup(func() { _ = f.handler.repo.Close() })
	metas, err := f.handler.repo.GetFlowUploadForwardMetas([]int64{f.forwardID})
	if err != nil {
		t.Fatal(err)
	}
	item := flowItem{N: fmt.Sprintf("%d_1_%d", f.forwardID, f.userTunnelID), D: 100}
	for _, ratio := range []float64{math.NaN(), math.Inf(1), -1} {
		meta := metas[f.forwardID]
		meta.TrafficRatio = ratio
		metas[f.forwardID] = meta
		if err := validateFlowUploadItems([]flowItem{item}, metas); err == nil {
			t.Errorf("accepted invalid ratio %v", ratio)
		}
	}
}
