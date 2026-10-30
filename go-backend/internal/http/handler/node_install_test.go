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
		// flux-panel / FLVX agents share the container name; they are left alone.
		{name: "foreign-legacy", existing: "flux_agent=ghcr.io/sagit-chu/flux-agent:2.0", mount: "/etc/flux_agent", wantName: "flyx-agent", wantMount: "/etc/flyx-agent:/etc/flyx-agent", wantImage: "flyx-agent"},
		{name: "foreign-legacy-and-current", existing: "flyx-agent,flux_agent=ghcr.io/sagit-chu/flux-agent:2.0", mount: "agent-volume", wantName: "flyx-agent", wantMount: "agent-volume:/etc/flyx-agent", wantImage: "flyx-agent"},
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
existing=dict((x.split("=")+["ghcr.io/kure29/flux-agent:3.3.0"])[:2] for x in os.environ["FLYX_TEST_EXISTING"].split(",") if x)
if args[0]=="inspect":
 if args[2]=="{{.Name}}" or args[2]=="{{.Config.Image}}":
  if args[-1] not in existing:sys.exit(1)
  print("/"+args[-1] if args[2]=="{{.Name}}" else existing[args[-1]])
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
				if args[0] == "rm" && args[len(args)-1] == "flux_agent" && tc.wantName != "flux_agent" {
					t.Fatalf("removed another panel's container: %v", args)
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
	want := strings.Replace(plain, "--cap-add NET_ADMIN", "--cap-add NET_ADMIN --privileged", 1)
	if privileged != want {
		t.Fatalf("privileged command changed unexpected arguments:\n got: %s\nwant: %s", privileged, want)
	}
}

func TestDockerComposeInstallCommand(t *testing.T) {
	old := githubRepo
	githubRepo = "Kure29/flyx"
	defer func() { githubRepo = old }()

	cmd := dockerComposeInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", false)
	for _, want := range []string{
		"mkdir -p /opt/docker/flyx-agent",
		"(umask 077 && cat > /opt/docker/flyx-agent/docker-compose.yaml) <<'EOF'",
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
		`case "$(docker inspect -f '{{.Config.Image}}' flux_agent 2>/dev/null || true)" in ghcr.io/kure29/*)`,
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

func TestDockerComposeInstallChecksLegacyContainerBeforeWriting(t *testing.T) {
	cmd := dockerComposeInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", false)
	legacy := strings.Index(cmd, "docker inspect -f '{{.Config.Image}}' flux_agent")
	write := strings.Index(cmd, "cat > /opt/docker/flyx-agent/docker-compose.yaml")
	if legacy < 0 || write < 0 || legacy > write {
		t.Fatalf("a refused install must not leave a compose file behind:\n%s", cmd)
	}
}

// The heredoc must be a Compose file that delivers the panel address and
// secret unchanged, even with characters Compose or YAML treat specially.
func TestDockerComposeInstallFileParses(t *testing.T) {
	if exec.Command("docker", "compose", "version").Run() != nil {
		t.Skip("docker compose is not installed")
	}
	const secret = `a$b${HOME}"c'\\d`
	const addr = "panel.example.com:6365"
	cmd := dockerComposeInstallCommand("3.4.0", addr, secret, true)
	start := strings.Index(cmd, "<<'EOF'\n")
	end := strings.Index(cmd, "\nEOF\n")
	if start < 0 || end < start {
		t.Fatalf("no compose heredoc in %s", cmd)
	}
	path := filepath.Join(t.TempDir(), "docker-compose.yaml")
	if err := os.WriteFile(path, []byte(cmd[start+len("<<'EOF'\n"):end+1]), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("docker", "compose", "-f", path, "config", "--format", "json").CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose config: %v\n%s", err, out)
	}
	var cfg struct {
		Services map[string]struct {
			Privileged  bool              `json:"privileged"`
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	svc := cfg.Services["flyx-agent"]
	if !svc.Privileged {
		t.Error("privileged switch lost")
	}
	for key, want := range map[string]string{
		"FLYX_AGENT_SECRET": secret, "FLUX_AGENT_SECRET": secret,
		"FLYX_AGENT_ADDR": addr, "FLUX_AGENT_ADDR": addr,
	} {
		// `config` escapes "$" again so its output reads back the same; an
		// unescaped "$" would have been interpolated away instead.
		if got := strings.ReplaceAll(svc.Environment[key], "$$", "$"); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
