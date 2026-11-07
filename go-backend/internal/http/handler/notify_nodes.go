package handler

import (
	"encoding/json"
	"errors"
)

func isNodeNotifyEvent(event string) bool {
	switch event {
	case notifyEventNodeOffline, notifyEventInboundDown, notifyEventLatencyBad,
		notifyEventRouteChange, notifyEventKernelError, notifyEventNodeExpiry:
		return true
	}
	return false
}

func parseNodeNotifyEvents(raw string) map[string]bool {
	events := make(map[string]bool)
	if json.Unmarshal([]byte(raw), &events) != nil || events == nil {
		return map[string]bool{}
	}
	return events
}

// A missing field preserves the saved overrides; null or {} restores defaults.
func nodeNotifyEventsFromRequest(req map[string]interface{}) (*string, error) {
	value, exists := req["notifyEvents"]
	if !exists {
		return nil, nil
	}
	raw := ""
	if value == nil {
		return &raw, nil
	}
	values, ok := value.(map[string]interface{})
	if !ok {
		return nil, errors.New("服务器通知设置格式错误")
	}
	events := make(map[string]bool, len(values))
	for event, value := range values {
		enabled, ok := value.(bool)
		if !isNodeNotifyEvent(event) || !ok {
			return nil, errors.New("服务器通知设置包含无效事件或开关值")
		}
		events[event] = enabled
	}
	if len(events) > 0 {
		encoded, err := json.Marshal(events)
		if err != nil {
			return nil, err
		}
		raw = string(encoded)
	}
	return &raw, nil
}
