package geoip

import (
	"bytes"
	"compress/gzip"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testdata/test-country.mmdb maps 203.0.113.0/24 → JP, 198.51.100.0/24 → HK,
// 2001:db8:1::/48 → US and 192.0.2.0/24 → ZZ (DB-IP's "unknown").
func testDB(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/test-country.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLookup(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, testDB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	db := New(path, false)
	if err := db.Load(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cases := map[string]string{
		"203.0.113.7":   "JP",
		"198.51.100.1":  "HK",
		"2001:db8:1::5": "US",
		"192.0.2.1":     "", // ZZ
		"8.8.8.8":       "", // not in the test database
		"10.0.0.1":      "", // private
		"100.64.0.1":    "", // CGNAT
		"127.0.0.1":     "",
	}
	for ip, want := range cases {
		if got := db.Lookup(net.ParseIP(ip)); got != want {
			t.Errorf("Lookup(%s) = %q, want %q", ip, got, want)
		}
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	db := New(filepath.Join(t.TempDir(), FileName), false)
	if err := db.Load(); err != nil {
		t.Fatal(err)
	}
	if db.Ready() || db.Lookup(net.ParseIP("203.0.113.7")) != "" {
		t.Fatal("expected no data")
	}
	// The admin's own (missing) file is never downloaded.
	if err := db.Ensure(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if db.Ready() {
		t.Fatal("unmanaged database must not be downloaded")
	}
}

func serve(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	old := DownloadURL
	DownloadURL = func(month time.Time) string { return srv.URL + "/" + month.Format("2006-01") }
	t.Cleanup(func() { DownloadURL = old })
}

func TestEnsureDownloadsAndFallsBackToLastMonth(t *testing.T) {
	body := gzipped(t, testDB(t))
	var asked []string
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if r.URL.Path == "/2026-11" { // not published yet
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	})
	path := filepath.Join(t.TempDir(), "geoip", FileName)
	db := New(path, true)
	defer db.Close()
	if err := db.Ensure(context.Background(), time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(asked, ","); got != "/2026-11,/2026-10" {
		t.Fatalf("asked %s", got)
	}
	if db.Lookup(net.ParseIP("203.0.113.7")) != "JP" {
		t.Fatal("downloaded database not loaded")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestEnsureSkipsFreshFileAndKeepsOldOnBadDownload(t *testing.T) {
	var requests atomic.Int32
	serve(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write(gzipped(t, []byte("not a database")))
	})
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, testDB(t), 0o644); err != nil {
		t.Fatal(err)
	}
	db := New(path, true)
	defer db.Close()
	now := time.Now()
	if err := db.Ensure(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatal("a fresh database must not be downloaded again")
	}
	// A month later the refresh fails; the old database stays in use.
	if err := db.Ensure(context.Background(), now.Add(RefreshAfter+time.Hour)); err == nil {
		t.Fatal("expected the bad download to fail")
	}
	if db.Lookup(net.ParseIP("198.51.100.1")) != "HK" {
		t.Fatal("old database should still answer")
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, testDB(t)) {
		t.Fatal("old database file was replaced")
	}
}

// With GEOIP_REAL_DB pointing at a downloaded DB-IP file, check a few
// well-known addresses against the real data.
func TestRealDatabase(t *testing.T) {
	path := os.Getenv("GEOIP_REAL_DB")
	if path == "" {
		t.Skip("GEOIP_REAL_DB not set")
	}
	db := New(path, false)
	if err := db.Load(); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for ip, want := range map[string]string{"1.1.1.1": "AU", "114.114.114.114": "CN", "8.8.8.8": "US"} {
		if got := db.Lookup(net.ParseIP(ip)); got == "" {
			t.Errorf("Lookup(%s) = empty, want a country (DB-IP says %s in 2026)", ip, want)
		}
	}
}
