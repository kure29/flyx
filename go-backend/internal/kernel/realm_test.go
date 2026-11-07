package kernel

import (
	"strings"
	"testing"
)

func TestRenderRealm(t *testing.T) {
	out, err := RenderRealm([]RealmEndpoint{
		{ForwardID: 9, Listen: "[::]:31002", Remotes: []string{"a.example:80", "1.2.3.4:80", "[2001:db8::1]:80"}, IPHash: true, Interface: "eth1"},
		{ForwardID: 3, Listen: "[::]:31001", Remotes: []string{"1.1.1.1:443"}, AcceptProxy: true, SendProxy: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Idle || len(out.Files) != 1 || out.Files[0].Name != "config.toml" || out.Hash == "" {
		t.Fatalf("unexpected render: %+v", out)
	}
	want := `[log]
level = "warn"

[network]
use_udp = true

[[endpoints]]
# forward 3
listen = "[::]:31001"
remote = "1.1.1.1:443"
network = { accept_proxy = true, send_proxy = true, send_proxy_version = 2 }

[[endpoints]]
# forward 9
listen = "[::]:31002"
remote = "a.example:80"
extra_remotes = ["1.2.3.4:80", "[2001:db8::1]:80"]
balance = "iphash: 1, 1, 1"
interface = "eth1"
`
	if got := out.Files[0].Content; got != want {
		t.Fatalf("config mismatch:\n%s", got)
	}
	if strings.Join(out.Args, " ") != "-c {RUN_DIR}/config.toml" {
		t.Fatalf("args = %v", out.Args)
	}

	empty, err := RenderRealm(nil)
	if err != nil || !empty.Idle {
		t.Fatalf("empty render should be idle: %+v %v", empty, err)
	}
	if _, err := RenderRealm([]RealmEndpoint{{ForwardID: 1, Listen: "[::]:1"}}); err == nil {
		t.Fatal("expected error for endpoint without remotes")
	}
}

func TestRealmCounterID(t *testing.T) {
	id := RealmCounterID(42)
	if fid, ok := RealmForwardID(id); !ok || fid != 42 {
		t.Fatalf("round trip = %d %v", fid, ok)
	}
	if _, ok := RealmForwardID(42); ok {
		t.Fatal("inbound id taken for a realm counter")
	}
}
