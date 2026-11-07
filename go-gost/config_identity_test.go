package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentIdentityEnvPreservesSavedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"addr":"saved","secret":"saved-secret","http":8080,"tls":443,"socks":1080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLUX_AGENT_ADDR", "legacy")
	t.Setenv("FLUX_AGENT_SECRET", "legacy-secret")
	t.Setenv("FLYX_AGENT_ADDR", "canonical")
	t.Setenv("FLYX_AGENT_SECRET", "canonical-secret")
	if err := ApplyEnvConfig(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "canonical" || cfg.Secret != "canonical-secret" || cfg.Http != 8080 || cfg.Tls != 443 || cfg.Socks != 1080 {
		t.Fatalf("configuration = %+v", cfg)
	}
	t.Setenv("FLYX_AGENT_ADDR", "")
	t.Setenv("FLYX_AGENT_SECRET", "")
	if err := ApplyEnvConfig(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("explicitly empty new variables unexpectedly reused legacy values")
	}
}
