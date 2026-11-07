package handler

import (
	"testing"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

// A relay's share host is what subscriptions put as the server address; the
// landing node's own address there makes clients skip the relay (issue #203).
func TestValidateRelayShareHost(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/relay-share-host.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	front := seedRelayTargetNode(t, r, "front", "198.51.100.1")
	landing := seedRelayTargetNode(t, r, "landing", "203.0.113.9")
	viaFront := seedRelayTargetTunnel(t, r, "via-front", front, landing)
	onLanding := seedRelayTargetTunnel(t, r, "on-landing", landing, landing)
	row := &model.NodeInbound{NodeID: landing, Listen: "::", Port: 443}

	cases := []struct {
		tunnel  int64
		host    string
		wantErr bool
	}{
		{viaFront, "", false},
		{viaFront, "198.51.100.1", false},
		{viaFront, "relay.example.com", false},
		{viaFront, "203.0.113.9", true},
		{viaFront, " [203.0.113.9] ", true},
		{onLanding, "203.0.113.9", false}, // entry is the landing node itself
	}
	for _, c := range cases {
		err := h.validateRelayShareHost(row, c.tunnel, c.host)
		if (err != nil) != c.wantErr {
			t.Errorf("tunnel=%d host=%q: err=%v, wantErr=%v", c.tunnel, c.host, err, c.wantErr)
		}
	}
}
