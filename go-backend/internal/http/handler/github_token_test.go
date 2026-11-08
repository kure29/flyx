package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/store/repo"
)

// fakePrivateGitHub serves a private repository: github.com pages and
// downloads 404, the API answers only with the token.
func fakePrivateGitHub(t *testing.T, token string) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authed := r.Header.Get("Authorization") == "Bearer "+token
		switch r.URL.Path {
		case "/repos/kure29/flyx":
			if !authed {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"private":true}`))
		case "/repos/kure29/flyx/releases/latest":
			if !authed {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"tag_name":"3.5.6"}`))
		case "/repos/kure29/flyx/releases/tags/3.5.6":
			if !authed {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"assets":[{"name":"gost-amd64","url":"` + srv.URL + `/repos/kure29/flyx/releases/assets/7"}]}`))
		case "/repos/kure29/flyx/releases/assets/7":
			if !authed || r.Header.Get("Accept") != "application/octet-stream" {
				http.NotFound(w, r)
				return
			}
			http.Redirect(w, r, "/storage/gost-amd64", http.StatusFound)
		case "/storage/gost-amd64":
			_, _ = w.Write([]byte("private-agent"))
		default:
			http.NotFound(w, r) // github.com pages of a private repository
		}
	}))
	t.Cleanup(srv.Close)

	oldBases, oldDir, oldHTTP, oldAPI := ghMirrorBases, ghMirrorDir, ghMirrorHTTP, releaseAPIBase
	ghMirrorBases.github, ghMirrorBases.api, ghMirrorBases.raw = srv.URL, srv.URL, srv.URL
	releaseAPIBase = srv.URL
	ghMirrorDir = t.TempDir()
	ghMirrorHTTP = &http.Client{Timeout: 10 * time.Second}
	resetGithubCaches()
	t.Cleanup(func() {
		ghMirrorBases, ghMirrorDir, ghMirrorHTTP, releaseAPIBase = oldBases, oldDir, oldHTTP, oldAPI
		resetGithubCaches()
	})
}

func useGithubToken(t *testing.T, token string) {
	t.Helper()
	githubTokenMu.Lock()
	old := githubTokenSource
	githubTokenSource = func() string { return token }
	githubTokenMu.Unlock()
	t.Cleanup(func() {
		githubTokenMu.Lock()
		githubTokenSource = old
		githubTokenMu.Unlock()
	})
}

// With a token the panel's mirror serves a private repository's release
// files, so nodes keep installing and upgrading without a token of their own.
func TestGithubMirrorPrivateRepository(t *testing.T) {
	fakePrivateGitHub(t, "tok")

	if rec := mirrorGet(t, "https://github.com/kure29/flyx/releases/download/3.5.6/gost-amd64"); rec.Code == http.StatusOK {
		t.Fatal("without a token a private release file cannot be fetched")
	}

	useGithubToken(t, "tok")
	for _, target := range []string{
		"https://github.com/kure29/flyx/releases/download/3.5.6/gost-amd64",
		"https://github.com/kure29/flyx/releases/latest/download/gost-amd64",
	} {
		rec := mirrorGet(t, target)
		if rec.Code != http.StatusOK || rec.Body.String() != "private-agent" {
			t.Fatalf("%s: %d %q", target, rec.Code, rec.Body.String())
		}
	}
	// install.sh learns the tag from …/releases/latest.
	rec := mirrorGet(t, "https://github.com/kure29/flyx/releases/latest")
	if rec.Code != http.StatusFound || !strings.HasSuffix(rec.Header().Get("Location"), "/kure29/flyx/releases/tag/3.5.6") {
		t.Fatalf("latest tag: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestGithubTokenIsNeverSentBack(t *testing.T) {
	if !repo.IsSensitiveConfigKey(githubTokenConfig) || !repo.IsSystemManagedConfigKey(githubTokenConfig) {
		t.Fatal("a github_token left stored must stay hidden and out of backups")
	}
}
