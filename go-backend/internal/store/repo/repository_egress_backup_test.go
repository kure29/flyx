package repo

import (
	"path/filepath"
	"testing"

	"go-backend/internal/store/model"
)

func TestBackupRoundTripsOutboundIPStrategy(t *testing.T) {
	source, err := Open(filepath.Join(t.TempDir(), "source.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for i, policy := range []string{"auto", "ipv4_only", "ipv6_only", "prefer_ipv4", "prefer_ipv6"} {
		in := model.NodeInbound{ID: int64(i + 1), KernelID: 1, NodeID: 1, Name: policy,
			Protocol: "shadowsocks", Listen: "::", Port: 8388 + i, OutboundIPStrategy: policy}
		if err := source.CreateNodeInbound(&in); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := source.ExportPartial([]string{"inbounds"})
	if err != nil {
		t.Fatal(err)
	}
	if backup.Inbounds == nil || len(backup.Inbounds.Inbounds) != 5 {
		t.Fatalf("incomplete backup: %+v", backup.Inbounds)
	}
	dest, err := Open(filepath.Join(t.TempDir(), "dest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dest.Close()
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := dest.Import(backup, []string{"inbounds"}); err != nil {
			t.Fatal(err)
		}
		for _, item := range backup.Inbounds.Inbounds {
			got, err := dest.GetNodeInbound(item.ID)
			if err != nil || got == nil || got.OutboundIPStrategy != item.Name {
				t.Fatalf("policy lost on export/import: %+v %v", got, err)
			}
		}
		// The second import must update an existing record's policy.
		for _, item := range backup.Inbounds.Inbounds {
			got, err := dest.GetNodeInbound(item.ID)
			if err != nil || got == nil {
				t.Fatalf("missing restored inbound: %v", err)
			}
			got.OutboundIPStrategy = "auto"
			if err := dest.SaveNodeInbound(got); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A legacy backup without the field restores automatic egress.
	backup.Inbounds.Inbounds[0].OutboundIPStrategy = ""
	if _, err := dest.Import(backup, []string{"inbounds"}); err != nil {
		t.Fatal(err)
	}
	got, err := dest.GetNodeInbound(backup.Inbounds.Inbounds[0].ID)
	if err != nil || got.OutboundIPStrategy != "auto" {
		t.Fatalf("legacy policy: %+v %v", got, err)
	}
}
