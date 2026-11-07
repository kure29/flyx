package traffic

import (
	"context"
	"io"
	"testing"

	corelogger "github.com/go-gost/core/logger"
	xlogger "github.com/go-gost/x/logger"
)

func TestCIDRLimitCreatesIndependentClientLimiters(t *testing.T) {
	limiter := NewTrafficLimiter(
		LimitsOption("0.0.0.0/0 2B 2B"),
		LoggerOption(xlogger.NewLogger(xlogger.OutputOption(io.Discard), xlogger.LevelOption(corelogger.ErrorLevel))),
	)
	first := limiter.In(context.Background(), "192.0.2.1:1000")
	second := limiter.In(context.Background(), "192.0.2.2:1000")
	if first == nil || second == nil {
		t.Fatalf("expected non-nil CIDR client limiters")
	}
	if first == second {
		t.Fatalf("expected different clients to receive independent limiter instances")
	}
	if first.Limit() != 2 || second.Limit() != 2 {
		t.Fatalf("expected both limits to be 2, got %d and %d", first.Limit(), second.Limit())
	}
}
