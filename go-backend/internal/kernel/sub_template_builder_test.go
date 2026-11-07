package kernel

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func builderFixture() TemplateBuilder {
	return TemplateBuilder{Version: 1, Preset: "custom", Final: builderMain,
		Groups: []TemplateBuilderGroup{{Name: "美国优选", Type: "url-test", Filter: "(?i)美国|US"}},
		Rules: []TemplateBuilderRule{
			{Type: "domain_suffix", Value: "example.com", Target: "DIRECT"},
			{Type: "ip_cidr", Value: "2001:db8::/32", Target: "REJECT"},
			{Type: "domain_keyword", Value: "special", Target: "美国优选"},
		},
	}
}

func TestTemplateBuilderAllFormats(t *testing.T) {
	b := builderFixture()
	disabled := false
	b.ServiceGroups = &disabled
	for _, c := range TemplateRuleCategories() {
		b.Rules = append(b.Rules, TemplateBuilderRule{Type: "category", Value: c.Key, Target: c.Target})
	}
	for _, kind := range TemplateFormats {
		t.Run(kind, func(t *testing.T) {
			content, err := BuildSubscriptionTemplate(kind, b)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := CheckTemplate(kind, content, []string{"美国 01", "香港 01"})
			if err != nil || len(summary.Groups) != 2 || strings.Join(summary.Groups[1].Nodes, ",") != "美国 01" {
				t.Fatalf("groups: %+v %v", summary, err)
			}
			body, _, err := Subscription(kind, namedEndpoints("美国 01", "香港 01"), SubOptions{Templates: map[string]string{kind: content}})
			if err != nil || !strings.Contains(string(body), "美国 01") {
				t.Fatalf("subscription: %s %v", body, err)
			}
			switch kind {
			case TemplateClash:
				var cfg struct {
					Rules     []string               `yaml:"rules"`
					Providers map[string]interface{} `yaml:"rule-providers"`
				}
				if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
					t.Fatal(err)
				}
				// Stash and Shadowrocket take Mihomo templates: no .mrs.
				if strings.Contains(content, ".mrs") || len(summary.Warnings) != 0 {
					t.Fatalf("Mihomo template must suit every Mihomo-format client: %v", summary.Warnings)
				}
				if cfg.Rules[0] != "DOMAIN-SUFFIX,example.com,DIRECT" || cfg.Rules[1] != "IP-CIDR6,2001:db8::/32,REJECT,no-resolve" || cfg.Rules[len(cfg.Rules)-1] != "MATCH,节点选择" {
					t.Fatal(cfg.Rules)
				}
			case TemplateSingbox:
				var cfg struct {
					Route struct {
						Rules []map[string]interface{} `json:"rules"`
						Final string                   `json:"final"`
					} `json:"route"`
				}
				if err := json.Unmarshal([]byte(body), &cfg); err != nil {
					t.Fatal(err)
				}
				// download_detour works from sing-box 1.12 (http_clients is 1.14+).
				if strings.Contains(content, "http_client") || !strings.Contains(content, `"download_detour"`) {
					t.Fatalf("rule sets must download on 1.12+: %s", content)
				}
				if cfg.Route.Rules[1]["outbound"] != "direct" || cfg.Route.Rules[2]["action"] != "reject" || cfg.Route.Final != builderMain || strings.Contains(string(body), "{all}") || strings.Contains(string(body), `"filter"`) {
					t.Fatalf("routing: %s", body)
				}
			case TemplateSurge:
				if !strings.Contains(content, "DOMAIN-SUFFIX,example.com,DIRECT\nIP-CIDR6,2001:db8::/32,REJECT,no-resolve") || !strings.HasSuffix(content, "FINAL,节点选择,dns-failed\n") {
					t.Fatal(content)
				}
			}
		})
	}
	// Empty selection is intentional: no dangling rule-set/DNS references.
	b.Rules, b.Groups, b.Final, b.Preset = nil, nil, "DIRECT", "custom"
	for _, kind := range TemplateFormats {
		text, err := BuildSubscriptionTemplate(kind, b)
		if err != nil || strings.Contains(text, "site-cn") {
			t.Fatalf("empty %s: %s %v", kind, text, err)
		}
	}
}

func TestTemplateBuilderRejectsInvalidRecipes(t *testing.T) {
	tests := map[string]func(*TemplateBuilder){
		"version": func(b *TemplateBuilder) { b.Version = 2 },
		"unknown category": func(b *TemplateBuilder) {
			b.Rules[0] = TemplateBuilderRule{Type: "category", Value: "typo", Target: "DIRECT"}
		},
		"missing target":   func(b *TemplateBuilder) { b.Rules[0].Target = "deleted group" },
		"invalid regex":    func(b *TemplateBuilder) { b.Groups[0].Filter = "[" },
		"duplicate group":  func(b *TemplateBuilder) { b.Groups = append(b.Groups, b.Groups[0]) },
		"reserved name":    func(b *TemplateBuilder) { b.Groups[0].Name = "direct" },
		"surge injection":  func(b *TemplateBuilder) { b.Groups[0].Name = "group\n[Rule]" },
		"domain injection": func(b *TemplateBuilder) { b.Rules[0].Value = "example.com,REJECT" },
		"domain URL":       func(b *TemplateBuilder) { b.Rules[0].Value = "https://example.com/path" },
		"invalid CIDR":     func(b *TemplateBuilder) { b.Rules[1].Value = "::/129" },
		"unsupported rule": func(b *TemplateBuilder) { b.Rules[1].Type = "raw" },
		"reject final":     func(b *TemplateBuilder) { b.Final = "REJECT" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			b := builderFixture()
			mutate(&b)
			for _, kind := range TemplateFormats {
				if _, err := BuildSubscriptionTemplate(kind, b); err == nil {
					t.Fatalf("accepted %s", kind)
				}
			}
		})
	}
}

func TestServiceSelectorsDefaultAndRouting(t *testing.T) {
	b := builderFixture()
	b.Rules = []TemplateBuilderRule{{Type: "category", Value: "ai", Target: builderMain}, {Type: "category", Value: "cn", Target: "DIRECT"}, {Type: "category", Value: "ads", Target: "REJECT"}}
	// Collision with a manually named node group must never change its references.
	b.Groups = append(b.Groups, TemplateBuilderGroup{Name: "AI 服务", Type: "select"})
	for _, kind := range TemplateFormats {
		content, err := BuildSubscriptionTemplate(kind, b)
		if err != nil {
			t.Fatal(err)
		}
		groups := TemplateBuilderServiceGroups(kind, b)
		summary, err := CheckTemplate(kind, content, []string{"US 01", "HK 01"})
		if err != nil {
			t.Fatal(err)
		}
		if len(summary.Groups) != len(groups)+3 {
			t.Fatalf("%s groups: %+v", kind, summary)
		}
		for _, g := range groups {
			if g.Members[0] != g.Target {
				t.Fatalf("lost default: %+v", g)
			}
			if g.Key == "ai" && g.Name == "AI 服务" {
				t.Fatal("name collision")
			}
			if !strings.Contains(content, g.Name) {
				t.Fatalf("missing group: %+v", g)
			}
		}
		body, _, err := Subscription(kind, namedEndpoints("US 01", "HK 01"), SubOptions{Templates: map[string]string{kind: content}})
		if err != nil {
			t.Fatal(err)
		}
		if kind == TemplateSingbox {
			var cfg struct {
				Route struct {
					Rules []map[string]interface{} `json:"rules"`
					Final string                   `json:"final"`
				} `json:"route"`
			}
			if err := json.Unmarshal([]byte(body), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.Route.Rules[1]["outbound"] == builderMain || cfg.Route.Rules[2]["outbound"] == "direct" || cfg.Route.Rules[3]["action"] != "reject" || cfg.Route.Final != "漏网之鱼" {
				t.Fatalf("routing: %s", body)
			}
		} else if kind == TemplateClash {
			var cfg struct {
				Rules []string `yaml:"rules"`
			}
			if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(cfg.Rules[0], ",AI 服务 · 分流 1") || cfg.Rules[len(cfg.Rules)-1] != "MATCH,漏网之鱼" {
				t.Fatal(cfg.Rules)
			}
		}
	}
}

func TestTemplateBuilderGroupIcons(t *testing.T) {
	for _, c := range TemplateRuleCategories() {
		if !strings.HasPrefix(c.Icon, "https://") {
			t.Fatalf("%s has no icon", c.Key)
		}
	}
	b := builderFixture()
	b.Groups = append(b.Groups, TemplateBuilderGroup{Name: "手动", Type: "select"}, TemplateBuilderGroup{Name: "日本", Type: "select", Icon: "https://example.com/jp.png"})
	b.Rules = append(b.Rules,
		TemplateBuilderRule{Type: "category", Value: "youtube", Target: builderMain},
		TemplateBuilderRule{Type: "category", Value: "google", Target: builderMain})
	b.Icons = map[string]string{"google": "https://example.com/g.png?size=64", IconFinal: "https://example.com/final.png"}
	youtube := builderGroupIcons["youtube"]
	for kind, want := range map[string][]string{
		TemplateClash: {`icon: "` + youtube + `"`, `icon: "` + builderGroupIcons[IconMain] + `"`, `icon: "https://example.com/final.png"`,
			`icon: "` + builderGroupIcons[IconAuto] + `"`, `icon: "https://example.com/g.png?size=64"`, `icon: "https://example.com/jp.png"`},
		TemplateSurge: {"YouTube = select, 节点选择, 美国优选, 手动, 日本, DIRECT, REJECT, include-all-proxies=1, icon-url=" + youtube + "\n",
			"icon-url=" + builderGroupIcons[IconAuto] + "\n", "icon-url=https://example.com/g.png?size=64\n", "icon-url=https://example.com/final.png\n",
			"手动 = select, include-all-proxies=1\n", "日本 = select, include-all-proxies=1, icon-url=https://example.com/jp.png\n"},
	} {
		content, err := BuildSubscriptionTemplate(kind, b)
		if err != nil {
			t.Fatal(err)
		}
		body, _, err := Subscription(kind, namedEndpoints("美国 01"), SubOptions{Templates: map[string]string{kind: content}})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(string(body), w) {
				t.Fatalf("%s: missing %q in\n%s", kind, w, body)
			}
		}
	}
	for _, bad := range []map[string]string{
		{"google": "https://example.com/a,b.png"},
		{"google": "javascript:alert(1)"},
		{"google": "https://example.com/a.png\nFINAL,DIRECT"},
		{"nope": "https://example.com/a.png"},
	} {
		b.Icons = bad
		if _, err := BuildSubscriptionTemplate(TemplateSurge, b); err == nil {
			t.Fatalf("accepted icons %v", bad)
		}
	}
	b.Icons = nil
	b.Groups[2].Icon = "https://example.com/a b.png"
	if _, err := BuildSubscriptionTemplate(TemplateClash, b); err == nil {
		t.Fatal("accepted a group icon with a space")
	}
}

func TestParseIconLibrary(t *testing.T) {
	lib, err := ParseIconLibrary("\ufeff" + `{"name":"Qure","icons":[{"name":"YouTube.png","url":"https://x.test/YouTube.png"},{"name":"bad","url":"https://x.test/a,b.png"},{"name":"","url":"https://x.test/Final.png"},{"name":"dup","url":"https://x.test/YouTube.png"}]}`)
	if err != nil || lib.Name != "Qure" || len(lib.Icons) != 2 || lib.Icons[1].Name != "Final.png" {
		t.Fatalf("%+v %v", lib, err)
	}
	lib, _ = ParseIconLibrary(`{"icons":[{"name":"a","url":"https://raw.githubusercontent.com/Koolson/Qure/master/IconSet/Color/Proxy.png"},{"name":"b","url":"https://raw.githubusercontent.com/o/r/refs/heads/main/b.png"}]}`)
	if lib.Icons[0].URL != builderGroupIcons[IconMain] || lib.Icons[1].URL != "https://cdn.jsdelivr.net/gh/o/r@main/b.png" {
		t.Fatalf("jsDelivr: %+v", lib.Icons)
	}
	for _, body := range []string{"not json", `{"name":"x","icons":[]}`, `{"icons":[{"url":"ftp://x"}]}`} {
		if _, err := ParseIconLibrary(body); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
