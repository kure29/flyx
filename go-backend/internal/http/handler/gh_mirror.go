package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// The panel's GitHub mirror. /api/v1/open_api/gh/<GitHub URL> works like the
// public "GitHub 加速" proxies (<proxy>/https://github.com/...), so setting
// 加速地址 to <panel>/api/v1/open_api/gh makes nodes that reach the panel but
// not GitHub (mainland China) install, upgrade and fetch kernels through the
// panel. Old agents need nothing new: they already prefix URLs with the proxy.
//
// Only the repositories the panel installs from are served. Release files
// under a tag never change and are kept on disk; "latest" files, raw files
// and API answers are refreshed after a while, and a stale copy is served
// when GitHub can't be reached.

const (
	ghMirrorPath     = "/api/v1/open_api/gh/"
	ghMirrorMaxFile  = 300 << 20
	ghMirrorMaxCache = 2 << 30
	ghMirrorFreshAPI = 10 * time.Minute
	ghMirrorFreshRaw = 6 * time.Hour
	ghMirrorLatest   = 10 * time.Minute
	ghMirrorServeFor = 30 * time.Minute
)

// ghMirrorRepos are the repositories the panel installs from: its own
// releases (agent, install.sh), the kernels, and the data files they read.
func ghMirrorRepos() map[string]bool {
	return map[string]bool{
		strings.ToLower(githubRepo):    true,
		"xtls/xray-core":               true,
		"sagernet/sing-box":            true,
		"ihciah/shadow-tls":            true,
		"zhboner/realm":                true,
		"v2fly/geoip":                  true,
		"v2fly/domain-list-community":  true,
		"sagernet/sing-geosite":        true,
		"sagernet/sing-geoip":          true,
		"metacubex/meta-rules-dat":     true,
		"blackmatrix7/ios_rule_script": true,
	}
}

var ghMirrorSegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ghMirrorUpstream names the GitHub resource a request asks for.
type ghMirrorUpstream struct {
	url   string        // what is fetched
	fresh time.Duration // how long a cached copy is used; 0 = forever
	// latest is set for …/releases/latest/download/<file>: the file is
	// served from the release the "latest" tag currently points at.
	repo, file string
	latest     bool
	// tagPage is set for …/releases/latest and …/releases/tag/<tag>, which
	// install.sh only follows to learn the tag.
	tagPage bool
}

var (
	ghMirrorDir   = "" // set from DB_PATH; tests point it elsewhere
	ghMirrorHTTP  = &http.Client{Timeout: 10 * time.Minute, CheckRedirect: ghMirrorCheckRedirect}
	ghMirrorBases = struct{ github, api, raw string }{"https://github.com", "https://api.github.com", "https://raw.githubusercontent.com"}

	ghMirrorFlightMu sync.Mutex
	ghMirrorFlights  = map[string]*ghMirrorFlight{}

	ghMirrorTagMu sync.Mutex
	ghMirrorTags  = map[string]ghMirrorTag{}
)

type ghMirrorFlight struct {
	done chan struct{}
	err  error
}

type ghMirrorTag struct {
	tag string
	at  time.Time
}

func ghMirrorCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("too many redirects")
	}
	host := strings.ToLower(req.URL.Hostname())
	if host == "github.com" || strings.HasSuffix(host, ".githubusercontent.com") {
		return nil
	}
	return fmt.Errorf("redirect to %s refused", host)
}

// parseGHMirrorPath turns the part after /gh/ into an upstream, refusing
// anything outside the allowed repositories.
func parseGHMirrorPath(rest, rawQuery string) (*ghMirrorUpstream, error) {
	// Slashes may have been merged on the way ("https:/github.com/...").
	rest = strings.TrimPrefix(rest, "/")
	for _, p := range []string{"https://", "https:/", "http://", "http:/"} {
		if strings.HasPrefix(rest, p) {
			rest = rest[len(p):]
			break
		}
	}
	host, path, _ := strings.Cut(rest, "/")
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if !ghMirrorSegment.MatchString(p) || p == "." || p == ".." {
			return nil, fmt.Errorf("不支持的地址")
		}
	}
	repos := ghMirrorRepos()
	allowed := func(owner, repo string) bool { return repos[strings.ToLower(owner+"/"+repo)] }

	switch strings.ToLower(host) {
	case "github.com":
		// owner/repo/releases/download/<tag>/<file>
		// owner/repo/releases/latest/download/<file>
		// owner/repo/releases/latest, owner/repo/releases/tag/<tag>
		if len(parts) < 4 || parts[2] != "releases" || !allowed(parts[0], parts[1]) {
			break
		}
		repo := parts[0] + "/" + parts[1]
		switch {
		case len(parts) == 6 && parts[3] == "download":
			return &ghMirrorUpstream{url: ghMirrorBases.github + "/" + path}, nil
		case len(parts) == 6 && parts[3] == "latest" && parts[4] == "download":
			return &ghMirrorUpstream{repo: repo, file: parts[5], latest: true}, nil
		case len(parts) == 4 && parts[3] == "latest":
			return &ghMirrorUpstream{repo: repo, tagPage: true}, nil
		case len(parts) == 5 && parts[3] == "tag":
			return &ghMirrorUpstream{repo: repo, file: parts[4], tagPage: true}, nil
		}
	case "api.github.com":
		// repos/owner/repo/releases[/latest | /tags/<tag>]
		if len(parts) < 4 || parts[0] != "repos" || parts[3] != "releases" || !allowed(parts[1], parts[2]) {
			break
		}
		if len(parts) == 4 || (len(parts) == 5 && parts[4] == "latest") || (len(parts) == 6 && parts[4] == "tags") {
			u := ghMirrorBases.api + "/" + path
			if rawQuery != "" {
				q, err := url.ParseQuery(rawQuery)
				if err != nil {
					break
				}
				u += "?" + q.Encode()
			}
			return &ghMirrorUpstream{url: u, fresh: ghMirrorFreshAPI}, nil
		}
	case "raw.githubusercontent.com":
		if len(parts) >= 4 && allowed(parts[0], parts[1]) {
			return &ghMirrorUpstream{url: ghMirrorBases.raw + "/" + path, fresh: ghMirrorFreshRaw}, nil
		}
	}
	return nil, fmt.Errorf("只能下载面板用到的 GitHub 仓库文件")
}

func (h *Handler) githubMirror(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	up, err := parseGHMirrorPath(strings.TrimPrefix(r.URL.Path, ghMirrorPath), r.URL.RawQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	if up.tagPage {
		tag := up.file
		if tag == "" {
			if tag, err = ghMirrorLatestTag(up.repo, ""); err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			// Point at the tag page, as GitHub does, so a client following
			// redirects ends on …/releases/tag/<tag>.
			http.Redirect(w, r, ghMirrorPath+"https://github.com/"+up.repo+"/releases/tag/"+tag, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, tag+"\n")
		return
	}
	if up.latest {
		tag, err := ghMirrorLatestTag(up.repo, up.file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		up.url = ghMirrorBases.github + "/" + up.repo + "/releases/download/" + tag + "/" + up.file
	}

	path, err := ghMirrorFetch(up)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "cache read failed", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "cache read failed", http.StatusInternalServerError)
		return
	}
	// Slow links need longer than the server's write timeout for a kernel.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(ghMirrorServeFor))
	if strings.HasPrefix(up.url, ghMirrorBases.api) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// ghMirrorLatestTag reads the latest release's tag from where GitHub
// redirects …/releases/latest (or, given a file, …/releases/latest/download/
// <file>); neither is rate limited like the API. Cached briefly.
func ghMirrorLatestTag(repo, file string) (string, error) {
	key := strings.ToLower(repo)
	ghMirrorTagMu.Lock()
	cached, ok := ghMirrorTags[key]
	ghMirrorTagMu.Unlock()
	if ok && time.Since(cached.at) < ghMirrorLatest {
		return cached.tag, nil
	}

	client := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	lookup := func(path, marker string) (string, error) {
		resp, err := client.Head(ghMirrorBases.github + "/" + repo + path)
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		_, rest, found := strings.Cut(resp.Header.Get("Location"), marker)
		tag, _, _ := strings.Cut(rest, "/")
		if !found || !ghMirrorSegment.MatchString(tag) {
			return "", fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
		}
		return tag, nil
	}
	tag, err := lookup("/releases/latest", "/releases/tag/")
	if err != nil && file != "" {
		tag, err = lookup("/releases/latest/download/"+file, "/releases/download/")
	}
	if err != nil {
		if ok {
			return cached.tag, nil
		}
		return "", fmt.Errorf("查询 %s 最新版本失败: %v", repo, err)
	}
	ghMirrorTagMu.Lock()
	ghMirrorTags[key] = ghMirrorTag{tag: tag, at: time.Now()}
	ghMirrorTagMu.Unlock()
	return tag, nil
}

func ghMirrorCacheDir() string {
	if ghMirrorDir != "" {
		return ghMirrorDir
	}
	if p := strings.TrimSpace(os.Getenv("DB_PATH")); p != "" {
		return filepath.Join(filepath.Dir(p), "github-cache")
	}
	return filepath.Join(os.TempDir(), "flyx-github-cache")
}

// ghMirrorFetch returns the cached file for up, downloading it when missing
// or stale. Concurrent requests for the same file share one download.
func ghMirrorFetch(up *ghMirrorUpstream) (string, error) {
	sum := sha256.Sum256([]byte(up.url))
	path := filepath.Join(ghMirrorCacheDir(), hex.EncodeToString(sum[:]))
	if st, err := os.Stat(path); err == nil && (up.fresh == 0 || time.Since(st.ModTime()) < up.fresh) {
		return path, nil
	}

	ghMirrorFlightMu.Lock()
	if f, ok := ghMirrorFlights[path]; ok {
		ghMirrorFlightMu.Unlock()
		<-f.done
		if f.err != nil {
			return "", f.err
		}
		return path, nil
	}
	f := &ghMirrorFlight{done: make(chan struct{})}
	ghMirrorFlights[path] = f
	ghMirrorFlightMu.Unlock()

	f.err = ghMirrorDownload(up.url, path)
	if f.err != nil {
		if _, err := os.Stat(path); err == nil {
			// GitHub unreachable: serve the stale copy, and count it as
			// fresh again so requests don't each wait on GitHub meanwhile.
			now := time.Now()
			_ = os.Chtimes(path, now, now)
			f.err = nil
		}
	}
	ghMirrorFlightMu.Lock()
	delete(ghMirrorFlights, path)
	ghMirrorFlightMu.Unlock()
	close(f.done)
	if f.err != nil {
		return "", f.err
	}
	return path, nil
}

func ghMirrorDownload(rawURL, path string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	if strings.HasPrefix(rawURL, ghMirrorBases.api) {
		req.Header.Set("Accept", "application/vnd.github+json")
		if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := ghMirrorHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("面板下载 GitHub 文件失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
	}
	if resp.ContentLength > ghMirrorMaxFile {
		return fmt.Errorf("文件过大")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, ghMirrorMaxFile+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("面板下载 GitHub 文件中断: %v", err)
	}
	if n > ghMirrorMaxFile {
		return fmt.Errorf("文件过大")
	}
	if resp.ContentLength >= 0 && n != resp.ContentLength {
		return fmt.Errorf("面板下载 GitHub 文件不完整")
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	ghMirrorPrune(dir, path)
	return nil
}

// ghMirrorPrune keeps the cache under ghMirrorMaxCache, dropping the files
// fetched longest ago (but never keep, the file just written).
func ghMirrorPrune(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		files = append(files, file{filepath.Join(dir, e.Name()), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if total <= ghMirrorMaxCache {
			return
		}
		if f.path == keep {
			continue
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// isPanelGithubMirror reports whether a 加速地址 is this panel's own mirror.
// The panel itself then downloads from GitHub directly, not through itself.
func isPanelGithubMirror(proxyURL string) bool {
	return strings.HasSuffix(strings.TrimRight(proxyURL, "/"), strings.TrimRight(ghMirrorPath, "/"))
}
