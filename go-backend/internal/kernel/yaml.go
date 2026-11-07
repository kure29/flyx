package kernel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// yamlNode is an ordered mapping so generated profiles keep a readable key
// order (name/type/server first) instead of Go's sorted map order.
type yamlNode []yamlField

type yamlField struct {
	Key   string
	Value interface{}
}

// marshalYAML renders a small YAML subset in block style. Strings are always
// emitted as JSON (double-quoted) scalars, which YAML parses identically, so
// no user-controlled value can change the document structure.
func marshalYAML(v interface{}) string {
	var b strings.Builder
	writeYAML(&b, v, 0, false)
	return b.String()
}

func writeYAML(b *strings.Builder, v interface{}, indent int, inList bool) {
	pad := strings.Repeat("  ", indent)
	switch t := v.(type) {
	case yamlNode:
		if len(t) == 0 {
			b.WriteString("{}\n")
			return
		}
		for i, f := range t {
			if !(inList && i == 0) {
				b.WriteString(pad)
			}
			b.WriteString(yamlKey(f.Key))
			b.WriteString(":")
			writeYAMLValue(b, f.Value, indent)
		}
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		node := make(yamlNode, 0, len(keys))
		for _, k := range keys {
			node = append(node, yamlField{k, t[k]})
		}
		writeYAML(b, node, indent, inList)
	default:
		b.WriteString(yamlScalar(v))
		b.WriteString("\n")
	}
}

func writeYAMLValue(b *strings.Builder, v interface{}, indent int) {
	switch t := v.(type) {
	case yamlNode, map[string]interface{}:
		if isEmptyMap(t) {
			b.WriteString(" {}\n")
			return
		}
		b.WriteString("\n")
		writeYAML(b, t, indent+1, false)
	case []interface{}:
		writeYAMLList(b, t, indent)
	case []string:
		list := make([]interface{}, len(t))
		for i, s := range t {
			list[i] = s
		}
		writeYAMLList(b, list, indent)
	default:
		b.WriteString(" ")
		b.WriteString(yamlScalar(v))
		b.WriteString("\n")
	}
}

func writeYAMLList(b *strings.Builder, list []interface{}, indent int) {
	if len(list) == 0 {
		b.WriteString(" []\n")
		return
	}
	b.WriteString("\n")
	pad := strings.Repeat("  ", indent+1)
	for _, item := range list {
		b.WriteString(pad)
		b.WriteString("- ")
		switch t := item.(type) {
		case yamlNode, map[string]interface{}:
			writeYAML(b, t, indent+2, true)
		default:
			b.WriteString(yamlScalar(t))
			b.WriteString("\n")
		}
	}
}

func isEmptyMap(v interface{}) bool {
	switch t := v.(type) {
	case yamlNode:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	}
	return false
}

func yamlKey(k string) string {
	for _, c := range k {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return yamlScalar(k)
		}
	}
	return k
}

func yamlScalar(v interface{}) string {
	switch t := v.(type) {
	case string:
		b, _ := json.Marshal(t)
		return string(b)
	case bool, int, int64, float64:
		return fmt.Sprint(t)
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
