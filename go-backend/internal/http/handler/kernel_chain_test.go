package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func seedChainInbound(t *testing.T, r *repo.Repository, k *model.NodeKernel, name string, in kernel.Inbound, outboundID int64) *model.NodeInbound {
	t.Helper()
	if err := kernel.Normalize(k.KernelType, &in); err != nil {
		t.Fatal(err)
	}
	row := model.NodeInbound{KernelID: k.ID, NodeID: k.NodeID, Name: name, Enabled: 1, OutboundID: outboundID,
		CreatedTime: time.Now().UnixMilli()}
	if err := applyInboundToModel(in, &row); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateNodeInbound(&row); err != nil {
		t.Fatal(err)
	}
	return &row
}

func seedChainKernel(t *testing.T, r *repo.Repository, nodeID int64, kernelType string) *model.NodeKernel {
	t.Helper()
	k := &model.NodeKernel{NodeID: nodeID, KernelType: kernelType, Version: "latest", Status: "pending", Enabled: 1,
		CreatedTime: time.Now().UnixMilli()}
	if err := r.CreateNodeKernel(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestInboundChain(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/chain.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	front := seedRelayTargetNode(t, r, "front", "198.51.100.1")
	landing := seedRelayTargetNode(t, r, "landing", "203.0.113.10")
	frontXray := seedChainKernel(t, r, front, kernel.TypeXray)
	landingBox := seedChainKernel(t, r, landing, kernel.TypeSingbox)

	target := seedChainInbound(t, r, landingBox, "landing-vless", kernel.Inbound{Protocol: kernel.ProtocolVLESS, Port: 443,
		Security: kernel.Security{Type: kernel.SecurityReality}}, 0)
	entry := seedChainInbound(t, r, frontXray, "front-vless", kernel.Inbound{Protocol: kernel.ProtocolVLESS, Port: 8443,
		Security: kernel.Security{Type: kernel.SecurityNone}}, target.ID)

	if err := h.validateChain(*entry, kernel.TypeXray); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
	payload, _, err := h.buildKernelApplyPayload(frontXray)
	if err != nil {
		t.Fatal(err)
	}
	cfg := payload["files"].([]kernel.File)[0].Content
	if !strings.Contains(cfg, `"tag": "via-`) || !strings.Contains(cfg, `"address": "203.0.113.10"`) {
		t.Fatalf("front kernel must dial the landing node:\n%s", cfg)
	}

	// Same node: dial locally.
	e, err := h.chainEndpoint(target, landing)
	if err != nil || e.Host != "127.0.0.1" {
		t.Fatalf("same-node chain: %q %v", e.Host, err)
	}

	loop := *target
	loop.OutboundID = entry.ID
	if err := h.validateChain(loop, kernel.TypeSingbox); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("loop not detected: %v", err)
	}
	self := *entry
	self.OutboundID = entry.ID
	if err := h.validateChain(self, kernel.TypeXray); err == nil {
		t.Fatal("chaining to itself must fail")
	}

	if msg := h.chainInUseMessage(target); !strings.Contains(msg, "front-vless") {
		t.Fatalf("delete guard: %q", msg)
	}
	if msg := h.chainInUseMessage(entry); msg != "" {
		t.Fatalf("nothing chains through the entry: %q", msg)
	}

	target.RelayWhitelist = 1
	allow := strings.Join(h.inboundAllowList(*target), ",")
	if !strings.Contains(allow, "198.51.100.1/32") {
		t.Fatalf("whitelist must admit the chained node: %s", allow)
	}

	target.AcceptProxyProtocol = 1
	if err := r.SaveNodeInbound(target); err != nil {
		t.Fatal(err)
	}
	if err := h.validateChain(*entry, kernel.TypeXray); err == nil || !strings.Contains(err.Error(), "PROXY") {
		t.Fatalf("PROXY protocol target must be refused: %v", err)
	}

	// A deleted target drops the traffic instead of leaking it out directly.
	if err := r.DeleteNodeInbound(target.ID); err != nil {
		t.Fatal(err)
	}
	payload, _, err = h.buildKernelApplyPayload(frontXray)
	if err != nil {
		t.Fatal(err)
	}
	cfg = payload["files"].([]kernel.File)[0].Content
	if !strings.Contains(cfg, `"outboundTag": "block"`) {
		t.Fatalf("missing target must be blocked:\n%s", cfg)
	}
}

func TestInboundSplitTargets(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/split.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	front := seedRelayTargetNode(t, r, "front", "198.51.100.1")
	unlock := seedRelayTargetNode(t, r, "unlock", "203.0.113.20")
	frontBox := seedChainKernel(t, r, front, kernel.TypeSingbox)
	unlockXray := seedChainKernel(t, r, unlock, kernel.TypeXray)

	target := seedChainInbound(t, r, unlockXray, "unlock-vless", kernel.Inbound{Protocol: kernel.ProtocolVLESS, Port: 443,
		Security: kernel.Security{Type: kernel.SecurityReality}}, 0)
	entry := seedChainInbound(t, r, frontBox, "front-trojan", kernel.Inbound{Protocol: kernel.ProtocolTrojan, Port: 8443,
		Security: kernel.Security{Type: kernel.SecurityTLS, SNI: "f.example.com"}}, 0)
	entry.SplitRules = `[{"categories":["netflix"],"action":"proxy","outboundId":` + jsonID(target.ID) + `},{"categories":["bt"],"action":"block"}]`
	if err := r.SaveNodeInbound(entry); err != nil {
		t.Fatal(err)
	}

	if err := h.validateChain(*entry, kernel.TypeSingbox); err != nil {
		t.Fatalf("valid split rejected: %v", err)
	}
	payload, _, err := h.buildKernelApplyPayload(frontBox)
	if err != nil {
		t.Fatal(err)
	}
	cfg := payload["files"].([]kernel.File)[0].Content
	if !strings.Contains(cfg, `"tag": "via-`+jsonID(entry.ID)+`-s1"`) || !strings.Contains(cfg, `"server": "203.0.113.20"`) ||
		!strings.Contains(cfg, "geosite-netflix.srs") {
		t.Fatalf("front kernel must route netflix to the unlock node:\n%s", cfg)
	}

	if msg := h.chainInUseMessage(target); !strings.Contains(msg, "front-trojan") {
		t.Fatalf("a 分流 target is in use: %q", msg)
	}
	target.RelayWhitelist = 1
	if allow := strings.Join(h.inboundAllowList(*target), ","); !strings.Contains(allow, "198.51.100.1/32") {
		t.Fatalf("whitelist must admit the node routing through it: %s", allow)
	}

	// The unlock node routing back through the entry would loop.
	loop := *target
	loop.SplitRules = `[{"categories":["ads"],"action":"proxy","outboundId":` + jsonID(entry.ID) + `}]`
	if err := h.validateChain(loop, kernel.TypeXray); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("loop through a 分流 rule: %v", err)
	}
	if got := changedTargets([]int64{1, 2}, []int64{2, 3}); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("changed targets: %v", got)
	}
}

func jsonID(id int64) string { return strconv.FormatInt(id, 10) }

func TestInboundSplitSave(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/split-save.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	node := seedRelayTargetNode(t, r, "front", "198.51.100.1")
	box := seedChainKernel(t, r, node, kernel.TypeSingbox)
	target := seedChainInbound(t, r, box, "unlock", kernel.Inbound{Protocol: kernel.ProtocolTrojan, Port: 443,
		Security: kernel.Security{Type: kernel.SecurityTLS, SNI: "u.example.com"}}, 0)
	entry := seedChainInbound(t, r, box, "entry", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8388}, 0)
	name := entry.Name

	save := func(body string) response.R {
		rec := httptest.NewRecorder()
		h.inboundSplitSave(rec, httptest.NewRequest(http.MethodPost, "/api/v1/inbound/split", strings.NewReader(body)))
		var out response.R
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	out := save(`{"id":` + jsonID(entry.ID) + `,"outboundId":0,"splitRules":[{"categories":["openai"],"action":"proxy","outboundId":` + jsonID(target.ID) + `}]}`)
	if out.Code != 0 {
		t.Fatalf("save: %+v", out)
	}
	got, _ := r.GetNodeInbound(entry.ID)
	if !strings.Contains(got.SplitRules, `"openai"`) || got.Name != name || got.UpdatedTime == 0 {
		t.Fatalf("only the routing changes: %+v", got)
	}

	if out := save(`{"id":` + jsonID(entry.ID) + `,"splitRules":[{"categories":["openai"],"action":"proxy"}]}`); out.Code == 0 {
		t.Fatal("a proxy rule needs a target")
	}
	if out := save(`{"id":` + jsonID(entry.ID) + `,"outboundId":` + jsonID(entry.ID) + `}`); out.Code == 0 {
		t.Fatal("an inbound cannot exit through itself")
	}
	if out := save(`{"id":` + jsonID(entry.ID) + `,"splitRules":[]}`); out.Code != 0 {
		t.Fatalf("clearing: %+v", out)
	}
	if got, _ := r.GetNodeInbound(entry.ID); got.SplitRules != "" {
		t.Fatalf("rules not cleared: %q", got.SplitRules)
	}
}

// sing-box dials Snell targets from 1.14 on: saving checks the installed
// version, the config carries the snell outbound, and a downgrade is refused.
func TestInboundChainThroughSnell(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/chain-snell.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	front := seedRelayTargetNode(t, r, "front", "198.51.100.1")
	landing := seedRelayTargetNode(t, r, "landing", "203.0.113.10")
	frontBox := seedChainKernel(t, r, front, kernel.TypeSingbox)
	landingSnell := seedChainKernel(t, r, landing, kernel.TypeSnell)
	target := seedChainInbound(t, r, landingSnell, "landing-snell", kernel.Inbound{Protocol: kernel.ProtocolSnell, Port: 6160}, 0)
	entry := seedChainInbound(t, r, frontBox, "front-vless", kernel.Inbound{Protocol: kernel.ProtocolVLESS, Port: 8443,
		Security: kernel.Security{Type: kernel.SecurityNone}}, target.ID)

	if err := h.validateChainFor(*entry, kernel.TypeSingbox, "1.12.8"); err == nil || !strings.Contains(err.Error(), "1.14") {
		t.Fatalf("sing-box 1.12 cannot dial snell: %v", err)
	}
	if err := h.validateChainFor(*entry, kernel.TypeSingbox, "1.14.3"); err != nil {
		t.Fatalf("sing-box 1.14 dials snell: %v", err)
	}
	if err := h.validateChain(*entry, kernel.TypeXray); err == nil {
		t.Fatal("xray cannot dial snell")
	}

	payload, _, err := h.buildKernelApplyPayload(frontBox)
	if err != nil {
		t.Fatal(err)
	}
	cfg := payload["files"].([]kernel.File)[0].Content
	if !strings.Contains(cfg, `"type": "snell"`) || !strings.Contains(cfg, `"server": "203.0.113.10"`) {
		t.Fatalf("chain outbound missing:\n%s", cfg)
	}
	if err := h.checkKernelVersionFits(frontBox.ID, kernel.TypeSingbox, "1.13.5"); err == nil || !strings.Contains(err.Error(), "Snell") {
		t.Fatalf("downgrade below 1.14 must be refused: %v", err)
	}
	if err := h.checkKernelVersionFits(frontBox.ID, kernel.TypeSingbox, "1.14.0"); err != nil {
		t.Fatal(err)
	}
}
