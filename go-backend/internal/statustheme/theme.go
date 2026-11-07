// Package statustheme hosts the public status page's themes, after the way
// monitor (github.com/monitor-probe/monitor) hosts its public themes.
//
// A theme is a single-page app built against the panel's public status API
// and served at /status/. The built-in theme is embedded in the binary;
// others are installed from a theme.tar.gz into <data>/themes/<short>/:
//
//	theme.json    manifest (see Theme)
//	preview.png   optional thumbnail for the panel's theme list
//	dist/         the built app, dist/index.html at least
package statustheme

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// APIVersion is the version of the public status API this panel serves to
// themes. A theme declares the version it was built against in its manifest
// (flyxApi); a theme asking for a newer one is refused, and one already
// installed is not served, which keeps a panel downgrade from showing a
// broken page.
const APIVersion = 1

// DefaultShort names the built-in theme. No installed theme may take it.
const DefaultShort = "default"

const (
	manifestFile = "theme.json"
	previewFile  = "preview.png"
	distDir      = "dist"
	indexFile    = "index.html"

	maxManifest = 64 << 10
	maxShort    = 32
	maxText     = 200
)

//go:embed all:builtin
var builtinFS embed.FS

// Theme is a theme's manifest as the panel lists it.
type Theme struct {
	Name        string `json:"name"`
	Short       string `json:"short"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	// URL is the theme's source, https://github.com/<owner>/<repo>, whose
	// latest release the panel updates the theme from.
	URL string `json:"url"`
	// FlyxAPI is the public status API version the theme was built for.
	FlyxAPI int `json:"flyxApi"`
	// Config is the settings form the panel draws for the theme, passed
	// through unparsed (the same shape as monitor's theme config).
	Config json.RawMessage `json:"config,omitempty"`

	// Filled in by the panel, never taken from a manifest.
	Selected bool `json:"selected"`
	Builtin  bool `json:"builtin"`
	Preview  bool `json:"preview"`
}

// Error is a refusal worded for the admin; other errors are the machine's.
type Error string

func (e Error) Error() string { return string(e) }

func refuse(format string, args ...any) error {
	return Error(fmt.Sprintf(format, args...))
}

// ValidShort reports whether short may name a theme directory: lower-case
// letters, digits, '-' and '_', at most 32 bytes.
func ValidShort(short string) bool {
	if short == "" || len(short) > maxShort {
		return false
	}
	for _, c := range []byte(short) {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// parseManifest reads a manifest and checks what the panel relies on.
func parseManifest(data []byte) (Theme, error) {
	var t Theme
	if len(data) > maxManifest {
		return t, refuse("theme.json 超过 %d KiB", maxManifest>>10)
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return t, refuse("theme.json 格式不对：%v", err)
	}
	t.Selected, t.Builtin, t.Preview = false, false, false
	t.Name = strings.TrimSpace(t.Name)
	switch {
	case t.Name == "":
		return t, refuse("theme.json 缺少 name")
	case !ValidShort(t.Short):
		return t, refuse("theme.json 里的 short 只能用小写字母、数字、- 和 _，最长 %d 个字符：%q", maxShort, t.Short)
	case t.FlyxAPI < 1:
		return t, refuse("theme.json 缺少 flyxApi，这可能不是 FLYX 的主题（monitor 的主题不能直接用）")
	case t.FlyxAPI > APIVersion:
		return t, refuse("这个主题需要更新版本的面板（主题接口版本 %d，当前面板支持 %d），先升级面板", t.FlyxAPI, APIVersion)
	}
	for _, s := range []string{t.Name, t.Description, t.Version, t.Author, t.URL} {
		if !utf8.ValidString(s) || len(s) > maxText*4 {
			return t, refuse("theme.json 里的文字过长或不是 UTF-8")
		}
	}
	if len(t.Config) > 0 && !json.Valid(t.Config) {
		t.Config = nil
	}
	return t, nil
}

func builtinTheme() Theme {
	data, err := builtinFS.ReadFile("builtin/" + manifestFile)
	if err != nil {
		panic("statustheme: built-in manifest missing: " + err.Error())
	}
	t, err := parseManifest(data)
	if err != nil {
		panic("statustheme: built-in manifest invalid: " + err.Error())
	}
	t.Builtin = true
	_, err = fs.Stat(builtinFS, "builtin/"+previewFile)
	t.Preview = err == nil
	return t
}

// BuiltinDist is the built-in theme's app. It is empty in a
// build made without the frontend (`go build` straight from a checkout);
// Serve answers with a notice then.
func BuiltinDist() fs.FS {
	sub, err := fs.Sub(builtinFS, "builtin/"+distDir)
	if err != nil {
		panic(err)
	}
	return sub
}

// Store is the directory installed themes live in.
type Store struct {
	Dir string
}

// installed returns the manifest of an installed theme that can be served,
// or false.
func (s *Store) installed(short string) (Theme, bool) {
	if s == nil || s.Dir == "" || !ValidShort(short) || short == DefaultShort {
		return Theme{}, false
	}
	root, err := os.OpenRoot(filepath.Join(s.Dir, short))
	if err != nil {
		return Theme{}, false
	}
	defer root.Close()
	if st, err := root.Stat(distDir + "/" + indexFile); err != nil || !st.Mode().IsRegular() {
		return Theme{}, false
	}
	data, err := readCapped(root, manifestFile, maxManifest)
	if err != nil {
		return Theme{}, false
	}
	t, err := parseManifest(data)
	if err != nil || t.Short != short {
		return Theme{}, false
	}
	if st, err := root.Stat(previewFile); err == nil && st.Mode().IsRegular() && st.Size() <= maxFile {
		t.Preview = true
	}
	return t, true
}

func readCapped(root *os.Root, name string, max int64) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > max {
		return nil, errors.New("not a regular file or too large")
	}
	buf := make([]byte, st.Size())
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// List returns the built-in theme first, then the installed ones by name,
// with the one in use marked. selected is the saved choice; an empty or
// unusable one means the built-in theme.
func (s *Store) List(selected string) []Theme {
	list := []Theme{builtinTheme()}
	if s != nil && s.Dir != "" {
		if entries, err := os.ReadDir(s.Dir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if t, ok := s.installed(e.Name()); ok {
					list = append(list, t)
				}
			}
		}
	}
	rest := list[1:]
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	in := s.Effective(selected)
	for i := range list {
		list[i].Selected = list[i].Short == in
	}
	return list
}

// Effective returns the theme actually served for a saved choice.
func (s *Store) Effective(selected string) string {
	if _, ok := s.installed(selected); ok {
		return selected
	}
	return DefaultShort
}

// Find returns a theme by short, built-in included.
func (s *Store) Find(short string) (Theme, bool) {
	if short == DefaultShort {
		return builtinTheme(), true
	}
	return s.installed(short)
}

// Open returns the app to serve for a saved choice: the installed theme's
// dist directory, or the built-in theme. close must be called when done.
func (s *Store) Open(selected string) (fsys fs.FS, short string, closeFn func()) {
	if _, ok := s.installed(selected); ok {
		root, err := os.OpenRoot(filepath.Join(s.Dir, selected, distDir))
		if err == nil {
			return root.FS(), selected, func() { _ = root.Close() }
		}
	}
	return BuiltinDist(), DefaultShort, func() {}
}

// Preview returns a theme's thumbnail.
func (s *Store) Preview(short string) ([]byte, bool) {
	if short == DefaultShort {
		data, err := builtinFS.ReadFile("builtin/" + previewFile)
		return data, err == nil
	}
	if _, ok := s.installed(short); !ok {
		return nil, false
	}
	root, err := os.OpenRoot(filepath.Join(s.Dir, short))
	if err != nil {
		return nil, false
	}
	defer root.Close()
	data, err := readCapped(root, previewFile, maxFile)
	return data, err == nil
}

// Remove deletes an installed theme. The built-in one cannot be removed.
func (s *Store) Remove(short string) error {
	if short == DefaultShort {
		return refuse("内置主题不能删除")
	}
	if s == nil || s.Dir == "" || !ValidShort(short) {
		return refuse("没有这个主题")
	}
	dir := filepath.Join(s.Dir, short)
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return refuse("没有这个主题")
	}
	return os.RemoveAll(dir)
}
