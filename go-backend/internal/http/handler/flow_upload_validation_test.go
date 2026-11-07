package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFlowUploadRejectsInvalidBatchWithoutSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items string
	}{
		{"negative_input", `{"n":"SERVICE","u":100,"d":-50}`},
		{"negative_output", `{"n":"SERVICE","u":-50,"d":100}`},
		{"integer_range", `{"n":"SERVICE","u":9223372036854775808,"d":0}`},
		{"raw_batch_sum", `{"n":"SERVICE","u":9223372036854775800,"d":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupNftablesCollectionFixture(t)
			h := f.handler
			t.Cleanup(func() { _ = h.repo.Close() })
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
