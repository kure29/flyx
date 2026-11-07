package handler

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchBlockedIP(t *testing.T) {
	for _, c := range []struct {
		ip                  string
		blocked, allowLocal bool
	}{
		{"203.0.113.7", false, false},
		{"2001:db8::1", false, false},
		{"127.0.0.1", true, false},
		{"10.1.2.3", true, false},
		{"192.168.1.1", true, false},
		{"100.64.0.1", true, false},
		{"fd00::1", true, false},
		{"::1", true, false},
		{"127.0.0.1", false, true},
		{"192.168.1.1", false, true},
		// Cloud metadata and the like stay out even when local is allowed.
		{"169.254.169.254", true, true},
		{"fe80::1", true, true},
		{"0.0.0.0", true, true},
	} {
		err := fetchBlockedIP(net.ParseIP(c.ip), c.allowLocal)
		if (err != nil) != c.blocked {
			t.Errorf("%s allowLocal=%v: %v", c.ip, c.allowLocal, err)
		}
	}
}

func TestFetchClientChecksEachDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	_, err := fetchClient(false, 0).Get(srv.URL)
	err = fetchErr(err)
	if err == nil || !errors.Is(err, errFetchLocal) || strings.Contains(err.Error(), "dial tcp") {
		t.Fatalf("loopback must be refused with the reason only: %v", err)
	}
	resp, err := fetchClient(true, 0).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowed: %v", err)
	}
	_ = resp.Body.Close()

	// A hostname resolving inward is refused after DNS too.
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	if _, err := fetchClient(false, 0).Get(u); !errors.Is(fetchErr(err), errFetchLocal) {
		t.Fatalf("localhost: %v", err)
	}
}
