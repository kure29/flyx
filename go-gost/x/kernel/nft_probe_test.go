package kernel

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func resetNftProbe(t *testing.T) {
	t.Helper()
	reset := func() {
		nftProbe.Lock()
		nftProbe.denied, nftProbe.ok = false, false
		nftProbe.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

// In unprivileged containers nft is installed but every call fails; that is
// probed once and reported as a permission problem, not "not installed".
func TestProbeNftPermission(t *testing.T) {
	resetNftProbe(t)
	calls := 0
	denied := func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		calls++
		return nil, errors.New("nft: Operation not permitted (you must be root)\nnetlink: Error: cache initialization failed: Operation not permitted")
	}
	for i := 0; i < 3; i++ {
		if err := probeNftPermission(denied); !errors.Is(err, errNftDenied) {
			t.Fatalf("permission failure not detected: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("permission probe ran %d times, want once", calls)
	}
	if !strings.Contains(nftUnavailableText(errNftDenied), "容器") {
		t.Fatalf("message should explain the container cause: %q", nftUnavailableText(errNftDenied))
	}

	resetNftProbe(t)
	flaky := func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		return nil, errors.New("nft: context deadline exceeded")
	}
	if err := probeNftPermission(flaky); err != nil {
		t.Fatalf("a non-permission failure must not be reported as denied: %v", err)
	}
	if nftProbe.denied || nftProbe.ok {
		t.Fatal("an inconclusive probe must not be cached")
	}
}
