package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
)

// Icon libraries for template group icons: icon subscriptions the admin
// adds by URL, kept in config. The panel downloads them so the editor can
// browse them; clients only get the chosen image addresses.

const (
	iconLibrariesConfig = "sub_icon_libraries"
	maxIconLibraries    = 20
	iconLibraryTTL      = 6 * time.Hour
)

type iconLibraryRef struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Builtin bool   `json:"builtin,omitempty"`
}

var iconLibraryCache = struct {
	sync.Mutex
	m map[string]iconLibraryEntry
}{m: map[string]iconLibraryEntry{}}

type iconLibraryEntry struct {
	lib kernel.IconLibrary
	at  time.Time
}

func (h *Handler) savedIconLibraries() []iconLibraryRef {
	out := []iconLibraryRef{}
	raw, err := h.repo.GetViteConfigValue(iconLibrariesConfig)
	if err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func (h *Handler) iconLibraries() []iconLibraryRef {
	return append([]iconLibraryRef{{Name: "Qure Color", URL: kernel.BuiltinIconLibrary, Builtin: true}}, h.savedIconLibraries()...)
}

// loadIconLibrary downloads a library, or returns it from the cache.
func (h *Handler) loadIconLibrary(r *http.Request, url string, fresh bool) (kernel.IconLibrary, error) {
	iconLibraryCache.Lock()
	e, ok := iconLibraryCache.m[url]
	iconLibraryCache.Unlock()
	if ok && !fresh && time.Since(e.at) < iconLibraryTTL {
		return e.lib, nil
	}
	body, _, err := h.fetchSubscriptionInfo(r.Context(), url, "")
	if err != nil {
		return kernel.IconLibrary{}, errText("下载图标库失败：" + err.Error())
	}
	lib, err := kernel.ParseIconLibrary(body)
	if err != nil {
		return kernel.IconLibrary{}, err
	}
	iconLibraryCache.Lock()
	iconLibraryCache.m[url] = iconLibraryEntry{lib, time.Now()}
	iconLibraryCache.Unlock()
	return lib, nil
}

func (h *Handler) subscriptionIconLibraryList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"libraries": h.iconLibraries(), "defaults": kernel.BuilderDefaultIcons(),
	}))
}

// subscriptionIconLibraryIcons lists an added library's icons.
func (h *Handler) subscriptionIconLibraryIcons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	for _, l := range h.iconLibraries() {
		if l.URL == req.URL {
			lib, err := h.loadIconLibrary(r, l.URL, false)
			if err != nil {
				response.WriteJSON(w, response.ErrDefault(err.Error()))
				return
			}
			response.WriteJSON(w, response.OK(lib))
			return
		}
	}
	response.WriteJSON(w, response.ErrDefault("图标库不存在"))
}

func (h *Handler) subscriptionIconLibrarySave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if !isSubscriptionURL(req.URL) || len(req.URL) > 1000 {
		response.WriteJSON(w, response.ErrDefault("请填写 http(s) 图标库地址"))
		return
	}
	saved := h.savedIconLibraries()
	for _, l := range h.iconLibraries() {
		if l.URL == req.URL {
			response.WriteJSON(w, response.ErrDefault("这个图标库已经添加过"))
			return
		}
	}
	if len(saved) >= maxIconLibraries {
		response.WriteJSON(w, response.ErrDefault("最多添加 20 个图标库"))
		return
	}
	lib, err := h.loadIconLibrary(r, req.URL, true)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	name := lib.Name
	if name == "" {
		name = "图标库 " + time.Now().Format("0102")
	}
	saved = append(saved, iconLibraryRef{Name: name, URL: req.URL})
	raw, _ := json.Marshal(saved)
	if err := h.repo.UpsertConfig(iconLibrariesConfig, string(raw), time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(h.iconLibraries()))
}

func (h *Handler) subscriptionIconLibraryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	kept := []iconLibraryRef{}
	for _, l := range h.savedIconLibraries() {
		if l.URL != req.URL {
			kept = append(kept, l)
		}
	}
	raw, _ := json.Marshal(kept)
	if err := h.repo.UpsertConfig(iconLibrariesConfig, string(raw), time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(h.iconLibraries()))
}
