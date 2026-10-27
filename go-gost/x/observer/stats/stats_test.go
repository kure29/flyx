package stats

import (
	corestats "github.com/go-gost/core/observer/stats"
	"sync"
	"testing"
)

func TestTakeTrafficPreservesOtherCountersAndSubsequentBytes(t *testing.T) {
	s := NewStats(false).(*Stats)
	s.Add(corestats.KindTotalConns, 3)
	s.Add(corestats.KindCurrentConns, 2)
	s.Add(corestats.KindTotalErrs, 1)
	s.Add(corestats.KindInputBytes, 100)
	s.Add(corestats.KindOutputBytes, 200)
	in, out := s.TakeTraffic()
	s.Add(corestats.KindInputBytes, 50)
	s.Add(corestats.KindOutputBytes, 60)
	nextIn, nextOut := s.TakeTraffic()
	if in != 100 || out != 200 || nextIn != 50 || nextOut != 60 {
		t.Fatalf("snapshots = %d/%d then %d/%d", in, out, nextIn, nextOut)
	}
	if s.Get(corestats.KindTotalConns) != 3 || s.Get(corestats.KindCurrentConns) != 2 || s.Get(corestats.KindTotalErrs) != 1 {
		t.Fatal("traffic collection changed connection/error counters")
	}
	if in, out = s.TakeTraffic(); in != 0 || out != 0 {
		t.Fatalf("traffic reported twice: %d/%d", in, out)
	}
}

func TestTakeTrafficConservesBytesWithConcurrentWriters(t *testing.T) {
	s := NewStats(false).(*Stats)
	const writers, iterations = 8, 10000
	var wg sync.WaitGroup
	for n := 0; n < writers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				s.Add(corestats.KindInputBytes, 3)
				s.Add(corestats.KindOutputBytes, 7)
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	var in, out uint64
	for {
		a, b := s.TakeTraffic()
		in += a
		out += b
		select {
		case <-done:
			a, b = s.TakeTraffic()
			in += a
			out += b
			if in != writers*iterations*3 || out != writers*iterations*7 {
				t.Fatalf("collected %d/%d bytes", in, out)
			}
			return
		default:
		}
	}
}
