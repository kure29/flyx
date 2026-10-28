package identity

import "testing"

func TestEnvironmentCompatibility(t *testing.T) {
	t.Setenv("FLUX_AGENT_CONTAINER", "1")
	if got := Env("FLYX_AGENT_CONTAINER", "FLUX_AGENT_CONTAINER"); got != "1" {
		t.Fatalf("legacy value = %q", got)
	}
	t.Setenv("FLYX_AGENT_CONTAINER", "0")
	if got := Env("FLYX_AGENT_CONTAINER", "FLUX_AGENT_CONTAINER"); got != "0" {
		t.Fatalf("new value = %q", got)
	}
	t.Setenv("FLYX_AGENT_CONTAINER", "")
	if got := Env("FLYX_AGENT_CONTAINER", "FLUX_AGENT_CONTAINER"); got != "" {
		t.Fatalf("empty new value = %q", got)
	}
}

func TestInstallationFollowsRunningBinary(t *testing.T) {
	for _, tc := range []struct{ executable, dir, binary, service string }{
		{"/etc/flux_agent/flux_agent", "/etc/flux_agent", "/etc/flux_agent/flux_agent", "flux_agent"},
		{"/etc/flyx-agent/flyx-agent", "/etc/flyx-agent", "/etc/flyx-agent/flyx-agent", "flyx-agent"},
		{"/tmp/test-agent", "/etc/flyx-agent", "/etc/flyx-agent/flyx-agent", "flyx-agent"},
	} {
		t.Run(tc.executable, func(t *testing.T) {
			dir, binary, service := installation(tc.executable)
			if dir != tc.dir || binary != tc.binary || service != tc.service {
				t.Fatalf("installation = %q %q %q", dir, binary, service)
			}
		})
	}
}
