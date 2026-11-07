package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestWhitelistSurvivesStateEncoding(t *testing.T) {
	for _, allow := range [][]string{nil, {}, {"127.0.0.1/32"}} {
		before := Port{InboundID: 1, Port: 443, Network: "tcp", Allow: allow}
		data, err := json.Marshal(before)
		if err != nil {
			t.Fatal(err)
		}
		var after Port
		if err := json.Unmarshal(data, &after); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before.Allow, after.Allow) {
			t.Fatalf("state encoding changed whitelist: before=%#v after=%#v", before.Allow, after.Allow)
		}
	}
}

func TestManagerWhitelistFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as fake kernel")
	}
	for _, mode := range []string{"missing nft", "apply failure"} {
		t.Run(mode, func(t *testing.T) {
			stubNft(t)
			available, runner := nftAvailable, nftRunner
			if mode == "missing nft" {
				nftAvailable = func() error { return errors.New("nft missing") }
			} else {
				nftRunner = func(context.Context, string, ...string) ([]byte, error) {
					return nil, errors.New("permission denied")
				}
			}
			srv, _ := serveKernel(t, zipWith(t, map[string]string{"xray": fakeKernel}))
			m := NewManager(t.TempDir())
			defer m.Shutdown()
			req := applyRequest(srv, `{}`, true)
			req.Ports[0].Allow = []string{"127.0.0.1/32"}
			assertBlocked := func(st State, err error) {
				t.Helper()
				if err == nil || st.Running || !strings.Contains(st.Error, "白名单") {
					t.Fatalf("protected kernel must fail closed: state=%+v err=%v", st, err)
				}
			}
			assertBlocked(m.Apply(req))
			for _, action := range []string{"start", "restart"} {
				assertBlocked(m.Control("xray", action))
			}
			if strings.Contains(strings.Join(m.state("xray", true).Logs, "\n"), "starting with") {
				t.Fatal("kernel started before its whitelist was installed")
			}
			restored := NewManager(m.baseDir)
			defer restored.Shutdown()
			restored.restore()
			if st := restored.state("xray", false); st.Running || !strings.Contains(st.Error, "白名单") {
				t.Fatalf("restore bypassed whitelist failure: %+v", st)
			}
			nftAvailable, nftRunner = available, runner
			if st, err := m.Control("xray", "start"); err != nil || !st.Running || st.Error != "" {
				t.Fatalf("retry after firewall recovery: %+v %v", st, err)
			}
		})
	}
}

func TestManagerWhitelistUpdateFailureStopsRunningKernel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as fake kernel")
	}
	stubNft(t)
	srv, _ := serveKernel(t, zipWith(t, map[string]string{"xray": fakeKernel}))
	m := NewManager(t.TempDir())
	defer m.Shutdown()
	req := applyRequest(srv, `{}`, true)
	req.Ports[0].Allow = []string{"127.0.0.1/32"}
	if st, err := m.Apply(req); err != nil || !st.Running {
		t.Fatalf("initial apply: %+v %v", st, err)
	}
	// Only the whitelist changes; the kernel configuration hash is unchanged.
	req.Ports[0].Allow = []string{"192.0.2.1/32"}
	nftRunner = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("permission denied")
	}
	if st, err := m.Apply(req); err == nil || st.Running {
		t.Fatalf("failed whitelist update left kernel running: %+v %v", st, err)
	}
}

func TestManagerWhitelistRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as fake kernel")
	}
	for _, failRollback := range []bool{false, true} {
		name := "restore rules"
		if failRollback {
			name = "restore rules fails"
		}
		t.Run(name, func(t *testing.T) {
			rec := stubNft(t)
			srv, _ := serveKernel(t, zipWith(t, map[string]string{"xray": fakeKernel}))
			m := NewManager(t.TempDir())
			defer m.Shutdown()
			runner := nftRunner
			rejectOld := false
			nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
				if stdin != "" {
					if m.state("xray", false).Running {
						t.Error("changed rules were installed while the old configuration was still running")
					}
					if rejectOld && strings.Contains(stdin, "tcp dport 443 ") {
						return nil, errors.New("rollback rules failed")
					}
				}
				return runner(ctx, stdin, args...)
			}
			req := applyRequest(srv, `{}`, true)
			req.Ports[0].Allow = []string{"127.0.0.1/32"}
			if st, err := m.Apply(req); err != nil || !st.Running {
				t.Fatalf("initial apply: %+v %v", st, err)
			}
			rejectOld = failRollback
			bad := applyRequest(srv, `{"crash":true}`, true)
			bad.Ports[0].Port = 444
			bad.Ports[0].Allow = []string{"192.0.2.1/32"}
			st, err := m.Apply(bad)
			if err == nil || st.ConfigHash != req.Hash || st.Running == failRollback {
				t.Fatalf("rollback: %+v %v", st, err)
			}
			if !failRollback {
				last := rec.scripts[len(rec.scripts)-1]
				if !strings.Contains(last, "tcp dport 443 ip saddr") || strings.Contains(last, "tcp dport 444 ") {
					t.Fatalf("rollback did not restore the old whitelist: %s", last)
				}
			}
		})
	}
}

func TestManagerWithoutWhitelistCanRunWithoutNft(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as fake kernel")
	}
	stubNft(t)
	nftAvailable = func() error { return errors.New("nft missing") }
	srv, _ := serveKernel(t, zipWith(t, map[string]string{"xray": fakeKernel}))
	m := NewManager(t.TempDir())
	defer m.Shutdown()
	if st, err := m.Apply(applyRequest(srv, `{}`, true)); err != nil || !st.Running {
		t.Fatalf("counter-only failure blocked kernel: %+v %v", st, err)
	}
}

func TestManagerWhitelistGeoDataRestartFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as fake kernel")
	}
	for _, failOn := range []int{1, 2} {
		name := "geo update"
		if failOn == 2 {
			name = "geo rollback"
		}
		t.Run(name, func(t *testing.T) {
			stubNft(t)
			// A bad geo file makes the fake kernel fail, exercising rollback.
			binary := strings.Replace(fakeKernel, `if grep -q crash "$3";`, `if grep -q crash "$3" || grep -q crash "$(dirname "$0")/geosite.dat";`, 1)
			srv, _ := serveKernel(t, zipWith(t, map[string]string{"xray": binary, "geosite.dat": "old"}))
			m := NewManager(t.TempDir())
			defer m.Shutdown()
			req := applyRequest(srv, `{}`, true)
			req.Ports[0].Allow = []string{"127.0.0.1/32"}
			if st, err := m.Apply(req); err != nil || !st.Running {
				t.Fatalf("initial apply: %+v %v", st, err)
			}
			calls := 0
			nftAvailable = func() error {
				calls++
				if calls >= failOn {
					return errors.New("nft unavailable")
				}
				return nil
			}
			content := "new"
			if failOn == 2 {
				content = "crash"
			}
			binDir := m.binDir(req.Type, req.Version)
			if err := os.WriteFile(filepath.Join(binDir, "geosite.dat.new"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			m.swapGeoFiles(geoJob{kernelType: req.Type, binDir: binDir}, []string{"geosite.dat"})
			st := m.state(req.Type, false)
			if st.Running || !strings.Contains(st.Error, "白名单") {
				t.Fatalf("geo update bypassed whitelist protection: %+v", st)
			}
			if failOn == 2 {
				data, err := os.ReadFile(filepath.Join(binDir, "geosite.dat"))
				if err != nil || string(data) != "old" {
					t.Fatalf("geo rollback did not restore file: %q %v", data, err)
				}
			}
		})
	}
}
