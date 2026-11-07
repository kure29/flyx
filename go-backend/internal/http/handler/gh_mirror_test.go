package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHubMirrorUpstream stands in for github.com, api.github.com and
// raw.githubusercontent.com, counting requests per path.
func fakeGitHubMirrorUpstream(t *testing.T, down *atomic.Bool) map[string]*atomic.Int32 {
	t.Helper()
	hits := map[string]*atomic.Int32{}
	for _, p := range []string{
		"/kure29/flyx/releases/download/3.3.6/gost-amd64",
		"/blob/gost-amd64",
		"/v2fly/geoip/releases/latest",
		"/v2fly/geoip/releases/download/202610/geoip.dat",
		"/repos/XTLS/Xray-core/releases",
		"/SagerNet/sing-geosite/rule-set/geosite-cn.srs",
	} {
		hits[p] = &atomic.Int32{}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := hits[r.URL.Path]; c != nil {
			c.Add(1)
		}
		if down.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/kure29/flyx/releases/download/3.3.6/gost-amd64":
			// GitHub sends release files on to its asset host.
			http.Redirect(w, r, "/blob/gost-amd64", http.StatusFound)
		case "/blob/gost-amd64":
			_, _ = w.Write([]byte("agent-binary"))
		// A moved repository: GitHub sends old URLs on.
		case "/seren233/flyx/releases/download/3.3.6/gost-amd64", "/seren233/flyx/releases/latest":
			w.Header().Set("Location", "http://"+r.Host+strings.Replace(r.URL.Path, "/seren233/", "/kure29/", 1))
			w.WriteHeader(http.StatusMovedPermanently)
		case "/kure29/flyx/releases/latest":
			w.Header().Set("Location", "https://github.com/kure29/flyx/releases/tag/3.9.0")
			w.WriteHeader(http.StatusFound)
		case "/v2fly/geoip/releases/latest":
			w.Header().Set("Location", "https://github.com/v2fly/geoip/releases/tag/202610")
			w.WriteHeader(http.StatusFound)
		case "/v2fly/geoip/releases/download/202610/geoip.dat":
			_, _ = w.Write([]byte("geoip"))
		case "/v2fly/domain-list-community/releases/latest":
			w.WriteHeader(http.StatusForbidden)
		case "/v2fly/domain-list-community/releases/latest/download/dlc.dat":
			w.Header().Set("Location", "https://github.com/v2fly/domain-list-community/releases/download/2026/dlc.dat")
			w.WriteHeader(http.StatusFound)
		case "/v2fly/domain-list-community/releases/download/2026/dlc.dat":
			_, _ = w.Write([]byte("dlc"))
		case "/repos/XTLS/Xray-core/releases":
			_, _ = w.Write([]byte(`[{"tag_name":"v26.1.1","per_page":"` + r.URL.Query().Get("per_page") + `"}]`))
		case "/SagerNet/sing-geosite/rule-set/geosite-cn.srs":
			_, _ = w.Write([]byte("srs"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	oldBases, oldDir, oldHTTP := ghMirrorBases, ghMirrorDir, ghMirrorHTTP
	ghMirrorBases.github, ghMirrorBases.api, ghMirrorBases.raw = srv.URL, srv.URL, srv.URL
	ghMirrorDir = t.TempDir()
	ghMirrorHTTP = &http.Client{Timeout: 10 * time.Second} // the fake asset host is 127.0.0.1
	ghMirrorTagMu.Lock()
	ghMirrorTags = map[string]ghMirrorTag{}
	ghMirrorTagMu.Unlock()
	t.Cleanup(func() { ghMirrorBases, ghMirrorDir, ghMirrorHTTP = oldBases, oldDir, oldHTTP })
	return hits
}

func mirrorGet(t *testing.T, target string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, ghMirrorPath+target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	(&Handler{}).githubMirror(rec, req)
	return rec
}

func TestGithubMirrorCachesReleaseFiles(t *testing.T) {
	var down atomic.Bool
	hits := fakeGitHubMirrorUpstream(t, &down)
	const file = "https://github.com/kure29/flyx/releases/download/3.3.6/gost-amd64"

	for i := 0; i < 3; i++ {
		rec := mirrorGet(t, file)
		if rec.Code != http.StatusOK || rec.Body.String() != "agent-binary" {
			t.Fatalf("get %d: %d %q", i, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Length") != "12" {
			t.Fatalf("content length: %q", rec.Header().Get("Content-Length"))
		}
	}
	if n := hits["/blob/gost-amd64"].Load(); n != 1 {
		t.Fatalf("upstream fetched %d times, want 1", n)
	}
	// Slashes merged by nginx / ServeMux, and ranges, work too.
	rec := mirrorGet(t, strings.Replace(file, "https://", "https:/", 1), "Range", "bytes=0-4")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "agent" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}
	// Tagged files are kept even when GitHub is down.
	down.Store(true)
	if rec := mirrorGet(t, file); rec.Code != http.StatusOK {
		t.Fatalf("cached while down: %d", rec.Code)
	}
}

func TestGithubMirrorLatest(t *testing.T) {
	var down atomic.Bool
	fakeGitHubMirrorUpstream(t, &down)

	rec := mirrorGet(t, "https://github.com/v2fly/geoip/releases/latest/download/geoip.dat")
	if rec.Code != http.StatusOK || rec.Body.String() != "geoip" {
		t.Fatalf("latest download: %d %q", rec.Code, rec.Body.String())
	}
	// install.sh learns the tag from where …/releases/latest ends up.
	rec = mirrorGet(t, "https://github.com/v2fly/geoip/releases/latest")
	want := ghMirrorPath + "https:/github.com/v2fly/geoip/releases/tag/202610" // http.Redirect merges the slashes
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
		t.Fatalf("latest page: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := mirrorGet(t, "https://github.com/v2fly/geoip/releases/tag/202610"); rec.Code != http.StatusOK {
		t.Fatalf("tag page: %d", rec.Code)
	}
	// Without the release page, the file's own "latest" redirect names the tag.
	rec = mirrorGet(t, "https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat")
	if rec.Code != http.StatusOK || rec.Body.String() != "dlc" {
		t.Fatalf("latest via file redirect: %d %q", rec.Code, rec.Body.String())
	}
}

func TestGithubMirrorRefreshesAPIAndServesStale(t *testing.T) {
	var down atomic.Bool
	hits := fakeGitHubMirrorUpstream(t, &down)
	const api = "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=30"

	rec := mirrorGet(t, api)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"per_page":"30"`) ||
		!strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("api: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
	mirrorGet(t, api)
	if n := hits["/repos/XTLS/Xray-core/releases"].Load(); n != 1 {
		t.Fatalf("api fetched %d times within the cache period", n)
	}

	// Expired and GitHub down: the old answer is served.
	entries, _ := os.ReadDir(ghMirrorDir)
	old := time.Now().Add(-time.Hour)
	for _, e := range entries {
		_ = os.Chtimes(filepath.Join(ghMirrorDir, e.Name()), old, old)
	}
	down.Store(true)
	if rec := mirrorGet(t, api); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "v26.1.1") {
		t.Fatalf("stale: %d %q", rec.Code, rec.Body.String())
	}
	if n := hits["/repos/XTLS/Xray-core/releases"].Load(); n != 2 {
		t.Fatalf("api fetched %d times, want a retry after expiry", n)
	}
	// Not cached at all and GitHub down: an error, not an empty file.
	if rec := mirrorGet(t, "https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-cn.srs"); rec.Code != http.StatusBadGateway {
		t.Fatalf("uncached while down: %d", rec.Code)
	}
}

func TestGithubMirrorRefusesOtherRepos(t *testing.T) {
	for _, target := range []string{
		"https://github.com/someone/else/releases/download/v1/file",
		"https://github.com/kure29/flyx/archive/refs/heads/main.zip",
		"https://github.com/kure29/flyx/releases/download/../../x/y",
		"https://api.github.com/repos/someone/else/releases",
		"https://api.github.com/user",
		"https://raw.githubusercontent.com/someone/else/main/x",
		"https://example.com/kure29/flyx/releases/download/v1/file",
		"https://gist.github.com/x",
	} {
		if rec := mirrorGet(t, target); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", target, rec.Code)
		}
	}
}

func TestGithubMirrorRedirectsStayOnGitHub(t *testing.T) {
	for host, ok := range map[string]bool{
		"github.com":                           true,
		"objects.githubusercontent.com":        true,
		"release-assets.githubusercontent.com": true,
		"example.com":                          false,
		"127.0.0.1":                            false,
	} {
		req := &http.Request{URL: &url.URL{Scheme: "https", Host: host, Path: "/x"}}
		if err := ghMirrorCheckRedirect(req, nil); (err == nil) != ok {
			t.Errorf("%s: %v", host, err)
		}
	}
	if !isPanelGithubMirror("https://panel.example.com/api/v1/open_api/gh/") || isPanelGithubMirror("https://gcode.hostcentral.cc") {
		t.Fatal("isPanelGithubMirror")
	}
}

func TestGithubMirrorFollowsMovedRepository(t *testing.T) {
	var down atomic.Bool
	fakeGitHubMirrorUpstream(t, &down)

	// Node scripts installed from seren233 still ask for seren233/flyx.
	rec := mirrorGet(t, "https://github.com/seren233/flyx/releases/download/3.3.6/gost-amd64")
	if rec.Code != http.StatusOK || rec.Body.String() != "agent-binary" {
		t.Fatalf("old repository download = %d %q", rec.Code, rec.Body.String())
	}
	tag, err := ghMirrorLatestTag("seren233/flyx", "")
	if err != nil || tag != "3.9.0" {
		t.Fatalf("latest tag of the moved repository = %q, %v", tag, err)
	}
	if got := ghMirrorMovedRepo("https://evil.example/a/b/releases/latest", "/releases/latest"); got != "" {
		t.Fatalf("redirect off GitHub followed: %q", got)
	}
}
