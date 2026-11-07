package kernel

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	maxDownloadBytes = 256 << 20
	maxEntryBytes    = 256 << 20
	downloadTimeout  = 4 * time.Minute
)

var sha256HexPattern = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)

// parseChecksum extracts the first SHA-256 digest from a checksum file. It
// accepts `sha256sum` output as well as Xray's .dgst files, where the 64-hex
// token is the only one of that length (MD5/SHA1/SHA512 differ).
func parseChecksum(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "SHA2-512") || strings.Contains(upper, "SHA512") {
			continue
		}
		if m := sha256HexPattern.FindString(line); m != "" {
			return strings.ToLower(m), true
		}
	}
	return "", false
}

func resolveArch(d *Download) (string, error) {
	if len(d.ArchMap) == 0 {
		return runtime.GOARCH, nil
	}
	if v, ok := d.ArchMap[runtime.GOARCH]; ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("内核不支持当前架构 %s", runtime.GOARCH)
}

func fetch(ctx context.Context, url string, limit int64, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d (%s)", resp.StatusCode, url)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("下载中断: %v", err)
	}
	if n > limit {
		return fmt.Errorf("下载文件过大")
	}
	return nil
}

// githubAPIBase is a variable so tests can point it at a local server.
var githubAPIBase = "https://api.github.com"

type githubReleaseJSON struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		Digest             string `json:"digest"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// resolveGitHub fills in the download URL and digest of a release asset from
// the GitHub API (the asset "digest" field is "sha256:<hex>").
func resolveGitHub(ctx context.Context, g *GitHubRelease, arch string) (url, digest string, err error) {
	path := "latest"
	if g.Tag != "" {
		path = "tags/" + g.Tag
	}
	apiURL := fmt.Sprintf("%s/repos/%s/releases/%s", githubAPIBase, g.Repo, path)
	// The panel's own mirror also answers API requests (public proxies may
	// not), so nodes that can't reach GitHub look the release up there too.
	if p := strings.TrimSuffix(strings.TrimSpace(g.Proxy), "/"); strings.HasSuffix(p, "/api/v1/open_api/gh") {
		apiURL = p + "/" + apiURL
	}
	var body strings.Builder
	if err := fetch(ctx, apiURL, 4<<20, &body); err != nil {
		return "", "", fmt.Errorf("查询 GitHub 发布信息失败: %v", err)
	}
	var rel githubReleaseJSON
	if err := json.Unmarshal([]byte(body.String()), &rel); err != nil {
		return "", "", fmt.Errorf("解析 GitHub 发布信息失败: %v", err)
	}
	version := strings.TrimPrefix(rel.TagName, "v")
	name := strings.NewReplacer("{VERSION}", version, "{ARCH}", arch).Replace(g.Asset)
	for _, a := range rel.Assets {
		if a.Name != name {
			continue
		}
		digest = strings.ToLower(strings.TrimPrefix(a.Digest, "sha256:"))
		if !sha256HexPattern.MatchString(digest) || len(digest) != 64 {
			return "", "", fmt.Errorf("GitHub 未提供 %s 的校验值", name)
		}
		url = a.BrowserDownloadURL
		if p := strings.TrimSuffix(strings.TrimSpace(g.Proxy), "/"); p != "" {
			url = p + "/" + url
		}
		return url, digest, nil
	}
	return "", "", fmt.Errorf("发布 %s 中没有文件 %s", rel.TagName, name)
}

// install downloads, verifies and unpacks a binary into binDir. It refuses to
// install anything whose SHA-256 it cannot establish.
func install(d *Download, binDir string) error {
	arch, err := resolveArch(d)
	if err != nil {
		return err
	}
	url := strings.ReplaceAll(d.URL, "{ARCH}", arch)
	sumURL := strings.ReplaceAll(d.ChecksumURL, "{ARCH}", arch)
	want := strings.ToLower(d.SHA256[arch])

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	if d.GitHub != nil && (strings.Contains(url, "{VERSION}") || url == "" || (want == "" && sumURL == "")) {
		ghURL, digest, err := resolveGitHub(ctx, d.GitHub, arch)
		if err != nil {
			return err
		}
		url, want = ghURL, digest
	}
	if strings.Contains(url, "{VERSION}") || url == "" {
		return fmt.Errorf("下载地址不完整")
	}
	if want == "" && sumURL == "" {
		return fmt.Errorf("没有可用的 SHA256 校验值，拒绝安装")
	}

	if err := os.MkdirAll(filepath.Dir(binDir), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(binDir), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	hasher := sha256.New()
	if err := fetch(ctx, url, maxDownloadBytes, io.MultiWriter(tmp, hasher)); err != nil {
		return err
	}
	if want == "" {
		var sb strings.Builder
		if err := fetch(ctx, sumURL, 64<<10, &sb); err != nil {
			return fmt.Errorf("获取校验文件失败: %v", err)
		}
		var ok bool
		if want, ok = parseChecksum(sb.String()); !ok {
			return fmt.Errorf("校验文件中没有 SHA256")
		}
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != want {
		return fmt.Errorf("SHA256 校验失败: %s != %s", got, want)
	}

	staging := binDir + ".staging"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	switch d.Archive {
	case "zip":
		err = extractZip(tmp.Name(), staging)
	case "tar.gz":
		err = extractTarGz(tmp.Name(), staging)
	default:
		err = copyFile(tmp.Name(), filepath.Join(staging, d.BinName), 0o755)
	}
	if err != nil {
		return err
	}
	bin := filepath.Join(staging, d.BinName)
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		return fmt.Errorf("压缩包中没有找到 %s", d.BinName)
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		return err
	}
	_ = os.RemoveAll(binDir)
	return os.Rename(staging, binDir)
}

// entryName flattens archive paths ("sing-box-1.2-linux-amd64/sing-box") to
// their base name and rejects anything unsafe.
func entryName(name string) (string, bool) {
	base := filepath.Base(filepath.Clean("/" + name))
	if base == "/" || base == "." || !safeName(base) {
		return "", false
	}
	return base, true
}

func extractZip(src, dst string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("解压失败: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if !f.Mode().IsRegular() {
			continue
		}
		name, ok := entryName(f.Name)
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeLimited(filepath.Join(dst, name), rc, f.Mode().Perm()|0o600)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("解压失败: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("解压失败: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name, ok := entryName(hdr.Name)
		if !ok {
			continue
		}
		if err := writeLimited(filepath.Join(dst, name), tr, os.FileMode(hdr.Mode).Perm()|0o600); err != nil {
			return err
		}
	}
}

func writeLimited(path string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, maxEntryBytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxEntryBytes {
		err = fmt.Errorf("压缩包内文件过大")
	}
	return err
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeLimited(dst, in, mode)
}

// writeFileAtomic replaces path without leaving a half-written config behind.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
