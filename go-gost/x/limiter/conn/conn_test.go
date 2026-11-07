package conn

import (
	"io"
	"testing"

	corelogger "github.com/go-gost/core/logger"
	xlogger "github.com/go-gost/x/logger"
)

func TestIPLimitKeyCreatesIndependentLimiters(t *testing.T) {
	limiter := NewConnLimiter(
		LimitsOption("$$ 1"),
		LoggerOption(xlogger.NewLogger(xlogger.OutputOption(io.Discard), xlogger.LevelOption(corelogger.ErrorLevel))),
	)
	first := limiter.Limiter("192.0.2.1")
	second := limiter.Limiter("192.0.2.2")
	if first == nil || second == nil {
		t.Fatalf("expected non-nil per-IP limiters")
	}
	if !first.Allow(1) {
		t.Fatalf("expected first IP first connection to be allowed")
	}
	if first.Allow(1) {
		t.Fatalf("expected first IP second connection to be rejected")
	}
	if !second.Allow(1) {
		t.Fatalf("expected second IP first connection to be allowed independently")
	}
}
