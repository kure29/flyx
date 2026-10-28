package handler

import (
	"fmt"
	"go-backend/internal/store/repo"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Node input (wire d) is the client's upload; node output (wire u) is download.
func TestFlowBillingDirectionsAcrossEngines(t *testing.T) {
	for _, mode := range []int64{1, 2} {
		for _, ratio := range []float64{0.5, 1, 2} {
			t.Run(fmt.Sprintf("mode=%d/ratio=%g", mode, ratio), func(t *testing.T) {
				metas := map[int64]repo.FlowUploadForwardMeta{20: {ForwardID: 20, UserID: 2, UserTunnelID: 10, TrafficRatio: ratio, TunnelFlow: mode}}
				gost := (&Handler{}).buildFlowUploadBatch([]flowItem{{N: "20_2_10", D: 100, U: 900}}, metas)
				nft := buildNftFlowUploadBatch([]nftTrafficDelta{{ForwardID: 20, BytesIn: 100, BytesOut: 900}}, metas)
				wantIn, wantOut := int64(100*ratio), int64(0)
				if mode == 2 {
					wantOut = int64(900 * ratio)
				}
				for engine, batch := range map[string]flowUploadBatch{"gost": gost, "nftables/realm": nft} {
					if len(batch.flowDeltas) != 1 {
						t.Fatalf("%s: missing accounting delta", engine)
					}
					delta := batch.flowDeltas[0]
					if delta.InFlow != wantIn || delta.OutFlow != wantOut || batch.quotaUsage[2] != wantIn+wantOut {
						t.Fatalf("%s: flow=%+v quota=%d want %d/%d", engine, delta, batch.quotaUsage[2], wantIn, wantOut)
					}
					if raw := batch.forwardTraffic[20]; raw.bytesIn != 100 || raw.bytesOut != 900 {
						t.Fatalf("%s: raw metrics changed: %+v", engine, raw)
					}
				}
			})
		}
	}
}

func TestFlowBillingUploadOnlyDoesNotPauseForDownloads(t *testing.T) {
	for _, mode := range []int64{1, 2} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := setupNftablesCollectionFixture(t)
			h := f.handler
			h.nftablesManager = &fakeNftablesManager{}
			t.Cleanup(func() { _ = h.repo.Close() })
			if err := h.repo.DB().Exec("UPDATE tunnel SET flow = ?", mode).Error; err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			if _, err := h.repo.AddUserQuotaUsage(1, 0, now); err != nil {
				t.Fatal(err)
			}
			if err := h.repo.DB().Exec("UPDATE user_quota SET daily_limit_gb=1 WHERE user_id=1").Error; err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`[{"n":"%d_1_%d","u":%d,"d":50}]`, f.forwardID, f.userTunnelID, 2*bytesPerGB)
			rec := httptest.NewRecorder()
			h.flowUpload(rec, httptest.NewRequest("POST", "/flow/upload?secret=secret", strings.NewReader(body)))
			if rec.Body.String() != "ok" {
				t.Fatalf("upload: %s", rec.Body.String())
			}
			want, status := int64(50), int64(1)
			if mode == 2 {
				want += 2 * bytesPerGB
				status = 0
			}
			for _, query := range []string{"SELECT in_flow+out_flow FROM forward", "SELECT in_flow+out_flow FROM user WHERE id=1", "SELECT in_flow+out_flow FROM user_tunnel", "SELECT daily_used_bytes FROM user_quota WHERE user_id=1"} {
				if got := mustHandlerCount(t, h, query); got != want {
					t.Errorf("%s: got %d want %d", query, got, want)
				}
			}
			if got := mustHandlerCount(t, h, "SELECT status FROM forward"); got != status {
				t.Errorf("status=%d want %d", got, status)
			}
		})
	}
}
