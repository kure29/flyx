package handler

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                   false,
		"gzip":               true,
		"gzip, deflate, br":  true,
		"br;q=1, GZIP;q=0.5": true,
		"gzip;q=0":           false,
		"identity":           false,
		"*":                  true,
	}
	for header, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Encoding", header)
		if got := acceptsGzip(r); got != want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

func TestWriteStatusBodyGzip(t *testing.T) {
	raw := bytes.Repeat([]byte(`{"code":0,"msg":"ok"}`), 200)
	body := newStatusBody(raw)
	if body.gz == nil || len(body.gz) >= len(raw) {
		t.Fatalf("expected a smaller gzipped body, got %d of %d bytes", len(body.gz), len(raw))
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/public/status", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	writeStatusBody(w, r, body, 5)
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q", got)
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if !bytes.Equal(plain, raw) {
		t.Fatal("gzipped body does not decode to the original")
	}

	// Without gzip support the plain body is sent.
	r = httptest.NewRequest(http.MethodGet, "/api/v1/public/status", nil)
	w = httptest.NewRecorder()
	writeStatusBody(w, r, body, 5)
	if w.Header().Get("Content-Encoding") != "" || !bytes.Equal(w.Body.Bytes(), raw) {
		t.Fatal("expected the plain body without Accept-Encoding")
	}

	// Small answers are not compressed.
	if small := newStatusBody([]byte(`{"code":404}`)); small.gz != nil {
		t.Fatal("small body should not be gzipped")
	}
}
