package handler

import (
	"strings"
	"testing"
)

func TestDockerInstallCommand(t *testing.T) {
	old := githubRepo
	githubRepo = "Kure29/flyx"
	defer func() { githubRepo = old }()
	plain := dockerInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", false)
	for _, want := range []string{
		"docker rm -f flux_agent",
		"--restart unless-stopped",
		"--network host",
		"--cap-add NET_ADMIN",
		"-v /etc/flux_agent:/etc/flux_agent",
		"-e FLUX_AGENT_ADDR=panel.example.com:6365",
		"-e FLUX_AGENT_SECRET=s3cret",
		" ghcr.io/kure29/flux-agent:3.4.0",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in %s", want, plain)
		}
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
