package handler

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go-backend/internal/ws"
)

func TestNodePingSlotsLimitPerNode(t *testing.T) {
	s := newNodePingSlots()
	for i := 0; i < agentTCPPingLimit; i++ {
		if !s.acquire(1, time.Second) {
			t.Fatalf("slot %d refused", i)
		}
	}
	if s.acquire(1, 30*time.Millisecond) {
		t.Fatal("a ninth ping must wait")
	}
	if !s.acquire(2, time.Second) {
		t.Fatal("another node has its own slots")
	}
	s.release(1)
	if !s.acquire(1, time.Second) {
		t.Fatal("a released slot is free again")
	}
}

// More pings than the agent takes queue for a slot instead of failing.
func TestSendTCPPingQueuesWithinAgentLimit(t *testing.T) {
	var running, peak int32
	orig := tcpPingSend
	tcpPingSend = func(*Handler, int64, interface{}, time.Duration) (ws.CommandResult, error) {
		n := atomic.AddInt32(&running, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		return ws.CommandResult{}, nil
	}
	t.Cleanup(func() { tcpPingSend = orig })

	h := &Handler{pingSlots: newNodePingSlots()}
	var wg sync.WaitGroup
	var failed int32
	for i := 0; i < 2*agentTCPPingLimit+3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.sendTCPPing(7, nil, 5*time.Second); err != nil {
				atomic.AddInt32(&failed, 1)
			}
		}()
	}
	wg.Wait()
	if failed != 0 || peak != agentTCPPingLimit {
		t.Fatalf("failed=%d peak=%d, want 0 and %d", failed, peak, agentTCPPingLimit)
	}
}

// An agent still busy (another panel, or pings the panel stopped waiting
// for) is asked once more.
func TestSendTCPPingRetriesBusyAgent(t *testing.T) {
	calls := 0
	var firstTimeout time.Duration
	orig := tcpPingSend
	tcpPingSend = func(_ *Handler, _ int64, _ interface{}, timeout time.Duration) (ws.CommandResult, error) {
		calls++
		if calls == 1 {
			firstTimeout = timeout
			return ws.CommandResult{}, errors.New("TCP探测任务过多，请稍后重试")
		}
		if timeout <= 0 || timeout > firstTimeout-agentBusyRetryDelay {
			t.Errorf("retry timeout %v must exclude retry delay from %v", timeout, firstTimeout)
		}
		return ws.CommandResult{Data: map[string]interface{}{"success": true}}, nil
	}
	t.Cleanup(func() { tcpPingSend = orig })

	res, err := (&Handler{}).sendTCPPing(1, nil, 3*time.Second)
	if err != nil || calls != 2 || res.Data["success"] != true {
		t.Fatalf("err=%v calls=%d res=%+v", err, calls, res)
	}
}

func TestSendTCPPingQueueUsesCommandBudget(t *testing.T) {
	h := &Handler{pingSlots: newNodePingSlots()}
	for i := 0; i < agentTCPPingLimit; i++ {
		if !h.pingSlots.acquire(1, time.Second) {
			t.Fatal("failed to occupy node slots")
		}
	}
	const budget = time.Second
	orig := tcpPingSend
	tcpPingSend = func(_ *Handler, _ int64, _ interface{}, timeout time.Duration) (ws.CommandResult, error) {
		if timeout <= 0 || timeout > budget-25*time.Millisecond {
			t.Errorf("command timeout %v must exclude panel queue time", timeout)
		}
		return ws.CommandResult{}, nil
	}
	t.Cleanup(func() { tcpPingSend = orig })
	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(50 * time.Millisecond)
		h.pingSlots.release(1)
	}()
	_, err := h.sendTCPPing(1, nil, budget)
	<-done
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendTCPPingDoesNotRetryBeyondBudget(t *testing.T) {
	calls := 0
	orig := tcpPingSend
	tcpPingSend = func(*Handler, int64, interface{}, time.Duration) (ws.CommandResult, error) {
		calls++
		return ws.CommandResult{}, errors.New(agentBusyMessage)
	}
	t.Cleanup(func() { tcpPingSend = orig })
	_, err := (&Handler{}).sendTCPPing(1, nil, agentBusyRetryDelay/2)
	if err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d, must not retry outside the command budget", err, calls)
	}
}

func TestSendTCPPingQueueTimeoutDoesNotSend(t *testing.T) {
	h := &Handler{pingSlots: newNodePingSlots()}
	for i := 0; i < agentTCPPingLimit; i++ {
		if !h.pingSlots.acquire(1, time.Second) {
			t.Fatal("failed to occupy node slots")
		}
	}
	orig := tcpPingSend
	tcpPingSend = func(*Handler, int64, interface{}, time.Duration) (ws.CommandResult, error) {
		t.Fatal("expired queued command must not be sent")
		return ws.CommandResult{}, nil
	}
	t.Cleanup(func() { tcpPingSend = orig })
	if _, err := h.sendTCPPing(1, nil, 10*time.Millisecond); !errors.Is(err, errTCPPingQueueTimeout) {
		t.Fatalf("err=%v, want queue timeout", err)
	}
}

func TestInterleaveByNode(t *testing.T) {
	type job struct {
		node int64
		i    int
	}
	in := []job{{1, 0}, {1, 1}, {1, 2}, {2, 0}, {2, 1}, {3, 0}}
	got := interleaveByNode(in, func(j job) int64 { return j.node })
	want := []job{{1, 0}, {2, 0}, {3, 0}, {1, 1}, {2, 1}, {1, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
