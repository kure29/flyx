package handler

import (
	"strings"
	"testing"
)

func TestDockerInstallCommand(t *testing.T) {
	old := githubRepo
	githubRepo = "Kure29/flyx"
	defer func() { githubRepo = old }()

	cmd := dockerInstallCommand("3.4.0", "panel.example.com:6365", "s3cret", false)
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
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in %s", want, cmd)
		}
	}
	if strings.Contains(cmd, "--privileged") {
		t.Errorf("unexpected --privileged in %s", cmd)
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
		"image: ghcr.io/kure29/flux-agent:3.4.0",
		"container_name: flyx-agent",
		"restart: unless-stopped",
		"network_mode: host",
		"      - NET_ADMIN",
		`FLUX_AGENT_ADDR: "panel.example.com:6365"`,
		`FLUX_AGENT_SECRET: "s3cret"`,
		"chmod 600 /opt/docker/flyx-agent/docker-compose.yaml",
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
