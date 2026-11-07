package kernel

import (
	"reflect"
	"testing"
)

func TestAutomaticSingboxLocalDNS(t *testing.T) {
	in := Inbound{ID: 1, Protocol: ProtocolVLESS, Listen: "::", Port: 8443,
		Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"}, Users: chainUser,
		Security: Security{Type: SecurityReality, SNI: "example.com", RealityDest: "example.com:443"}}
	if err := Normalize(TypeSingbox, &in); err != nil {
		t.Fatal(err)
	}
	legacy, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	modern, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{SingboxPreferGoDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	oldCfg := renderJSONString(t, legacy.Files[0].Content)
	cfg := renderJSONString(t, modern.Files[0].Content)
	if oldCfg["dns"] != nil {
		t.Fatal("older sing-box configurations must remain compatible")
	}
	servers := cfg["dns"].(map[string]interface{})["servers"].([]interface{})
	resolver := servers[0].(map[string]interface{})
	if len(servers) != 1 || resolver["type"] != "local" || resolver["prefer_go"] != true {
		t.Fatalf("must use the local Go resolver: %v", servers)
	}
	route := cfg["route"].(map[string]interface{})
	if route["default_domain_resolver"] != resolver["tag"] {
		t.Fatalf("handshakes and dialers must have a default resolver: %v", route)
	}
	delete(route, "default_domain_resolver")
	if !reflect.DeepEqual(route, oldCfg["route"]) || !reflect.DeepEqual(cfg["inbounds"], oldCfg["inbounds"]) || !reflect.DeepEqual(cfg["outbounds"], oldCfg["outbounds"]) {
		t.Fatal("DNS fix must preserve listener, user accounting and routing")
	}
}

func TestAutomaticSingboxDNSWithoutRouteRules(t *testing.T) {
	r, err := RenderWith(TypeSingbox, nil, RenderOptions{SingboxPreferGoDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := renderJSONString(t, r.Files[0].Content)
	if cfg["route"].(map[string]interface{})["default_domain_resolver"] != "flyx-local-resolver" {
		t.Fatal("an unmarked direct outbound still needs the explicit resolver")
	}
}
