package socket

import (
	"context"
	"time"
)

const (
	// tcpPingReplyMargin is kept back from the panel's budget for the
	// command's trip here and the reply's trip back.
	tcpPingReplyMargin = 500 * time.Millisecond
	minTCPPingBudget   = 500 * time.Millisecond
)

type queuedTCPPing struct {
	command CommandMessage
	ctx     context.Context
	cancel  context.CancelFunc
}

func (w *WebSocketReporter) startTCPPingWorkers() {
	w.workers.Add(maxConcurrentTCPPings)
	for i := 0; i < maxConcurrentTCPPings; i++ {
		go func() {
			defer w.workers.Done()
			w.runTCPPings()
		}()
	}
}

func (w *WebSocketReporter) enqueueTCPPing(cmd CommandMessage) {
	if w.ctx.Err() != nil {
		w.sendCommandFailure(cmd, "TCP探测已取消")
		return
	}
	// Older panels do not send a budget. Keep their tasks bounded as well.
	timeout := reporterReadWait
	if cmd.TimeoutMS > 0 {
		// The panel's clock started before the command got here: stop a
		// little earlier so the reason (or a partial result) reaches it
		// before it gives up waiting.
		timeout = time.Duration(cmd.TimeoutMS)*time.Millisecond - tcpPingReplyMargin
		if timeout < minTCPPingBudget {
			timeout = minTCPPingBudget
		}
	}
	ctx, cancel := context.WithTimeout(w.ctx, timeout)
	task := queuedTCPPing{command: cmd, ctx: ctx, cancel: cancel}
	select {
	case w.tcpPingQueue <- task:
	case <-w.ctx.Done():
		cancel()
		w.sendCommandFailure(cmd, "TCP探测已取消")
	default:
		cancel()
		w.sendCommandFailure(cmd, "TCP探测队列已满，请稍后重试")
	}
}

func (w *WebSocketReporter) runTCPPings() {
	for {
		if w.ctx.Err() != nil {
			return
		}
		select {
		case <-w.ctx.Done():
			return
		case task := <-w.tcpPingQueue:
			func() {
				defer task.cancel()
				if err := task.ctx.Err(); err != nil {
					message := "TCP探测已取消"
					if err == context.DeadlineExceeded {
						message = "TCP探测排队超时"
					}
					w.sendCommandFailure(task.command, message)
					return
				}
				w.routeCommandSafely(task.ctx, task.command)
			}()
		}
	}
}
