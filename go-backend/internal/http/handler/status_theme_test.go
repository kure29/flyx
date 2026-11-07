package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-backend/internal/statustheme"
)

func themePackage(t *testing.T, short, version, page string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"theme.json":      fmt.Sprintf(`{"name":"T %s","short":%q,"version":%q,"url":"https://github.com/kure29/%s","flyxApi":1}`, short, short, version, short),
		"dist/index.html": page,
	}
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func themeHandler(t *testing.T) *Handler {
	t.Helper()
	h, _ := setupStatusPageHandler(t)
	h.SetStatusThemesDir(t.TempDir())
	return h
}

func callTheme(t *testing.T, fn http.HandlerFunc, body string) (int, string, json.RawMessage) {
	t.Helper()
	w := httptest.NewRecorder()
	fn(w, httptest.NewRequest(http.MethodPost, "/api/v1/status-theme/x", strings.NewReader(body)))
	var res struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return res.Code, res.Msg, res.Data
}

func statusPage(h *Handler, target string) string {
	w := httptest.NewRecorder()
	h.serveStatusTheme(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w.Body.String()
}

func uploadTheme(t *testing.T, h *Handler, pkg []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "theme.tar.gz")
	_, _ = fw.Write(pkg)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/status-theme/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.statusThemeUpload(w, req)
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	return res.Code, res.Msg
}

func TestStatusThemeUploadSelectServeDelete(t *testing.T) {
	h := themeHandler(t)

	if code, msg := uploadTheme(t, h, themePackage(t, "neon", "1.0.0", "<p>neon</p>")); code != 0 {
		t.Fatalf("upload: %d %s", code, msg)
	}
	// Installing does not switch to it.
	if strings.Contains(statusPage(h, "/status/"), "neon") {
		t.Fatal("an installed theme must not be served before it is chosen")
	}

	if code, msg, _ := callTheme(t, h.statusThemeSelect, `{"short":"neon"}`); code != 0 {
		t.Fatalf("select: %d %s", code, msg)
	}
	if got := statusPage(h, "/status/node/1"); got != "<p>neon</p>" {
		t.Fatalf("served %q", got)
	}

	_, _, data := callTheme(t, h.statusThemeList, ``)
	var list statusThemeListResult
	_ = json.Unmarshal(data, &list)
	if len(list.Themes) != 2 || !list.Themes[1].Selected || list.Themes[0].Selected || list.APIVersion != statustheme.APIVersion {
		t.Fatalf("list %+v", list)
	}

	if code, _, _ := callTheme(t, h.statusThemeSelect, `{"short":"nope"}`); code == 0 {
		t.Fatal("selecting a missing theme must fail")
	}
	if code, _, _ := callTheme(t, h.statusThemeDelete, `{"short":"default"}`); code == 0 {
		t.Fatal("the built-in theme must not be deletable")
	}
	if code, msg, _ := callTheme(t, h.statusThemeDelete, `{"short":"neon"}`); code != 0 {
		t.Fatalf("delete: %d %s", code, msg)
	}
	if h.selectedStatusTheme() != "" {
		t.Fatal("deleting the chosen theme must clear the choice")
	}
	if strings.Contains(statusPage(h, "/status/"), "neon") {
		t.Fatal("a deleted theme must not be served")
	}
}

func TestStatusThemeUploadRefusesNonPackages(t *testing.T) {
	h := themeHandler(t)
	if code, msg := uploadTheme(t, h, []byte("PK\x03\x04")); code == 0 || !strings.Contains(msg, "Source code") {
		t.Fatalf("zip upload: %d %q", code, msg)
	}
}

func TestStatusThemeInstallAndUpdateFromGithub(t *testing.T) {
	h := themeHandler(t)
	version := "1.0.0"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/kure29/neon/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v%s","assets":[{"name":"theme.tar.gz","size":10,"browser_download_url":"%s/kure29/neon/releases/download/v%s/theme.tar.gz"}]}`,
				version, "http://"+r.Host, version)
		case "/kure29/neon/releases/download/v" + version + "/theme.tar.gz":
			_, _ = w.Write(themePackage(t, "neon", version, "<p>neon "+version+"</p>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldAPI, oldWeb := statusThemeGithubAPI, statusThemeGithubWeb
	statusThemeGithubAPI, statusThemeGithubWeb = srv.URL, srv.URL
	defer func() { statusThemeGithubAPI, statusThemeGithubWeb = oldAPI, oldWeb }()

	if code, msg, _ := callTheme(t, h.statusThemeInstall, `{"url":"https://github.com/kure29/neon"}`); code != 0 {
		t.Fatalf("install: %d %s", code, msg)
	}
	_, _, data := callTheme(t, h.statusThemeUpdate, `{"short":"neon"}`)
	var res statusThemeUpdateResult
	_ = json.Unmarshal(data, &res)
	if res.Updated {
		t.Fatal("the same version must not be reinstalled")
	}

	version = "1.1.0"
	code, msg, data := callTheme(t, h.statusThemeUpdate, `{"short":"neon"}`)
	_ = json.Unmarshal(data, &res)
	if code != 0 || !res.Updated || res.Version != "1.1.0" {
		t.Fatalf("update: %d %s %+v", code, msg, res)
	}

	if code, msg, _ := callTheme(t, h.statusThemeInstall, `{"url":"https://github.com/kure29/missing"}`); code == 0 || !strings.Contains(msg, "release") {
		t.Fatalf("missing repo: %d %s", code, msg)
	}
	if code, _, _ := callTheme(t, h.statusThemeInstall, `{"url":"https://gitlab.com/a/b"}`); code == 0 {
		t.Fatal("only GitHub repositories may be installed from")
	}
}

func TestParseThemeRepo(t *testing.T) {
	ok := map[string]string{
		"https://github.com/kure29/Flyx-Theme-LuminaPlus":    "kure29/Flyx-Theme-LuminaPlus",
		"github.com/kure29/Flyx-Theme-LuminaPlus/":           "kure29/Flyx-Theme-LuminaPlus",
		"https://github.com/kure29/repo.git":                 "kure29/repo",
		"https://github.com/kure29/repo/releases/tag/v1.0.0": "kure29/repo",
		"kure29/repo": "kure29/repo",
	}
	for in, want := range ok {
		o, r, good := parseThemeRepo(in)
		if !good || o+"/"+r != want {
			t.Errorf("parseThemeRepo(%q) = %q %q %v", in, o, r, good)
		}
	}
	for _, in := range []string{"", "kure29", "https://gitlab.com/a/b", "https://evil.example/a/b", "a/..", "a b/c"} {
		if _, _, good := parseThemeRepo(in); good {
			t.Errorf("parseThemeRepo(%q) accepted", in)
		}
	}
}

func publicThemeConfig(t *testing.T, h *Handler) (int, string, statusThemeConfigAnswer) {
	t.Helper()
	w := httptest.NewRecorder()
	h.publicStatusThemeConfig(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/status/theme-config", nil))
	var res struct {
		Code int                     `json:"code"`
		Msg  string                  `json:"msg"`
		Data statusThemeConfigAnswer `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return res.Code, res.Msg, res.Data
}

func TestStatusThemeConfigSaveServeAndDelete(t *testing.T) {
	h := themeHandler(t)
	if code, msg := uploadTheme(t, h, themePackage(t, "neon", "1.0.0", "<p>neon</p>")); code != 0 {
		t.Fatalf("upload: %d %s", code, msg)
	}

	// Saved per theme; the public answer follows the theme in use.
	if code, msg, _ := callTheme(t, h.statusThemeConfigSave, `{"short":"neon","values":{"view":"compact","cards":3,"dark":true}}`); code != 0 {
		t.Fatalf("save: %d %s", code, msg)
	}
	if code, msg, _ := callTheme(t, h.statusThemeConfigSave, `{"short":"default","values":{"x":1}}`); code != 0 {
		t.Fatalf("save default: %d %s", code, msg)
	}
	_, _, data := callTheme(t, h.statusThemeConfigGet, `{"short":"neon"}`)
	var got statusThemeConfigAnswer
	_ = json.Unmarshal(data, &got)
	if got.Theme != "neon" || string(got.Values) != `{"cards":3,"dark":true,"view":"compact"}` {
		t.Fatalf("get = %s", data)
	}

	if code, msg, _ := publicThemeConfig(t, h); code != 404 || !strings.Contains(msg, "未开启") {
		t.Fatalf("page off: %d %s", code, msg)
	}
	_ = h.repo.UpsertConfig(statusPageEnabledKey, "true", 1)
	h.statusHistory = &statusHistoryCache{}
	if _, _, pub := publicThemeConfig(t, h); pub.Theme != "default" || string(pub.Values) != `{"x":1}` {
		t.Fatalf("public with the built-in theme = %+v", pub)
	}
	if code, msg, _ := callTheme(t, h.statusThemeSelect, `{"short":"neon"}`); code != 0 {
		t.Fatalf("select: %d %s", code, msg)
	}
	if _, _, pub := publicThemeConfig(t, h); pub.Theme != "neon" || !strings.Contains(string(pub.Values), `"compact"`) {
		t.Fatalf("public with neon = %+v", pub)
	}

	// Deleting the theme deletes its settings: a reinstall starts clean.
	if code, msg, _ := callTheme(t, h.statusThemeDelete, `{"short":"neon"}`); code != 0 {
		t.Fatalf("delete: %d %s", code, msg)
	}
	if code, _ := uploadTheme(t, h, themePackage(t, "neon", "1.0.0", "<p>neon</p>")); code != 0 {
		t.Fatal("reinstall failed")
	}
	_, _, data = callTheme(t, h.statusThemeConfigGet, `{"short":"neon"}`)
	_ = json.Unmarshal(data, &got)
	if string(got.Values) != "{}" {
		t.Fatalf("settings survived deletion: %s", data)
	}
}

func TestStatusThemeConfigRefusals(t *testing.T) {
	h := themeHandler(t)
	for _, body := range []string{
		`{"short":"missing","values":{}}`,
		`{"short":"default","values":[1,2]}`,
		`{"short":"default","values":"x"}`,
		`{"short":"default"}`,
		`{"short":"default","values":null}`,
		`{"short":"default","values":{},"extra":1}`,
		`{"short":"default","values":{"big":"` + strings.Repeat("a", maxStatusThemeConfig) + `"}}`,
	} {
		if code, _, _ := callTheme(t, h.statusThemeConfigSave, body); code == 0 {
			t.Fatalf("accepted %.80s", body)
		}
	}
	if code, _, _ := callTheme(t, h.statusThemeConfigGet, `{"short":"missing"}`); code == 0 {
		t.Fatal("get of a missing theme succeeded")
	}
}
