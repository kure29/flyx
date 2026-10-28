package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNftIdentityUsesLiveTablesWhenStateIsLost(t *testing.T) {
	for _, state := range []string{"", "{broken"} {
		t.Run("state="+state, func(t *testing.T) {
			rec := stubNft(t)
			rec.scripts = []string{"table inet " + legacyNftTable + " {}"}
			dir := t.TempDir()
			if state != "" {
				if err := os.WriteFile(filepath.Join(dir, "nft.json"), []byte(state), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			n := newNftCounters(dir)
			if err := n.sync([]Port{{InboundID: 7, Port: 443, Network: "tcp"}}, nil); err != nil {
				t.Fatal(err)
			}
			if n.state.Table != legacyNftTable || strings.Contains(rec.scripts[1], nftTable) {
				t.Fatal("lost state created a second table beside the legacy rules")
			}
			if n.state.Epoch == "" || newNftCounters(dir).state.Table != legacyNftTable {
				t.Fatal("recovered identity and new counter epoch were not persisted")
			}
		})
	}
}

func TestNftIdentityRefusesAmbiguousOrUnreadableTables(t *testing.T) {
	for _, output := range []string{
		`{"nftables":[{"table":{"family":"inet","name":"flyx_kernel"}},{"table":{"family":"inet","name":"flvx_kernel"}}]}`,
		`{broken`,
		"permission denied",
	} {
		t.Run(output, func(t *testing.T) {
			rec := stubNft(t)
			runner := nftRunner
			nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
				if strings.Join(args, " ") == "-j list tables" {
					if output == "permission denied" {
						return nil, errors.New(output)
					}
					return []byte(output), nil
				}
				return runner(ctx, stdin, args...)
			}
			n := newNftCounters(t.TempDir())
			if err := n.sync([]Port{{InboundID: 7, Port: 443, Network: "tcp"}}, nil); err == nil {
				t.Fatal("unsafe table selection accepted")
			}
			if len(rec.scripts) != 0 {
				t.Fatal("rules were changed after failed preflight")
			}
		})
	}
}

func TestNftIdentityApplyFailureKeepsPreviousEpoch(t *testing.T) {
	rec := stubNft(t)
	rec.scripts = []string{"table inet " + legacyNftTable + " {}"}
	runner := nftRunner
	nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		if stdin != "" {
			return nil, errors.New("apply failed")
		}
		return runner(ctx, stdin, args...)
	}
	n := newNftCounters(t.TempDir())
	n.state.Epoch = "previous-epoch"
	before := n.state
	if err := n.sync([]Port{{InboundID: 7, Port: 443, Network: "tcp"}}, nil); err == nil {
		t.Fatal("apply failure was ignored")
	}
	if n.state != before {
		t.Fatal("failed apply paired an existing epoch with a different table")
	}
}

func TestLegacyNftIdentityDoesNotResetCountersOnUpgrade(t *testing.T) {
	rec := stubNft(t)
	ports := []Port{{InboundID: 7, Port: 443, Network: "tcp"}}
	script := strings.ReplaceAll(nftScript(expandPorts(ports), expandGuards(ports), nil, nil), nftTable, legacyNftTable)
	rec.scripts = []string{script}
	dir := t.TempDir()
	state := map[string]string{"signature": hashString(script), "epoch": "existing-epoch"}
	raw, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(dir, "nft.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	n := newNftCounters(dir)
	if n.state.Table != legacyNftTable {
		t.Fatalf("table = %q", n.state.Table)
	}
	if err := n.sync(ports, nil); err != nil {
		t.Fatal(err)
	}
	if n.state.Epoch != "existing-epoch" {
		t.Fatal("upgrade reset the counter epoch")
	}
	if len(rec.scripts) != 1 {
		t.Fatal("unchanged legacy rules were reapplied")
	}
}

func TestNewNftIdentity(t *testing.T) {
	n := newNftCounters(t.TempDir())
	if n.state.Table != nftTable {
		t.Fatalf("new table = %q", n.state.Table)
	}
	for _, table := range []string{nftTable, legacyNftTable} {
		raw := []byte(`{"nftables":[{"counter":{"name":"uup_7","table":"` + table + `","bytes":123}}]}`)
		_, users, err := parseCounters(raw)
		if err != nil || len(users) != 1 || users[0].Bytes != 123 {
			t.Fatalf("%s: %+v %v", table, users, err)
		}
	}
}
