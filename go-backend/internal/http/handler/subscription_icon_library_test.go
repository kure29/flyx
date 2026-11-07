package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/kernel"
	"go-backend/internal/store/repo"
)

func TestIconLibraryAddListDelete(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/icons.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	lib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/bad.json" {
			_, _ = w.Write([]byte(`<html>`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"Mine","icons":[{"name":"YouTube.png","url":"https://x.test/YouTube.png"}]}`))
	}))
	t.Cleanup(lib.Close)
	// The test server is on loopback.
	if err := r.UpsertConfig(fetchAllowLocalConfigKey, "true", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	call := func(fn http.HandlerFunc, body string, out interface{}) (int, string) {
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
		var res struct {
			Code int             `json:"code"`
			Msg  string          `json:"msg"`
			Data json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if out != nil {
			_ = json.Unmarshal(res.Data, out)
		}
		return res.Code, res.Msg
	}

	if code, _ := call(h.subscriptionIconLibrarySave, `{"url":"`+lib.URL+`/bad.json"}`, nil); code == 0 {
		t.Fatal("saved a library that is not an icon subscription")
	}
	var libs []iconLibraryRef
	if code, msg := call(h.subscriptionIconLibrarySave, `{"url":"`+lib.URL+`/mine.json"}`, &libs); code != 0 || len(libs) != 2 || !libs[0].Builtin || libs[1].Name != "Mine" {
		t.Fatalf("save: %d %s %+v", code, msg, libs)
	}
	if code, _ := call(h.subscriptionIconLibrarySave, `{"url":"`+lib.URL+`/mine.json"}`, nil); code == 0 {
		t.Fatal("saved a library twice")
	}
	var got kernel.IconLibrary
	if code, msg := call(h.subscriptionIconLibraryIcons, `{"url":"`+lib.URL+`/mine.json"}`, &got); code != 0 || len(got.Icons) != 1 {
		t.Fatalf("icons: %d %s %+v", code, msg, got)
	}
	// Only added libraries are downloaded.
	if code, _ := call(h.subscriptionIconLibraryIcons, `{"url":"`+lib.URL+`/other.json"}`, nil); code == 0 {
		t.Fatal("downloaded a library that was not added")
	}
	var list struct {
		Libraries []iconLibraryRef  `json:"libraries"`
		Defaults  map[string]string `json:"defaults"`
	}
	if call(h.subscriptionIconLibraryList, `{}`, &list); len(list.Libraries) != 2 || list.Defaults[kernel.IconMain] == "" {
		t.Fatalf("list: %+v", list)
	}
	if code, _ := call(h.subscriptionIconLibraryDelete, `{"url":"`+lib.URL+`/mine.json"}`, &libs); code != 0 || len(libs) != 1 {
		t.Fatalf("delete: %+v", libs)
	}
}
