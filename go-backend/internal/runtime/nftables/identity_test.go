package nftables

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuleIdentityPreservesLegacyTablesAndRejectsAmbiguity(t *testing.T) {
	for _, table := range []string{"", "flyx", "flvx", "both"} {
		t.Run(table, func(t *testing.T) {
			dir := t.TempDir()
			batch := filepath.Join(dir, "batch")
			nft := filepath.Join(dir, "nft")
			fake := `#!/bin/sh
if [ "$1 $2" = 'list tables' ]; then exit 0; fi
if [ "$1 $2" = 'list table' ]; then
  [ "$FLYX_TEST_TABLE" = both ] || [ "$4" = "$FLYX_TEST_TABLE" ]
  exit $?
fi
if [ "$1" = '-f' ]; then cp "$2" "$FLYX_TEST_BATCH"; fi
`
			if err := os.WriteFile(nft, []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("FLYX_TEST_TABLE", table)
			t.Setenv("FLYX_TEST_BATCH", batch)
			script := RenderTable(NodePlan{Rules: []Rule{{ForwardID: 1, InPort: 1234, TargetHost: "127.0.0.1", TargetPort: 5678}}})
			out, err := exec.Command("sh", "-c", buildApplyCommand(nft, script)).CombinedOutput()
			if table == "both" {
				if err == nil {
					t.Fatal("ambiguous tables were accepted")
				}
				if _, err := os.Stat(batch); !os.IsNotExist(err) {
					t.Fatal("rules were applied despite ambiguity")
				}
				return
			}
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			data, err := os.ReadFile(batch)
			if err != nil {
				t.Fatal(err)
			}
			want := table
			if want == "" {
				want = "flyx"
			}
			if !strings.Contains(string(data), "table inet "+want+" {") || !strings.Contains(string(data), want+" forward:1") {
				t.Fatalf("wrong table/comment identity: %s", data)
			}
		})
	}
}

func TestCounterIdentityAliases(t *testing.T) {
	for _, prefix := range []string{"flyx", "flvx"} {
		sample, ok := ParseCounterComment(prefix + " forward:42 to-target tcp epoch")
		if !ok || sample.ForwardID != 42 || sample.Epoch != "epoch" {
			t.Fatalf("%s: %+v", prefix, sample)
		}
	}
}

func TestLegacyQuotaResumeUsesExistingTable(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	nft := filepath.Join(dir, "nft")
	fake := `#!/bin/sh
echo "$*" >> "$FLYX_TEST_LOG"
if [ "$1 $2" = 'list table' ]; then [ "$4" = flvx_quota ]; exit $?; fi
exit 0
`
	if err := os.WriteFile(nft, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLYX_TEST_LOG", log)
	if out, err := exec.Command("sh", "-c", buildSetBlockedCommand(nft, 443, false)).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "delete element inet flvx_quota blocked_ports { 443 }") || strings.Contains(string(data), "delete element inet flyx_quota") {
		t.Fatalf("wrong quota resumed: %s", data)
	}
}
