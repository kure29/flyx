package kernel

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func allRuleGroupKeys() []string {
	var keys []string
	for _, g := range RuleGroups() {
		keys = append(keys, g.Key)
	}
	return keys
}

func TestSubscriptionRulesClash(t *testing.T) {
	eps := namedEndpoints("香港 01", "美国 01", "其他")
	body, _, err := Subscription(FormatClash, eps, SubOptions{Rules: true, RuleGroups: allRuleGroupKeys()})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Providers map[string]struct {
			Behavior string `yaml:"behavior"`
			URL      string `yaml:"url"`
			Proxy    string `yaml:"proxy"`
		} `yaml:"rule-providers"`
		Rules []string               `yaml:"rules"`
		DNS   map[string]interface{} `yaml:"dns"`
	}
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	groups := map[string][]string{"DIRECT": nil, "REJECT": nil}
	for _, g := range cfg.Groups {
		groups[g.Name] = g.Proxies
	}
	// Rules imply region groups; AI prefers the US one.
	if _, ok := groups["🇭🇰 香港"]; !ok {
		t.Fatalf("region groups missing:\n%s", body)
	}
	if ai := groups["AI 服务"]; len(ai) == 0 || ai[0] != "🇺🇸 美国" {
		t.Fatalf("AI group: %v", ai)
	}
	if ads := groups["广告拦截"]; len(ads) == 0 || ads[0] != "REJECT" {
		t.Fatalf("ads group: %v", ads)
	}
	for _, g := range cfg.Groups {
		for _, m := range g.Proxies {
			if _, ok := groups[m]; !ok && !strings.HasSuffix(m, " 01") && m != "其他" {
				t.Errorf("group %s references unknown %q", g.Name, m)
			}
		}
	}
	for _, r := range cfg.Rules {
		parts := strings.Split(r, ",")
		target := parts[len(parts)-1]
		if target == "no-resolve" {
			target = parts[len(parts)-2]
		}
		if _, ok := groups[target]; !ok {
			t.Errorf("rule %q targets unknown group", r)
		}
		if parts[0] == "RULE-SET" {
			p, ok := cfg.Providers[parts[1]]
			if !ok {
				t.Errorf("rule %q uses an undefined provider", r)
			}
			if !strings.HasPrefix(p.URL, "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/meta/geo/") ||
				!strings.HasSuffix(p.URL, ".mrs") || p.Proxy != "节点选择" {
				t.Errorf("provider %s: %+v", parts[1], p)
			}
		}
	}
	if last := cfg.Rules[len(cfg.Rules)-1]; last != "MATCH,漏网之鱼" {
		t.Fatalf("last rule %q", last)
	}
	if cfg.DNS["enhanced-mode"] != "fake-ip" || cfg.DNS["respect-rules"] != true {
		t.Fatalf("dns: %v", cfg.DNS)
	}
	if p := cfg.Providers["site-geolocation-not-cn"]; !strings.HasSuffix(p.URL, "/geosite/geolocation-!cn.mrs") {
		t.Fatalf("geolocation provider: %+v", p)
	}
	if p := cfg.Providers["ip-telegram"]; p.Behavior != "ipcidr" {
		t.Fatalf("ip provider: %+v", p)
	}
}

func TestSubscriptionRulesOnlyPickedGroups(t *testing.T) {
	eps := namedEndpoints("香港 01")
	body, _, _ := Subscription(FormatClash, eps, SubOptions{Rules: true, RuleGroups: []string{"telegram"}})
	if !strings.Contains(body, `name: "Telegram"`) || strings.Contains(body, "AI 服务") || strings.Contains(body, "广告拦截") {
		t.Fatalf("picked groups only:\n%s", body)
	}
	// Without a US region group the AI group starts at the node selection.
	body, _, _ = Subscription(FormatClash, eps, SubOptions{Rules: true, RuleGroups: []string{"ai"}})
	if !strings.Contains(body, "name: \"AI 服务\"\n    type: \"select\"\n    proxies:\n      - \"节点选择\"") {
		t.Fatalf("AI group without US nodes:\n%s", body)
	}
}

func TestSubscriptionRulesOffAndStash(t *testing.T) {
	eps := namedEndpoints("香港 01")
	plain, _, _ := Subscription(FormatClash, eps, SubOptions{})
	if strings.Contains(plain, "rule-providers") || strings.Contains(plain, "dns:") ||
		!strings.Contains(plain, "MATCH,节点选择") {
		t.Fatalf("rules off must keep the minimal profile:\n%s", plain)
	}
	if stash, _, _ := Subscription(FormatStash, eps, SubOptions{}); stash != plain {
		t.Fatalf("stash without rules must match clash:\n%s", stash)
	}

	// With rules Stash gets Mihomo's rules, read from YAML rule sets, and
	// none of the Mihomo-only DNS / sniffer settings.
	opts := SubOptions{Rules: true, RuleGroups: allRuleGroupKeys()}
	mihomo, _, _ := Subscription(FormatClash, eps, opts)
	stash, _, _ := Subscription(FormatStash, eps, opts)
	if strings.Contains(stash, "mrs") || strings.Contains(stash, "dns:") || strings.Contains(stash, "sniffer:") ||
		!strings.Contains(stash, `format: "yaml"`) || !strings.Contains(stash, "/geo/geosite/cn.yaml") {
		t.Fatalf("stash rule sets / settings:\n%s", stash)
	}
	rulesOf := func(body string) string { return body[strings.Index(body, "\nrules:"):] }
	if rulesOf(stash) != rulesOf(mihomo) || !strings.Contains(stash, "MATCH,漏网之鱼") {
		t.Fatalf("stash rules differ from mihomo:\n%s\n---\n%s", stash, mihomo)
	}
}

func TestSubscriptionRulesSingbox(t *testing.T) {
	eps := namedEndpoints("香港 01", "美国 01")
	body, _, err := Subscription(FormatSingbox, eps, SubOptions{Rules: true, RuleGroups: allRuleGroupKeys()})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []struct {
			Tag       string   `json:"tag"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbounds"`
		DNS struct {
			Servers []struct {
				Tag    string `json:"tag"`
				Detour string `json:"detour"`
			} `json:"servers"`
			Rules []struct {
				RuleSet []string `json:"rule_set"`
				Server  string   `json:"server"`
			} `json:"rules"`
			Final string `json:"final"`
		} `json:"dns"`
		Route struct {
			Rules []struct {
				RuleSet  []string `json:"rule_set"`
				Outbound string   `json:"outbound"`
				Action   string   `json:"action"`
			} `json:"rules"`
			RuleSet []struct {
				Tag    string `json:"tag"`
				URL    string `json:"url"`
				Detour string `json:"download_detour"`
			} `json:"rule_set"`
			Final    string `json:"final"`
			Resolver string `json:"default_domain_resolver"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	tags := map[string][]string{}
	for _, o := range cfg.Outbounds {
		tags[o.Tag] = o.Outbounds
	}
	if _, ok := tags["广告拦截"]; ok {
		t.Fatal("sing-box has no reject outbound for an ads group")
	}
	if tags["AI 服务"][0] != "🇺🇸 美国" || tags["苹果服务"][0] != "direct" {
		t.Fatalf("groups: %v", tags)
	}
	for tag, members := range tags {
		for _, m := range members {
			if _, ok := tags[m]; !ok {
				t.Errorf("%s references unknown outbound %q", tag, m)
			}
		}
	}
	sets := map[string]bool{}
	for _, s := range cfg.Route.RuleSet {
		sets[s.Tag] = true
		if !strings.HasPrefix(s.URL, "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/sing/geo/") ||
			!strings.HasSuffix(s.URL, ".srs") || s.Detour != "节点选择" {
			t.Errorf("rule set %+v", s)
		}
	}
	rejects := 0
	for _, r := range cfg.Route.Rules {
		for _, s := range r.RuleSet {
			if !sets[s] {
				t.Errorf("route rule uses undefined rule set %q", s)
			}
		}
		if r.Action == "reject" {
			rejects++
		} else if _, ok := tags[r.Outbound]; r.Outbound != "" && !ok {
			t.Errorf("route rule targets unknown outbound %q", r.Outbound)
		}
	}
	if rejects != 1 || cfg.Route.Final != "漏网之鱼" || cfg.Route.Resolver != "dns-local" {
		t.Fatalf("route: %+v", cfg.Route)
	}
	servers := map[string]bool{}
	for _, s := range cfg.DNS.Servers {
		servers[s.Tag] = true
		if _, ok := tags[s.Detour]; s.Detour != "" && !ok {
			t.Errorf("dns server detours through unknown %q", s.Detour)
		}
	}
	for _, r := range cfg.DNS.Rules {
		for _, s := range r.RuleSet {
			if !sets[s] {
				t.Errorf("dns rule uses undefined rule set %q", s)
			}
		}
	}
	if !servers[cfg.DNS.Final] || !servers[cfg.Route.Resolver] {
		t.Fatalf("dns: %+v", cfg.DNS)
	}
}

func TestSubscriptionRulesSurge(t *testing.T) {
	eps := namedEndpoints("香港 01", "美国 01")
	body, _, _ := Subscription(FormatSurge, eps, SubOptions{Rules: true, RuleGroups: allRuleGroupKeys()})
	groups := map[string]bool{"DIRECT": true, "REJECT": true}
	section := ""
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "["):
			section = line
		case section == "[Proxy Group]" && strings.Contains(line, " = "):
			groups[strings.SplitN(line, " = ", 2)[0]] = true
		}
	}
	section = ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[Rule]" || line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		target := parts[len(parts)-1]
		if parts[0] == "FINAL" {
			target = parts[1]
		}
		if !groups[target] {
			t.Errorf("rule %q targets unknown group", line)
		}
		if parts[0] == "RULE-SET" && !strings.HasPrefix(parts[1], "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Surge/") {
			t.Errorf("rule set url %q", parts[1])
		}
	}
	if !strings.Contains(body, "AI 服务 = select, 🇺🇸 美国, 节点选择") || !strings.Contains(body, "FINAL,漏网之鱼,dns-failed") {
		t.Fatalf("surge:\n%s", body)
	}
}

func TestRuleGroupNamesAreReserved(t *testing.T) {
	body, _, _ := Subscription(FormatClash, namedEndpoints("Telegram"), SubOptions{Rules: true, RuleGroups: []string{"telegram"}})
	if !strings.Contains(body, "Telegram #2") {
		t.Fatalf("a node named like a rule group must be renamed:\n%s", body)
	}
	if body, _, _ = Subscription(FormatClash, namedEndpoints("Telegram"), SubOptions{}); strings.Contains(body, "Telegram #2") {
		t.Fatalf("without rules the name stays:\n%s", body)
	}
	if NormalizeRuleGroups([]string{"games", "bogus", "ai"})[0] != "ai" {
		t.Fatal("normalize keeps display order")
	}
	if RegionOf("美国 01") != usGroup {
		t.Fatal("usGroup must match the US region group")
	}
}

// iniSection returns the non-empty lines of a config section.
func iniSection(body, section string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "[") {
			in = line == section
			continue
		}
		if in && line != "" {
			out = append(out, line)
		}
	}
	return out
}

func TestSubscriptionRulesLoon(t *testing.T) {
	eps := namedEndpoints("香港 01", "美国 01")
	opts := SubOptions{Rules: true, RuleGroups: allRuleGroupKeys()}
	body, _, _ := Subscription(FormatLoon, eps, opts)
	known := map[string]bool{"DIRECT": true, "REJECT": true}
	for _, line := range iniSection(body, "[Proxy]") {
		known[strings.SplitN(line, "=", 2)[0]] = true
	}
	groups := iniSection(body, "[Proxy Group]")
	for _, line := range groups {
		known[strings.SplitN(line, " = ", 2)[0]] = true
	}
	for _, line := range groups {
		for _, m := range strings.Split(strings.SplitN(line, " = ", 2)[1], ",")[1:] {
			if !known[m] && !strings.Contains(m, "=") {
				t.Errorf("group %q references unknown %q", line, m)
			}
		}
	}
	remote := iniSection(body, "[Remote Rule]")
	for _, line := range remote {
		policy := strings.TrimPrefix(strings.Split(line, ",")[1], "policy=")
		if !known[policy] || !strings.HasPrefix(line, "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Loon/") {
			t.Errorf("remote rule %q", line)
		}
	}
	if !strings.Contains(remote[len(remote)-1], "/ChinaIPs/") || iniSection(body, "[Rule]")[0] != "FINAL,漏网之鱼" ||
		!strings.Contains(body, "AI 服务 = select,🇺🇸 美国,") {
		t.Fatalf("loon:\n%s", body)
	}
	if plain, _, _ := Subscription(FormatLoon, eps, SubOptions{}); strings.Contains(plain, "[") {
		t.Fatalf("rules off must keep the node list:\n%s", plain)
	}
}

func TestSubscriptionRulesQX(t *testing.T) {
	eps := namedEndpoints("香港 01", "美国 01")
	body, _, _ := Subscription(FormatQX, eps, SubOptions{Rules: true, RuleGroups: allRuleGroupKeys()})
	known := map[string]bool{"direct": true, "reject": true}
	for _, line := range iniSection(body, "[server_local]") {
		known[line[strings.LastIndex(line, "tag=")+4:]] = true
	}
	policies := iniSection(body, "[policy]")
	for _, line := range policies {
		known[strings.Split(strings.SplitN(line, " = ", 2)[1], ", ")[0]] = true
	}
	for _, line := range policies {
		for _, m := range strings.Split(strings.SplitN(line, " = ", 2)[1], ", ")[1:] {
			if !known[m] && !strings.Contains(m, "=") {
				t.Errorf("policy %q references unknown %q", line, m)
			}
		}
	}
	for _, line := range iniSection(body, "[filter_remote]") {
		policy := ""
		for _, f := range strings.Split(line, ", ") {
			if strings.HasPrefix(f, "force-policy=") {
				policy = strings.TrimPrefix(f, "force-policy=")
			}
		}
		if !known[policy] || !strings.HasPrefix(line, "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/QuantumultX/") {
			t.Errorf("filter %q", line)
		}
	}
	if !strings.Contains(body, "static = 广告拦截, reject, direct") || iniSection(body, "[filter_local]")[0] != "final, 漏网之鱼" ||
		!strings.Contains(body, "/SteamCN/SteamCN.list, tag=SteamCN, force-policy=direct,") {
		t.Fatalf("qx:\n%s", body)
	}
}

func TestSubscriptionRulesShadowrocket(t *testing.T) {
	chain := SubOptions{Rules: true, RuleGroups: allRuleGroupKeys(), ChainLanding: "落地", ChainRelay: "中转"}
	body, _, _ := Subscription(FormatShadowrocket, chainEndpoints(), chain)
	var cfg struct {
		Proxies []struct {
			Name   string `yaml:"name"`
			Dialer string `yaml:"dialer-proxy"`
		} `yaml:"proxies"`
		Groups []struct {
			Name    string   `yaml:"name"`
			Proxies []string `yaml:"proxies"`
		} `yaml:"proxy-groups"`
		Providers map[string]struct {
			Behavior string `yaml:"behavior"`
			URL      string `yaml:"url"`
		} `yaml:"rule-providers"`
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	known := map[string]bool{"DIRECT": true, "REJECT": true}
	for _, p := range cfg.Proxies {
		known[p.Name] = true
	}
	for _, g := range cfg.Groups {
		known[g.Name] = true
	}
	for _, g := range cfg.Groups {
		for _, m := range g.Proxies {
			if !known[m] {
				t.Errorf("group %s references unknown %q", g.Name, m)
			}
		}
	}
	for _, r := range cfg.Rules {
		parts := strings.Split(r, ",")
		if !known[parts[len(parts)-1]] {
			t.Errorf("rule %q targets unknown group", r)
		}
		if p, ok := cfg.Providers[parts[1]]; parts[0] == "RULE-SET" && (!ok || p.Behavior != "classical" ||
			!strings.HasPrefix(p.URL, "https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/")) {
			t.Errorf("rule %q provider %+v", r, p)
		}
	}
	if known[ChainRelayGroup] || cfg.Rules[len(cfg.Rules)-1] != "MATCH,漏网之鱼" {
		t.Fatalf("shadowrocket:\n%s", body)
	}
	dialed := false
	for _, p := range cfg.Proxies {
		dialed = dialed || p.Dialer != ""
	}
	if !dialed {
		t.Fatalf("chained landing copies missing:\n%s", body)
	}
}
