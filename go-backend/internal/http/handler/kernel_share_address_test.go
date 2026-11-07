package handler

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestShareAddressFamilyAndSubscriptions(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/share.db")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	h := New(r, "share-test")
	id := seedRelayTargetNode(t, r, "dual", "192.0.2.1")
	node, _ := r.GetNodeByID(id)
	node.ServerIPV6 = sql.NullString{String: "2001:db8::1234", Valid: true}
	if err := r.DB().Model(node).Update("server_ip_v6", node.ServerIPV6.String).Error; err != nil {
		t.Fatal(err)
	}
	box := seedChainKernel(t, r, id, kernel.TypeSingbox)
	row := seedChainInbound(t, r, box, "IPv6 入 IPv4 出", kernel.Inbound{Protocol: kernel.ProtocolVLESS, Listen: "::", Port: 443, OutboundIPStrategy: kernel.OutboundIPv4Only}, 0)
	row.ShareAddressMode = "ipv6"
	if err := r.SaveNodeInbound(row); err != nil {
		t.Fatal(err)
	}
	row, err = r.GetNodeInbound(row.ID)
	if err != nil || row.ShareAddressMode != "ipv6" {
		t.Fatalf("address mode did not persist: %v", err)
	}
	endpoints, _ := h.inboundEndpoints([]model.NodeInbound{*row})
	if len(endpoints) != 1 || endpoints[0].Host != "2001:db8::1234" || endpoints[0].Inbound.OutboundIPStrategy != kernel.OutboundIPv4Only {
		t.Fatalf("endpoint: %+v", endpoints)
	}
	link, _ := kernel.ShareURI(endpoints[0])
	if !strings.Contains(link, "@[2001:db8::1234]:443") {
		t.Fatalf("IPv6 URI: %s", link)
	}
	for _, kind := range []string{kernel.TemplateClash, kernel.TemplateSingbox} {
		body, _, err := kernel.Subscription(kind, endpoints, kernel.SubOptions{})
		if err != nil || !strings.Contains(string(body), "2001:db8::1234") {
			t.Fatalf("%s export: %v", kind, err)
		}
	}
	for _, tc := range []struct{ mode, host, want string }{
		{"auto", "", "192.0.2.1"}, {"ipv4", "", "192.0.2.1"}, {"ipv6", "", "2001:db8::1234"}, {"custom", "entry.example.com", "entry.example.com"}, {"auto", "old.example.com", "old.example.com"}, {"", "old.example.com", "old.example.com"},
	} {
		row.ShareAddressMode, row.ShareHost = tc.mode, tc.host
		if got := inboundShareAddress(row, node); got != tc.want {
			t.Fatalf("%+v got %s", tc, got)
		}
	}
	if err := validateShareAddress("ipv6", "", &model.Node{ServerIP: "192.0.2.1"}); err == nil {
		t.Fatal("missing IPv6 must not silently fall back to IPv4")
	}
	if err := validateShareAddress("custom", "", node); err == nil {
		t.Fatal("empty custom address")
	}
	// The selected family follows subsequent node changes, rather than copying IPs.
	node.ServerIPV6.String = "2001:db8::5678"
	row.ShareHost = ""
	row.ShareAddressMode = "ipv6"
	if inboundShareAddress(row, node) != "2001:db8::5678" {
		t.Fatal("stale node address")
	}
}

func TestExistingVisualTemplatesKeepSavedContent(t *testing.T) {
	b := kernel.TemplateBuilder{Version: 1, Preset: "custom", Final: "节点选择", Rules: []kernel.TemplateBuilderRule{{Type: "category", Value: "ai", Target: "节点选择"}}}
	// A generator change must not rewrite an already saved template when
	// it is read: this content predates the independent service selectors.
	shared := false
	savedBuilder := b
	savedBuilder.ServiceGroups = &shared
	savedContent, err := kernel.BuildSubscriptionTemplate(kernel.TemplateClash, savedBuilder)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	tpl := &model.SubscriptionTemplate{Kind: kernel.TemplateClash, Content: savedContent, BuilderConfig: string(raw)}
	content, recipe := effectiveTemplate(tpl)
	if recipe == nil || len(recipe.Rules) != 1 || recipe.Rules[0].Value != "ai" || content != savedContent {
		t.Fatal("saved content and editable recipe must stay independent")
	}
	if tpl.Content != savedContent || tpl.BuilderConfig != string(raw) {
		t.Fatal("read mutated saved data")
	}
	view := templateView(tpl, 1)
	if view.Groups != 1 || view.Content != content {
		t.Fatalf("view differs: %+v", view)
	}
	tpl.BuilderConfig = ""
	if c, recipe := effectiveTemplate(tpl); c != savedContent || recipe != nil {
		t.Fatal("raw templates must stay untouched")
	}
	tpl.BuilderConfig = "invalid JSON"
	if c, recipe := effectiveTemplate(tpl); c != savedContent || recipe != nil {
		t.Fatal("invalid recipe must not replace saved content")
	}
}
