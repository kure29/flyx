package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeGitHub serves the releases API (failing once limited is set) and the
// github.com "latest" redirect.
func fakeGitHub(t *testing.T, limited *atomic.Bool, apiCalls *atomic.Int32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases") && strings.HasPrefix(r.URL.Path, "/repos/"):
			apiCalls.Add(1)
			if limited.Load() {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"API rate limit exceeded for 1.2.3.4."}`))
				return
			}
			_, _ = w.Write([]byte(`[{"tag_name":"3.4.0-beta1"},{"tag_name":"3.3.5"},{"tag_name":"3.3.4"}]`))
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			w.Header().Set("Location", "https://github.com/"+githubRepo+"/releases/tag/3.3.6")
			w.WriteHeader(http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	oldAPI, oldHTML := releaseAPIBase, releaseHTMLBase
	releaseAPIBase, releaseHTMLBase = srv.URL, srv.URL
	resetReleaseCache()
	t.Cleanup(func() {
		releaseAPIBase, releaseHTMLBase = oldAPI, oldHTML
		resetReleaseCache()
	})
}

func resetReleaseCache() {
	releaseCacheMu.Lock()
	releaseCache, releaseCacheAt = nil, time.Time{}
	releaseCacheMu.Unlock()
}

func TestLatestReleaseIsCachedAndSurvivesRateLimit(t *testing.T) {
	var limited atomic.Bool
	var calls atomic.Int32
	fakeGitHub(t, &limited, &calls)

	for i := 0; i < 3; i++ {
		if tag, err := resolveLatestReleaseByChannel(releaseChannelStable); err != nil || tag != "3.3.5" {
			t.Fatalf("stable: %q %v", tag, err)
		}
	}
	if tag, _ := resolveLatestReleaseByChannel(releaseChannelDev); tag != "3.4.0-beta1" {
		t.Fatalf("dev: %q", tag)
	}
	if calls.Load() != 1 {
		t.Fatalf("API called %d times, want 1", calls.Load())
	}

	// Expired cache + rate limit: the stale copy is still served.
	limited.Store(true)
	releaseCacheMu.Lock()
	releaseCacheAt = time.Now().Add(-time.Hour)
	releaseCacheMu.Unlock()
	if tag, err := resolveLatestReleaseByChannel(releaseChannelStable); err != nil || tag != "3.3.5" {
		t.Fatalf("stale: %q %v", tag, err)
	}
}

func TestLatestStableFallsBackToRedirect(t *testing.T) {
	var limited atomic.Bool
	var calls atomic.Int32
	limited.Store(true)
	fakeGitHub(t, &limited, &calls)

	if tag, err := resolveLatestReleaseByChannel(releaseChannelStable); err != nil || tag != "3.3.6" {
		t.Fatalf("redirect: %q %v", tag, err)
	}
	// There is no redirect for test builds: the API error is reported.
	_, err := resolveLatestReleaseByChannel(releaseChannelDev)
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("dev error: %v", err)
	}
}

// The panel's own version check shares the cache and falls back to the
// redirect, so a rate-limited API no longer hides the latest version.
func TestPanelVersionCheckSurvivesRateLimit(t *testing.T) {
	var limited atomic.Bool
	var calls atomic.Int32
	fakeGitHub(t, &limited, &calls)
	h, r, _ := setupNotifyHandler(t)
	// The default shared GitHub proxy has used up its API quota.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	t.Cleanup(proxy.Close)
	if err := r.UpsertConfig("github_proxy_url", proxy.URL, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	if tag, err := h.resolveSystemUpgradeLatestReleaseByChannel(releaseChannelStable); err != nil || tag != "3.3.5" {
		t.Fatalf("stable: %q %v", tag, err)
	}
	if _, err := fetchGitHubReleases(); err != nil || calls.Load() != 1 {
		t.Fatalf("cache not shared with node upgrades: %d calls, %v", calls.Load(), err)
	}

	limited.Store(true)
	resetReleaseCache()
	if tag, err := h.resolveSystemUpgradeLatestReleaseByChannel(releaseChannelStable); err != nil || tag != "3.3.6" {
		t.Fatalf("redirect fallback: %q %v", tag, err)
	}
	if _, err := h.fetchSystemUpgradeReleases(); err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("rate limit error: %v", err)
	}
}
