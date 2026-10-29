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
		"table inet flyx_kernel\ntable inet flvx_kernel\n",
		"signal: segmentation fault (core dumped)",
		"permission denied",
	} {
		t.Run(output, func(t *testing.T) {
			rec := stubNft(t)
			runner := nftRunner
			nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
				if strings.Join(args, " ") == "list tables" {
					if !strings.HasPrefix(output, "table ") {
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
		raw := []byte("table inet " + table + " {\n\tcounter uup_7 {\n\t\tpackets 1 bytes 123\n\t}\n}\n")
		_, users, err := parseCounters(raw)
		if err != nil || len(users) != 1 || users[0].Bytes != 123 {
			t.Fatalf("%s: %+v %v", table, users, err)
		}
	}
}

// testdata/nft-list-counters.txt is real `nft list counters table inet
// flyx_kernel` output; nft 1.0.6, 1.0.9 and 1.1.3 print it identically.
func TestParseCountersFromNftTextListing(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "nft-list-counters.txt"))
	if err != nil {
		t.Fatal(err)
	}
	counters, users, err := parseCounters(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(counters) != 6 || len(users) != 4 {
		t.Fatalf("counters=%+v users=%+v", counters, users)
	}
	found := false
	for _, c := range counters {
		if c == (Counter{InboundID: 2, Proto: "udp", Dir: "in", Bytes: 102}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing in_2_udp bytes: %+v", counters)
	}
	if _, _, err := parseCounters([]byte("table inet flyx_kernel {\n\tcounter in_1_tcp {\n\t\tpackets 1 bytes x\n\t}\n}\n")); err == nil {
		t.Fatal("malformed byte count accepted")
	}
}

func TestUninstallRemovesCurrentAndLegacyTables(t *testing.T) {
	var deleted []string
	runner, available := nftRunner, nftAvailable
	t.Cleanup(func() { nftRunner, nftAvailable = runner, available })
	nftAvailable = func() error { return nil }
	nftRunner = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch joined := strings.Join(args, " "); {
		case joined == "list tables":
			return []byte("table inet flyx_kernel\ntable inet flvx_kernel\ntable inet firewalld\ntable ip nat\n"), nil
		case strings.HasPrefix(joined, "delete table inet "):
			deleted = append(deleted, args[3])
			return nil, nil
		default:
			return nil, errors.New("unexpected nft call: " + joined)
		}
	}
	m := NewManager(t.TempDir())
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	// Tables of other software are never touched.
	if strings.Join(deleted, ",") != "flyx_kernel,flvx_kernel" {
		t.Fatalf("deleted %v", deleted)
	}

	nftAvailable = func() error { return errors.New("nft missing") }
	if err := NewManager(t.TempDir()).Uninstall(); err != nil {
		t.Fatalf("missing nft must not block the uninstall: %v", err)
	}
}
