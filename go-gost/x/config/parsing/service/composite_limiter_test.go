package service

import (
	"context"
	"io"
	"testing"

	corelimiter "github.com/go-gost/core/limiter"
	corelogger "github.com/go-gost/core/logger"
	xconn "github.com/go-gost/x/limiter/conn"
	xtraffic "github.com/go-gost/x/limiter/traffic"
	xlogger "github.com/go-gost/x/logger"
	"github.com/go-gost/x/registry"
)

func TestResolveTrafficLimiterComposesCommaSeparatedNames(t *testing.T) {
	const totalName = "test_total_speed_composite"
	const ruleName = "test_rule_speed_composite"
	registry.TrafficLimiterRegistry().Unregister(totalName)
	registry.TrafficLimiterRegistry().Unregister(ruleName)
	defer registry.TrafficLimiterRegistry().Unregister(totalName)
	defer registry.TrafficLimiterRegistry().Unregister(ruleName)

	logger := xlogger.NewLogger(xlogger.OutputOption(io.Discard), xlogger.LevelOption(corelogger.ErrorLevel))
	if err := registry.TrafficLimiterRegistry().Register(totalName, xtraffic.NewTrafficLimiter(xtraffic.LimitsOption("$ 10B 10B"), xtraffic.LoggerOption(logger))); err != nil {
		t.Fatalf("register total limiter: %v", err)
	}
	if err := registry.TrafficLimiterRegistry().Register(ruleName, xtraffic.NewTrafficLimiter(xtraffic.LimitsOption("0.0.0.0/0 3B 3B"), xtraffic.LoggerOption(logger))); err != nil {
		t.Fatalf("register rule limiter: %v", err)
	}

	lim := resolveTrafficLimiter(totalName + "," + ruleName)
	if lim == nil {
		t.Fatalf("expected composite traffic limiter")
	}
	serviceLimiter := lim.In(context.Background(), "192.0.2.1:1000", corelimiter.ScopeOption(corelimiter.ScopeService))
	if serviceLimiter == nil || serviceLimiter.Limit() != 10 {
		t.Fatalf("expected service-scope total limiter 10, got %#v", serviceLimiter)
	}
	connLimiter := lim.In(context.Background(), "192.0.2.1:1000", corelimiter.ScopeOption(corelimiter.ScopeConn))
	if connLimiter == nil || connLimiter.Limit() != 3 {
		t.Fatalf("expected conn-scope per-IP limiter 3, got %#v", connLimiter)
	}
}

func TestResolveConnLimiterComposesCommaSeparatedNames(t *testing.T) {
	const totalName = "test_total_conn_composite"
	const ruleName = "test_rule_conn_composite"
	registry.ConnLimiterRegistry().Unregister(totalName)
	registry.ConnLimiterRegistry().Unregister(ruleName)
	defer registry.ConnLimiterRegistry().Unregister(totalName)
	defer registry.ConnLimiterRegistry().Unregister(ruleName)

	logger := xlogger.NewLogger(xlogger.OutputOption(io.Discard), xlogger.LevelOption(corelogger.ErrorLevel))
	if err := registry.ConnLimiterRegistry().Register(totalName, xconn.NewConnLimiter(xconn.LimitsOption("$ 2"), xconn.LoggerOption(logger))); err != nil {
		t.Fatalf("register total conn limiter: %v", err)
	}
	if err := registry.ConnLimiterRegistry().Register(ruleName, xconn.NewConnLimiter(xconn.LimitsOption("$$ 1"), xconn.LoggerOption(logger))); err != nil {
		t.Fatalf("register rule conn limiter: %v", err)
	}

	lim := resolveConnLimiter(totalName + "," + ruleName)
	if lim == nil {
		t.Fatalf("expected composite conn limiter")
	}
	clientLimiter := lim.Limiter("192.0.2.1")
	if clientLimiter == nil || clientLimiter.Limit() != 1 {
		t.Fatalf("expected composite client limiter with strictest limit 1, got %#v", clientLimiter)
	}
	if !clientLimiter.Allow(1) {
		t.Fatalf("expected first connection to be allowed")
	}
	if clientLimiter.Allow(1) {
		t.Fatalf("expected per-IP rule limiter to reject second connection")
	}
	if !lim.Limiter("192.0.2.2").Allow(1) {
		t.Fatalf("expected another client to share total limiter but have independent per-IP capacity")
	}
}
