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
			out, err := exec.Command("sh", "-c", dockerInstallCommand("3.4.0", "panel.example.com:6365", secret, false)).CombinedOutput()
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

func TestDockerInstallCommandPrivileged(t *testing.T) {
	plain := dockerInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", false)
	if strings.Contains(plain, "--privileged") {
		t.Errorf("unexpected --privileged in %s", plain)
	}

	privileged := dockerInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", true)
	if !strings.Contains(privileged, "--cap-add NET_ADMIN --privileged") {
		t.Errorf("missing --privileged in %s", privileged)
	}
}

func TestDockerComposeInstallCommand(t *testing.T) {
	old := githubRepo
	githubRepo = "Kure29/flyx"
	defer func() { githubRepo = old }()

	cmd := dockerComposeInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", false)
	for _, want := range []string{
		"[ -d /opt/docker/flyx-agent ] || mkdir -p /opt/docker/flyx-agent",
		"/opt/docker/flyx-agent/docker-compose.yaml",
		"image: ghcr.io/kure29/flyx-agent:3.4.0",
		"container_name: flyx-agent",
		"restart: unless-stopped",
		"network_mode: host",
		"      - NET_ADMIN",
		"      - /etc/flyx-agent:/etc/flyx-agent",
		`FLYX_AGENT_DATA_DIR: "/etc/flyx-agent"`,
		`FLYX_AGENT_ADDR: "panel.example.com:6365"`,
		`FLUX_AGENT_ADDR: "panel.example.com:6365"`,
		`FLYX_AGENT_SECRET: "s3cret"`,
		`FLUX_AGENT_SECRET: "s3cret"`,
		"chmod 600 /opt/docker/flyx-agent/docker-compose.yaml",
		"if docker container inspect flux_agent >/dev/null 2>&1; then",
		"检测到旧版 flux_agent 容器",
		"if docker container inspect flyx-agent >/dev/null 2>&1; then",
		"docker rm -f flyx-agent >/dev/null",
		"docker compose -f /opt/docker/flyx-agent/docker-compose.yaml up -d",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "privileged: true") {
		t.Errorf("unexpected privileged mode in %s", cmd)
	}
	for _, unwanted := range []string{
		"/opt/docker/docker-compose.yaml",
		"container_name: flux_agent",
		"/etc/flux_agent:/etc/flux_agent",
		"image: ghcr.io/kure29/flux-agent:3.4.0",
	} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("unexpected %q in %s", unwanted, cmd)
		}
	}

	privileged := dockerComposeInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", true)
	if !strings.Contains(privileged, "    privileged: true") {
		t.Errorf("missing privileged mode in %s", privileged)
	}
}
