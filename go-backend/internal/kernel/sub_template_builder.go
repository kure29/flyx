package kernel

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

const builderMain = "节点选择"

// TemplateBuilder is the editable recipe, kept alongside its generated profile.
// Rules are evaluated in order; Final applies after every selected/custom rule.
type TemplateBuilder struct {
	// ServiceGroups gives each category its own selector (nil = on); false
	// routes rules straight to their targets.
	ServiceGroups *bool                  `json:"serviceGroups,omitempty"`
	Version       int                    `json:"version"`
	Preset        string                 `json:"preset"`
	Groups        []TemplateBuilderGroup `json:"groups"`
	Rules         []TemplateBuilderRule  `json:"rules"`
	Final         string                 `json:"final"`
	// Icons overrides group icons by category key, IconMain or IconFinal;
	// a missing key keeps the default.
	Icons map[string]string `json:"icons,omitempty"`
}

type TemplateBuilderGroup struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Filter string `json:"filter"`
	Icon   string `json:"icon,omitempty"`
}

type TemplateBuilderRule struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Target string `json:"target"`
}

type TemplateRuleCategory struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Icon    string   `json:"icon"` // image URL
	Target  string   `json:"target"`
	Presets []string `json:"presets"`
	set     ruleSet
}

// Group icons are Koolson/Qure PNGs (the set Surge and Quantumult X users
// know), served through jsDelivr. Mihomo and Stash read them from a group's
// icon field, Surge from icon-url; sing-box has no group icon.
const (
	qureIconURL = "https://cdn.jsdelivr.net/gh/Koolson/Qure@master/IconSet/Color/"
	// Qure has no Pixiv icon.
	pixivIconURL = "https://cdn.jsdelivr.net/gh/lige47/QuanX-icon-rule@master/icon/04ProxySoft/pixiv.png"
)

func qureIcon(name string) string { return qureIconURL + name + ".png" }

// Icon keys besides the category keys.
const (
	IconMain  = "__main"
	IconFinal = "__final"
	IconAuto  = "__auto" // default for url-test groups
)

// BuilderDefaultIcons are the default icons of the groups that are not
// categories.
func BuilderDefaultIcons() map[string]string {
	return map[string]string{IconMain: builderGroupIcons[IconMain], IconFinal: builderGroupIcons[IconFinal], IconAuto: builderGroupIcons[IconAuto]}
}

var builderGroupIcons = map[string]string{
	IconMain: qureIcon("Proxy"), IconFinal: qureIcon("Final"), IconAuto: qureIcon("Auto"),
	"private": qureIcon("WiFi"), "ads": qureIcon("Advertising"), "ai": qureIcon("AI"),
	"github": qureIcon("GitHub"), "youtube": qureIcon("YouTube"), "netflix": qureIcon("Netflix"),
	"spotify": qureIcon("Spotify"), "abema": qureIcon("AbemaTV"), "media": qureIcon("ForeignMedia"),
	"telegram": qureIcon("Telegram"), "google": qureIcon("Google_Search"), "microsoft": qureIcon("Microsoft"),
	"apple": qureIcon("Apple"), "bilibili": qureIcon("bilibili"), "games": qureIcon("Game"),
	"pixiv": pixivIconURL, "scholar": qureIcon("Scholar"), "cn": qureIcon("China"), "noncn": qureIcon("Global"),
}

// TemplateRuleCategories reuses our subscription rule sources, with a few
// smaller categories for users who want to choose services individually.
func TemplateRuleCategories() []TemplateRuleCategory {
	all := []TemplateRuleCategory{{Key: "private", Name: "私有网络", Target: "DIRECT",
		set: ruleSet{sites: []string{"private"}, ips: []string{"private"}, lists: []string{"Lan"}}}}
	for _, d := range ruleGroupDefs {
		target := builderMain
		if d.pick == pickDirect {
			target = "DIRECT"
		} else if d.pick == pickReject {
			target = "REJECT"
		}
		all = append(all, TemplateRuleCategory{Key: d.key, Name: d.name, Target: target, set: d.set})
	}
	for _, d := range []struct{ key, name, site, list, target string }{
		{"github", "GitHub", "github", "GitHub", builderMain},
		{"bilibili", "哔哩哔哩", "bilibili", "BiliBili", "DIRECT"},
		{"spotify", "Spotify", "spotify", "Spotify", builderMain},
		{"pixiv", "Pixiv", "pixiv", "Pixiv", builderMain},
		{"abema", "Abema", "abema", "Abema", builderMain},
		{"scholar", "教育资源", "category-scholar-!cn", "Scholar", builderMain},
	} {
		all = append(all, TemplateRuleCategory{Key: d.key, Name: d.name, Target: d.target,
			set: ruleSet{sites: []string{d.site}, lists: []string{d.list}}})
	}
	all = append(all,
		TemplateRuleCategory{Key: "cn", Name: "国内服务", Target: "DIRECT", set: ruleSet{sites: []string{"cn"}, ips: []string{"cn"}, lists: []string{"China", "ChinaIPs"}}},
		TemplateRuleCategory{Key: "noncn", Name: "非中国", Target: builderMain, set: ruleSet{sites: []string{"geolocation-!cn"}, lists: []string{"Global"}}},
	)
	for i := range all {
		all[i].Icon = builderGroupIcons[all[i].Key]
	}
	order := strings.Fields("private ads ai github youtube netflix spotify abema media telegram google microsoft apple bilibili games pixiv scholar cn noncn")
	rank := map[string]int{}
	for i, key := range order {
		rank[key] = i
	}
	sort.SliceStable(all, func(i, j int) bool { return rank[all[i].Key] < rank[all[j].Key] })
	minimal := toSet([]string{"private", "cn", "noncn"})
	balanced := toSet([]string{"private", "ai", "youtube", "google", "telegram", "github", "cn", "noncn"})
	for i := range all {
		all[i].Presets = []string{"full"}
		if minimal[all[i].Key] {
			all[i].Presets = append(all[i].Presets, "minimal")
		}
		if balanced[all[i].Key] {
			all[i].Presets = append(all[i].Presets, "balanced")
		}
	}
	return all
}

func builderCategories() map[string]TemplateRuleCategory {
	out := map[string]TemplateRuleCategory{}
	for _, c := range TemplateRuleCategories() {
		out[c.Key] = c
	}
	return out
}

func (b TemplateBuilder) validate() error {
	if b.Version != 1 {
		return fmt.Errorf("不支持的规则编辑器版本")
	}
	if b.Preset != "custom" && b.Preset != "minimal" && b.Preset != "balanced" && b.Preset != "full" {
		return fmt.Errorf("请选择规则预设")
	}
	if len(b.Groups) > 20 || len(b.Rules) > 500 {
		return fmt.Errorf("最多 20 个策略组和 500 条规则")
	}
	targets := map[string]bool{builderMain: true, "DIRECT": true, "REJECT": true}
	for _, g := range b.Groups {
		if g.Name == "" || g.Name != strings.TrimSpace(g.Name) || len([]rune(g.Name)) > 50 || strings.ContainsAny(g.Name, ",=\r\n[]{}#\"\\") || g.Name == "direct" || g.Name == "reject" || targets[g.Name] {
			return fmt.Errorf("策略组名称无效或重复：%s", g.Name)
		}
		if g.Type != "select" && g.Type != "url-test" {
			return fmt.Errorf("策略组仅支持手动选择或自动测速")
		}
		if len(g.Filter) > 200 || strings.ContainsAny(g.Filter, ",\r\n") {
			return fmt.Errorf("节点过滤条件最多 200 字节，不能包含逗号或换行")
		}
		if _, err := regexp.Compile(g.Filter); err != nil {
			return fmt.Errorf("策略组「%s」的正则表达式无效：%w", g.Name, err)
		}
		if g.Icon != "" && !ValidIconURL(g.Icon) {
			return fmt.Errorf("策略组「%s」的图标地址无效", g.Name)
		}
		targets[g.Name] = true
	}
	if !targets[b.Final] || b.Final == "REJECT" {
		return fmt.Errorf("兜底去向请选择节点选择、策略组或直连")
	}
	categories, seen := builderCategories(), map[string]bool{}
	if len(b.Icons) > 64 {
		return fmt.Errorf("图标设置过多")
	}
	for key, icon := range b.Icons {
		if _, ok := categories[key]; !ok && key != IconMain && key != IconFinal {
			return fmt.Errorf("未知的图标位置：%s", key)
		}
		if !ValidIconURL(icon) {
			return fmt.Errorf("图标地址无效：%s", icon)
		}
	}
	for i, r := range b.Rules {
		if !targets[r.Target] {
			return fmt.Errorf("第 %d 条规则的策略组不存在：%s", i+1, r.Target)
		}
		switch r.Type {
		case "category":
			if _, ok := categories[r.Value]; !ok || seen[r.Value] {
				return fmt.Errorf("规则分类不存在或重复：%s", r.Value)
			}
			seen[r.Value] = true
		case "domain", "domain_suffix":
			if !validBuilderDomain(r.Value) {
				return fmt.Errorf("第 %d 条规则请填写域名（不带协议、路径或通配符）", i+1)
			}
		case "domain_keyword":
			if r.Value == "" || len(r.Value) > 253 || strings.ContainsAny(r.Value, ",\r\n \t\"\\") {
				return fmt.Errorf("第 %d 条规则的域名关键词无效", i+1)
			}
		case "ip_cidr":
			if _, err := netip.ParsePrefix(r.Value); err != nil {
				return fmt.Errorf("第 %d 条规则请填写有效的 IPv4 / IPv6 CIDR", i+1)
			}
		default:
			return fmt.Errorf("不支持的规则类型：%s", r.Type)
		}
	}
	return nil
}

// ValidIconURL accepts an http(s) image address that fits in a Surge group
// line (comma-separated) and a YAML scalar.
func ValidIconURL(s string) bool {
	if len(s) > 500 || !(strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")) || len(s) <= len("https://") {
		return false
	}
	return !strings.ContainsAny(s, ", \t\r\n\"'\\`")
}

// groupIcon is a group's icon: the recipe's choice, else the default.
func (b TemplateBuilder) groupIcon(key string) string {
	if icon, ok := b.Icons[key]; ok && icon != "" {
		return icon
	}
	return builderGroupIcons[key]
}

// userGroupIcon is a node group's icon; url-test groups default to Auto.
func userGroupIcon(g TemplateBuilderGroup) string {
	if g.Icon != "" || g.Type != "url-test" {
		return g.Icon
	}
	return builderGroupIcons[IconAuto]
}

func validBuilderDomain(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// BuildSubscriptionTemplate always generates from a validated recipe. Native
// profiles remain the source used by existing subscription rendering.
func BuildSubscriptionTemplate(kind string, b TemplateBuilder) (string, error) {
	if err := b.validate(); err != nil {
		return "", err
	}
	var text string
	switch kind {
	case TemplateClash:
		text = buildClashTemplate(b)
	case TemplateSingbox:
		text = buildSingboxTemplate(b)
	case TemplateSurge:
		text = buildSurgeTemplate(b)
	default:
		return "", fmt.Errorf("不支持的模板格式 %s", kind)
	}
	if _, err := CheckTemplate(kind, text, nil); err != nil {
		return "", err
	}
	return text, nil
}

func buildClashTemplate(b TemplateBuilder) string {
	services := TemplateBuilderServiceGroups(TemplateClash, b)
	targets := builderServiceTargets(services)
	members := []string{}
	for _, g := range b.Groups {
		members = append(members, g.Name)
	}
	members = append(members, "DIRECT")
	groups := []interface{}{yamlNode{{"name", builderMain}, {"type", "select"}, {"include-all", true}, {"proxies", members}, {"icon", b.groupIcon(IconMain)}}}
	for _, g := range b.Groups {
		v := yamlNode{{"name", g.Name}, {"type", g.Type}, {"include-all", true}}
		if g.Filter != "" {
			v = append(v, yamlField{"filter", g.Filter})
		}
		if g.Type == "url-test" {
			v = append(v, yamlField{"url", "https://www.gstatic.com/generate_204"}, yamlField{"interval", 300})
		}
		if icon := userGroupIcon(g); icon != "" {
			v = append(v, yamlField{"icon", icon})
		}
		groups = append(groups, v)
	}
	for _, g := range services {
		v := yamlNode{{"name", g.Name}, {"type", "select"}, {"proxies", g.Members}, {"include-all", true}}
		if icon := b.groupIcon(g.Key); icon != "" {
			v = append(v, yamlField{"icon", icon})
		}
		groups = append(groups, v)
	}
	providers, rules := yamlNode{}, []string{}
	seen, categories := map[string]bool{}, builderCategories()
	provider := func(kind, name string) string {
		tag := ruleSetTag(kind, name)
		if !seen[tag] {
			seen[tag] = true
			behavior, dir := "domain", "geosite"
			if kind == "ip" {
				behavior, dir = "ipcidr", "geoip"
			}
			// YAML, not .mrs: Stash and Shadowrocket take Mihomo templates
			// too and only Mihomo reads .mrs.
			providers = append(providers, yamlField{tag, yamlNode{{"type", "http"}, {"behavior", behavior}, {"format", "yaml"},
				{"url", metaRuleURL("meta", dir, name, ".yaml")}, {"path", "./ruleset/" + tag + ".yaml"}, {"interval", 86400}, {"proxy", builderMain}}})
		}
		return tag
	}
	for _, r := range b.Rules {
		r = builderRoutedRule(r, targets)
		if r.Type == "category" {
			s := categories[r.Value].set
			for _, n := range s.sites {
				rules = append(rules, "RULE-SET,"+provider("site", n)+","+r.Target)
			}
			for _, n := range s.ips {
				rules = append(rules, "RULE-SET,"+provider("ip", n)+","+r.Target+",no-resolve")
			}
		} else {
			rules = append(rules, builderNativeRule(r))
		}
	}
	rules = append(rules, "MATCH,"+builderFinal(b, targets))
	return marshalYAML(yamlNode{{"mixed-port", 7890}, {"mode", "rule"}, {"ipv6", true},
		{"dns", yamlNode{{"enable", true}, {"ipv6", true}, {"enhanced-mode", "fake-ip"}, {"fake-ip-range", "198.18.0.1/16"},
			{"default-nameserver", []string{"223.5.5.5", "119.29.29.29"}}, {"proxy-server-nameserver", domesticDoH}, {"nameserver", domesticDoH}}},
		{"proxies", []interface{}{}}, {"proxy-groups", groups}, {"rule-providers", providers}, {"rules", rules}})
}

func buildSingboxTemplate(b TemplateBuilder) string {
	services := TemplateBuilderServiceGroups(TemplateSingbox, b)
	targets := builderServiceTargets(services)
	members := []string{}
	for _, g := range b.Groups {
		members = append(members, g.Name)
	}
	members = append(members, "{all}", "direct")
	outbounds := []interface{}{map[string]interface{}{"type": "selector", "tag": builderMain, "outbounds": members}}
	for _, g := range b.Groups {
		typ := "selector"
		if g.Type == "url-test" {
			typ = "urltest"
		}
		v := map[string]interface{}{"type": typ, "tag": g.Name, "outbounds": []string{"{all}"}}
		if g.Filter != "" {
			v["filter"] = []interface{}{map[string]interface{}{"action": "include", "keywords": []string{g.Filter}}}
		}
		if typ == "urltest" {
			v["url"], v["interval"] = "https://www.gstatic.com/generate_204", "5m"
		}
		outbounds = append(outbounds, v)
	}
	for _, g := range services {
		members := []string{}
		for _, m := range g.Members {
			members = append(members, ruleTarget(m, "direct"))
		}
		members = append(members, "{all}")
		outbounds = append(outbounds, map[string]interface{}{"type": "selector", "tag": g.Name, "outbounds": members})
	}
	outbounds = append(outbounds, map[string]interface{}{"type": "direct", "tag": "direct"})
	sets, rules := []interface{}{}, []interface{}{map[string]interface{}{"action": "sniff"}}
	seen, categories := map[string]bool{}, builderCategories()
	addSet := func(kind, dir, name string) string {
		tag := ruleSetTag(kind, name)
		if !seen[tag] {
			seen[tag] = true
			sets = append(sets, map[string]interface{}{"type": "remote", "tag": tag, "format": "binary",
				"url": metaRuleURL("sing", dir, name, ".srs"), "download_detour": builderMain, "update_interval": "1d"})
		}
		return tag
	}
	for _, r := range b.Rules {
		r = builderRoutedRule(r, targets)
		v := map[string]interface{}{}
		if r.Type == "category" {
			tags := []string{}
			s := categories[r.Value].set
			for _, n := range s.sites {
				tags = append(tags, addSet("site", "geosite", n))
			}
			for _, n := range s.ips {
				tags = append(tags, addSet("ip", "geoip", n))
			}
			v["rule_set"] = tags
		} else {
			v[r.Type] = []string{r.Value}
		}
		if r.Target == "REJECT" {
			v["action"] = "reject"
		} else {
			v["outbound"] = ruleTarget(r.Target, "direct")
		}
		rules = append(rules, v)
	}
	profile := map[string]interface{}{
		"log":          map[string]interface{}{"level": "warn"},
		"dns":          map[string]interface{}{"servers": []interface{}{map[string]interface{}{"type": "local", "tag": "dns-local"}}, "final": "dns-local"},
		"inbounds":     []interface{}{map[string]interface{}{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 2080}},
		"outbounds":    outbounds,
		"route":        map[string]interface{}{"rules": rules, "rule_set": sets, "final": ruleTarget(builderFinal(b, targets), "direct"), "auto_detect_interface": true, "default_domain_resolver": "dns-local"},
		"experimental": map[string]interface{}{"cache_file": map[string]interface{}{"enabled": true}},
	}
	text, _ := json.MarshalIndent(profile, "", "  ")
	return string(text) + "\n"
}

func builderNativeRule(r TemplateBuilderRule) string {
	typ := map[string]string{"domain": "DOMAIN", "domain_suffix": "DOMAIN-SUFFIX", "domain_keyword": "DOMAIN-KEYWORD", "ip_cidr": "IP-CIDR"}[r.Type]
	if r.Type == "ip_cidr" && strings.Contains(r.Value, ":") {
		typ = "IP-CIDR6"
	}
	line := typ + "," + r.Value + "," + r.Target
	if r.Type == "ip_cidr" {
		line += ",no-resolve"
	}
	return line
}

func buildSurgeTemplate(b TemplateBuilder) string {
	services := TemplateBuilderServiceGroups(TemplateSurge, b)
	targets := builderServiceTargets(services)
	var out strings.Builder
	out.WriteString("[General]\nloglevel = notify\n\n[Proxy]\n\n[Proxy Group]\n")
	members := []string{}
	for _, g := range b.Groups {
		members = append(members, g.Name)
	}
	members = append(members, "DIRECT", "include-all-proxies=1", "icon-url="+b.groupIcon(IconMain))
	out.WriteString(builderMain + " = select, " + strings.Join(members, ", ") + "\n")
	for _, g := range b.Groups {
		line := g.Name + " = " + g.Type + ", include-all-proxies=1"
		if g.Filter != "" {
			line += ", policy-regex-filter=" + g.Filter
		}
		if g.Type == "url-test" {
			line += ", url=https://www.gstatic.com/generate_204, interval=300"
		}
		if icon := userGroupIcon(g); icon != "" {
			line += ", icon-url=" + icon
		}
		out.WriteString(line + "\n")
	}
	for _, g := range services {
		line := g.Name + " = select, " + strings.Join(g.Members, ", ") + ", include-all-proxies=1"
		if icon := b.groupIcon(g.Key); icon != "" {
			line += ", icon-url=" + icon
		}
		out.WriteString(line + "\n")
	}
	out.WriteString("\n[Rule]\n")
	categories := builderCategories()
	for _, r := range b.Rules {
		r = builderRoutedRule(r, targets)
		if r.Type == "category" {
			for _, n := range categories[r.Value].set.lists {
				out.WriteString("RULE-SET," + listRuleURL("Surge", n, ".list") + "," + r.Target + "\n")
			}
		} else {
			out.WriteString(builderNativeRule(r) + "\n")
		}
	}
	out.WriteString("FINAL," + builderFinal(b, targets) + ",dns-failed\n")
	return out.String()
}
