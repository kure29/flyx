package service

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-gost/core/handler"
	"github.com/go-gost/core/metadata"
	"github.com/go-gost/core/observer"
	"github.com/go-gost/core/observer/stats"
	xlogger "github.com/go-gost/x/logger"
	xstats "github.com/go-gost/x/observer/stats"
	"github.com/go-gost/x/registry"
)

type finalTrafficListener struct {
	conn   net.Conn
	closed chan struct{}
	once   sync.Once
}

func (*finalTrafficListener) Init(metadata.Metadata) error { return nil }
func (*finalTrafficListener) Addr() net.Addr               { return &net.TCPAddr{} }
func (l *finalTrafficListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		conn := l.conn
		l.conn = nil
		return conn, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *finalTrafficListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

type finalTrafficHandler struct {
	stats           stats.Stats
	started, finish chan struct{}
}

func (*finalTrafficHandler) Init(metadata.Metadata) error { return nil }
func (h *finalTrafficHandler) Handle(_ context.Context, conn net.Conn, _ ...handler.HandleOption) error {
	defer conn.Close()
	h.stats.Add(stats.KindInputBytes, 100)
	h.stats.Add(stats.KindOutputBytes, 40)
	close(h.started)
	<-h.finish
	h.stats.Add(stats.KindInputBytes, 20)
	h.stats.Add(stats.KindOutputBytes, 10)
	return nil
}

type finalTrafficObserver struct{ sampled chan struct{} }

func (o finalTrafficObserver) Observe(_ context.Context, events []observer.Event, _ ...observer.Option) error {
	for _, event := range events {
		if _, ok := event.(xstats.StatsEvent); ok {
			select {
			case o.sampled <- struct{}{}:
			default:
			}
		}
	}
	return nil
}

func TestServiceFinalTrafficWaitsForHandlersAndSampler(t *testing.T) {
	m := GetGlobalTrafficManager()
	// Keep pending bytes observable without starting real HTTP reports.
	m.reportTicker.Stop()
	t.Cleanup(m.Stop)
	for _, tc := range []struct {
		name       string
		unregister bool
		periodic   bool
	}{
		{"close_before_first_sample", false, false},
		{"unregister_before_first_sample", true, false},
		{"sample_while_unregistered_connection_drains", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "final-traffic-" + tc.name
			conn, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			ln := &finalTrafficListener{conn: conn, closed: make(chan struct{})}
			st := xstats.NewStats(false)
			h := &finalTrafficHandler{stats: st, started: make(chan struct{}), finish: make(chan struct{})}
			obs := finalTrafficObserver{sampled: make(chan struct{}, 1)}
			period := time.Hour
			if tc.periodic {
				period = time.Second
			}
			svc := NewService(name, ln, h, StatsOption(st), ObserverOption(obs), ObserverPeriodOption(period), LoggerOption(xlogger.Nop()))
			if err := registry.ServiceRegistry().Register(name, svc); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var finishOnce sync.Once
			finish := func() { finishOnce.Do(func() { close(h.finish) }) }
			t.Cleanup(func() {
				registry.ServiceRegistry().Unregister(name)
				finish()
				waitFinalTrafficSignal(t, done)
			})
			go func() {
				_ = svc.Serve()
				close(done)
			}()
			waitFinalTrafficSignal(t, h.started)
			if tc.unregister {
				registry.ServiceRegistry().Unregister(name)
			} else if err := svc.Close(); err != nil {
				t.Fatal(err)
			}
			if tc.periodic {
				waitFinalTrafficSignal(t, obs.sampled)
				if up, down := m.GetServiceTraffic(name); up != 40 || down != 100 {
					t.Fatalf("unregister lost in-flight sample: up=%d down=%d", up, down)
				}
			}
			finish()
			waitFinalTrafficSignal(t, done)
			if up, down := m.GetServiceTraffic(name); up != 50 || down != 120 {
				t.Fatalf("final handoff lost or duplicated traffic: up=%d down=%d", up, down)
			}
			_ = svc.Close()
			if up, down := m.GetServiceTraffic(name); up != 50 || down != 120 {
				t.Fatalf("repeated close duplicated final traffic: up=%d down=%d", up, down)
			}
		})
	}
}

func waitFinalTrafficSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for service lifecycle")
	}
}
