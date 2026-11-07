package kernel

import "strings"

// Routing rules in subscriptions (SubOptions.Rules): the profile gets
// policy groups per service, remote rule sets and, for Mihomo and sing-box,
// a DNS setup that keeps Chinese domains on domestic resolvers. Mihomo and
// sing-box fetch rule sets through the node selection group, so a client in
// China can still download them. Mihomo reads MetaCubeX .mrs files and
// sing-box (1.12+) .srs files; Surge, Loon, Quantumult X and Shadowrocket
// read blackmatrix7 lists. With rules, Loon / QX / Shadowrocket get a full
// configuration instead of a node list (see share_clients.go).

const (
	ruleDirectGroup = "全球直连"
	ruleFinalGroup  = "漏网之鱼"

	metaRulesURL = "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/"
	listRulesURL = "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/"
)

// What a rule group selects first.
const (
	pickProxy  = "proxy"
	pickUS     = "us" // the US region group, if any: AI services refuse HK
	pickDirect = "direct"
	pickReject = "reject"
)

// ruleSet names rule sets for every client: MetaCubeX geosite / geoip
// names and blackmatrix7 list names (the same in each client's directory).
type ruleSet struct {
	sites []string
	ips   []string
	lists []string
	// ipLists are blackmatrix7 IP lists for Loon and Quantumult X, whose
	// local GEOIP rule would run before the remote lists.
	ipLists []string
}

type ruleGroupDef struct {
	key, name string
	pick      string
	set       ruleSet
	// before routes these sets ahead of the group's own rules, e.g. the
	// Chinese Steam domains straight to DIRECT.
	before       ruleSet
	beforeTarget string
}

// ruleGroupDefs are the optional service groups, in rule order (a more
// specific set comes before one that contains it: YouTube before Google).
var ruleGroupDefs = []ruleGroupDef{
	{key: "ads", name: "广告拦截", pick: pickReject,
		set: ruleSet{sites: []string{"category-ads-all"}, lists: []string{"Advertising"}}},
	{key: "ai", name: "AI 服务", pick: pickUS,
		set: ruleSet{sites: []string{"openai", "anthropic", "google-gemini", "category-ai-!cn"}, lists: []string{"OpenAI", "Claude", "Gemini"}}},
	{key: "youtube", name: "YouTube", pick: pickProxy,
		set: ruleSet{sites: []string{"youtube"}, lists: []string{"YouTube"}}},
	{key: "netflix", name: "Netflix", pick: pickProxy,
		set: ruleSet{sites: []string{"netflix"}, ips: []string{"netflix"}, lists: []string{"Netflix"}}},
	{key: "media", name: "国外媒体", pick: pickProxy,
		set: ruleSet{sites: []string{"disney", "spotify", "tiktok"}, lists: []string{"Disney", "Spotify", "TikTok"}}},
	{key: "telegram", name: "Telegram", pick: pickProxy,
		set: ruleSet{sites: []string{"telegram"}, ips: []string{"telegram"}, lists: []string{"Telegram"}}},
	{key: "google", name: "Google", pick: pickProxy,
		set: ruleSet{sites: []string{"google"}, ips: []string{"google"}, lists: []string{"Google"}}},
	// GitHub belongs to Microsoft but is slow or blocked when direct.
	{key: "microsoft", name: "微软服务", pick: pickDirect,
		set:    ruleSet{sites: []string{"onedrive", "microsoft"}, lists: []string{"OneDrive", "Microsoft"}},
		before: ruleSet{sites: []string{"github"}, lists: []string{"GitHub"}}, beforeTarget: "节点选择"},
	{key: "apple", name: "苹果服务", pick: pickDirect,
		set: ruleSet{sites: []string{"apple"}, lists: []string{"Apple"}}},
	{key: "games", name: "游戏平台", pick: pickDirect,
		set:    ruleSet{sites: []string{"steam", "category-games"}, lists: []string{"Steam", "Game"}},
		before: ruleSet{sites: []string{"steam@cn", "category-games@cn"}, lists: []string{"SteamCN"}}, beforeTarget: "DIRECT"},
}

// RuleGroup is an optional service group, for the editor.
type RuleGroup struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// RuleGroups lists the optional service groups in display order.
func RuleGroups() []RuleGroup {
	out := make([]RuleGroup, len(ruleGroupDefs))
	for i, d := range ruleGroupDefs {
		out[i] = RuleGroup{Key: d.key, Name: d.name}
	}
	return out
}

// NormalizeRuleGroups keeps the known keys, in display order.
func NormalizeRuleGroups(keys []string) []string {
	want := toSet(keys)
	out := []string{}
	for _, d := range ruleGroupDefs {
		if want[d.key] {
			out = append(out, d.key)
		}
	}
	return out
}

func enabledRuleGroups(opts SubOptions) []ruleGroupDef {
	want := toSet(opts.RuleGroups)
	var out []ruleGroupDef
	for _, d := range ruleGroupDefs {
		if want[d.key] {
			out = append(out, d)
		}
	}
	return out
}

// ruleStep routes a rule set to a group ("DIRECT" is the client's direct
// policy).
type ruleStep struct {
	set    ruleSet
	target string
	// reject marks the ad step, which sing-box rejects outright: it has no
	// reject outbound to put in a group.
	reject bool
}

// ruleSteps lists the rules in order; the final rule (ruleFinalGroup) is
// left to each format.
func ruleSteps(opts SubOptions) []ruleStep {
	steps := []ruleStep{{set: ruleSet{sites: []string{"private"}, ips: []string{"private"}, lists: []string{"Lan"}}, target: ruleDirectGroup}}
	for _, d := range enabledRuleGroups(opts) {
		if d.beforeTarget != "" {
			steps = append(steps, ruleStep{set: d.before, target: d.beforeTarget})
		}
		steps = append(steps, ruleStep{set: d.set, target: d.name, reject: d.pick == pickReject})
	}
	return append(steps,
		ruleStep{set: ruleSet{sites: []string{"geolocation-!cn"}, lists: []string{"Global"}}, target: "节点选择"},
		ruleStep{set: ruleSet{sites: []string{"cn"}, ips: []string{"cn"}, lists: []string{"China"}, ipLists: []string{"ChinaIPs"}}, target: ruleDirectGroup})
}

// policyGroup is a select group in client-neutral form; "DIRECT" and
// "REJECT" are the client's built-in policies.
type policyGroup struct {
	name    string
	members []string
}

// rulePolicyGroups are the service groups plus the direct and final
// groups. regions are the region group names in the profile.
func rulePolicyGroups(opts SubOptions, regions []string) []policyGroup {
	proxies := append([]string{"节点选择", "自动选择"}, regions...)
	var out []policyGroup
	for _, d := range enabledRuleGroups(opts) {
		var members []string
		switch d.pick {
		case pickReject:
			members = []string{"REJECT", "DIRECT"}
		case pickDirect:
			members = append([]string{"DIRECT"}, proxies...)
		case pickUS:
			members = append(usFirst(proxies), "DIRECT")
		default:
			members = append(append([]string{}, proxies...), "DIRECT")
		}
		out = append(out, policyGroup{d.name, members})
	}
	return append(out,
		policyGroup{ruleDirectGroup, []string{"DIRECT", "节点选择"}},
		policyGroup{ruleFinalGroup, append(append([]string{}, proxies...), "DIRECT")})
}

// usGroup is the US region group's name (see regions).
const usGroup = "🇺🇸 美国"

// usFirst moves the US region group to the front.
func usFirst(names []string) []string {
	out := []string{}
	for _, n := range names {
		if n == usGroup {
			out = append(out, n)
		}
	}
	for _, n := range names {
		if n != usGroup {
			out = append(out, n)
		}
	}
	return out
}

// ruleGroupNames are every group name the rules can add, which node names
// must not take.
func ruleGroupNames() []string {
	out := []string{ruleDirectGroup, ruleFinalGroup}
	for _, d := range ruleGroupDefs {
		out = append(out, d.name)
	}
	return out
}

// ruleSetTag names a rule set in a profile: "site-geolocation-not-cn".
func ruleSetTag(kind, name string) string {
	return kind + "-" + strings.NewReplacer("-!", "-not-", "@", "-").Replace(name)
}

func metaRuleURL(branch, kind, name, ext string) string {
	return metaRulesURL + branch + "/geo/" + kind + "/" + name + ext
}

// listRuleURL is a blackmatrix7 list in a client's directory ("Surge",
// "Loon", "QuantumultX", "Clash").
func listRuleURL(dir, name, ext string) string {
	return listRulesURL + dir + "/" + name + "/" + name + ext
}

// ruleTarget maps a step target to a client's direct policy name.
func ruleTarget(target, direct string) string {
	if target == "DIRECT" {
		return direct
	}
	return target
}

// ---- Mihomo ----

var (
	domesticDoH = []string{"https://dns.alidns.com/dns-query", "https://doh.pub/dns-query"}
	foreignDoH  = []string{"https://dns.google/dns-query", "https://cloudflare-dns.com/dns-query"}
)

// clashRules returns the rule providers and rules for a Mihomo profile;
// yamlSets writes the rule sets as YAML for Stash, which cannot read .mrs.
func clashRules(opts SubOptions, yamlSets bool) (yamlNode, []interface{}) {
	format, ext := "mrs", ".mrs"
	if yamlSets {
		format, ext = "yaml", ".yaml"
	}
	providers := yamlNode{}
	seen := map[string]bool{}
	provider := func(kind, name string) string {
		tag := ruleSetTag(kind, name)
		if !seen[tag] {
			seen[tag] = true
			behavior, dir := "domain", "geosite"
			if kind == "ip" {
				behavior, dir = "ipcidr", "geoip"
			}
			providers = append(providers, yamlField{tag, yamlNode{
				{"type", "http"}, {"behavior", behavior}, {"format", format},
				{"url", metaRuleURL("meta", dir, name, ext)},
				{"path", "./ruleset/" + tag + ext}, {"interval", 86400}, {"proxy", "节点选择"},
			}})
		}
		return tag
	}
	var rules []interface{}
	for _, s := range ruleSteps(opts) {
		for _, n := range s.set.sites {
			rules = append(rules, "RULE-SET,"+provider("site", n)+","+s.target)
		}
		for _, n := range s.set.ips {
			rules = append(rules, "RULE-SET,"+provider("ip", n)+","+s.target+",no-resolve")
		}
	}
	return providers, append(rules, "MATCH,"+ruleFinalGroup)
}

// clashRuleSettings are the top-level Mihomo settings that go with the
// rules: fake-ip DNS that resolves Chinese domains domestically and the
// rest over DoH through the proxies, plus sniffing.
func clashRuleSettings() yamlNode {
	private, cn := ruleSetTag("site", "private"), ruleSetTag("site", "cn")
	return yamlNode{
		{"unified-delay", true},
		{"tcp-concurrent", true},
		{"dns", yamlNode{
			{"enable", true},
			{"ipv6", true},
			{"enhanced-mode", "fake-ip"},
			{"fake-ip-range", "198.18.0.1/16"},
			{"fake-ip-filter", []string{"rule-set:" + private + "," + cn, "+.lan", "+.local",
				"+.msftconnecttest.com", "+.msftncsi.com"}},
			// DNS queries follow the rules, so the foreign DoH servers are
			// reached through the proxies.
			{"respect-rules", true},
			{"default-nameserver", []string{"223.5.5.5", "119.29.29.29"}},
			{"proxy-server-nameserver", domesticDoH},
			{"nameserver", foreignDoH},
			{"nameserver-policy", yamlNode{{"rule-set:" + private + "," + cn, domesticDoH}}},
		}},
		{"sniffer", yamlNode{
			{"enable", true},
			{"parse-pure-ip", true},
			{"sniff", yamlNode{
				{"HTTP", yamlNode{{"ports", []interface{}{80, "8080-8880"}}, {"override-destination", true}}},
				{"TLS", yamlNode{{"ports", []interface{}{443, 8443}}}},
				{"QUIC", yamlNode{{"ports", []interface{}{443, 8443}}}},
			}},
		}},
	}
}

// ---- sing-box ----

// singboxRules returns the route rules (after sniff / hijack-dns), the
// remote rule sets and the DNS section for a sing-box 1.12+ profile.
func singboxRules(opts SubOptions) ([]interface{}, []interface{}, map[string]interface{}) {
	var sets []interface{}
	seen := map[string]bool{}
	ruleSetTags := func(s ruleSet) []string {
		var tags []string
		add := func(kind, dir, name string) {
			tag := ruleSetTag(kind, name)
			tags = append(tags, tag)
			if !seen[tag] {
				seen[tag] = true
				sets = append(sets, map[string]interface{}{"type": "remote", "tag": tag, "format": "binary",
					"url": metaRuleURL("sing", dir, name, ".srs"), "download_detour": "节点选择", "update_interval": "1d"})
			}
		}
		for _, n := range s.sites {
			add("site", "geosite", n)
		}
		for _, n := range s.ips {
			add("ip", "geoip", n)
		}
		return tags
	}
	rules := []interface{}{map[string]interface{}{"ip_is_private": true, "outbound": ruleDirectGroup}}
	for _, s := range ruleSteps(opts) {
		rule := map[string]interface{}{"rule_set": ruleSetTags(s.set)}
		if s.reject {
			rule["action"] = "reject"
		} else {
			rule["outbound"] = ruleTarget(s.target, "direct")
		}
		rules = append(rules, rule)
	}
	dns := map[string]interface{}{
		"servers": []interface{}{
			map[string]interface{}{"type": "https", "tag": "dns-remote", "server": "1.1.1.1", "detour": "节点选择"},
			map[string]interface{}{"type": "https", "tag": "dns-local", "server": "223.5.5.5"},
		},
		"rules": []interface{}{
			map[string]interface{}{"rule_set": []string{ruleSetTag("site", "private"), ruleSetTag("site", "cn")}, "server": "dns-local"},
		},
		"final": "dns-remote",
	}
	return rules, sets, dns
}

// ---- Surge ----

func surgeRules(opts SubOptions) []string {
	var lines []string
	for _, s := range ruleSteps(opts) {
		for _, n := range s.set.lists {
			lines = append(lines, "RULE-SET,"+listRuleURL("Surge", n, ".list")+","+s.target)
		}
	}
	return append(lines, "GEOIP,CN,"+ruleDirectGroup, "FINAL,"+ruleFinalGroup+",dns-failed")
}
