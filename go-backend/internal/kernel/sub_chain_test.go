package kernel

import (
	"encoding/json"
	"strings"
	"testing"
)

func chainEndpoints() []Endpoint {
	eps := namedEndpoints("美国 落地 01", "美国 落地 02", "香港 中转", "日本 直连")
	// A ShadowTLS landing node: only its shadowtls outbound dials.
	eps[1].Inbound.Security = Security{Type: SecurityShadowTLS, SNI: "www.example.com", ShadowTLSPassword: "stlspassword",
		Fingerprint: "chrome"}
	return eps
}

var chainOpts = SubOptions{ChainLanding: "落地", ChainRelay: "中转|落地"}

func TestSubscriptionChainClash(t *testing.T) {
	clash, _, err := Subscription(FormatClash, chainEndpoints(), chainOpts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(clash, `dialer-proxy: "中转节点"`) != 2 {
		t.Fatalf("both landing nodes dial through the relay group:\n%s", clash)
	}
	for _, want := range []string{
		"- name: \"落地节点\"\n    type: \"select\"\n    proxies:\n      - \"美国 落地 01\"\n      - \"美国 落地 02\"\n",
		// A landing node never relays for itself, though it matches both.
		"- name: \"中转节点\"\n    type: \"select\"\n    proxies:\n      - \"香港 中转\"\n",
		"- name: \"节点选择\"\n    type: \"select\"\n    proxies:\n      - \"落地节点\"\n      - \"自动选择\"\n",
	} {
		if !strings.Contains(clash, want) {
			t.Fatalf("missing %q:\n%s", want, clash)
		}
	}
}

func TestSubscriptionChainSingboxAndSurge(t *testing.T) {
	sb, _, err := Subscription(FormatSingbox, chainEndpoints(), chainOpts)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []map[string]interface{} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(sb), &cfg); err != nil {
		t.Fatal(err)
	}
	tags := map[string]map[string]interface{}{}
	for _, o := range cfg.Outbounds {
		tags[o["tag"].(string)] = o
	}
	if tags["美国 落地 01"]["detour"] != ChainRelayGroup || tags["香港 中转"]["detour"] != nil {
		t.Fatalf("detour: %v / %v", tags["美国 落地 01"], tags["香港 中转"])
	}
	if tags["美国 落地 02"]["detour"] != "美国 落地 02 (shadowtls)" || tags["美国 落地 02 (shadowtls)"]["detour"] != ChainRelayGroup {
		t.Fatalf("shadowtls pair must dial through the relay group from its outer outbound: %v", tags)
	}
	if g := tags[ChainRelayGroup]; g["type"] != "selector" || len(g["outbounds"].([]interface{})) != 1 {
		t.Fatalf("relay group: %v", g)
	}

	surge, _, _ := Subscription(FormatSurge, chainEndpoints(), chainOpts)
	if !strings.Contains(surge, "美国 落地 01 = ss, 203.0.113.1, 8388, encrypt-method=aes-256-gcm, password=pw, udp-relay=true, underlying-proxy=中转节点") ||
		!strings.Contains(surge, "中转节点 = select, 香港 中转") || !strings.Contains(surge, "节点选择 = select, 落地节点, ") {
		t.Fatalf("surge chain:\n%s", surge)
	}
}

func TestSubscriptionChainNeedsBothSides(t *testing.T) {
	for _, opts := range []SubOptions{{ChainLanding: "落地"}, {ChainLanding: "落地", ChainRelay: "不存在"}} {
		clash, _, _ := Subscription(FormatClash, chainEndpoints(), opts)
		if strings.Contains(clash, "dialer-proxy") || strings.Contains(clash, ChainLandingGroup) {
			t.Fatalf("no chain without relay nodes (%+v):\n%s", opts, clash)
		}
	}
	if err := (SubOptions{ChainRelay: "/[/"}).Validate(); err == nil || !strings.Contains(err.Error(), "中转节点") {
		t.Fatalf("bad relay expression: %v", err)
	}
	// Group names are reserved.
	clash, _, _ := Subscription(FormatClash, namedEndpoints(ChainRelayGroup), SubOptions{})
	if !strings.Contains(clash, ChainRelayGroup+" #2") {
		t.Fatalf("name clash with the relay group:\n%s", clash)
	}
}

func TestMarkChainClearsOneSidedMatch(t *testing.T) {
	for _, e := range ApplySubOptions(chainEndpoints(), SubOptions{ChainLanding: "落地", ChainRelay: "不存在"}) {
		if e.ChainRole != "" {
			t.Fatalf("one-sided chain must not tag %q", e.Name)
		}
	}
}

// An airport source relays for the panel's own nodes; 不用 and picked
// names override the source, and hidden relays stay out of every list.
func TestSubscriptionChainBySource(t *testing.T) {
	eps := namedEndpoints("自建 美国", "机场 香港", "机场 日本", "机场 新加坡")
	for i := 1; i < len(eps); i++ {
		eps[i].SourceID = 7
	}
	opts := SubOptions{ChainRelaySources: []int64{7}, ChainLandingOwn: true, ChainNoneNames: []string{"机场 日本"},
		ChainLandingNames: []string{"机场 新加坡"}, ChainHideRelays: true}
	marked := ApplySubOptions(eps, opts)
	roles := []string{}
	for _, e := range marked {
		roles = append(roles, e.ChainRole)
	}
	if strings.Join(roles, ",") != "landing,relay,,landing" {
		t.Fatalf("roles: %v", roles)
	}

	clash, _, err := Subscription(FormatClash, eps, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clash, "- name: \"中转节点\"\n    type: \"select\"\n    proxies:\n      - \"机场 香港\"\n") ||
		!strings.Contains(clash, "name: \"机场 香港\"") {
		t.Fatalf("the relay is a proxy and in the relay group:\n%s", clash)
	}
	surge, _, _ := Subscription(FormatSurge, eps, opts)
	sb, _, _ := Subscription(FormatSingbox, eps, opts)
	for format, body := range map[string]string{FormatClash: clash, FormatSurge: surge, FormatSingbox: sb} {
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, "机场 香港") && (strings.Contains(line, "节点选择") || strings.Contains(line, "自动选择")) {
				t.Fatalf("%s lists the hidden relay: %s", format, line)
			}
		}
	}
	var cfg struct {
		Outbounds []struct {
			Tag       string   `json:"tag"`
			Outbounds []string `json:"outbounds"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(sb), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, o := range cfg.Outbounds {
		if (o.Tag == "节点选择" || o.Tag == "自动选择") && strings.Contains(strings.Join(o.Outbounds, ","), "机场 香港") {
			t.Fatalf("sing-box lists the hidden relay: %v", o)
		}
	}

	// Templates whose groups take every node get the relays left out too.
	for kind, tpl := range map[string]string{
		TemplateClash:   "proxies: []\nproxy-groups:\n  - name: 全部\n    type: select\n    include-all: true\nrules:\n  - MATCH,全部\n",
		TemplateSurge:   "[General]\n\n[Proxy]\n\n[Proxy Group]\n全部 = select, DIRECT, include-all-proxies=1, icon-url=https://x.test/a.png\n香港 = select, include-all-proxies=1, policy-regex-filter=香港\n\n[Rule]\nFINAL,全部\n",
		TemplateSingbox: `{"outbounds":[{"type":"selector","tag":"全部","outbounds":["{all}"]},{"type":"direct","tag":"direct"}],"route":{"final":"全部"}}`,
	} {
		body, _, err := Subscription(kind, eps, SubOptions{Templates: map[string]string{kind: tpl}, ChainRelaySources: opts.ChainRelaySources,
			ChainLandingOwn: true, ChainNoneNames: opts.ChainNoneNames, ChainLandingNames: opts.ChainLandingNames, ChainHideRelays: true})
		if err != nil {
			t.Fatal(err)
		}
		switch kind {
		case TemplateClash:
			if !strings.Contains(body, "  - name: \"全部\"\n    type: \"select\"\n    proxies:\n      - \"自建 美国\"\n      - \"机场 日本\"\n      - \"机场 新加坡\"\n") {
				t.Fatalf("mihomo template:\n%s", body)
			}
		case TemplateSurge:
			if !strings.Contains(body, "全部 = select, DIRECT, 自建 美国, 机场 日本, 机场 新加坡, icon-url=https://x.test/a.png\n") ||
				!strings.Contains(body, "香港 = select, DIRECT\n") {
				t.Fatalf("surge template:\n%s", body)
			}
		case TemplateSingbox:
			var cfg struct {
				Outbounds []struct {
					Tag       string   `json:"tag"`
					Outbounds []string `json:"outbounds"`
				} `json:"outbounds"`
			}
			_ = json.Unmarshal([]byte(body), &cfg)
			if cfg.Outbounds[0].Tag != "全部" || strings.Join(cfg.Outbounds[0].Outbounds, ",") != "自建 美国,机场 日本,机场 新加坡" {
				t.Fatalf("sing-box template group: %+v", cfg.Outbounds[0])
			}
		}
	}
}

// Only an airport's matching nodes relay; with ChainDropUnused the rest
// of it is left out, while own nodes and per-node picks stay.
func TestSubscriptionChainSourceFilter(t *testing.T) {
	eps := namedEndpoints("自建 美国", "香港 IEPL 01 1.5x", "日本 IEPL 01", "美国 普通 01", "台湾 01")
	for i := 1; i < len(eps); i++ {
		eps[i].SourceID = 7
	}
	opts := SubOptions{ChainRelaySources: []int64{7}, ChainLandingOwn: true,
		ChainSourceFilters: map[int64]string{7: "香港|日本"}, ChainLandingNames: []string{"台湾 01"}}
	roles := func(list []Endpoint) string {
		out := []string{}
		for _, e := range list {
			out = append(out, e.Name+"="+e.ChainRole)
		}
		return strings.Join(out, ",")
	}
	if got := roles(ApplySubOptions(eps, opts)); got != "自建 美国=landing,香港 IEPL 01 1.5x=relay,日本 IEPL 01=relay,美国 普通 01=,台湾 01=landing" {
		t.Fatalf("filter: %s", got)
	}
	opts.ChainDropUnused = true
	if got := roles(ApplySubOptions(eps, opts)); got != "自建 美国=landing,香港 IEPL 01 1.5x=relay,日本 IEPL 01=relay,台湾 01=landing" {
		t.Fatalf("drop unused: %s", got)
	}
	opts.ChainSourceFilters = map[int64]string{7: "/(?i)iepl/"}
	if got := roles(ApplySubOptions(eps, opts)); got != "自建 美国=landing,香港 IEPL 01 1.5x=relay,日本 IEPL 01=relay,台湾 01=landing" {
		t.Fatalf("regex filter: %s", got)
	}
	// Without a chain (no relay matches) nothing is dropped.
	opts.ChainSourceFilters = map[int64]string{7: "新加坡"}
	opts.ChainLandingNames = nil
	if got := ApplySubOptions(eps, opts); len(got) != len(eps) {
		t.Fatalf("no chain, nothing dropped: %s", roles(got))
	}
	if err := (SubOptions{ChainSourceFilters: map[int64]string{7: "/(/"}}).Validate(); err == nil {
		t.Fatal("a bad filter must be rejected")
	}
}
