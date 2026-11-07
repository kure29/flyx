package kernel

import (
	"encoding/json"
	"errors"
	"strings"
)

// Icon libraries use the icon subscription format of Quantumult X, Loon and
// BoxJs: {"name": "...", "icons": [{"name": "YouTube.png", "url": "..."}]}.

// BuiltinIconLibrary is offered without being added.
const BuiltinIconLibrary = "https://cdn.jsdelivr.net/gh/Koolson/Qure@master/Other/QureColor.json"

const maxLibraryIcons = 5000

type IconLibraryIcon struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type IconLibrary struct {
	Name  string            `json:"name"`
	Icons []IconLibraryIcon `json:"icons"`
}

// viaJsDelivr serves a raw.githubusercontent.com file from jsDelivr, which
// clients in mainland China reach far more often.
func viaJsDelivr(url string) string {
	rest, ok := strings.CutPrefix(url, "https://raw.githubusercontent.com/")
	if !ok {
		return url
	}
	rest = strings.Replace(rest, "/refs/heads/", "/", 1)
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 4 || parts[3] == "" || parts[2] == "refs" {
		return url
	}
	return "https://cdn.jsdelivr.net/gh/" + parts[0] + "/" + parts[1] + "@" + parts[2] + "/" + parts[3]
}

// ParseIconLibrary reads an icon subscription. Icons whose address cannot
// go into a client profile are left out.
func ParseIconLibrary(body string) (IconLibrary, error) {
	var raw struct {
		Name  string `json:"name"`
		Icons []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"icons"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(body, "\ufeff")), &raw); err != nil {
		return IconLibrary{}, errors.New("不是图标订阅 JSON（需要 name 和 icons 字段）")
	}
	lib := IconLibrary{Name: strings.TrimSpace(raw.Name), Icons: []IconLibraryIcon{}}
	if r := []rune(lib.Name); len(r) > 50 {
		lib.Name = string(r[:50])
	}
	seen := map[string]bool{}
	for _, ic := range raw.Icons {
		url := viaJsDelivr(strings.TrimSpace(ic.URL))
		if !ValidIconURL(url) || seen[url] {
			continue
		}
		seen[url] = true
		name := strings.TrimSpace(ic.Name)
		if name == "" {
			name = url[strings.LastIndex(url, "/")+1:]
		}
		lib.Icons = append(lib.Icons, IconLibraryIcon{Name: name, URL: url})
		if len(lib.Icons) == maxLibraryIcons {
			break
		}
	}
	if len(lib.Icons) == 0 {
		return IconLibrary{}, errors.New("图标订阅里没有可用的图标")
	}
	return lib, nil
}
