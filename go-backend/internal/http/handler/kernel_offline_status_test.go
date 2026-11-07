package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

// A server that went offline (e.g. reinstalled) must not keep showing its
// kernels as running.
func TestKernelStatusShowsOfflineNode(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/offline.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	online := mustCreateRelayNode(t, r, "online", "198.51.100.1")
	offline := mustCreateRelayNode(t, r, "offline", "198.51.100.2")
	if err := r.DB().Model(&model.Node{}).Where("id = ?", offline).Update("status", 0).Error; err != nil {
		t.Fatal(err)
	}
	kernels := []model.NodeKernel{
		{NodeID: online, KernelType: "xray", Status: kernelStatusRunning, Enabled: 1},
		{NodeID: offline, KernelType: "xray", Status: kernelStatusRunning, Enabled: 1},
		{NodeID: offline, KernelType: "sing-box", Status: kernelStatusPending, Enabled: 1},
	}
	for i := range kernels {
		if err := r.DB().Create(&kernels[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, in := range []model.NodeInbound{
		{NodeID: online, KernelID: kernels[0].ID, Name: "on", Protocol: "vless", Port: 443, Enabled: 1},
		{NodeID: offline, KernelID: kernels[1].ID, Name: "off", Protocol: "vless", Port: 443, Enabled: 1},
		{Name: "imported", Protocol: "ss", Listen: "203.0.113.9", Port: 8388, Enabled: 1},
	} {
		in := in
		if err := r.CreateNodeInbound(&in); err != nil {
			t.Fatal(err)
		}
	}

	list := func(fn http.HandlerFunc) []map[string]interface{} {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
		var out struct {
			response.R
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Code != 0 {
			t.Fatalf("%s: %v", rec.Body.String(), err)
		}
		return out.Data
	}

	got := map[string]string{}
	for _, k := range list(h.kernelList) {
		got[k["nodeName"].(string)+"/"+k["kernelType"].(string)] = k["status"].(string)
	}
	want := map[string]string{"online/xray": "running", "offline/xray": "offline", "offline/sing-box": "pending"}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("kernel %s: got %q want %q (%v)", k, got[k], v, got)
		}
	}

	byName := map[string]string{}
	for _, in := range list(h.inboundList) {
		byName[in["name"].(string)] = in["kernelStatus"].(string)
	}
	if byName["on"] != "running" || byName["off"] != "offline" || byName["imported"] != "" {
		t.Fatalf("inbound kernel status: %v", byName)
	}

	// The stored status is left alone; the next report replaces it.
	k, _ := r.GetNodeKernel(kernels[1].ID)
	if k.Status != kernelStatusRunning {
		t.Fatalf("stored status changed: %q", k.Status)
	}
}
