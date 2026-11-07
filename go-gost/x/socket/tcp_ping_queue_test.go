package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newTCPPingTestReporter(t *testing.T) (*WebSocketReporter, *websocket.Conn) {
	t.Helper()
	connections := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{}
	panel := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		conn, err := upgrader.Upgrade(rw, req, nil)
		if err != nil {
			return
		}
		connections <- conn
	}))
	t.Cleanup(panel.Close)
	agentConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(panel.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	panelConn := <-connections
	t.Cleanup(func() { panelConn.Close() })
	reporter := NewWebSocketReporter("", "tcp-ping-test-secret")
	reporter.conn, reporter.connected, reporter.aesCrypto = agentConn, true, nil
	t.Cleanup(reporter.Stop)
	panelConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	return reporter, panelConn
}

func newTCPPingTestTarget(t *testing.T) (int, *atomic.Int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	accepted := new(atomic.Int64)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conn.Close()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, accepted
}

func TestTCPPingQueueCompletesMoreThanEightTargets(t *testing.T) {
	for _, total := range []int{9, 30} {
		t.Run(fmt.Sprintf("targets_%d", total), func(t *testing.T) {
			reporter, panel := newTCPPingTestReporter(t)
			port, _ := newTCPPingTestTarget(t)
			// Even when other read-only commands are busy, TCP probes must queue.
			for i := 0; i < maxConcurrentReadCommands; i++ {
				reporter.readCommandSem <- struct{}{}
			}
			for i := 0; i < total; i++ {
				raw, err := json.Marshal(CommandMessage{
					Type: "TcpPing", RequestId: fmt.Sprintf("target-%d", i), TimeoutMS: 5000,
					Data: TcpPingRequest{IP: "127.0.0.1", Port: port, Count: 4, Timeout: 2000},
				})
				if err != nil {
					t.Fatal(err)
				}
				reporter.handleReceivedMessage(websocket.TextMessage, raw)
			}
			reporter.startTCPPingWorkers()
			seen := make(map[string]bool)
			for i := 0; i < total; i++ {
				var response struct {
					CommandResponse
					Data TcpPingResponse `json:"data"`
				}
				if err := panel.ReadJSON(&response); err != nil {
					t.Fatal(err)
				}
				if response.Type != "TcpPingResponse" || !response.Success || !response.Data.Success || response.Data.PacketLoss != 0 {
					t.Fatalf("healthy probe failed: %+v", response)
				}
				if seen[response.RequestId] || !strings.HasPrefix(response.RequestId, "target-") {
					t.Fatalf("duplicate or invalid request ID: %q", response.RequestId)
				}
				seen[response.RequestId] = true
			}
		})
	}
}

func TestTCPPingQueueSkipsExpiredTargets(t *testing.T) {
	reporter, panel := newTCPPingTestReporter(t)
	port, accepted := newTCPPingTestTarget(t)
	reporter.dispatchCommand(CommandMessage{
		Type: "TcpPing", RequestId: "expired", TimeoutMS: 1,
		Data: TcpPingRequest{IP: "127.0.0.1", Port: port, Count: 4, Timeout: 2000},
	})
	// Wait on the actual queued task's deadline, without timing the worker.
	task := <-reporter.tcpPingQueue
	<-task.ctx.Done()
	reporter.tcpPingQueue <- task
	reporter.startTCPPingWorkers()
	var response CommandResponse
	if err := panel.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.RequestId != "expired" || response.Message != "TCP探测排队超时" {
		t.Fatalf("unexpected expired response: %+v", response)
	}
	if got := accepted.Load(); got != 0 {
		t.Fatalf("expired target was probed %d times", got)
	}
}

func TestTCPPingQueueReportsFullWithoutBlockingReceiver(t *testing.T) {
	reporter, panel := newTCPPingTestReporter(t)
	for i := 0; i < maxQueuedTCPPings; i++ {
		reporter.dispatchCommand(CommandMessage{Type: "TcpPing", RequestId: fmt.Sprint(i)})
	}
	reporter.dispatchCommand(CommandMessage{Type: "TcpPing", RequestId: "overflow"})
	var response CommandResponse
	if err := panel.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	if response.Success || response.RequestId != "overflow" || response.Message != "TCP探测队列已满，请稍后重试" {
		t.Fatalf("unexpected queue-full response: %+v", response)
	}
}

func TestTCPPingExecutionHonorsCancellation(t *testing.T) {
	reporter, _ := newTCPPingTestReporter(t)
	port, accepted := newTCPPingTestTarget(t)
	ctx, cancel := context.WithCancel(reporter.ctx)
	defer cancel()
	type result struct {
		res TcpPingResponse
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := reporter.handleTcpPing(ctx, TcpPingRequest{IP: "127.0.0.1", Port: port, Count: 1000, Timeout: 5000})
		done <- result{res, err}
	}()
	deadline := time.After(time.Second)
	for accepted.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("probe did not start")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case r := <-done:
		// The attempts made before the cancellation are the result.
		if r.err != nil || !r.res.Success || r.res.PacketLoss != 0 {
			t.Fatalf("unexpected canceled probe result: %+v, %v", r.res, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("probe did not stop after cancellation")
	}
}

func TestTCPPingQueueStopsWithPendingAndRunningProbes(t *testing.T) {
	reporter, _ := newTCPPingTestReporter(t)
	port, accepted := newTCPPingTestTarget(t)
	for i := 0; i < 30; i++ {
		reporter.dispatchCommand(CommandMessage{
			Type: "TcpPing", RequestId: fmt.Sprint(i),
			Data: TcpPingRequest{IP: "127.0.0.1", Port: port, Count: 1000, Timeout: 5000},
		})
	}
	reporter.startTCPPingWorkers()
	deadline := time.After(time.Second)
	for accepted.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("probe did not start")
		case <-time.After(time.Millisecond):
		}
	}
	done := make(chan struct{})
	go func() {
		reporter.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for pending or running probes")
	}
}

func TestTCPPingCanceledBeforeAnyAttempt(t *testing.T) {
	reporter, _ := newTCPPingTestReporter(t)
	ctx, cancel := context.WithCancel(reporter.ctx)
	cancel()
	if _, err := reporter.handleTcpPing(ctx, TcpPingRequest{IP: "127.0.0.1", Port: 1, Count: 4, Timeout: 5000}); err == nil || err.Error() != "TCP探测已取消" {
		t.Fatalf("unexpected result: %v", err)
	}
}
