package handler

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Execute the generated command with an isolated Docker stub: exercise shell
// quoting, old mounts, image fallback and the order of destructive operations.
func TestDockerInstallCommand(t *testing.T) {
	old := githubRepo
	githubRepo = "Kure29/flyx"
	defer func() { githubRepo = old }()
	for _, tc := range []struct {
		name, existing, mount, pullFailure, wantName, wantMount, wantImage string
		fail                                                               bool
	}{
		{name: "fresh", wantName: "flyx-agent", wantMount: "/etc/flyx-agent:/etc/flyx-agent", wantImage: "flyx-agent"},
		{name: "legacy", existing: "flux_agent", mount: "/srv/old agent", wantName: "flux_agent", wantMount: "/srv/old agent:/etc/flux_agent", wantImage: "flyx-agent"},
		{name: "repeat", existing: "flyx-agent", mount: "agent-volume", wantName: "flyx-agent", wantMount: "agent-volume:/etc/flyx-agent", wantImage: "flyx-agent"},
		{name: "historical-image", pullFailure: "canonical", wantName: "flyx-agent", wantMount: "/etc/flyx-agent:/etc/flux_agent", wantImage: "flux-agent"},
		{name: "ambiguous", existing: "flyx-agent,flux_agent", fail: true},
		{name: "missing-mount", existing: "flux_agent", fail: true},
		{name: "multiple-mounts", existing: "flux_agent", mount: "one\ntwo", fail: true},
		{name: "pull-failure", existing: "flux_agent", mount: "keep-me", pullFailure: "all", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "calls.jsonl")
			fake := `#!/usr/bin/env python3
import json,os,sys
args=sys.argv[1:]
with open(os.environ["FLYX_TEST_LOG"],"a") as f:f.write(json.dumps(args)+"\n")
if args[0]=="inspect":
 if args[2]=="{{.Name}}":
  if args[-1] in os.environ["FLYX_TEST_EXISTING"].split(","):print("/"+args[-1])
  else:sys.exit(1)
 else:print(os.environ["FLYX_TEST_MOUNT"])
elif args[0]=="pull":
 failure=os.environ["FLYX_TEST_PULL_FAILURE"]
 if failure=="all" or (failure=="canonical" and "/flyx-agent:" in args[1]):sys.exit(1)
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("FLYX_TEST_LOG", log)
			t.Setenv("FLYX_TEST_EXISTING", tc.existing)
			t.Setenv("FLYX_TEST_MOUNT", tc.mount)
			t.Setenv("FLYX_TEST_PULL_FAILURE", tc.pullFailure)
			secret := "secret with '$() and spaces"
			out, err := exec.Command("sh", "-c", dockerInstallCommand("3.4.0", "panel.example.com:6365", secret)).CombinedOutput()
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v output=%s", err, out)
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var run []string
			pulled := false
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				var args []string
				if err := json.Unmarshal([]byte(line), &args); err != nil {
					t.Fatal(err)
				}
				if args[0] == "pull" {
					pulled = true
				}
				if args[0] == "rm" || args[0] == "run" {
					if tc.fail {
						t.Fatalf("mutated Docker after failed preflight: %v", args)
					}
					if !pulled {
						t.Fatal("changed container before pulling the image")
					}
				}
				if args[0] == "run" {
					run = args
				}
			}
			if tc.fail {
				return
			}
			for _, want := range []string{tc.wantName, tc.wantMount, "FLYX_AGENT_SECRET=" + secret, "FLUX_AGENT_SECRET=" + secret, "ghcr.io/kure29/" + tc.wantImage + ":3.4.0"} {
				if !slices.Contains(run, want) {
					t.Fatalf("missing argument %q in %q", want, run)
				}
			}
		})
	}
}
