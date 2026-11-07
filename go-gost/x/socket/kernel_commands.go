package socket

import (
	"encoding/json"
	"fmt"

	"github.com/go-gost/x/kernel"
)

// handleKernelCommand dispatches proxy-kernel commands. They are deliberately
// not mutation commands: an install may download for minutes and must not
// block forwarding changes behind the shared mutation queue.
func handleKernelCommand(cmdType string, data interface{}) (interface{}, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化数据失败: %v", err)
	}
	m := kernel.Default()
	switch cmdType {
	case "KernelApply":
		var req kernel.ApplyRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("解析内核配置失败: %v", err)
		}
		return m.Apply(req)
	case "KernelControl":
		var req struct {
			Type   string `json:"type"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("解析内核操作失败: %v", err)
		}
		return m.Control(req.Type, req.Action)
	case "KernelStatus":
		var req struct {
			Logs string `json:"logs"`
		}
		_ = json.Unmarshal(raw, &req)
		return m.Status(req.Logs), nil
	}
	return nil, fmt.Errorf("未知命令类型: %s", cmdType)
}
