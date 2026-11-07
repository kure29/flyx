package handler

import (
	"context"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"go-backend/internal/store/model"
)

func TestPanelNoRouteMatchesRealDialErrors(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.ENETUNREACH, syscall.EADDRNOTAVAIL, syscall.EAFNOSUPPORT} {
		err := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("socket", errno)}
		if !panelNoRoute(err) {
			t.Errorf("%v not recognised", errno)
		}
	}
	if panelNoRoute(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}) {
		t.Error("connection refused is a real failure")
	}
}

// An IPv6-only node probed from a panel without IPv6: the dial fails with
// "network unreachable", which says nothing about the node. Built inbounds
// are checked by their own node instead; imports are skipped, not down.
func TestProbeInboundReachPanelWithoutIPv6(t *testing.T) {
	h, r := setupLatencyHandler(t, testLatencyPool)
	node := model.Node{Name: "v6", Secret: "v6", ServerIP: "2001:db8::1", Status: 1}
	if err := r.DB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	k := model.NodeKernel{NodeID: node.ID, KernelType: "xray", Status: kernelStatusRunning, CreatedTime: now, UpdatedTime: now}
	if err := r.DB().Create(&k).Error; err != nil {
		t.Fatal(err)
	}
	built := model.NodeInbound{KernelID: k.ID, NodeID: node.ID, Name: "built", Protocol: "vless", Port: 443, Enabled: 1}
	imported := model.NodeInbound{NodeID: node.ID, Name: "imported", Protocol: "ss", Listen: "2001:db8::2", Port: 8388, Enabled: 1}
	for _, row := range []*model.NodeInbound{&built, &imported} {
		if err := r.DB().Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	old := reachDial
	reachDial = func(_ context.Context, addr string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ENETUNREACH)}
	}
	t.Cleanup(func() { reachDial = old })
	calls := stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		return map[string]interface{}{"success": true, "averageTime": 0.4}, nil
	})

	for round := 0; round < notifyInboundDownStreak+1; round++ {
		results, err := h.probeInboundReach(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if p := results[built.ID]; !p.OK || p.From == "" || p.LatencyMs != 1 {
			t.Fatalf("built inbound = %+v", p)
		}
		if p := results[imported.ID]; !p.Skipped || !strings.Contains(p.Error, "IPv6") {
			t.Fatalf("imported inbound = %+v", p)
		}
	}
	if len(*calls) == 0 || (*calls)[0] != "2001:db8::1" {
		t.Fatalf("node self-check dialed %v", *calls)
	}
	if h.reachProbes.down[built.ID] || h.reachProbes.down[imported.ID] {
		t.Fatal("neither inbound may be reported down")
	}

	// The node cannot reach its own port either: that is a real failure.
	stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		return map[string]interface{}{"success": false}, nil
	})
	results, _ := h.probeInboundReach(context.Background(), []int64{built.ID})
	if p := results[built.ID]; p.OK || p.Skipped || p.Error != "无法连接" {
		t.Fatalf("failed self-check = %+v", p)
	}
}
