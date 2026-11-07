package handler

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGistEncryptRoundTrip(t *testing.T) {
	plain := []byte(`{"version":"1.0","configs":{"app_name":"FLYX"}}`)
	sealed, err := gistEncrypt(plain, "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "app_name") || !strings.Contains(string(sealed), `"flyx": "encrypted-backup"`) {
		t.Fatalf("not an encrypted envelope: %s", sealed)
	}
	got, encrypted, err := gistOpen(sealed, "hunter2")
	if err != nil || !encrypted || string(got) != string(plain) {
		t.Fatalf("open: %q %v %v", got, encrypted, err)
	}
	if _, _, err := gistOpen(sealed, "wrong"); err == nil || !strings.Contains(err.Error(), "密码不对") {
		t.Fatalf("wrong password: %v", err)
	}
	if _, _, err := gistOpen(sealed, ""); err == nil || !strings.Contains(err.Error(), "加密") {
		t.Fatalf("no password: %v", err)
	}
	// A plain backup opens as is.
	if got, encrypted, err := gistOpen(plain, ""); err != nil || encrypted || string(got) != string(plain) {
		t.Fatalf("plain: %q %v %v", got, encrypted, err)
	}
}

func TestGistDue(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	ms := func(d time.Duration) string { return strconv.FormatInt(now.Add(-d).UnixMilli(), 10) }
	for _, c := range []struct {
		cfg  map[string]string
		want bool
	}{
		{map[string]string{"gist_interval_hours": "24"}, false}, // no token
		{map[string]string{"gist_token": "t", "gist_interval_hours": "0"}, false},
		{map[string]string{"gist_token": "t", "gist_interval_hours": "24"}, true}, // never ran
		{map[string]string{"gist_token": "t", "gist_interval_hours": "24", "gist_last_at": ms(23 * time.Hour)}, false},
		{map[string]string{"gist_token": "t", "gist_interval_hours": "24", "gist_last_at": ms(24 * time.Hour)}, true},
		{map[string]string{"gist_token": "t", "gist_interval_hours": "6", "gist_last_at": ms(7 * time.Hour)}, true},
	} {
		if got := gistDue(c.cfg, now); got != c.want {
			t.Errorf("%v: %v", c.cfg, got)
		}
	}
}

func TestTokenHint(t *testing.T) {
	if got := tokenHint("ghp_abcdefghijklmnopqrstuvwxyz12"); got != "ghp_••••••••yz12" {
		t.Fatalf("hint: %s", got)
	}
	if got := tokenHint("short"); strings.Contains(got, "s") {
		t.Fatalf("short token leaked: %s", got)
	}
}
