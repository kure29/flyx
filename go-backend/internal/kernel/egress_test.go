package kernel

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestIPv4OnlyMixedInbounds(t *testing.T) {
	for _, kt := range []string{TypeXray, TypeSingbox} {
		t.Run(kt, func(t *testing.T) {
			in := Inbound{ID: 1, Protocol: ProtocolVLESS, Listen: "::", Port: 8388,
				Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"},
				Users:    chainUser, OutboundIPStrategy: OutboundIPv4Only}
			auto := in
			auto.ID, auto.Port, auto.OutboundIPStrategy = 2, 8389, OutboundIPAuto
			cfg := renderJSON(t, kt, []Inbound{in, auto})
			routeKey, obKey := "route", "outbound"
			if kt == TypeXray {
				routeKey, obKey = "routing", "outboundTag"
			}
			rules := cfg[routeKey].(map[string]interface{})["rules"].([]interface{})
			want := []string{"reject", "ipv4-in-1-flyx-u7", "ipv4-in-1-direct", "flyx-u7"}
			if kt == TypeXray {
				want[0] = "block"
			}
			if got := ruleOutbounds(rules, obKey); !reflect.DeepEqual(got, want) {
				t.Fatalf("scoped rules must precede shared user routing: %v", got)
			}
			first := rules[0].(map[string]interface{})
			if kt == TypeXray {
				if !reflect.DeepEqual(first["ip"], []interface{}{"::/0"}) {
					t.Fatalf("IPv6 destinations must be blocked: %v", first)
				}
			} else if first["ip_version"] != float64(6) {
				t.Fatalf("IPv6 destinations must be rejected: %v", first)
			}
			for _, tag := range []string{"ipv4-in-1-direct", "ipv4-in-1-flyx-u7"} {
				assertIPv4Dialer(t, kt, outboundByTag(cfg, tag), tag == "ipv4-in-1-flyx-u7")
			}
			// The IPv6 listener and the other inbound's shared dialers remain automatic.
			if cfg["inbounds"].([]interface{})[0].(map[string]interface{})["listen"] != "::" {
				t.Fatal("IPv4 egress must preserve the IPv6 listener")
			}
			shared := outboundByTag(cfg, "flyx-u7")
			if shared["domain_resolver"] != nil || shared["sendThrough"] != nil || shared["inet4_bind_address"] != nil {
				t.Fatalf("shared outbound was modified: %v", shared)
			}
			if kt == TypeSingbox {
				dns := cfg["dns"].(map[string]interface{})["servers"].([]interface{})[0].(map[string]interface{})
				if dns["tag"] != ipv4ResolverTag || dns["type"] != "local" {
					t.Fatalf("missing system resolver: %v", dns)
				}
			}
		})
	}
}

func assertIPv4Dialer(t *testing.T, kt string, ob map[string]interface{}, counted bool) {
	assertIPDialer(t, kt, ob, counted, OutboundIPv4Only)
}

func assertIPDialer(t *testing.T, kt string, ob map[string]interface{}, counted bool, policy string) {
	t.Helper()
	bind, sockStrategy, bindKey, resolverTag := "0.0.0.0", "ForceIPv4", "inet4_bind_address", ipv4ResolverTag
	if policy == OutboundIPv6Only {
		bind, sockStrategy, bindKey, resolverTag = "::", "ForceIPv6", "inet6_bind_address", ipv6ResolverTag
	}
	if ob == nil {
		t.Fatal("missing restricted outbound")
	}
	if kt == TypeXray {
		sock := ob["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{})
		if ob["sendThrough"] != bind || sock["domainStrategy"] != sockStrategy {
			t.Fatalf("Xray must enforce %s without fallback: %v", policy, ob)
		}
		if ob["protocol"] == "freedom" && ob["settings"].(map[string]interface{})["domainStrategy"] != sockStrategy {
			t.Fatalf("freedom UDP packet writer must enforce %s: %v", policy, ob)
		}
		if counted && sock["mark"] != float64(UserMark(7)) {
			t.Fatalf("lost accounting socket mark: %v", ob)
		}
	} else {
		resolver := ob["domain_resolver"].(map[string]interface{})
		if ob[bindKey] != bind || resolver["strategy"] != policy || resolver["server"] != resolverTag {
			t.Fatalf("sing-box must resolve/dial %s: %v", policy, ob)
		}
		if counted && ob["routing_mark"] != float64(UserMark(7)) {
			t.Fatalf("lost accounting socket mark: %v", ob)
		}
	}
}

func TestIPv6OnlyAlongsideIPv4AndAutomatic(t *testing.T) {
	for _, kt := range []string{TypeXray, TypeSingbox} {
		t.Run(kt, func(t *testing.T) {
			v6 := Inbound{ID: 1, Protocol: ProtocolVLESS, Listen: "0.0.0.0", Port: 8388,
				Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"},
				Users:    chainUser, OutboundIPStrategy: OutboundIPv6Only}
			v4, auto := v6, v6
			v4.ID, v4.Port, v4.OutboundIPStrategy = 2, 8389, OutboundIPv4Only
			auto.ID, auto.Port, auto.OutboundIPStrategy = 3, 8390, OutboundIPAuto
			cfg := renderJSON(t, kt, []Inbound{v6, v4, auto})
			if cfg["inbounds"].([]interface{})[0].(map[string]interface{})["listen"] != "0.0.0.0" {
				t.Fatal("IPv6 egress must preserve the IPv4 listener")
			}
			routeKey, obKey, inKey := "route", "outbound", "inbound"
			if kt == TypeXray {
				routeKey, obKey, inKey = "routing", "outboundTag", "inboundTag"
			}
			rules := cfg[routeKey].(map[string]interface{})["rules"].([]interface{})
			// Each strict inbound rejects the other family right before
			// its direct rules.
			want := []string{"reject", "ipv6-in-1-flyx-u7", "ipv6-in-1-direct", "reject", "ipv4-in-2-flyx-u7", "ipv4-in-2-direct", "flyx-u7"}
			if kt == TypeXray {
				want[0], want[3] = "block", "block"
			}
			if got := ruleOutbounds(rules, obKey); !reflect.DeepEqual(got, want) {
				t.Fatalf("mixed policies must stay scoped ahead of user-wide routing: %v", got)
			}
			first := rules[0].(map[string]interface{})
			if !reflect.DeepEqual(first[inKey], []interface{}{"in-1"}) {
				t.Fatalf("IPv4 rejection leaked to another inbound: %v", first)
			}
			if kt == TypeXray {
				if !reflect.DeepEqual(first["ip"], []interface{}{"0.0.0.0/0"}) {
					t.Fatalf("IPv4 literal destinations must be blocked: %v", first)
				}
			} else if first["ip_version"] != float64(4) {
				t.Fatalf("IPv4 literal destinations must be rejected: %v", first)
			}
			for _, suffix := range []string{"direct", "flyx-u7"} {
				assertIPDialer(t, kt, outboundByTag(cfg, "ipv6-in-1-"+suffix), suffix == "flyx-u7", OutboundIPv6Only)
				assertIPv4Dialer(t, kt, outboundByTag(cfg, "ipv4-in-2-"+suffix), suffix == "flyx-u7")
			}
			shared := outboundByTag(cfg, "flyx-u7")
			for _, key := range []string{"domain_resolver", "sendThrough", "inet4_bind_address", "inet6_bind_address"} {
				if shared[key] != nil {
					t.Fatalf("automatic inbound's dialer was modified: %v", shared)
				}
			}
			if kt == TypeSingbox {
				servers := cfg["dns"].(map[string]interface{})["servers"].([]interface{})
				if len(servers) != 2 || servers[0].(map[string]interface{})["tag"] != ipv6ResolverTag || servers[1].(map[string]interface{})["tag"] != ipv4ResolverTag {
					t.Fatalf("both policies must have valid local DNS references: %v", servers)
				}
				if cfg["route"].(map[string]interface{})["default_domain_resolver"] != ipv6ResolverTag {
					t.Fatal("mixed policies must define a default resolver for automatic inbounds")
				}
			}
		})
	}
}

func TestIPv6OnlySplitChainAndAdvancedConfiguration(t *testing.T) {
	for _, kt := range []string{TypeXray, TypeSingbox} {
		t.Run(kt, func(t *testing.T) {
			in := splitInbound()
			in.OutboundIPStrategy = OutboundIPv6Only
			in.Split[0].Via.Host = "2001:db8::2"
			in.Split[0].Via.Proxy["server"] = "2001:db8::2"
			in.Via = &Endpoint{Host: "exit.example.com", Port: 443, Proxy: Proxy{"type": "trojan", "server": "exit.example.com", "port": 443, "password": "x"}}
			cfg := renderJSON(t, kt, []Inbound{in})
			for _, tag := range []string{"via-1", "via-1-u7", "via-1-s1", "via-1-s1-u7", "ipv6-in-1-flyx-u7", "ipv6-in-1-direct"} {
				assertIPDialer(t, kt, outboundByTag(cfg, tag), strings.HasSuffix(tag, "u7"), OutboundIPv6Only)
			}
			for _, address := range []string{"192.0.2.1", "::ffff:192.0.2.1"} {
				in.Via = &Endpoint{Host: address, Port: 8388, Proxy: Proxy{"type": "ss", "server": address, "port": 8388, "cipher": "aes-128-gcm", "password": "x"}}
				assertUnreachableChainBlocked(t, kt, in)
			}
			in.Via = nil
			for _, extra := range []string{`{"tag":"custom"}`, `{"detour":"in-2"}`} {
				in.Extra = json.RawMessage(extra)
				if err := Normalize(kt, &in); err == nil {
					t.Fatalf("IPv6 policy accepted conflicting advanced routing: %s", extra)
				}
				if _, err := Render(kt, []Inbound{in}); err == nil {
					t.Fatalf("render accepted conflicting advanced routing: %s", extra)
				}
			}
		})
	}
	if _, err := NormalizeOutboundIPStrategy(TypeSnell, OutboundIPv6Only); err == nil {
		t.Fatal("standalone Snell must reject IPv6 egress policy")
	}
}

func TestOutboundIPServerFamily(t *testing.T) {
	for _, policy := range []string{OutboundIPAuto, OutboundIPv4Only, OutboundIPv6Only, OutboundPreferIPv4, OutboundPreferIPv6} {
		for _, address := range []string{"192.0.2.1", "::ffff:192.0.2.1", "2001:db8::1", "[2001:db8::1]", "exit.example.com"} {
			v4 := address == "192.0.2.1" || address == "::ffff:192.0.2.1"
			wantError := address != "exit.example.com" && ((policy == OutboundIPv4Only && !v4) || (policy == OutboundIPv6Only && v4))
			if err := ValidateOutboundIPServer(policy, address); (err != nil) != wantError {
				t.Fatalf("%s / %s: unexpected validation result: %v", policy, address, err)
			}
		}
	}
}

func TestIPv4OnlySplitAndChain(t *testing.T) {
	for _, kt := range []string{TypeXray, TypeSingbox} {
		t.Run(kt, func(t *testing.T) {
			in := splitInbound()
			in.OutboundIPStrategy = OutboundIPv4Only
			in.Via = &Endpoint{Host: "exit.example.com", Port: 443, Proxy: Proxy{"type": "trojan", "server": "exit.example.com", "port": 443, "password": "x"}}
			cfg := renderJSON(t, kt, []Inbound{in})
			for _, tag := range []string{"via-1", "via-1-u7", "via-1-s1", "via-1-s1-u7", "ipv4-in-1-flyx-u7", "ipv4-in-1-direct"} {
				assertIPv4Dialer(t, kt, outboundByTag(cfg, tag), strings.HasSuffix(tag, "u7"))
			}
			routeKey, obKey := "route", "outbound"
			if kt == TypeXray {
				routeKey, obKey = "routing", "outboundTag"
			}
			rules := cfg[routeKey].(map[string]interface{})["rules"].([]interface{})
			if kt == TypeSingbox && rules[0].(map[string]interface{})["action"] != "sniff" {
				t.Fatal("protocol sniffing must still run before split rules")
			}
			assertFamilyBlocksOnlyDirect(t, kt, rules, obKey, "ipv4-in-1-")
			targets := strings.Join(ruleOutbounds(rules, obKey), ",")
			if strings.Contains(targets, ",direct,") || !strings.HasSuffix(targets, "via-1-u7,via-1,flyx-u7") {
				t.Fatalf("split direct must use scoped dialer and default must retain chain: %s", targets)
			}
			in.Via = &Endpoint{Host: "2001:db8::1", Port: 8388, Proxy: Proxy{"type": "ss", "server": "2001:db8::1", "port": 8388, "cipher": "aes-128-gcm", "password": "x"}}
			assertUnreachableChainBlocked(t, kt, in)
		})
	}
}

func TestIPv4OnlyShadowTLSAndValidation(t *testing.T) {
	in := Inbound{ID: 3, Protocol: ProtocolShadowsocks, Port: 443,
		Settings:           Settings{Method: "aes-128-gcm", Password: "x"},
		Security:           Security{Type: SecurityShadowTLS, SNI: "www.example.com", ShadowTLSPassword: "pw"},
		OutboundIPStrategy: OutboundIPv4Only}
	cfg := renderJSON(t, TypeSingbox, []Inbound{in})
	rules := cfg["route"].(map[string]interface{})["rules"].([]interface{})
	if got := rules[0].(map[string]interface{})["inbound"]; !reflect.DeepEqual(got, []interface{}{"in-3", "in-3-ss"}) {
		t.Fatalf("both ShadowTLS and decrypted traffic must be covered: %v", got)
	}
	for _, tt := range []struct{ kernel, policy string }{{TypeSnell, OutboundIPv4Only}, {TypeXray, "invalid_policy"}} {
		if _, err := NormalizeOutboundIPStrategy(tt.kernel, tt.policy); err == nil {
			t.Fatalf("unsupported policy accepted: %+v", tt)
		}
	}
	if got, err := NormalizeOutboundIPStrategy(TypeSnell, ""); err != nil || got != OutboundIPAuto {
		t.Fatalf("old configurations must remain automatic: %q %v", got, err)
	}
}

func TestIPv4OnlyExtraCannotBypassPolicy(t *testing.T) {
	for _, kt := range []string{TypeXray, TypeSingbox} {
		for _, extra := range []string{`{"tag":"custom"}`, `{"detour":"in-2"}`} {
			in := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 8388, Extra: json.RawMessage(extra), OutboundIPStrategy: OutboundIPv4Only}
			if err := Normalize(kt, &in); err == nil {
				t.Fatalf("%s normalization accepted conflicting extra %s", kt, extra)
			}
			if _, err := Render(kt, []Inbound{in}); err == nil {
				t.Fatalf("%s rendering accepted conflicting extra %s", kt, extra)
			}
			in.OutboundIPStrategy = OutboundIPAuto
			if err := Normalize(kt, &in); err != nil {
				t.Fatalf("automatic inbounds must retain advanced customization: %v", err)
			}
		}
	}
}

func TestRestrictedLocalDNSVersionCompatibility(t *testing.T) {
	for _, policy := range []string{OutboundIPv4Only, OutboundIPv6Only, OutboundPreferIPv4, OutboundPreferIPv6} {
		for _, preferGo := range []bool{false, true} {
			in := Inbound{ID: 1, Protocol: ProtocolVLESS, Listen: "::", Port: 8388, OutboundIPStrategy: policy}
			if err := Normalize(TypeSingbox, &in); err != nil {
				t.Fatal(err)
			}
			r, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{SingboxPreferGoDNS: preferGo})
			if err != nil {
				t.Fatal(err)
			}
			cfg := renderJSONString(t, r.Files[0].Content)
			resolver := cfg["dns"].(map[string]interface{})["servers"].([]interface{})[0].(map[string]interface{})
			if preferGo && resolver["prefer_go"] != true {
				t.Fatalf("modern local resolver must bypass unavailable systemd DBus: %v", resolver)
			}
			if !preferGo && resolver["prefer_go"] != nil {
				t.Fatalf("sing-box 1.12 must not receive the unsupported prefer_go field: %v", resolver)
			}
		}
	}
}

func TestPreferredEgressRoutingAndFallback(t *testing.T) {
	for _, kt := range []string{TypeXray, TypeSingbox} {
		for _, policy := range []string{OutboundPreferIPv4, OutboundPreferIPv6} {
			t.Run(kt+"/"+policy, func(t *testing.T) {
				in := Inbound{ID: 1, Protocol: ProtocolVLESS, Listen: "::", Port: 8388,
					Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"},
					Users:    chainUser, OutboundIPStrategy: policy}
				auto := in
				auto.ID, auto.Port, auto.OutboundIPStrategy = 2, 8389, OutboundIPAuto
				cfg := renderJSON(t, kt, []Inbound{in, auto})
				prefix := "prefer-ipv4-in-1-"
				if policy == OutboundPreferIPv6 {
					prefix = "prefer-ipv6-in-1-"
				}
				for _, suffix := range []string{"direct", "flyx-u7"} {
					assertPreferredDialer(t, kt, outboundByTag(cfg, prefix+suffix), policy, suffix == "flyx-u7")
					if kt == TypeXray {
						udp := outboundByTag(cfg, prefix+suffix+"-udp")
						want := "ForceIPv4v6"
						if policy == OutboundPreferIPv6 {
							want = "ForceIPv6v4"
						}
						if udp == nil || udp["settings"].(map[string]interface{})["domainStrategy"] != want {
							t.Fatalf("UDP packet writer needs its own DNS preference: %v", udp)
						}
						if sock := udp["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{}); sock["happyEyeballs"] != nil || sock["domainStrategy"] != want || (suffix == "flyx-u7" && sock["mark"] != float64(UserMark(7))) {
							t.Fatalf("UDP preference lost policy or accounting: %v", sock)
						}
					}
				}
				routeKey, outKey := "route", "outbound"
				if kt == TypeXray {
					routeKey, outKey = "routing", "outboundTag"
				}
				rules := cfg[routeKey].(map[string]interface{})["rules"].([]interface{})
				for _, value := range rules {
					rule := value.(map[string]interface{})
					if rule["action"] == "reject" || rule[outKey] == "block" {
						t.Fatalf("preference must not reject the other family: %v", rule)
					}
				}
				if last := rules[len(rules)-1].(map[string]interface{}); last[outKey] != "flyx-u7" {
					t.Fatalf("global user routing must follow inbound-scoped preference: %v", last)
				}
				shared := outboundByTag(cfg, "flyx-u7")
				if shared["domain_resolver"] != nil || shared["sendThrough"] != nil {
					t.Fatalf("automatic inbound was modified: %v", shared)
				}
				if kt == TypeXray && shared["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{})["happyEyeballs"] != nil {
					t.Fatal("automatic inbound inherited preference")
				}
				for _, extra := range []string{`{"tag":"custom"}`, `{"detour":"in-2"}`} {
					in.Extra = json.RawMessage(extra)
					if err := Normalize(kt, &in); err == nil {
						t.Fatalf("preference accepted a routing bypass: %s", extra)
					}
				}
			})
		}
	}
}

func assertPreferredDialer(t *testing.T, kt string, ob map[string]interface{}, policy string, counted bool) {
	t.Helper()
	if ob == nil {
		t.Fatal("missing preferred outbound")
	}
	for _, key := range []string{"sendThrough", "inet4_bind_address", "inet6_bind_address"} {
		if ob[key] != nil {
			t.Fatalf("binding a family prevents connection fallback: %v", ob)
		}
	}
	if kt == TypeXray {
		sock := ob["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{})
		happy := sock["happyEyeballs"].(map[string]interface{})
		if sock["domainStrategy"] != "ForceIP" || happy["prioritizeIPv6"] != (policy == OutboundPreferIPv6) || happy["tryDelayMs"] != float64(250) || happy["maxConcurrentTry"] != float64(4) {
			t.Fatalf("must resolve both families and enable connection racing: %v", sock)
		}
		if counted && sock["mark"] != float64(UserMark(7)) {
			t.Fatalf("lost user mark: %v", sock)
		}
	} else {
		resolver := ob["domain_resolver"].(map[string]interface{})
		if resolver["strategy"] != policy || ob["fallback_delay"] != "250ms" {
			t.Fatalf("missing preferred resolver/connection fallback: %v", ob)
		}
		if counted && ob["routing_mark"] != float64(UserMark(7)) {
			t.Fatalf("lost user mark: %v", ob)
		}
	}
}

func TestPreferredEgressSplitAndChain(t *testing.T) {
	for _, kt := range []string{TypeXray, TypeSingbox} {
		for _, policy := range []string{OutboundPreferIPv4, OutboundPreferIPv6} {
			in := splitInbound()
			in.OutboundIPStrategy = policy
			in.Via = &Endpoint{Host: "exit.example.com", Port: 443, Proxy: Proxy{"type": "trojan", "server": "exit.example.com", "port": 443, "password": "x"}}
			cfg := renderJSON(t, kt, []Inbound{in})
			for _, tag := range []string{"via-1", "via-1-u7", "via-1-s1", "via-1-s1-u7"} {
				assertPreferredDialer(t, kt, outboundByTag(cfg, tag), policy, strings.HasSuffix(tag, "u7"))
			}
			if kt == TypeXray {
				for _, tag := range []string{"via-1-s1-udp", "via-1-s1-u7-udp"} {
					udp := outboundByTag(cfg, tag)
					if udp == nil || udp["protocol"] != "shadowsocks" {
						t.Fatalf("Shadowsocks UDP next hop needs preferred-family dialer: %v", udp)
					}
				}
			}
		}
	}
}

// assertUnreachableChainBlocked: a next hop outside the inbound's family
// rejects that inbound's chained traffic; the rest of the node still renders.
func assertUnreachableChainBlocked(t *testing.T, kt string, in Inbound) {
	t.Helper()
	other := Inbound{ID: 9, Protocol: ProtocolVLESS, Port: 9443, Settings: Settings{UUID: "00000000-0000-4000-8000-000000000009"}}
	cfg := renderJSON(t, kt, []Inbound{in, other})
	routeKey, obKey := "route", "outbound"
	if kt == TypeXray {
		routeKey, obKey = "routing", "outboundTag"
	}
	targets := ruleOutbounds(cfg[routeKey].(map[string]interface{})["rules"].([]interface{}), obKey)
	for _, tag := range targets {
		if tag == "via-1" || tag == "via-1-u7" {
			t.Fatalf("traffic still routed to an unreachable next hop: %v", targets)
		}
	}
	if !strings.Contains(strings.Join(targets, ","), map[bool]string{true: "block", false: "reject"}[kt == TypeXray]) {
		t.Fatalf("chained traffic must be rejected: %v", targets)
	}
}

// assertFamilyBlocksOnlyDirect: every other-family block sits right before
// a rule leaving through the inbound's own direct dialer, so traffic handed
// to a next hop is never filtered.
func assertFamilyBlocksOnlyDirect(t *testing.T, kt string, rules []interface{}, obKey, directPrefix string) {
	t.Helper()
	for i, r := range rules {
		m := r.(map[string]interface{})
		isFamilyBlock := m["ip_version"] != nil || (kt == TypeXray && m[obKey] == "block" && m["ip"] != nil)
		if !isFamilyBlock {
			continue
		}
		if i+1 >= len(rules) {
			t.Fatalf("family block %d is last", i)
		}
		next, _ := rules[i+1].(map[string]interface{})[obKey].(string)
		if !strings.HasPrefix(next, directPrefix) {
			t.Fatalf("family block %d precedes %q, not a direct rule", i, next)
		}
	}
}

func TestOutboundIPPolicyKeepsLocalResolver(t *testing.T) {
	in := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 8443, Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"},
		OutboundIPStrategy: OutboundIPv4Only}
	r, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{SingboxPreferGoDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(r.Files[0].Content), &cfg); err != nil {
		t.Fatal(err)
	}
	servers := cfg["dns"].(map[string]interface{})["servers"].([]interface{})
	if len(servers) != 2 || servers[0].(map[string]interface{})["tag"] != "flyx-local-resolver" || servers[1].(map[string]interface{})["tag"] != ipv4ResolverTag {
		t.Fatalf("policy resolvers must be added to the local one: %v", servers)
	}
	if cfg["route"].(map[string]interface{})["default_domain_resolver"] != "flyx-local-resolver" {
		t.Fatal("automatic dialers must keep the local default resolver")
	}
}
