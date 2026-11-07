package nftables

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaBlocksEstablishedTrafficIntegration(t *testing.T) {
	if os.Getenv("FLYX_NFT_NETWORK_TEST") != "1" {
		t.Skip("opt-in Linux namespace test: FLYX_NFT_NETWORK_TEST=1")
	}
	dir := t.TempDir()
	plan := NodePlan{Rules: []Rule{
		{ForwardID: 9223372036854775807, InPort: 20000, TargetHost: "10.250.2.2", TargetPort: 8080},
		{ForwardID: 2, InPort: 20001, TargetHost: "fd00:2::2", TargetPort: 8080},
	}}
	paths := []string{filepath.Join(dir, "init.sh"), filepath.Join(dir, "block.sh"), filepath.Join(dir, "resume.sh")}
	scripts := []string{buildApplyCommand("nft", RenderTable(plan), false),
		buildSetBlockedCommand("nft", 20000, true, false) + "\n" + buildSetBlockedCommand("nft", 20001, true, false),
		buildSetBlockedCommand("nft", 20000, false, false) + "\n" + buildSetBlockedCommand("nft", 20001, false, false)}
	for i, script := range scripts {
		if err := os.WriteFile(paths[i], []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "unshare", "-Urn", "python3", "testdata/quota_network.py", paths[0], paths[1], paths[2])
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("namespace traffic test: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
}
