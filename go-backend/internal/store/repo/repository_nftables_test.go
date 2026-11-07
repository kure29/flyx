package repo

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNftablesNodeModeSSHConfigAndBindingPersistence(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.CreateNode(
		"nft-node",
		"secret",
		"203.0.113.10",
		nil,
		nil,
		"10000-20000",
		nil,
		nil,
		nil,
		nil,
		nil,
		0,
		0,
		0,
		now,
		1,
		"[::]",
		"[::]",
		1,
		0,
		nil,
		nil,
		nil,
		nil,
		"nftables",
		nil,
	); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	nodes, err := r.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	nodeID := nodes[0]["id"].(int64)
	if got := nodes[0]["forwardMode"]; got != "nftables" {
		t.Fatalf("expected forwardMode nftables, got %#v", got)
	}

	cfg := NftSSHConfigInput{
		Host:       "203.0.113.10",
		Port:       22,
		Username:   "root",
		AuthType:   "private_key",
		PrivateKey: "encrypted-private-key",
		SudoMode:   "none",
	}
	if err := r.UpsertNodeSSHConfig(nodeID, cfg, now); err != nil {
		t.Fatalf("UpsertNodeSSHConfig: %v", err)
	}
	loaded, err := r.GetNodeSSHConfig(nodeID)
	if err != nil {
		t.Fatalf("GetNodeSSHConfig: %v", err)
	}
	if loaded.Host != cfg.Host || loaded.Port != cfg.Port || loaded.Username != cfg.Username || loaded.AuthType != cfg.AuthType {
		t.Fatalf("unexpected ssh config: %+v", loaded)
	}

	binding := NftRuleBindingInput{
		ForwardID:  42,
		NodeID:     nodeID,
		InPort:     24000,
		Protocols:  "tcp,udp",
		TargetAddr: "198.51.100.20:443",
		BindIP:     "",
		RuleHash:   "hash-a",
		Status:     "applied",
		LastError:  "",
	}
	if err := r.UpsertNftRuleBinding(binding, now); err != nil {
		t.Fatalf("UpsertNftRuleBinding: %v", err)
	}
	bindings, err := r.ListNftRuleBindingsByNode(nodeID)
	if err != nil {
		t.Fatalf("ListNftRuleBindingsByNode: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings))
	}
	if bindings[0].ForwardID != 42 || bindings[0].RuleHash != "hash-a" || bindings[0].Status != "applied" {
		t.Fatalf("unexpected binding: %+v", bindings[0])
	}

	if err := r.MarkNftRuleBindingError(42, nodeID, "nft failed", now+1); err != nil {
		t.Fatalf("MarkNftRuleBindingError: %v", err)
	}
	bindings, err = r.ListNftRuleBindingsByNode(nodeID)
	if err != nil {
		t.Fatalf("ListNftRuleBindingsByNode after error: %v", err)
	}
	if bindings[0].Status != "error" || !strings.Contains(bindings[0].LastError, "nft failed") {
		t.Fatalf("expected error binding, got %+v", bindings[0])
	}

	if err := r.DeleteNftRuleBindingsByForward(42); err != nil {
		t.Fatalf("DeleteNftRuleBindingsByForward: %v", err)
	}
	bindings, err = r.ListNftRuleBindingsByNode(nodeID)
	if err != nil {
		t.Fatalf("ListNftRuleBindingsByNode after delete: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected no bindings after delete, got %+v", bindings)
	}
}

func TestNftablesNodeSSHConfigSurvivesRepositoryReopen(t *testing.T) {
	tests := []struct {
		name string
		cfg  NftSSHConfigInput
	}{
		{
			name: "password",
			cfg: NftSSHConfigInput{
				Host:       "203.0.113.10",
				Port:       2222,
				Username:   "root",
				AuthType:   "password",
				Password:   "ssh-password",
				Passphrase: "key-passphrase",
				SudoMode:   "sudo",
			},
		},
		{
			name: "private-key",
			cfg: NftSSHConfigInput{
				Host:       "203.0.113.11",
				Port:       2223,
				Username:   "admin",
				AuthType:   "private_key",
				PrivateKey: "PRIVATE-KEY-SHOULD-PERSIST",
				Passphrase: "key-passphrase",
				SudoMode:   "sudo_su",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "nftables-ssh.sqlite")
			r, err := Open(dbPath)
			if err != nil {
				t.Fatalf("open repo: %v", err)
			}

			now := time.Now().UnixMilli()
			if err := r.CreateNode(
				"nft-node",
				"secret",
				tt.cfg.Host,
				nil,
				nil,
				"10000-20000",
				nil,
				nil,
				nil,
				nil,
				nil,
				0,
				0,
				0,
				now,
				1,
				"[::]",
				"[::]",
				1,
				0,
				nil,
				nil,
				nil,
				nil,
				"nftables",
				nil,
			); err != nil {
				t.Fatalf("CreateNode: %v", err)
			}

			nodes, err := r.ListNodes()
			if err != nil {
				t.Fatalf("ListNodes: %v", err)
			}
			nodeID := nodes[0]["id"].(int64)
			if err := r.UpsertNodeSSHConfig(nodeID, tt.cfg, now); err != nil {
				t.Fatalf("UpsertNodeSSHConfig: %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatalf("close repo: %v", err)
			}

			reopened, err := Open(dbPath)
			if err != nil {
				t.Fatalf("reopen repo: %v", err)
			}
			defer reopened.Close()

			loaded, err := reopened.GetNodeSSHConfig(nodeID)
			if err != nil {
				t.Fatalf("GetNodeSSHConfig after reopen: %v", err)
			}
			if loaded.Host != tt.cfg.Host || loaded.Port != tt.cfg.Port || loaded.Username != tt.cfg.Username || loaded.AuthType != tt.cfg.AuthType || loaded.SudoMode != tt.cfg.SudoMode {
				t.Fatalf("unexpected ssh config after reopen: %+v", loaded)
			}
			if tt.cfg.Password != "" && (!loaded.Password.Valid || loaded.Password.String != tt.cfg.Password) {
				t.Fatalf("expected password to persist after reopen, got %+v", loaded.Password)
			}
			if tt.cfg.PrivateKey != "" && (!loaded.PrivateKey.Valid || loaded.PrivateKey.String != tt.cfg.PrivateKey) {
				t.Fatalf("expected private key to persist after reopen, got %+v", loaded.PrivateKey)
			}
			if tt.cfg.Passphrase != "" && (!loaded.Passphrase.Valid || loaded.Passphrase.String != tt.cfg.Passphrase) {
				t.Fatalf("expected passphrase to persist after reopen, got %+v", loaded.Passphrase)
			}
		})
	}
}

func TestUpdateNodeWithoutForwardModePreservesExistingMode(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.CreateNode(
		"nft-node",
		"secret",
		"203.0.113.11",
		nil,
		nil,
		"10000-20000",
		nil,
		nil,
		nil,
		nil,
		nil,
		0,
		0,
		0,
		now,
		1,
		"[::]",
		"[::]",
		1,
		0,
		nil,
		nil,
		nil,
		nil,
		"nftables",
		nil,
	); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	nodes, err := r.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	nodeID := nodes[0]["id"].(int64)

	if err := r.UpdateNode(
		nodeID,
		"nft-node-updated",
		"203.0.113.11",
		nil,
		nil,
		"10000-20000",
		nil,
		nil,
		nil,
		nil,
		nil,
		"",
		0,
		0,
		0,
		"[::]",
		"[::]",
		now+1,
		nil,
	); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	gotNode, err := r.GetNodeRecord(nodeID)
	if err != nil {
		t.Fatalf("GetNodeRecord: %v", err)
	}
	if gotNode == nil {
		t.Fatal("expected node record, got nil")
	}
	if gotNode.ForwardMode != "nftables" {
		t.Fatalf("expected mapped forward mode nftables, got %q", gotNode.ForwardMode)
	}

	nodes, err = r.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes after update: %v", err)
	}
	if got := nodes[0]["forwardMode"]; got != "nftables" {
		t.Fatalf("expected persisted forwardMode nftables after update, got %#v", got)
	}
}

// Nodes configured before the upgrade may own flvx tables; nodes added
// afterwards never do, since on a shared server FLVX uses those names.
func TestLegacyNftTablesBackfillOnlyExistingNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	r, err := Open(path)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	cfg := NftSSHConfigInput{Host: "203.0.113.10", Username: "root", AuthType: "password", Password: "pw"}
	if err := r.UpsertNodeSSHConfig(1, cfg, 1); err != nil {
		t.Fatalf("UpsertNodeSSHConfig: %v", err)
	}
	if err := r.db.Exec("ALTER TABLE node_ssh_config DROP COLUMN legacy_nft_tables").Error; err != nil {
		t.Fatalf("simulate pre-upgrade schema: %v", err)
	}
	_ = r.Close()

	r, err = Open(path)
	if err != nil {
		t.Fatalf("reopen repo: %v", err)
	}
	defer r.Close()
	if err := r.UpsertNodeSSHConfig(2, cfg, 2); err != nil {
		t.Fatalf("UpsertNodeSSHConfig: %v", err)
	}
	if err := r.UpsertNodeSSHConfig(1, cfg, 3); err != nil {
		t.Fatalf("UpsertNodeSSHConfig update: %v", err)
	}
	for nodeID, want := range map[int64]bool{1: true, 2: false} {
		got, err := r.GetNodeSSHConfig(nodeID)
		if err != nil {
			t.Fatalf("GetNodeSSHConfig(%d): %v", nodeID, err)
		}
		if got.LegacyNftTables != want {
			t.Fatalf("node %d: LegacyNftTables=%v, want %v", nodeID, got.LegacyNftTables, want)
		}
	}
}
