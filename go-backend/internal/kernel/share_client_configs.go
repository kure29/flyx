package kernel

import "strings"

// Full configurations for the iOS clients, served instead of their node
// lists when the subscription has rules (SubOptions.Rules). Their node
// lists cannot carry groups or rules; a configuration replaces the user's
// whole configuration in the client.
//
// Loon and Quantumult X get their own configuration formats with the
// nodes written in; Shadowrocket gets a Clash YAML configuration, which it
// documents as supported together with the nodes in it.

const testURL = "http://www.gstatic.com/generate_204"

// clientGroup is a policy group in client-neutral form: auto groups test
// latency, the others are picked by hand. "DIRECT" / "REJECT" are the
// client's built-in policies.
type clientGroup struct {
	name    string
	auto    bool
	members []string
}

// clientGroups are the node selection, auto, rule and region groups for
// the nodes (by their names in the configuration).
func clientGroups(names []string, opts SubOptions) []clientGroup {
	var regionNames []string
	var regionGroupsOut []clientGroup
	for _, g := range regionGroups(names) {
		regionNames = append(regionNames, g.name)
		regionGroupsOut = append(regionGroupsOut, clientGroup{g.name, true, g.members})
	}
	sel := append(append([]string{"自动选择"}, regionNames...), names...)
	auto := clientGroup{"自动选择", true, names}
	if len(names) == 0 {
		sel = []string{"DIRECT"}
		auto = clientGroup{"自动选择", false, []string{"DIRECT"}}
	}
	out := []clientGroup{{"节点选择", false, sel}, auto}
	for _, g := range rulePolicyGroups(opts, regionNames) {
		out = append(out, clientGroup{g.name, false, g.members})
	}
	return append(out, regionGroupsOut...)
}

// ---- Loon ----

func loonConfig(endpoints []Endpoint, opts SubOptions) string {
	var proxies, names []string
	for _, e := range endpoints {
		if p, ok := endpointProxy(e); ok {
			if line, ok := loonLine(p); ok {
				proxies = append(proxies, line)
				names = append(names, loonName(p))
			}
		}
	}
	lines := []string{
		"[General]",
		"ipv6 = true",
		"dns-server = system,223.5.5.5,119.29.29.29",
		"doh-server = " + strings.Join(domesticDoH, ","),
		"skip-proxy = 192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,localhost,*.local",
		"bypass-tun = 10.0.0.0/8,100.64.0.0/10,127.0.0.0/8,169.254.0.0/16,172.16.0.0/12,192.168.0.0/16,224.0.0.0/4,255.255.255.255/32",
		"proxy-test-url = " + testURL,
		"test-timeout = 3",
		"",
		"[Proxy]",
	}
	lines = append(lines, proxies...)
	lines = append(lines, "", "[Proxy Group]")
	for _, g := range clientGroups(names, opts) {
		if g.auto {
			lines = append(lines, g.name+" = url-test,"+strings.Join(g.members, ",")+",url = "+testURL+",interval = 600")
		} else {
			lines = append(lines, g.name+" = select,"+strings.Join(g.members, ","))
		}
	}
	// Local rules come before the remote ones, so only the final rule is
	// local; Chinese IPs come from a list instead of GEOIP.
	lines = append(lines, "", "[Rule]", "FINAL,"+ruleFinalGroup, "", "[Remote Rule]")
	for _, s := range ruleSteps(opts) {
		for _, n := range append(append([]string{}, s.set.lists...), s.set.ipLists...) {
			lines = append(lines, listRuleURL("Loon", n, ".list")+",policy="+s.target+",tag="+n+",enabled=true")
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// ---- Quantumult X ----

// qxPolicy maps a built-in policy to Quantumult X's lower-case name.
func qxPolicy(name string) string {
	switch name {
	case "DIRECT":
		return "direct"
	case "REJECT":
		return "reject"
	}
	return name
}

func qxConfig(endpoints []Endpoint, opts SubOptions) string {
	var servers, names []string
	for _, e := range endpoints {
		if p, ok := endpointProxy(e); ok {
			if line, ok := qxLine(p); ok {
				servers = append(servers, line)
				names = append(names, qxName(p))
			}
		}
	}
	lines := []string{
		"[general]",
		"server_check_url = " + testURL,
		"",
		"[dns]",
		"server = 223.5.5.5",
		"server = 119.29.29.29",
		"",
		"[policy]",
	}
	for _, g := range clientGroups(names, opts) {
		members := make([]string, len(g.members))
		for i, m := range g.members {
			members[i] = qxPolicy(m)
		}
		if g.auto {
			lines = append(lines, "url-latency-benchmark = "+g.name+", "+strings.Join(members, ", ")+
				", check-interval=600, alive-checking=false, tolerance=0")
		} else {
			lines = append(lines, "static = "+g.name+", "+strings.Join(members, ", "))
		}
	}
	lines = append(lines, "", "[server_remote]", "", "[filter_remote]")
	for _, s := range ruleSteps(opts) {
		for _, n := range append(append([]string{}, s.set.lists...), s.set.ipLists...) {
			lines = append(lines, listRuleURL("QuantumultX", n, ".list")+", tag="+n+", force-policy="+qxPolicy(s.target)+
				", update-interval=86400, opt-parser=false, enabled=true")
		}
	}
	lines = append(lines, "", "[rewrite_remote]", "", "[server_local]")
	lines = append(lines, servers...)
	// Local filters come before the remote ones, so only the final one is
	// local.
	lines = append(lines, "", "[filter_local]", "final, "+ruleFinalGroup, "", "[rewrite_local]", "", "[mitm]")
	return strings.Join(lines, "\n") + "\n"
}

// ---- Shadowrocket ----

// shadowrocketConfig is a Clash YAML configuration. Groups name the relay
// directly in chained copies (see shadowrocketProxies), so there are no
// chain groups.
func shadowrocketConfig(endpoints []Endpoint, opts SubOptions) string {
	proxies, names := shadowrocketProxies(endpoints)
	var groups []interface{}
	for _, g := range clientGroups(names, opts) {
		node := yamlNode{{"name", g.name}, {"type", "select"}}
		if g.auto {
			node = yamlNode{{"name", g.name}, {"type", "url-test"}, {"url", testURL}, {"interval", 600}}
		}
		groups = append(groups, append(node, yamlField{"proxies", g.members}))
	}
	providers := yamlNode{}
	var rules []interface{}
	for _, s := range ruleSteps(opts) {
		for _, n := range s.set.lists {
			tag := "list-" + strings.ToLower(n)
			providers = append(providers, yamlField{tag, yamlNode{
				{"type", "http"}, {"behavior", "classical"},
				{"url", listRuleURL("Clash", n, ".yaml")},
				{"path", "./ruleset/" + tag + ".yaml"}, {"interval", 86400},
			}})
			rules = append(rules, "RULE-SET,"+tag+","+s.target)
		}
	}
	rules = append(rules, "GEOIP,CN,"+ruleDirectGroup, "MATCH,"+ruleFinalGroup)
	return marshalYAML(yamlNode{
		{"mode", "rule"},
		{"proxies", proxies},
		{"proxy-groups", groups},
		{"rule-providers", providers},
		{"rules", rules},
	})
}
