package kernel

import (
	"strings"
	"testing"
	"time"
)

func TestSubScriptFilterRenameReorder(t *testing.T) {
	eps := namedEndpoints("香港 01", "日本 01", "香港 02")
	script := `function operator(nodes) {
	  return nodes
	    .filter(n => n.region === "🇭🇰 香港")
	    .reverse()
	    .map(n => { n.name = "HK-" + n.port; n.port = 1; return n })
	}`
	out := ApplySubOptions(eps, SubOptions{Script: script})
	if got := endpointNames(out); strings.Join(got, ",") != "HK-8390,HK-8388" {
		t.Fatalf("names: %v", got)
	}
	if out[0].Port != 8390 || out[0].Original != "香港 02" {
		t.Fatalf("only the name is taken back: %+v", out[0])
	}
}

func TestSubScriptSeesNodeFields(t *testing.T) {
	eps := namedEndpoints("美国 01")
	eps[0].Direct = true
	script := `function operator(nodes) {
	  const n = nodes[0]
	  n.name = [n.server, n.port, n.protocol, n.region, n.line, n.original].join("|")
	  return nodes
	}`
	out, err := ApplySubScript(ApplySubOptions(eps, SubOptions{}), script)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Name != "203.0.113.1|8388|shadowsocks|🇺🇸 美国|direct|美国 01" {
		t.Fatalf("fields: %q", out[0].Name)
	}
}

func TestSubScriptErrors(t *testing.T) {
	eps := namedEndpoints("香港 01")
	cases := map[string]string{
		"function operator(nodes) {": "语法错误",
		"var x = 1":                  "operator",
		"function operator(nodes) { throw new Error('boom') }": "boom",
		"function operator(nodes) { return 1 }":                "节点数组",
		"function operator(nodes) { return [1] }":              "节点对象",
	}
	for script, want := range cases {
		_, err := ApplySubScript(eps, script)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", script, err, want)
		}
	}
	// A failing script leaves the pipeline's output alone.
	out := ApplySubOptions(eps, SubOptions{Script: "function operator(nodes) { throw 1 }"})
	if len(out) != 1 || SubScriptError(eps, SubOptions{Script: "function operator(nodes) { throw 1 }"}) == nil {
		t.Fatalf("fallback: %v", out)
	}
}

func TestSubScriptTimeout(t *testing.T) {
	start := time.Now()
	_, err := ApplySubScript(namedEndpoints("a"), "function operator(nodes) { while (true) {} }")
	if err == nil || !strings.Contains(err.Error(), "超过") || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout: %v after %v", err, time.Since(start))
	}
}

func TestSubScriptDropsUnknownAndDuplicateIDs(t *testing.T) {
	out, err := ApplySubScript(namedEndpoints("a", "b"),
		`function operator(nodes) { return [nodes[1], nodes[1], {id: 9, name: "x"}, {name: "y"}] }`)
	if err != nil || len(out) != 1 || out[0].Name != "b" {
		t.Fatalf("got %v %v", endpointNames(out), err)
	}
}

func TestChainByPickedNames(t *testing.T) {
	eps := namedEndpoints("美国 落地", "香港 中转", "日本")
	opts := SubOptions{
		ChainLandingNames: []string{"美国 落地"},
		ChainRelayNames:   []string{"香港 中转"},
		// Picked names win over the old expressions.
		ChainLanding: "日本", ChainRelay: "日本",
		// Renaming doesn't break the picks: they match the original name.
		Prefix: "x-",
	}
	out := ApplySubOptions(eps, opts)
	if out[0].ChainRole != ChainRoleLanding || out[1].ChainRole != ChainRoleRelay || out[2].ChainRole != "" {
		t.Fatalf("roles: %+v", out)
	}
	opts.ChainRelayNames = nil
	if out = ApplySubOptions(eps, opts); out[0].ChainRole != "" {
		t.Fatal("both sides must be picked")
	}
}
