package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

// A default exit of 阻断 only lets out what a 分流 rule sends somewhere:
// a node that reaches Claude and nothing else.
func TestInboundDefaultBlock(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/default-block.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	node := seedRelayTargetNode(t, r, "claude-only", "198.51.100.7")
	for _, kernelType := range []string{kernel.TypeXray, kernel.TypeSingbox} {
		k := seedChainKernel(t, r, node, kernelType)
		entry := seedChainInbound(t, r, k, "claude-"+kernelType, kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8388 + int(k.ID)}, 0)
		save := func(body string) response.R {
			rec := httptest.NewRecorder()
			h.inboundSplitSave(rec, httptest.NewRequest(http.MethodPost, "/api/v1/inbound/split", strings.NewReader(body)))
			var out response.R
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		id := jsonID(entry.ID)
		if out := save(`{"id":` + id + `,"defaultBlock":true,"splitRules":[]}`); out.Code == 0 {
			t.Fatal("阻断 without a rule letting traffic out must be refused")
		}
		if out := save(`{"id":` + id + `,"defaultBlock":true,"splitRules":[{"categories":["bt"],"action":"block"}]}`); out.Code == 0 {
			t.Fatal("阻断 with only blocking rules must be refused")
		}
		out := save(`{"id":` + id + `,"defaultBlock":true,"outboundId":99,"splitRules":[{"categories":["anthropic"],"action":"direct"}]}`)
		if out.Code != 0 {
			t.Fatalf("%s save: %+v", kernelType, out)
		}
		got, _ := r.GetNodeInbound(entry.ID)
		if got.DefaultBlock != 1 || got.OutboundID != 0 {
			t.Fatalf("阻断 has no outbound: %+v", got)
		}

		payload, _, err := h.buildKernelApplyPayload(k)
		if err != nil {
			t.Fatal(err)
		}
		cfg := payload["files"].([]kernel.File)[0].Content
		claude, block := strings.Index(cfg, "anthropic"), -1
		if kernelType == kernel.TypeXray {
			block = strings.Index(cfg, `"outboundTag": "block"`)
		} else {
			block = strings.Index(cfg, `"action": "reject"`)
		}
		if claude < 0 || block < 0 || block < claude {
			t.Fatalf("%s: Claude goes out, the rest is dropped after it:\n%s", kernelType, cfg)
		}

		// Saving the routing again without the field keeps 阻断.
		if out := save(`{"id":` + id + `,"splitRules":[{"categories":["anthropic"],"action":"direct"}]}`); out.Code != 0 {
			t.Fatal(out.Msg)
		}
		if got, _ := r.GetNodeInbound(entry.ID); got.DefaultBlock != 1 {
			t.Fatal("an absent defaultBlock keeps the stored choice")
		}
		if out := save(`{"id":` + id + `,"defaultBlock":false,"splitRules":[]}`); out.Code != 0 {
			t.Fatal(out.Msg)
		}
		if got, _ := r.GetNodeInbound(entry.ID); got.DefaultBlock != 0 {
			t.Fatal("defaultBlock false goes back to 直连")
		}
	}
}

func TestInboundRatesInPayload(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/rates.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	node := seedRelayTargetNode(t, r, "limited", "198.51.100.8")
	k := seedChainKernel(t, r, node, kernel.TypeSingbox)
	limited := seedChainInbound(t, r, k, "limited", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8388}, 0)
	free := seedChainInbound(t, r, k, "free", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8389}, 0)
	limited.RateDownMbps, limited.RateUpMbps = 100, 20
	if err := r.SaveNodeInbound(limited); err != nil {
		t.Fatal(err)
	}
	payload, _, err := h.buildKernelApplyPayload(k)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range payload["ports"].([]map[string]interface{}) {
		switch p["inboundId"] {
		case limited.ID:
			if p["limitDownMbps"] != 100 || p["limitUpMbps"] != 20 {
				t.Fatalf("limits not sent: %v", p)
			}
		case free.ID:
			if _, ok := p["limitDownMbps"]; ok {
				t.Fatalf("no limit for an unlimited inbound: %v", p)
			}
		}
	}
}

func TestValidateInboundRates(t *testing.T) {
	node := &model.Node{Version: sql.NullString{String: "3.9.4", Valid: true}}
	if err := validateInboundRates(node, 100, 0); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ up, down int }{{-1, 0}, {0, model.MaxInboundRateMbps + 1}} {
		if validateInboundRates(node, c.up, c.down) == nil {
			t.Fatalf("%+v out of range must be refused", c)
		}
	}
	old := &model.Node{Version: sql.NullString{String: "3.9.3", Valid: true}}
	if err := validateInboundRates(old, 0, 50); err == nil || !strings.Contains(err.Error(), "3.9.4") {
		t.Fatalf("an agent that ignores limits must be refused: %v", err)
	}
	if err := validateInboundRates(old, 0, 0); err != nil {
		t.Fatal("no limit needs no new agent")
	}
	if err := validateInboundRates(&model.Node{NftError: "未安装 nftables"}, 10, 0); err == nil {
		t.Fatal("limits need nftables")
	}
}
