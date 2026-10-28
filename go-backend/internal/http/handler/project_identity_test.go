package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeUpdatesBothVersionNamesForRollback(t *testing.T) {
	for _, original := range []string{
		"FLUX_VERSION=old\nJWT_SECRET=keep\n",
		"FLYX_VERSION=new\nFLUX_VERSION=old\nJWT_SECRET=keep\n",
		"JWT_SECRET=keep\n",
	} {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
			t.Fatal(err)
		}
		e := &systemUpgradeExecutor{}
		for i := 0; i < 2; i++ {
			if err := e.updateEnvVersion(path, "next"); err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range []string{"FLYX_VERSION=next\n", "FLUX_VERSION=next\n", "JWT_SECRET=keep\n"} {
			if strings.Count(string(data), line) != 1 {
				t.Fatalf("missing or duplicate %q in %q", line, data)
			}
		}
	}
}

func TestPanelVersionUsesNewNameBeforeLegacy(t *testing.T) {
	t.Setenv("FLUX_VERSION", "old")
	t.Setenv("FLYX_VERSION", "new")
	if got := currentPanelVersion(); got != "new" {
		t.Fatalf("version = %q", got)
	}
}
