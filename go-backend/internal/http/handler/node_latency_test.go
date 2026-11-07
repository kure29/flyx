package handler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestParseLatencyTargets(t *testing.T) {
	got, err := parseLatencyTargets(`[{"name":" 广东电信 ","host":"gd-ct-v4.ip.zstaticcdn.com","port":80,"enabled":true,"pinned":true}]`)
	if err != nil || len(got) != 1 || got[0].Name != "广东电信" || !got[0].Pinned {
		t.Fatalf("got %+v, %v", got, err)
	}
	if got, err := parseLatencyTargets(""); err != nil || got != nil {
		t.Fatalf("empty must be no targets: %+v, %v", got, err)
	}

	for raw, want := range map[string]string{
		`not json`: "格式无效",
		`[{"name":"a","host":"x.com","port":80},{"name":"a","host":"y.com","port":80}]`: "重复",
		`[{"name":"a","host":"http://x.com","port":80}]`:                                "地址无效",
		`[{"name":"a","host":"x.com","port":0}]`:                                        "地址无效",
		`[{"name":"","host":"x.com","port":80}]`:                                        "名称",
		`[{"name":"a","host":"a.com","port":80,"pinned":true},{"name":"b","host":"b.com","port":80,"pinned":true},{"name":"c","host":"c.com","port":80,"pinned":true},{"name":"d","host":"d.com","port":80,"pinned":true}]`: "最多固定",
	} {
		if _, err := parseLatencyTargets(raw); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", raw, err, want)
		}
	}
}

func setupLatencyHandler(t *testing.T, targets string) (*Handler, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir() + "/latency.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.UpsertConfig(latencyTargetsConfigKey, targets, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return New(r, "unit-test-secret"), r
}

func mustCreateLatencyNode(t *testing.T, r *repo.Repository, name string, status, isRemote int) int64 {
	t.Helper()
	n := model.Node{Name: name, Secret: name, ServerIP: "198.51.100.1", Status: status, IsRemote: isRemote}
	if err := r.DB().Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func stubLatencyPing(t *testing.T, fn func(nodeID int64, host string) (map[string]interface{}, error)) *[]string {
	t.Helper()
	old := nodeLatencyPing
	var mu sync.Mutex
	var calls []string
	nodeLatencyPing = func(_ *Handler, nodeID int64, host string, _ int) (map[string]interface{}, error) {
		mu.Lock()
		calls = append(calls, host)
		mu.Unlock()
		return fn(nodeID, host)
	}
	t.Cleanup(func() { nodeLatencyPing = old })
	return &calls
}

func TestProbeNodeLatencyMeasuresOnlineLocalNodes(t *testing.T) {
	h, r := setupLatencyHandler(t, `[
		{"name":"电信","host":"ct.example.com","port":80,"enabled":true,"pinned":true},
		{"name":"联通","host":"cu.example.com","port":80,"enabled":true},
		{"name":"停用","host":"off.example.com","port":80,"enabled":false},
		{"name":"移动","host":"cm.example.com","port":80,"enabled":true}]`)
	online := mustCreateLatencyNode(t, r, "online", 1, 0)
	offline := mustCreateLatencyNode(t, r, "offline", 0, 0)
	remote := mustCreateLatencyNode(t, r, "remote", 1, 1)

	calls := stubLatencyPing(t, func(_ int64, host string) (map[string]interface{}, error) {
		switch host {
		case "ct.example.com":
			return map[string]interface{}{"success": true, "averageTime": 37.6, "packetLoss": 25.0}, nil
		case "cu.example.com":
			return map[string]interface{}{"success": false, "errorMessage": "所有TCP连接尝试都失败"}, nil
		default:
			return nil, errors.New("timeout")
		}
	})

	results, err := h.probeNodeLatency(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 {
		t.Fatalf("one node × three enabled targets expected, got calls %v", *calls)
	}
	if _, ok := results[offline]; ok {
		t.Fatal("offline node must not be measured")
	}
	if _, ok := results[remote]; ok {
		t.Fatal("remote node must not be measured")
	}
	p := results[online]
	if !p.OK || p.LatencyMs != 38 || len(p.Points) != 3 {
		t.Fatalf("summary = %+v", p)
	}
	want := []latencyPoint{
		{Name: "电信", Pinned: true, OK: true, LatencyMs: 38, LossPct: 25},
		{Name: "联通", Error: "测试点连接失败"},
		{Name: "移动", Error: "服务器响应超时"},
	}
	for i, w := range want {
		if p.Points[i] != w {
			t.Errorf("point %d = %+v, want %+v", i, p.Points[i], w)
		}
	}
	if stored, ok := h.nodeLatency.get(online); !ok || len(stored.Points) != 3 {
		t.Fatalf("result not stored: %+v", stored)
	}
}

func TestLatencyPointPreservesFailureReason(t *testing.T) {
	for _, tt := range []struct {
		name string
		data map[string]interface{}
		err  error
		want string
	}{
		{name: "legacy busy", err: errors.New("TCP探测任务过多，请稍后重试"), want: "服务器繁忙，请稍后重试"},
		{name: "read commands busy", err: errors.New("只读命令并发过多，请稍后重试"), want: "服务器繁忙，请稍后重试"},
		{name: "queue full", err: errors.New("TCP探测队列已满，请稍后重试"), want: "TCP探测队列已满，请稍后重试"},
		{name: "queue expired", err: errors.New("TCP探测排队超时"), want: "TCP探测排队超时"},
		{name: "panel queue expired", err: errTCPPingQueueTimeout, want: "服务器上的 TCP 探测排队超时"},
		{name: "execution expired", err: errors.New("TCP探测执行超时（含排队等待）"), want: "TCP探测执行超时（含排队等待）"},
		{name: "offline", err: errors.New("节点不在线"), want: "服务器离线"},
		{name: "command timeout", err: errors.New("等待节点响应超时"), want: "服务器响应超时"},
		{name: "deadline", err: context.DeadlineExceeded, want: "服务器响应超时"},
		{name: "cancel", err: context.Canceled, want: "探测已取消"},
		{name: "shutdown", err: errors.New("Agent is shutting down"), want: "探测已取消"},
		{name: "unexpected failure", err: errors.New("节点未返回诊断数据"), want: "探测失败：节点未返回诊断数据"},
		{name: "dns", data: map[string]interface{}{"success": false, "errorMessage": "DNS解析失败: no such host"}, want: "域名解析失败"},
		{name: "target unreachable", data: map[string]interface{}{"success": false, "errorMessage": "所有TCP连接尝试都失败"}, want: "测试点连接失败"},
		{name: "invalid target", data: map[string]interface{}{"success": false, "errorMessage": "无效的IP地址或主机名"}, want: "无效的IP地址或主机名"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := latencyPointFrom(latencyTarget{Name: "test"}, tt.data, tt.err)
			if got.OK || got.Error != tt.want {
				t.Fatalf("point = %+v, want error %q", got, tt.want)
			}
		})
	}
}

func TestInboundProbeViewUsesNodeLatency(t *testing.T) {
	h, _ := setupLatencyHandler(t, "")
	h.nodeLatency.replace(map[int64]reachProbe{
		5: latencyProbe([]latencyPoint{{Name: "电信", Error: "超时"}}, 1),
	}, true)
	built := &model.NodeInbound{ID: 1, NodeID: 5, KernelID: 9}

	// No panel check yet: the node's points, reachable.
	v := h.inboundProbeView(built)
	if v == nil || !v.OK || len(v.Points) != 1 || v.Error != "" {
		t.Fatalf("view = %+v", v)
	}

	// A port the panel found down wins over the node's points.
	h.reachProbes.replace(map[int64]reachProbe{1: {Error: "连接超时", CheckedAt: 2}}, true)
	if v := h.inboundProbeView(built); v == nil || v.OK || v.Error != "连接超时" || len(v.Points) != 0 {
		t.Fatalf("down port view = %+v", v)
	}

	// Skipped panel checks (UDP, whitelist) still show the node's points.
	h.reachProbes.replace(map[int64]reachProbe{1: {Skipped: true, CheckedAt: 2}}, true)
	if v := h.inboundProbeView(built); v == nil || !v.OK || len(v.Points) != 1 {
		t.Fatalf("skipped view = %+v", v)
	}

	// A node without measurements falls back to the panel check.
	other := &model.NodeInbound{ID: 2, NodeID: 6, KernelID: 9}
	h.reachProbes.replace(map[int64]reachProbe{2: {OK: true, LatencyMs: 80, CheckedAt: 2}}, true)
	if v := h.inboundProbeView(other); v == nil || v.LatencyMs != 80 || len(v.Points) != 0 {
		t.Fatalf("fallback view = %+v", v)
	}
}
