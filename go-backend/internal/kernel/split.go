package kernel

import (
	"fmt"
	"regexp"
	"strings"
)

// 分流 (split routing): rules that send part of an inbound's traffic
// somewhere other than its default exit (direct, or its chained outbound).
// Rules are tried in order; the first match wins.

const (
	SplitDirect = "direct"
	SplitBlock  = "block"
	SplitProxy  = "proxy"

	maxSplitRules   = 50
	maxSplitEntries = 1000
)

// SplitRule is one 分流 rule as stored on an inbound.
type SplitRule struct {
	// Categories are SplitCategories keys (geosite lists, China IPs, BT).
	Categories []string `json:"categories,omitempty"`
	// Domains match the domain and its subdomains.
	Domains []string `json:"domains,omitempty"`
	// IPs are CIDRs (bare IPs become /32 or /128).
	IPs    []string `json:"ips,omitempty"`
	Action string   `json:"action"`
	// OutboundID is the inbound traffic goes through for SplitProxy.
	OutboundID int64 `json:"outboundId,omitempty"`
	// Via is the resolved OutboundID, filled when building a config; a Via
	// without a host (deleted target) drops the traffic.
	Via *Endpoint `json:"-"`
}

// SplitCategory is a preset set of destinations.
type SplitCategory struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Group   string   `json:"group"`
	Geosite []string `json:"-"`
	GeoIP   []string `json:"-"`
	// Protocols are sniffed protocols (bittorrent).
	Protocols []string `json:"-"`
}

// SplitCategories lists the presets; the names exist both in the
// geosite.dat / geoip.dat shipped with Xray and in sing-box's rule sets.
var SplitCategories = []SplitCategory{
	{Key: "netflix", Label: "Netflix", Group: "流媒体", Geosite: []string{"netflix"}},
	{Key: "disney", Label: "Disney+", Group: "流媒体", Geosite: []string{"disney"}},
	{Key: "youtube", Label: "YouTube", Group: "流媒体", Geosite: []string{"youtube"}},
	{Key: "hbo", Label: "HBO", Group: "流媒体", Geosite: []string{"hbo"}},
	{Key: "primevideo", Label: "Prime Video", Group: "流媒体", Geosite: []string{"primevideo"}},
	{Key: "spotify", Label: "Spotify", Group: "流媒体", Geosite: []string{"spotify"}},
	{Key: "tiktok", Label: "TikTok", Group: "流媒体", Geosite: []string{"tiktok"}},
	{Key: "openai", Label: "ChatGPT", Group: "AI", Geosite: []string{"openai"}},
	{Key: "anthropic", Label: "Claude", Group: "AI", Geosite: []string{"anthropic"}},
	{Key: "gemini", Label: "Gemini", Group: "AI", Geosite: []string{"google-gemini"}},
	{Key: "google", Label: "Google", Group: "常用", Geosite: []string{"google"}},
	{Key: "telegram", Label: "Telegram", Group: "常用", Geosite: []string{"telegram"}},
	{Key: "ads", Label: "广告", Group: "其他", Geosite: []string{"category-ads-all"}},
	{Key: "bt", Label: "BT 下载", Group: "其他", Protocols: []string{"bittorrent"}},
	{Key: "cn", Label: "回国流量", Group: "其他", Geosite: []string{"cn"}, GeoIP: []string{"cn"}},
}

func splitCategory(key string) (SplitCategory, bool) {
	for _, c := range SplitCategories {
		if c.Key == key {
			return c, true
		}
	}
	return SplitCategory{}, false
}

var splitDomainPattern = regexp.MustCompile(`^([a-z0-9_]([a-z0-9_-]*[a-z0-9_])?\.)*[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?$`)

// NormalizeSplitRules validates rules and puts them in canonical form.
func NormalizeSplitRules(rules []SplitRule) ([]SplitRule, error) {
	if len(rules) > maxSplitRules {
		return nil, fmt.Errorf("分流规则最多 %d 条", maxSplitRules)
	}
	out := make([]SplitRule, 0, len(rules))
	entries := 0
	for i, r := range rules {
		n := i + 1
		var cats []string
		seen := map[string]bool{}
		for _, c := range r.Categories {
			c = strings.TrimSpace(c)
			if _, ok := splitCategory(c); !ok {
				return nil, fmt.Errorf("第 %d 条分流：未知分类 %s", n, c)
			}
			if !seen[c] {
				seen[c] = true
				cats = append(cats, c)
			}
		}
		var domains []string
		for _, d := range r.Domains {
			d = strings.ToLower(strings.Trim(strings.TrimSpace(d), "."))
			d = strings.TrimPrefix(d, "*.")
			if d == "" {
				continue
			}
			if len(d) > 253 || !splitDomainPattern.MatchString(d) {
				return nil, fmt.Errorf("第 %d 条分流：域名 %q 格式错误", n, d)
			}
			domains = append(domains, d)
		}
		var ips []string
		for _, ip := range r.IPs {
			if strings.TrimSpace(ip) == "" {
				continue
			}
			c, err := NormalizeAllowEntry(ip)
			if err != nil {
				return nil, fmt.Errorf("第 %d 条分流：%q 不是合法的 IP 或网段", n, strings.TrimSpace(ip))
			}
			ips = append(ips, c)
		}
		if len(cats)+len(domains)+len(ips) == 0 {
			return nil, fmt.Errorf("第 %d 条分流：请选择分类或填写域名、IP", n)
		}
		entries += len(domains) + len(ips)
		if entries > maxSplitEntries {
			return nil, fmt.Errorf("分流规则里的域名和 IP 最多 %d 条", maxSplitEntries)
		}
		rule := SplitRule{Categories: cats, Domains: domains, IPs: ips, Action: r.Action}
		switch r.Action {
		case SplitDirect, SplitBlock:
		case SplitProxy:
			if r.OutboundID <= 0 {
				return nil, fmt.Errorf("第 %d 条分流：请选择出站节点", n)
			}
			rule.OutboundID = r.OutboundID
		default:
			return nil, fmt.Errorf("第 %d 条分流：未知去向 %q", n, r.Action)
		}
		out = append(out, rule)
	}
	return out, nil
}

// routed reports whether the inbound needs routing rules of its own.
func (in Inbound) routed() bool { return in.Via != nil || len(in.Split) > 0 }

// splitMatch is one rule's destinations, resolved for a kernel.
type splitMatch struct {
	geosite, geoip, domains, ips, protocols []string
}

func (r SplitRule) match() splitMatch {
	m := splitMatch{domains: r.Domains, ips: r.IPs}
	for _, key := range r.Categories {
		c, _ := splitCategory(key)
		m.geosite = append(m.geosite, c.Geosite...)
		m.geoip = append(m.geoip, c.GeoIP...)
		m.protocols = append(m.protocols, c.Protocols...)
	}
	return m
}

// target is where a split rule sends traffic, as the action the kernel
// rules use.
func (r SplitRule) target() string {
	if r.Action == SplitProxy && (r.Via == nil || r.Via.Host == "") {
		return SplitBlock // target gone: drop rather than leak
	}
	return r.Action
}

func splitTag(in Inbound, i int) string { return fmt.Sprintf("via-%d-s%d", in.ID, i+1) }

func splitUserTag(in Inbound, i int, userID int64) string {
	return fmt.Sprintf("via-%d-s%d-u%d", in.ID, i+1, userID)
}

// ---- Xray ----

// xray splits a rule into Xray rule fragments: Xray ANDs the fields
// of one rule, so domains, IPs and protocols each get their own.
func (m splitMatch) xray() []obj {
	var out []obj
	var domains []string
	for _, g := range m.geosite {
		domains = append(domains, "geosite:"+g)
	}
	for _, d := range m.domains {
		domains = append(domains, "domain:"+d)
	}
	if len(domains) > 0 {
		out = append(out, obj{"domain": domains})
	}
	var ips []string
	for _, g := range m.geoip {
		ips = append(ips, "geoip:"+g)
	}
	ips = append(ips, m.ips...)
	if len(ips) > 0 {
		out = append(out, obj{"ip": ips})
	}
	if len(m.protocols) > 0 {
		out = append(out, obj{"protocol": m.protocols})
	}
	return out
}

// xraySplitRoutes returns the outbounds and rules for an inbound's split
// rules; they go before its default (chained) exit.
func xraySplitRoutes(in Inbound) ([]interface{}, []interface{}, error) {
	var outbounds, rules []interface{}
	tags := []string{in.Tag()}
	users := in.countedUsers()
	for i, r := range in.Split {
		ownerTag := "direct"
		userTag := userOutboundTag
		switch r.target() {
		case SplitBlock:
			ownerTag, userTag = "block", func(int64) string { return "block" }
		case SplitProxy:
			ownerTag = splitTag(in, i)
			ob, err := xrayChainOutbound(*r.Via, ownerTag)
			if err != nil {
				return nil, nil, fmt.Errorf("入站 %d 第 %d 条分流: %v", in.ID, i+1, err)
			}
			outbounds = append(outbounds, ob)
			for _, id := range users {
				ob, _ := xrayChainOutbound(*r.Via, splitUserTag(in, i, id))
				ob["streamSettings"].(obj)["sockopt"] = obj{"mark": UserMark(id)}
				outbounds = append(outbounds, ob)
			}
			i := i
			userTag = func(id int64) string { return splitUserTag(in, i, id) }
		}
		for _, m := range r.match().xray() {
			for _, id := range users {
				rule := obj{"type": "field", "inboundTag": tags, "user": xrayUserMatch(id), "outboundTag": userTag(id)}
				deepMerge(rule, m)
				rules = append(rules, rule)
			}
			rule := obj{"type": "field", "inboundTag": tags, "outboundTag": ownerTag}
			deepMerge(rule, m)
			rules = append(rules, rule)
		}
	}
	return outbounds, rules, nil
}

// ---- sing-box ----

// RuleSetURL is where sing-box downloads a rule set; mirror (the panel's
// GitHub proxy, "" for none) is prefixed like kernel downloads.
func RuleSetURL(tag, mirror string) string {
	repo := "sing-geosite"
	if strings.HasPrefix(tag, "geoip-") {
		repo = "sing-geoip"
	}
	u := "https://raw.githubusercontent.com/SagerNet/" + repo + "/rule-set/" + tag + ".srs"
	if mirror != "" {
		return strings.TrimSuffix(mirror, "/") + "/" + u
	}
	return u
}

// singbox splits a rule into sing-box rule fragments. sing-box ORs the
// destination fields within a rule, but rule sets are kept separate to stay
// clear of how they merge with inline fields.
func (m splitMatch) singbox() []obj {
	var out []obj
	var sets []string
	for _, g := range m.geosite {
		sets = append(sets, "geosite-"+g)
	}
	for _, g := range m.geoip {
		sets = append(sets, "geoip-"+g)
	}
	if len(sets) > 0 {
		out = append(out, obj{"rule_set": sets})
	}
	if len(m.domains) > 0 {
		out = append(out, obj{"domain_suffix": m.domains})
	}
	if len(m.ips) > 0 {
		out = append(out, obj{"ip_cidr": m.ips})
	}
	if len(m.protocols) > 0 {
		out = append(out, obj{"protocol": m.protocols})
	}
	return out
}

// singboxSplitRoutes is xraySplitRoutes for sing-box; it also returns the
// rule-set tags the rules use.
func singboxSplitRoutes(in Inbound, snell bool) ([]interface{}, []interface{}, []string, error) {
	var outbounds, rules []interface{}
	var sets []string
	tags := singboxInboundTags(in)
	users := in.countedUsers()
	for i, r := range in.Split {
		route := func(tag string) obj { return obj{"outbound": tag} }
		ownerRoute := route("direct")
		userRoute := func(id int64) obj { return route(userOutboundTag(id)) }
		switch r.target() {
		case SplitBlock:
			ownerRoute = obj{"action": "reject"}
			userRoute = func(int64) obj { return obj{"action": "reject"} }
		case SplitProxy:
			obs, err := singboxChainOutbounds(*r.Via, splitTag(in, i), 0, snell)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("入站 %d 第 %d 条分流: %v", in.ID, i+1, err)
			}
			outbounds = append(outbounds, obs...)
			for _, id := range users {
				obs, _ := singboxChainOutbounds(*r.Via, splitUserTag(in, i, id), UserMark(id), snell)
				outbounds = append(outbounds, obs...)
			}
			ownerRoute = route(splitTag(in, i))
			i := i
			userRoute = func(id int64) obj { return route(splitUserTag(in, i, id)) }
		}
		for _, m := range r.match().singbox() {
			if s, ok := m["rule_set"].([]string); ok {
				sets = append(sets, s...)
			}
			for _, id := range users {
				rule := obj{"inbound": tags, "auth_user": []string{User{ID: id}.Name()}}
				deepMerge(rule, m)
				deepMerge(rule, userRoute(id))
				rules = append(rules, rule)
			}
			rule := obj{"inbound": tags}
			deepMerge(rule, m)
			deepMerge(rule, ownerRoute)
			rules = append(rules, rule)
		}
	}
	return outbounds, rules, sets, nil
}

// splitRequiresSniff reports whether any rule matches by domain or
// protocol, which needs the connection sniffed first.
func (in Inbound) splitRequiresSniff() bool {
	for _, r := range in.Split {
		m := r.match()
		if len(m.geosite)+len(m.domains)+len(m.protocols) > 0 {
			return true
		}
	}
	return false
}

// GeoFile is a data file the agent keeps current next to a kernel binary.
type GeoFile struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	ChecksumURL string `json:"checksumUrl"`
}

// XrayGeoData are the geosite / geoip files Xray's 分流 reads, from the
// v2fly releases Xray bundles; the agent refreshes them daily so the
// categories stay current between kernel upgrades. mirror is the panel's
// GitHub proxy ("" for none).
func XrayGeoData(mirror string) []GeoFile {
	file := func(name, repo, asset string) GeoFile {
		u := "https://github.com/v2fly/" + repo + "/releases/latest/download/" + asset
		if mirror != "" {
			u = strings.TrimSuffix(mirror, "/") + "/" + u
		}
		return GeoFile{Name: name, URL: u, ChecksumURL: u + ".sha256sum"}
	}
	return []GeoFile{
		file("geosite.dat", "domain-list-community", "dlc.dat"),
		file("geoip.dat", "geoip", "geoip.dat"),
	}
}
