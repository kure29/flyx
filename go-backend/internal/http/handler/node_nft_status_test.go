package handler

import (
	"testing"

	"go-backend/internal/store/repo"
)

func TestSetNodeNftError(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/node-nft.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	id := seedRelayTargetNode(t, r, "lxc", "198.51.100.7")

	msg := "本机没有 nftables 权限（常见于 LXC、OpenVZ 等容器）"
	if changed, err := r.SetNodeNftError(id, msg); err != nil || !changed {
		t.Fatalf("first report must change the node: %v %v", changed, err)
	}
	if changed, _ := r.SetNodeNftError(id, msg); changed {
		t.Fatal("the same report must not count as a change")
	}
	node, _ := r.GetNodeByID(id)
	if node == nil || node.NftError != msg {
		t.Fatalf("nft error not stored: %+v", node)
	}
	items, err := r.ListNodes()
	if err != nil || len(items) != 1 || items[0]["nftError"] != msg {
		t.Fatalf("node list must expose nftError: %v %v", items, err)
	}
	if _, err := r.SetNodeNftError(id, ""); err != nil {
		t.Fatal(err)
	}
	if node, _ = r.GetNodeByID(id); node.NftError != "" {
		t.Fatalf("nft error not cleared: %q", node.NftError)
	}
}

// A relay whose forward is paused or deleted is left out of subscriptions;
// the preview must say so instead of letting it vanish.
func TestInactiveRelays(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/inactive-relays.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	entry := seedRelayTargetNode(t, r, "entry", "198.51.100.1")

	active := mustCreateRelayLine(t, r, "active", 1, "203.0.113.1:443", entry, nil, 0)
	paused := mustCreateRelayLine(t, r, "paused", 1, "203.0.113.2:443", entry, nil, 0)
	deleted := mustCreateRelayLine(t, r, "deleted", 1, "203.0.113.3:443", entry, nil, 0)
	db := r.DB()
	if err := db.Exec(`UPDATE forward SET status = 0 WHERE id = (SELECT forward_id FROM node_inbound_relay WHERE id = ?)`, paused).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DELETE FROM forward WHERE id = (SELECT forward_id FROM node_inbound_relay WHERE id = ?)`, deleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE node_inbound SET enabled = 1`).Error; err != nil {
		t.Fatal(err)
	}
	var inbounds []int64
	if err := db.Raw(`SELECT inbound_id FROM node_inbound_relay WHERE id IN (?, ?, ?) ORDER BY id`, active, paused, deleted).Scan(&inbounds).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := r.ListNodeInbounds()
	if err != nil {
		t.Fatal(err)
	}
	got := h.inactiveRelays(&subContext{rows: rows}, inbounds)
	if len(got) != 2 || got[0] != (inactiveRelay{Name: "paused", Reason: "转发已暂停"}) || got[1] != (inactiveRelay{Name: "deleted", Reason: "转发已删除"}) {
		t.Fatalf("inactive relays = %+v", got)
	}
	// Inbounds outside the subscription are not reported.
	if got := h.inactiveRelays(&subContext{rows: rows}, inbounds[:1]); len(got) != 0 {
		t.Fatalf("relays of other inbounds reported: %+v", got)
	}
}

func TestNftCannotMark(t *testing.T) {
	for msg, want := range map[string]bool{
		// 3.5.5+ agents
		"本机没有 nftables 权限（常见于 LXC、OpenVZ 等容器），无法使用流量统计、入站白名单和端口跳跃": true,
		// older agents pass nft's own error through
		"nft: Operation not permitted (you must be root)\nnetlink: Error: cache initialization failed: Operation not permitted": true,
		"nft: Permission denied": true,
		// nft missing says nothing about socket marks
		"未安装 nftables，无法统计流量":            false,
		"nft: context deadline exceeded": false,
		"":                               false,
	} {
		if got := nftCannotMark(msg); got != want {
			t.Errorf("nftCannotMark(%q) = %v, want %v", msg, got, want)
		}
	}
}
