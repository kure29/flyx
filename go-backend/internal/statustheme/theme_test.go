package statustheme

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

type entry struct {
	name string
	body string
	typ  byte
	link string
}

func pack(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Typeflag: typ, Mode: 0o644, Size: int64(len(e.body)), Linkname: e.link}
		if typ != tar.TypeReg {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const luminaManifest = `{"name":"LuminaPlus","short":"luminaplus","version":"1.0.0","author":"kure29","url":"https://github.com/kure29/Flyx-Theme-LuminaPlus","flyxApi":1}`

func goodTheme(t *testing.T, manifest string) []byte {
	return pack(t,
		entry{name: "theme.json", body: manifest},
		entry{name: "preview.png", body: "png"},
		entry{name: "dist/", typ: tar.TypeDir},
		entry{name: "dist/index.html", body: "<html>lumina</html>"},
		entry{name: "dist/assets/app-abc.js", body: "console.log(1)"},
	)
}

func refusal(t *testing.T, err error, want string) {
	t.Helper()
	var e Error
	if !errors.As(err, &e) {
		t.Fatalf("want refusal containing %q, got %v", want, err)
	}
	if !strings.Contains(e.Error(), want) {
		t.Fatalf("want refusal containing %q, got %q", want, e.Error())
	}
}

func TestInstallListSelectRemove(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "themes")}

	got, err := s.Install(bytes.NewReader(goodTheme(t, luminaManifest)), "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Short != "luminaplus" || !got.Preview || got.Builtin {
		t.Fatalf("unexpected theme %+v", got)
	}

	list := s.List("")
	if len(list) != 2 || list[0].Short != DefaultShort || !list[0].Builtin || !list[0].Selected || list[1].Selected {
		t.Fatalf("unexpected list %+v", list)
	}
	list = s.List("luminaplus")
	if list[0].Selected || !list[1].Selected {
		t.Fatalf("selection not applied: %+v", list)
	}
	if s.Effective("missing") != DefaultShort {
		t.Fatal("an unknown choice must fall back to the built-in theme")
	}
	if img, ok := s.Preview("luminaplus"); !ok || string(img) != "png" {
		t.Fatalf("preview = %q %v", img, ok)
	}

	// Nothing but the theme itself is left behind.
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 1 {
		t.Fatalf("leftovers in themes dir: %v", entries)
	}

	if err := s.Remove("luminaplus"); err != nil {
		t.Fatal(err)
	}
	if s.Effective("luminaplus") != DefaultShort {
		t.Fatal("a removed theme must fall back to the built-in one")
	}
	refusal(t, s.Remove(DefaultShort), "内置主题")
	refusal(t, s.Remove("../x"), "没有这个主题")
}

func TestInstallReplacesAndChecksExpectedShort(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.Install(bytes.NewReader(goodTheme(t, luminaManifest)), ""); err != nil {
		t.Fatal(err)
	}
	newer := strings.Replace(luminaManifest, `"1.0.0"`, `"1.1.0"`, 1)
	got, err := s.Install(bytes.NewReader(goodTheme(t, newer)), "luminaplus")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.1.0" {
		t.Fatalf("version = %q", got.Version)
	}
	_, err = s.Install(bytes.NewReader(goodTheme(t, newer)), "other")
	refusal(t, err, "不是")
	if th, ok := s.Find("luminaplus"); !ok || th.Version != "1.1.0" {
		t.Fatal("a refused update must leave the installed theme in place")
	}
}

func TestInstallRefusals(t *testing.T) {
	cases := []struct {
		name    string
		archive []byte
		want    string
	}{
		{"not gzip", []byte("PK\x03\x04zip"), "不是主题包"},
		{"truncated", goodTheme(t, luminaManifest)[:60], "损坏"},
		{"no manifest", pack(t, entry{name: "dist/index.html", body: "x"}), "没有 theme.json"},
		{"no index", pack(t, entry{name: "theme.json", body: luminaManifest}), "dist/index.html"},
		{"monitor theme", goodTheme(t, `{"name":"LuminaPlus","short":"LuminaPlus","version":"0.2.12"}`), "short"},
		{"no flyxApi", goodTheme(t, `{"name":"X","short":"x"}`), "flyxApi"},
		{"newer api", goodTheme(t, `{"name":"X","short":"x","flyxApi":99}`), "升级面板"},
		{"default short", goodTheme(t, `{"name":"X","short":"default","flyxApi":1}`), "内置主题"},
		{"dotdot", pack(t, entry{name: "theme.json", body: luminaManifest}, entry{name: "../evil", body: "x"}), "越出"},
		{"absolute", pack(t, entry{name: "/etc/evil", body: "x"}), "越出"},
		{"symlink", pack(t, entry{name: "dist", typ: tar.TypeSymlink, link: "/etc"}), "不支持"},
		{"hardlink", pack(t, entry{name: "x", typ: tar.TypeLink, link: "/etc/passwd"}), "不支持"},
		{"file too big", pack(t, entry{name: "big", body: strings.Repeat("a", maxFile+1)}), "上限"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Store{Dir: t.TempDir()}
			_, err := s.Install(bytes.NewReader(c.archive), "")
			refusal(t, err, c.want)
			entries, _ := os.ReadDir(s.Dir)
			if len(entries) != 0 {
				t.Fatalf("a refused package left %v behind", entries)
			}
		})
	}
}

func TestInstalledThemeMustMatchItsDirectory(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	dir := filepath.Join(s.Dir, "copied")
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "dist", "index.html"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "theme.json"), []byte(luminaManifest), 0o644)
	if len(s.List("")) != 1 {
		t.Fatal("a directory whose manifest names another short must not be listed")
	}
}

func serve(fsys fstestMap, target string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	Serve(w, r, fstest.MapFS(fsys))
	return w
}

type fstestMap = fstest.MapFS

func TestServe(t *testing.T) {
	app := fstestMap{
		"index.html":           {Data: []byte("<html>app</html>")},
		"assets/app-abc.js":    {Data: []byte("js")},
		"assets/font.woff2":    {Data: []byte("font")},
		"manifest.webmanifest": {Data: []byte("{}")},
	}

	for _, target := range []string{"/status", "/status/", "/status/node/3", "/status/../../etc/passwd"} {
		w := serve(app, target)
		if w.Code != 200 || w.Body.String() != "<html>app</html>" {
			t.Fatalf("%s: %d %q", target, w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("ETag") == "" {
			t.Fatalf("%s: index must revalidate: %v", target, w.Header())
		}
	}

	w := serve(app, "/status/assets/app-abc.js")
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") ||
		!strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %v", w.Code, w.Header())
	}
	if w := serve(app, "/status/assets/font.woff2"); w.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("woff2 type = %q", w.Header().Get("Content-Type"))
	}
	if w := serve(app, "/status/assets/missing.js"); w.Code != 404 {
		t.Fatalf("missing asset = %d", w.Code)
	}

	etag := serve(app, "/status/").Header().Get("ETag")
	if w := serve(app, "/status/", "If-None-Match", etag); w.Code != http.StatusNotModified {
		t.Fatalf("conditional request = %d", w.Code)
	}

	if w := serve(fstestMap{}, "/status/"); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "make status-theme") {
		t.Fatalf("unbuilt theme = %d %q", w.Code, w.Body.String())
	}
}

func TestBuiltinManifest(t *testing.T) {
	b := builtinTheme()
	if b.Short != DefaultShort || !b.Builtin || b.FlyxAPI != APIVersion {
		t.Fatalf("built-in manifest %+v", b)
	}
}
