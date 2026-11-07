package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

func TestInboundTags(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/tags.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	call := func(fn http.HandlerFunc, body string) response.R {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		var out response.R
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	out := call(h.inboundImport, `{"links":"trojan://pw@t.example.com:443#T\nanytls://pw@5.6.7.8:8443#A","tags":[" 出站 ","落地","出站",""]}`)
	if out.Code != 0 {
		t.Fatalf("import: %+v", out)
	}
	rows, _ := r.ListNodeInbounds()
	if len(rows) != 2 || rows[0].Tags != "出站,落地" || rows[1].Tags != "出站,落地" {
		t.Fatalf("imported nodes carry the trimmed, de-duplicated tags: %+v", rows)
	}

	if out := call(h.inboundTags, `{"ids":[`+jsonID(rows[0].ID)+`],"tags":["香港"]}`); out.Code != 0 {
		t.Fatalf("set tags: %+v", out)
	}
	got, _ := r.GetNodeInbound(rows[0].ID)
	other, _ := r.GetNodeInbound(rows[1].ID)
	if got.Tags != "香港" || other.Tags != "出站,落地" {
		t.Fatalf("only the given inbound changes: %q %q", got.Tags, other.Tags)
	}

	// Editing the node keeps its tags.
	if out := call(h.inboundExternalUpdate, `{"id":`+jsonID(got.ID)+`,"name":"T2","host":"t.example.com","port":443}`); out.Code != 0 {
		t.Fatalf("edit: %+v", out)
	}
	if got, _ = r.GetNodeInbound(got.ID); got.Tags != "香港" || got.Name != "T2" {
		t.Fatalf("edit must not clear tags: %+v", got)
	}

	for _, body := range []string{
		`{"ids":[` + jsonID(got.ID) + `],"tags":["a,b"]}`,
		`{"ids":[` + jsonID(got.ID) + `],"tags":["` + strings.Repeat("长", 21) + `"]}`,
		`{"ids":[],"tags":["a"]}`,
		`{"ids":[999999],"tags":["a"]}`,
	} {
		if out := call(h.inboundTags, body); out.Code == 0 {
			t.Fatalf("%s must be rejected", body)
		}
	}

	if out := call(h.inboundTags, `{"ids":[`+jsonID(got.ID)+`,`+jsonID(other.ID)+`],"tags":[]}`); out.Code != 0 {
		t.Fatalf("clear: %+v", out)
	}
	rows, _ = r.ListNodeInbounds()
	if rows[0].Tags != "" || rows[1].Tags != "" {
		t.Fatalf("tags cleared: %+v", rows)
	}
}
