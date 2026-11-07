package handler

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// statusGzipMin is the size from which a status answer is also kept
// gzipped: below it compression saves too little to be worth it.
const statusGzipMin = 1024

// statusBody is an encoded status page answer, plain and (when large
// enough) gzipped. Both are made once when the answer is cached, so a
// request costs no compression however many arrive.
type statusBody struct {
	raw []byte
	gz  []byte
}

func newStatusBody(raw []byte) statusBody {
	b := statusBody{raw: raw}
	if len(raw) < statusGzipMin {
		return b
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return b
	}
	if _, err := zw.Write(raw); err != nil {
		return b
	}
	if err := zw.Close(); err != nil {
		return b
	}
	if buf.Len() < len(raw) {
		b.gz = buf.Bytes()
	}
	return b
}

// acceptsGzip reports whether the request's Accept-Encoding allows gzip
// (and does not refuse it with q=0).
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "gzip" && name != "*" {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
					q = f
				}
			}
		}
		if q > 0 {
			return true
		}
	}
	return false
}

// writeStatusBody sends a public status answer with the headers all of
// them share, gzipped when the client accepts it.
func writeStatusBody(w http.ResponseWriter, r *http.Request, b statusBody, maxAge int) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json; charset=utf-8")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Referrer-Policy", "no-referrer")
	// The same answer for everyone: a CDN or browser may keep it as long
	// as the panel does, which takes repeated requests off the panel.
	hdr.Set("Cache-Control", "public, max-age="+strconv.Itoa(maxAge))
	hdr.Add("Vary", "Accept-Encoding")
	data := b.raw
	if b.gz != nil && acceptsGzip(r) {
		hdr.Set("Content-Encoding", "gzip")
		data = b.gz
	}
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}
