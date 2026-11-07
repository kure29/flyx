package statustheme

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// BasePath is where themes are served. A theme is built with this as its
// base (Vite: base: "/status/") and routes under it.
const BasePath = "/status/"

// Content types Go's table may lack on a minimal image.
var contentTypes = map[string]string{
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".html":        "text/html; charset=utf-8",
	".json":        "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".mp4":         "video/mp4",
	".webm":        "video/webm",
	".wasm":        "application/wasm",
	".txt":         "text/plain; charset=utf-8",
	".map":         "application/json",
}

const notBuilt = `<!doctype html><meta charset="utf-8"><title>状态页</title>
<p style="font:14px system-ui;padding:2em">这个面板构建时没有包含默认状态页主题。
开发环境请在 go-backend 目录运行 <code>make status-theme</code> 后重新构建。</p>`

// Serve answers a request under BasePath from fsys. A path naming no file
// gets index.html when it looks like a route (no extension), so the theme
// can route on the client; a missing asset is a 404.
func Serve(w http.ResponseWriter, r *http.Request, fsys fs.FS) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

	rel := strings.TrimPrefix(r.URL.Path, strings.TrimSuffix(BasePath, "/"))
	rel = strings.TrimPrefix(path.Clean("/"+rel), "/")
	if rel == "" || rel == "." {
		rel = indexFile
	}

	data, name, err := readAsset(fsys, rel)
	if errors.Is(err, fs.ErrNotExist) && path.Ext(rel) == "" {
		data, name, err = readAsset(fsys, indexFile)
	}
	if errors.Is(err, fs.ErrNotExist) && name == indexFile {
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, notBuilt)
		return
	}
	if err != nil {
		h.Set("Cache-Control", "no-store")
		http.NotFound(w, r)
		return
	}

	if ct, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
		h.Set("Content-Type", ct)
	}
	if strings.HasPrefix(name, "assets/") {
		// Vite's hashed output: the name changes with the content.
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// index.html and other fixed names revalidate, so switching or
		// updating a theme shows at once.
		sum := sha256.Sum256(data)
		h.Set("ETag", `"`+hex.EncodeToString(sum[:12])+`"`)
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func readAsset(fsys fs.FS, name string) ([]byte, string, error) {
	if !fs.ValidPath(name) {
		return nil, name, fs.ErrNotExist
	}
	f, err := fsys.Open(name)
	if err != nil {
		return nil, name, fs.ErrNotExist
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, name, fs.ErrNotExist
	}
	if st.Size() > maxFile {
		return nil, name, errors.New("file too large")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, name, err
	}
	return data, name, nil
}
