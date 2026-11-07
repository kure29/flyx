package loader_test

import (
	"net"
	"testing"

	"github.com/go-gost/x/config"
	"github.com/go-gost/x/config/loader"
	"github.com/go-gost/x/registry"
)

// A rule whose port is taken (by another process or a duplicate rule) must not
// stop the agent from starting; see issue #201.
func TestLoadStartupSkipsServicesThatCannotListen(t *testing.T) {
	unlock := config.LockMutation()
	defer unlock()
	defer func() {
		for name := range registry.ServiceRegistry().GetAll() {
			registry.ServiceRegistry().Unregister(name)
		}
	}()

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	tcp := func(name, addr string) *config.ServiceConfig {
		return &config.ServiceConfig{Name: name, Addr: addr, Handler: &config.HandlerConfig{Type: "auto"}, Listener: &config.ListenerConfig{Type: "tcp"}}
	}
	cfg := &config.Config{Services: []*config.ServiceConfig{
		tcp("port-taken", occupied.Addr().String()),
		tcp("healthy", "127.0.0.1:0"),
		tcp("healthy", "127.0.0.1:0"), // duplicate name
	}}

	skipped, err := loader.LoadStartup(cfg)
	if err != nil {
		t.Fatalf("startup load failed: %v", err)
	}
	if len(skipped) != 2 {
		t.Fatalf("expected 2 skipped services, got %v", skipped)
	}
	if registry.ServiceRegistry().Get("healthy") == nil {
		t.Fatal("healthy service was not started")
	}
	if registry.ServiceRegistry().Get("port-taken") != nil {
		t.Fatal("service on an occupied port was registered")
	}

	// The strict loader used for reloads still fails.
	for name := range registry.ServiceRegistry().GetAll() {
		registry.ServiceRegistry().Unregister(name)
	}
	if err := loader.Load(cfg); err == nil {
		t.Fatal("strict load accepted an occupied port")
	}
}
