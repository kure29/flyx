package kernel

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// Subscription scripts (SubOptions.Script): JavaScript the admin writes to
// filter, rename and reorder the nodes, run after the built-in steps:
//
//	function operator(nodes) {
//	  return nodes.filter(n => n.region === "🇭🇰 香港").map(n => {
//	    n.name = "HK " + n.name
//	    return n
//	  })
//	}
//
// Each node is a plain object; only a changed name is taken back, the other
// fields are for reading. The script runs in a bare goja VM (no network,
// files or timers) and is stopped after scriptTimeout.

const (
	scriptTimeout   = time.Second
	MaxScriptLength = 20000
)

// scriptNode is what the script sees for an endpoint; id ties a returned
// node back to its endpoint.
func scriptNode(i int, e Endpoint) map[string]interface{} {
	line := "relay"
	if e.Direct {
		line = "direct"
	}
	return map[string]interface{}{
		"id":       i,
		"name":     e.Name,
		"original": e.Original,
		"server":   strings.Trim(e.Host, "[]"),
		"port":     e.Port,
		"protocol": EndpointProtocol(e),
		"region":   RegionOf(e.Name),
		"line":     line,
	}
}

// ValidateSubScript checks that the script compiles.
func ValidateSubScript(script string) error {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	if len(script) > MaxScriptLength {
		return fmt.Errorf("脚本最多 %d 个字符", MaxScriptLength)
	}
	if _, err := goja.Compile("script.js", script, false); err != nil {
		return fmt.Errorf("脚本语法错误：%v", err)
	}
	return nil
}

// ApplySubScript runs the script's operator(nodes) over the endpoints and
// returns them in the order, and with the names, it returned.
func ApplySubScript(endpoints []Endpoint, script string) ([]Endpoint, error) {
	if strings.TrimSpace(script) == "" {
		return endpoints, nil
	}
	if err := ValidateSubScript(script); err != nil {
		return nil, err
	}
	vm := goja.New()
	timer := time.AfterFunc(scriptTimeout, func() { vm.Interrupt("timeout") })
	defer timer.Stop()
	// console.log is common in pasted scripts; keep it harmless.
	console := vm.NewObject()
	_ = console.Set("log", func(goja.FunctionCall) goja.Value { return goja.Undefined() })
	_ = vm.Set("console", console)

	if _, err := vm.RunString(script); err != nil {
		return nil, scriptError(err)
	}
	operator, ok := goja.AssertFunction(vm.Get("operator"))
	if !ok {
		return nil, errors.New("脚本里需要定义 function operator(nodes)")
	}
	nodes := make([]interface{}, len(endpoints))
	for i, e := range endpoints {
		nodes[i] = scriptNode(i, e)
	}
	result, err := operator(goja.Undefined(), vm.ToValue(nodes))
	if err != nil {
		return nil, scriptError(err)
	}
	list, ok := result.Export().([]interface{})
	if !ok {
		return nil, errors.New("operator 需要返回节点数组")
	}
	out := make([]Endpoint, 0, len(list))
	seen := map[int]bool{}
	for _, item := range list {
		node, ok := item.(map[string]interface{})
		if !ok {
			return nil, errors.New("operator 返回的数组里只能是节点对象")
		}
		i, ok := scriptIndex(node["id"])
		if !ok || i < 0 || i >= len(endpoints) || seen[i] {
			continue
		}
		seen[i] = true
		e := endpoints[i]
		if name, ok := node["name"].(string); ok && strings.TrimSpace(name) != "" {
			e.Name = strings.TrimSpace(name)
		}
		out = append(out, e)
	}
	return out, nil
}

func scriptIndex(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int64:
		return int(n), true
	case int:
		return n, true
	case float64:
		return int(n), n == float64(int(n))
	}
	return 0, false
}

func scriptError(err error) error {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return fmt.Errorf("脚本运行超过 %v，已停止", scriptTimeout)
	}
	var exc *goja.Exception
	if errors.As(err, &exc) {
		return fmt.Errorf("脚本出错：%s", exc.Value().String())
	}
	return fmt.Errorf("脚本出错：%v", err)
}
