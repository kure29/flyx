package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"go-backend/internal/security"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReliableFlowUploadRetriesAtomically(t *testing.T) {
	f := setupNftablesCollectionFixture(t)
	h := f.handler
	t.Cleanup(func() { _ = h.repo.Close() })
	batch := flowUploadEnvelope{Stream: strings.Repeat("a", 32), Sequence: 1, Items: []flowItem{{N: fmt.Sprintf("%d_1_%d", f.forwardID, f.userTunnelID), D: 100}}}
	post := func() int {
		data, _ := json.Marshal(batch)
		crypto, err := security.NewAESCrypto("secret")
		if err != nil {
			t.Fatal(err)
		}
		encrypted, err := crypto.Encrypt(data)
		if err != nil {
			t.Fatal(err)
		}
		data, _ = json.Marshal(map[string]any{"encrypted": true, "data": encrypted})
		w := httptest.NewRecorder()
		h.serveFlowUpload(w, httptest.NewRequest("POST", "/flow/upload/v2?secret=secret", strings.NewReader(string(data))), true)
		return w.Code
	}
	if err := h.repo.DB().Callback().Create().Before("gorm:create").Register("test:quota_failure", func(db *gorm.DB) {
		if db.Statement.Table == "user_quota" {
			db.AddError(errors.New("injected quota failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if status := post(); status != 503 {
		t.Fatalf("write failure acknowledged: %d", status)
	}
	for _, query := range []string{"SELECT in_flow FROM forward", "SELECT COUNT(*) FROM traffic_cursor", "SELECT COUNT(*) FROM tunnel_metric"} {
		if got := mustHandlerCount(t, h, query); got != 0 {
			t.Fatalf("partial commit %s=%d", query, got)
		}
	}
	if err := h.repo.DB().Callback().Create().Remove("test:quota_failure"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if status := post(); status != 200 {
			t.Fatalf("retry rejected: %d", status)
		}
	}
	for _, query := range []string{"SELECT in_flow FROM forward", "SELECT in_flow FROM user WHERE id=1", "SELECT in_flow FROM user_tunnel", "SELECT daily_used_bytes FROM user_quota WHERE user_id=1", "SELECT SUM(bytes_in) FROM tunnel_metric"} {
		if got := mustHandlerCount(t, h, query); got != 100 {
			t.Fatalf("duplicate or inconsistent accounting %s=%d", query, got)
		}
	}
	batch.Items[0].D = 200
	if status := post(); status != 409 {
		t.Fatalf("reused identity accepted: %d", status)
	}
	batch.Sequence = 3
	if status := post(); status != 409 {
		t.Fatalf("sequence gap accepted: %d", status)
	}
	batch.Sequence = 2
	if status := post(); status != 200 {
		t.Fatalf("next batch rejected: %d", status)
	}
	if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward"); got != 300 {
		t.Fatalf("next batch not applied: %d", got)
	}
}

func TestReliableUploadRecoversMissingCursorWithoutDuplicateBilling(t *testing.T) {
	f := setupNftablesCollectionFixture(t)
	h := f.handler
	t.Cleanup(func() { _ = h.repo.Close() })
	batch := flowUploadEnvelope{Stream: strings.Repeat("b", 32), Sequence: 7, Items: []flowItem{{N: fmt.Sprintf("%d_1_%d", f.forwardID, f.userTunnelID), D: 100}}}
	post := func() *httptest.ResponseRecorder {
		data, _ := json.Marshal(batch)
		w := httptest.NewRecorder()
		h.serveFlowUpload(w, httptest.NewRequest("POST", "/flow/upload/v2?secret=secret", strings.NewReader(string(data))), true)
		return w
	}
	w := post()
	var rejection struct {
		Stream   string
		Sequence int64
		Error    string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rejection); err != nil {
		t.Fatal(err)
	}
	if w.Code != 409 || rejection.Stream != batch.Stream || rejection.Sequence != 7 || rejection.Error != "reset_stream" {
		t.Fatalf("unrecoverable gap: %d %s", w.Code, w.Body.String())
	}
	if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward"); got != 0 {
		t.Fatal("gap partially billed")
	}
	batch.Stream = strings.Repeat("c", 32)
	batch.Sequence = 1
	for i := 0; i < 2; i++ {
		if w := post(); w.Code != 200 {
			t.Fatalf("reset stream: %s", w.Body.String())
		}
	}
	if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward"); got != 100 {
		t.Fatalf("recovery billing=%d", got)
	}
	batch.Sequence = 2
	batch.Items[0].D = -100
	w = post()
	if w.Code != 400 || !strings.Contains(w.Body.String(), `"error":"invalid_batch"`) {
		t.Fatalf("unrecognized rejection: %s", w.Body.String())
	}
	if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward"); got != 100 {
		t.Fatal("invalid batch changed usage")
	}
	batch.Sequence = 1
	batch.Items[0].D = 200
	w = post()
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"error":"identity_conflict"`) {
		t.Fatalf("identity conflict must not request replay: %s", w.Body.String())
	}
}
