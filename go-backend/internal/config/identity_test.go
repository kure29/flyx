package config

import "testing"

func TestIdentityEnvPrefersNewNameIncludingEmptyValue(t *testing.T) {
	t.Setenv("FLVX_WEBAUTHN_ORIGIN", "https://old.example")
	if got := IdentityEnv("FLYX_WEBAUTHN_ORIGIN", "FLVX_WEBAUTHN_ORIGIN"); got != "https://old.example" {
		t.Fatalf("legacy fallback = %q", got)
	}
	t.Setenv("FLYX_WEBAUTHN_ORIGIN", "https://new.example")
	if got := IdentityEnv("FLYX_WEBAUTHN_ORIGIN", "FLVX_WEBAUTHN_ORIGIN"); got != "https://new.example" {
		t.Fatalf("new value = %q", got)
	}
	t.Setenv("FLYX_WEBAUTHN_ORIGIN", "")
	if got := IdentityEnv("FLYX_WEBAUTHN_ORIGIN", "FLVX_WEBAUTHN_ORIGIN"); got != "" {
		t.Fatalf("explicit empty value must override legacy configuration, got %q", got)
	}
}
