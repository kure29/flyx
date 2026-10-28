package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTunnelBillingRejectsInvalidConfigBeforeMutation(t *testing.T) {
	for _, fields := range []string{
		`"trafficRatio":0`, `"trafficRatio":-1`, `"trafficRatio":"NaN"`, `"trafficRatio":"+Inf"`,
		`"trafficRatio":"garbage"`, `"trafficRatio":null`, `"flow":0`, `"flow":3`, `"flow":1.5`, `"flow":"garbage"`,
	} {
		t.Run(fields, func(t *testing.T) {
			h := setupProbeTargetTunnelHandler(t)
			seedProbeTargetTunnel(t, h, 77, "existing", "", 0)
			seedProbeTargetFederationBinding(t, h, 77)
			for _, update := range []bool{false, true} {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"id":77,"name":"changed","type":1,"inNodeId":[{"nodeId":10}],%s}`, fields)))
				if update {
					h.tunnelUpdate(rec, req)
				} else {
					h.tunnelCreate(rec, req)
				}
				var payload struct {
					Code int
					Msg  string
				}
				decodeProbeTargetResponse(t, rec, &payload)
				if payload.Code == 0 || !strings.Contains(payload.Msg, "流量倍率") {
					t.Fatalf("expected billing rejection: %s", rec.Body.String())
				}
				if got := mustHandlerCount(t, h, `SELECT COUNT(*) FROM tunnel WHERE id=77 AND name='existing' AND flow=1 AND traffic_ratio=1`); got != 1 {
					t.Fatal("existing tunnel changed")
				}
				if got := mustHandlerCount(t, h, `SELECT COUNT(*) FROM tunnel`); got != 1 {
					t.Fatal("created invalid tunnel")
				}
				if got := mustHandlerCount(t, h, `SELECT COUNT(*) FROM federation_tunnel_binding WHERE tunnel_id=77`); got != 1 {
					t.Fatal("removed remote binding")
				}
			}
		})
	}
}

func TestTunnelBillingAcceptsDefaultsAndValidValues(t *testing.T) {
	for _, fields := range []string{``, `,"flow":2,"trafficRatio":0.5`, `,"flow":"1","trafficRatio":"2.5"`} {
		t.Run("valid"+fields, func(t *testing.T) {
			h := setupProbeTargetTunnelHandler(t)
			rec := httptest.NewRecorder()
			h.tunnelCreate(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"valid","type":1,"inNodeId":[{"nodeId":10,"protocol":"tls"}]`+fields+`}`)))
			assertProbeTargetSuccess(t, rec)
		})
	}
}
