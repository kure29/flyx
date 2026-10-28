package handler

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/repo"
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
		{"billing_directions_overflow", `{"n":"SERVICE","u":2305843009213693952,"d":2305843009213693952}`, 2, 2},
		{"billing_batch_sum", `{"n":"SERVICE","u":0,"d":4611686018427387804}`, 2, 1},
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

func TestFlowUploadIsolatesInvalidBillingConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ratio float64
		flow  int64
	}{
		{"zero", 0, 1}, {"negative", -1, 1}, {"nan", math.NaN(), 1},
		{"infinite", math.Inf(1), 1}, {"invalid_direction", 1, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{}
			metas := map[int64]repo.FlowUploadForwardMeta{
				1: {UserID: 1, UserTunnelID: 1, TrafficRatio: tc.ratio, TunnelFlow: tc.flow},
				2: {UserID: 2, UserTunnelID: 2, TrafficRatio: 2, TunnelFlow: 2},
			}
			items := []flowItem{{N: "1_1_1", U: 40, D: 60}, {N: "2_2_2", U: 40, D: 60}}
			if err := validateFlowUploadItems(items, metas); err != nil {
				t.Fatal(err)
			}
			batch := h.buildFlowUploadBatch(items, metas)
			if len(batch.flowDeltas) != 1 || batch.flowDeltas[0].ForwardID != 2 || batch.quotaUsage[2] != 200 || batch.quotaUsage[1] != 0 {
				t.Fatalf("invalid config affected healthy forward: %+v", batch)
			}
			if batch.forwardTraffic[1].bytesIn != 60 {
				t.Fatal("lost raw monitoring traffic")
			}
		})
	}
}

func TestFlowUploadValidationUsesBillableDirections(t *testing.T) {
	metas := map[int64]repo.FlowUploadForwardMeta{1: {TrafficRatio: 4, TunnelFlow: 1}}
	if err := validateFlowUploadItems([]flowItem{{N: "1_1_1", U: math.MaxInt64 - 100, D: 1}}, metas); err != nil {
		t.Fatalf("unbilled direction should not be scaled: %v", err)
	}
	metas[1] = repo.FlowUploadForwardMeta{TrafficRatio: 1, TunnelFlow: 2}
	if err := validateFlowUploadItems([]flowItem{{N: "1_1_1", D: 1 << 62}}, metas); err != nil {
		t.Fatalf("direction selector must not multiply bytes: %v", err)
	}
}

func TestFlowUploadAccountsHealthyForwardBesideInvalidStoredConfig(t *testing.T) {
	f := setupNftablesCollectionFixture(t)
	h := f.handler
	t.Cleanup(func() { _ = h.repo.Close() })
	badTunnel := seedTunnelForNftables(t, h, "invalid-config", f.nodeID)
	badForward := seedForwardForNftables(t, h, badTunnel, f.nodeID, "203.0.113.10:8080")
	if err := h.repo.DB().Exec(`UPDATE tunnel SET traffic_ratio=0 WHERE id=?`, badTunnel).Error; err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	body := fmt.Sprintf(`[{"n":"%d_1_0","u":40,"d":60},{"n":"%d_1_%d","u":40,"d":60}]`, badForward.ID, f.forwardID, f.userTunnelID)
	h.flowUpload(rec, httptest.NewRequest("POST", "/flow/upload?secret=secret", strings.NewReader(body)))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("upload rejected: %d %s", rec.Code, rec.Body.String())
	}
	if got := mustHandlerCount(t, h, `SELECT in_flow+out_flow FROM forward WHERE id=?`, f.forwardID); got != 100 {
		t.Fatalf("healthy usage=%d", got)
	}
	if got := mustHandlerCount(t, h, `SELECT in_flow+out_flow FROM forward WHERE id=?`, badForward.ID); got != 0 {
		t.Fatalf("invalid config charged=%d", got)
	}
	if got := mustHandlerCount(t, h, `SELECT daily_used_bytes FROM user_quota WHERE user_id=1`); got != 100 {
		t.Fatalf("quota usage=%d", got)
	}
}
