package kernel

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const clashTpl = `# 我的分流
mixed-port: 7890
dns:
  enable: true
  nameserver: [223.5.5.5]
proxies: []
proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies: [♻️ 自动选择, 🇭🇰 香港, DIRECT]
  - name: ♻️ 自动选择
    type: url-test
    include-all: true
    url: https://www.gstatic.com/generate_204
    interval: 300
  - name: 🇭🇰 香港
    type: url-test
    include-all: true
    filter: "(?i)港|hk"
    exclude-filter: "IPLC"
  - name: 香港 01
    type: select
    include-all-proxies: true
    filter: "(?<!美)国"
rule-providers:
  netflix: {type: http, behavior: domain, url: "https://example.com/netflix.yaml"}
rules:
  - RULE-SET,netflix,🇭🇰 香港
  - MATCH,🚀 节点选择 # 兜底
`

func TestCheckClashTemplate(t *testing.T) {
	s, err := CheckTemplate(TemplateClash, clashTpl, []string{"香港 01", "香港 IPLC", "HK 02", "美国 01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Groups) != 4 || s.AutoGroups() != 3 || s.Rules != 2 || s.Providers != 1 {
		t.Fatalf("summary: %+v", s)
	}
	if got := strings.Join(s.Groups[2].Nodes, ","); got != "香港 01,HK 02" {
		t.Fatalf("香港 group nodes = %s", got)
	}
	if len(s.Groups[1].Nodes) != 4 {
		t.Fatalf("include-all without a filter takes every node: %v", s.Groups[1].Nodes)
	}
	// Look-behind previews like Mihomo (regexp2): "美国" is left out.
	if s.Groups[3].FilterError != "" || s.Groups[3].Nodes == nil || len(s.Groups[3].Nodes) != 0 {
		t.Fatalf("look-behind filter: %+v", s.Groups[3])
	}
	s, err = CheckTemplate(TemplateClash, "proxy-groups:\n  - {name: a, type: select, include-all: true, filter: \"(港\"}\n", []string{"港"})
	if err != nil || s.Groups[0].FilterError == "" || s.Groups[0].Nodes != nil {
		t.Fatalf("unpreviewable filter: %+v %v", s.Groups, err)
	}

	for tpl, want := range map[string]string{
		"proxies: [a": "YAML 格式错误",
		"- a\n- b\n":  "YAML 对象",
		"rules: []\n": "没有 proxy-groups",
		"   ":         "空的",
	} {
		if _, err := CheckTemplate(TemplateClash, tpl, nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", tpl, err)
		}
	}
	s, err = CheckTemplate(TemplateClash, "proxy-groups:\n  - {name: 空, type: select}\n", nil)
	if err != nil || len(s.Warnings) != 2 {
		t.Fatalf("an empty group and no auto group are warned about: %+v %v", s, err)
	}
}

func TestSubscriptionClashTemplate(t *testing.T) {
	eps := namedEndpoints("香港 01", "HK IPLC", "美国 01")
	opts := SubOptions{Templates: map[string]string{TemplateClash: clashTpl}, Rules: true}
	body, ct, err := Subscription(FormatClash, eps, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ct, "text/yaml") {
		t.Fatalf("content type %s", ct)
	}
	var cfg struct {
		DNS     map[string]interface{}   `yaml:"dns"`
		Proxies []map[string]interface{} `yaml:"proxies"`
		Groups  []map[string]interface{} `yaml:"proxy-groups"`
		Rules   []string                 `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if len(cfg.Proxies) != 3 || cfg.DNS["enable"] != true || len(cfg.Groups) != 4 || len(cfg.Rules) != 2 {
		t.Fatalf("profile:\n%s", body)
	}
	// A node named like a template group is renamed; groups stay as written.
	if cfg.Proxies[0]["name"] != "香港 01 #2" || cfg.Groups[2]["filter"] != "(?i)港|hk" {
		t.Fatalf("names / groups:\n%s", body)
	}
	// Everything but the node list comes back exactly as written.
	tail := clashTpl[strings.Index(clashTpl, "proxy-groups:"):]
	if !strings.HasPrefix(body, clashTpl[:strings.Index(clashTpl, "proxies: []")]) || !strings.HasSuffix(body, tail) {
		t.Fatalf("template text changed:\n%s", body)
	}

	// A flow-style document still works (re-encoded).
	flow, _, err := Subscription(FormatClash, eps, SubOptions{Templates: map[string]string{
		TemplateClash: `{proxy-groups: [{name: 选择, type: select, include-all: true}], rules: ["MATCH,选择"]}`}})
	if err != nil || !strings.Contains(flow, "香港 01") || !strings.Contains(flow, "选择") {
		t.Fatalf("flow template: %v\n%s", err, flow)
	}
	// Stash without a template of its own takes the Mihomo one...
	stash, _, _ := Subscription(FormatStash, eps, opts)
	if !strings.Contains(stash, "🚀 节点选择") {
		t.Fatalf("Stash must take the Mihomo template:\n%s", stash)
	}
	// ...unless that uses .mrs rule sets, which Stash cannot read.
	mrs := opts
	mrs.Templates = map[string]string{TemplateClash: strings.Replace(clashTpl,
		`url: "https://example.com/netflix.yaml"`, `format: mrs, url: "https://example.com/netflix.mrs"`, 1)}
	if stash, _, _ := Subscription(FormatStash, eps, mrs); strings.Contains(stash, "🚀 节点选择") {
		t.Fatalf("Stash must not take an .mrs template:\n%s", stash)
	}
	// The Sing-box format has no Mihomo fallback.
	if sb, _, _ := Subscription(FormatSingbox, eps, opts); strings.Contains(sb, "🚀 节点选择") {
		t.Fatal("sing-box must not take the Mihomo template")
	}
}

func TestSubscriptionClashTemplateChain(t *testing.T) {
	opts := chainOpts
	opts.Templates = map[string]string{TemplateClash: "proxy-groups:\n  - {name: 选择, type: select, include-all: true}\nrules: [MATCH,选择]\n"}
	body, _, err := Subscription(FormatClash, chainEndpoints(), opts)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
		Groups  []map[string]interface{} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups) != 3 || cfg.Groups[1]["name"] != ChainLandingGroup || cfg.Proxies[0]["dialer-proxy"] != ChainRelayGroup {
		t.Fatalf("chain groups and dialer-proxy:\n%s", body)
	}
}

const singboxTpl = `{
  "dns": {"servers": [{"tag": "local", "type": "local"}]},
  "outbounds": [
    {"type": "selector", "tag": "节点选择", "outbounds": ["香港", "{all}"]},
    {"type": "urltest", "tag": "香港", "outbounds": ["{all}"],
     "filter": [{"action": "include", "keywords": ["港|HK"]}, {"action": "exclude", "keywords": ["IPLC"]}]},
    {"type": "urltest", "tag": "新加坡", "outbounds": ["{all}"], "filter": [{"action": "include", "keywords": ["新加坡"]}]},
    {"type": "direct", "tag": "direct"}
  ],
  "route": {"rules": [{"action": "sniff"}], "final": "节点选择", "rule_set": []}
}`

func TestSubscriptionSingboxTemplate(t *testing.T) {
	s, err := CheckTemplate(TemplateSingbox, singboxTpl, []string{"香港 01", "HK IPLC"})
	if err != nil || len(s.Groups) != 3 || s.AutoGroups() != 3 || strings.Join(s.Groups[1].Nodes, ",") != "香港 01" ||
		strings.Join(s.Groups[0].Proxies, ",") != "香港" || s.Rules != 1 {
		t.Fatalf("summary: %+v %v", s, err)
	}

	eps := namedEndpoints("香港 01", "HK IPLC", "美国 01")
	body, _, err := Subscription(FormatSingbox, eps, SubOptions{Templates: map[string]string{TemplateSingbox: singboxTpl}})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		DNS       map[string]interface{}   `json:"dns"`
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	tags := map[string]map[string]interface{}{}
	for _, o := range cfg.Outbounds {
		tags[o["tag"].(string)] = o
	}
	members := func(tag string) string {
		var out []string
		for _, m := range tags[tag]["outbounds"].([]interface{}) {
			out = append(out, m.(string))
		}
		return strings.Join(out, ",")
	}
	if members("节点选择") != "香港,香港 01,HK IPLC,美国 01" || members("香港") != "香港 01" {
		t.Fatalf("{all} expansion: %s / %s", members("节点选择"), members("香港"))
	}
	// An empty group would stop sing-box: it falls back to direct.
	if members("新加坡") != "direct" || tags["香港"]["filter"] != nil {
		t.Fatalf("empty group / filter left: %v", tags["新加坡"])
	}
	if len(cfg.Outbounds) != 7 || cfg.DNS == nil || tags["美国 01"]["type"] == nil {
		t.Fatalf("nodes appended, rest kept:\n%s", body)
	}
	if _, err := CheckTemplate(TemplateSingbox, `{"route": {}}`, nil); err == nil {
		t.Fatal("a template without outbounds is rejected")
	}
}

const surgeTpl = `[General]
loglevel = notify

[Proxy]
DIRECT-CN = direct

[Proxy Group]
节点选择 = select, 香港, DIRECT
香港 = url-test, include-all-proxies=1, policy-regex-filter=港|HK, interval=300

[Rule]
# 注释
DOMAIN-SUFFIX,cn,DIRECT
FINAL,节点选择
`

func TestSubscriptionSurgeTemplate(t *testing.T) {
	s, err := CheckTemplate(TemplateSurge, surgeTpl, []string{"香港 01", "美国 01"})
	if err != nil || len(s.Groups) != 2 || !s.Groups[1].Auto || strings.Join(s.Groups[1].Nodes, ",") != "香港 01" ||
		strings.Join(s.Groups[0].Proxies, ",") != "香港,DIRECT" || s.Rules != 2 {
		t.Fatalf("summary: %+v %v", s, err)
	}
	body, _, err := Subscription(FormatSurge, namedEndpoints("香港 01", "美国 01"), SubOptions{Templates: map[string]string{TemplateSurge: surgeTpl}})
	if err != nil {
		t.Fatal(err)
	}
	proxy := body[strings.Index(body, "[Proxy]"):strings.Index(body, "[Proxy Group]")]
	if !strings.HasPrefix(proxy, "[Proxy]\nDIRECT-CN = direct\n香港 01 = ") || !strings.Contains(proxy, "\n美国 01 = ") ||
		!strings.HasSuffix(proxy, "\n\n") || !strings.Contains(body, "FINAL,节点选择") {
		t.Fatalf("nodes go after the template's own proxies:\n%s", body)
	}

	// No [Proxy] section: one is added before [Proxy Group]; chain groups too.
	opts := chainOpts
	opts.Templates = map[string]string{TemplateSurge: "[Proxy Group]\n选择 = select, include-all-proxies=1\n"}
	body, _, err = Subscription(FormatSurge, chainEndpoints(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body, "[Proxy]\n美国 落地 01 = ") || !strings.Contains(body, "underlying-proxy="+ChainRelayGroup) ||
		!strings.Contains(body, "\n"+ChainLandingGroup+" = select, 美国 落地 01, 美国 落地 02\n") {
		t.Fatalf("surge without [Proxy]:\n%s", body)
	}
	if _, err := CheckTemplate(TemplateSurge, "[General]\n", nil); err == nil {
		t.Fatal("a template without [Proxy Group] is rejected")
	}
}

func TestSubscriptionStashAndShadowrocketTemplates(t *testing.T) {
	tpl := `# 共用模板
proxies: []
proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies: [🇭🇰 香港, DIRECT]
  - name: 🇭🇰 香港
    type: url-test
    include-all: true
    filter: "(?i)港|hk"
    url: https://www.gstatic.com/generate_204
    interval: 300 # 秒
  - name: 🇸🇬 新加坡
    type: select
    include-all-proxies: true
    filter: "新加坡"
rules:
  - MATCH,🚀 节点选择
`
	eps := namedEndpoints("香港 01", "HK 02", "美国 01")
	opts := SubOptions{Templates: map[string]string{TemplateStash: tpl, TemplateShadowrocket: tpl}}
	type profile struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
		Groups  []map[string]interface{} `yaml:"proxy-groups"`
		Rules   []string                 `yaml:"rules"`
	}
	for _, format := range []string{FormatStash, FormatShadowrocket} {
		body, ct, err := Subscription(format, eps, opts)
		if err != nil || !strings.HasPrefix(ct, "text/yaml") {
			t.Fatalf("%s: %v %s", format, err, ct)
		}
		var cfg profile
		if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
			t.Fatalf("%s: %v\n%s", format, err, body)
		}
		hk, sg := cfg.Groups[1], cfg.Groups[2]
		if fmt.Sprint(hk["proxies"]) != "[香港 01 HK 02]" || hk["include-all"] != nil || hk["filter"] != nil || hk["interval"] != 300 {
			t.Fatalf("%s: auto group written out:\n%s", format, body)
		}
		// Nothing matches: DIRECT keeps the group valid.
		if fmt.Sprint(sg["proxies"]) != "[DIRECT]" || len(cfg.Proxies) != 3 || len(cfg.Rules) != 1 {
			t.Fatalf("%s: empty group / rest:\n%s", format, body)
		}
		if !strings.HasPrefix(body, "# 共用模板\n") || !strings.Contains(body, "  - name: 🚀 节点选择\n    type: select\n") {
			t.Fatalf("%s: untouched parts stay as written:\n%s", format, body)
		}
	}
	// Mihomo keeps include-all for the client.
	opts.Templates[TemplateClash] = tpl
	if body, _, _ := Subscription(FormatClash, eps, opts); !strings.Contains(body, "include-all: true") {
		t.Fatalf("mihomo template changed:\n%s", body)
	}

	// Shadowrocket: chained copies named after their relay, no chain groups.
	chain := chainOpts
	chain.Templates = map[string]string{TemplateShadowrocket: "proxy-groups:\n  - {name: 选择, type: select, include-all: true}\nrules: [MATCH,选择]\n"}
	body, _, err := Subscription(FormatShadowrocket, chainEndpoints(), chain)
	if err != nil {
		t.Fatal(err)
	}
	var cfg profile
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups) != 1 || !strings.Contains(fmt.Sprint(cfg.Groups[0]["proxies"]), "美国 落地 01") || cfg.Proxies[0]["dialer-proxy"] != "香港 中转" {
		t.Fatalf("shadowrocket chain:\n%s", body)
	}
}

// Stash / Shadowrocket groups are written out the way Mihomo would fill
// them: anchors and merge keys, look-arounds and exclude-type included.
func TestStashTemplateExpandsLikeMihomo(t *testing.T) {
	tpl := `base: &base {type: select, include-all: true}
proxy-groups:
  - &hk {<<: *base, name: 香港, filter: "港"}
  - name: 其他
    type: select
    include-all: true
    filter: "^(?!.*(港|美)).*$"
  - {name: 非 SS, type: select, include-all: true, exclude-type: "Shadowsocks|Http"}
  - {<<: [*hk, *base], name: 香港备用}
rules:
  - MATCH,香港
`
	eps := namedEndpoints("香港 01", "美国 01", "德国 01")
	body, _, err := Subscription(FormatStash, eps, SubOptions{Templates: map[string]string{TemplateStash: tpl}})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Groups []map[string]interface{} `yaml:"proxy-groups"`
	}
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	want := map[string]string{"香港": "[香港 01]", "其他": "[德国 01]", "非 SS": "[DIRECT]", "香港备用": "[香港 01]"}
	if len(cfg.Groups) != len(want) {
		t.Fatalf("groups:\n%s", body)
	}
	for _, g := range cfg.Groups {
		name, _ := g["name"].(string)
		if got := fmt.Sprint(g["proxies"]); got != want[name] || g["include-all"] != nil || g["type"] != "select" {
			t.Errorf("%s: proxies %s, want %s\n%s", name, got, want[name], body)
		}
	}
}

func TestCheckTemplateWarnsAboutMRS(t *testing.T) {
	s, err := CheckTemplate(TemplateClash, `proxy-groups: [{name: a, type: select, include-all: true}]
rule-providers:
  cn: {type: http, behavior: domain, format: mrs, url: "https://example.com/cn.mrs"}
`, nil)
	if err != nil || len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "mrs") {
		t.Fatalf("mrs warning: %+v %v", s, err)
	}
}
