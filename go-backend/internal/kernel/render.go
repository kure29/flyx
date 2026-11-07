package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// File is one configuration file the agent writes before (re)starting a
// kernel. Name is relative to the kernel's run directory.
type File struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Process is one supervised process; Bin is "" for the kernel's own binary
// or the name of an extra binary.
type Process struct {
	Name string   `json:"name"`
	Bin  string   `json:"bin"`
	Args []string `json:"args"`
}

// Rendered is everything the agent needs to run one kernel.
type Rendered struct {
	Files     []File    `json:"files"`
	Args      []string  `json:"args"`
	Processes []Process `json:"processes,omitempty"`
	// Extras names helper binaries the processes need (e.g. shadow-tls).
	Extras []string `json:"extras,omitempty"`
	// Idle means there is nothing to run (a per-inbound kernel without
	// inbounds); the kernel is applied disabled.
	Idle bool `json:"idle,omitempty"`
	// UserMarks lists the users whose outgoing traffic carries UserMark, so
	// the agent can count it per user.
	UserMarks []int64 `json:"userMarks,omitempty"`
	Hash      string  `json:"hash"`
}

// RenderOptions are panel settings a kernel config depends on.
type RenderOptions struct {
	// RuleSetMirror prefixes the GitHub URLs sing-box downloads 分流 rule
	// sets from ("" downloads directly).
	RuleSetMirror string
	// SingboxHTTPClient defines the HTTP client rule sets download with
	// (sing-box 1.14+; older versions reject the field, newer ones drop
	// the implicit default in 1.16).
	SingboxHTTPClient bool
	// SingboxSnell lets sing-box dial Snell targets (chained outbounds and
	// 分流 targets); sing-box 1.14+.
	SingboxSnell bool
	// SingboxPreferGoDNS bypasses systemd-resolved DBus in local DNS
	// resolvers; supported by sing-box 1.13+.
	SingboxPreferGoDNS bool
	// NoUserMarks: the server may not set socket marks (no CAP_NET_ADMIN,
	// e.g. LXC/OpenVZ containers), where every marked dial fails with
	// "operation not permitted". Users then share the default outbounds and
	// their traffic is not counted per user (which needs nftables anyway).
	NoUserMarks bool
}

// Render produces the kernel configuration for the given enabled inbounds.
// Inbounds must already be normalized.
func Render(kernelType string, inbounds []Inbound) (Rendered, error) {
	return RenderWith(kernelType, inbounds, RenderOptions{})
}

// RenderWith is Render with panel settings.
func RenderWith(kernelType string, inbounds []Inbound, opts RenderOptions) (Rendered, error) {
	sorted := make([]Inbound, len(inbounds))
	copy(sorted, inbounds)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	if opts.NoUserMarks {
		for i := range sorted {
			sorted[i].noUserMarks = true
		}
	}
	for _, in := range sorted {
		if err := ValidateOutboundIPExtra(kernelType, in.OutboundIPStrategy, in.Extra); err != nil {
			return Rendered{}, err
		}
	}

	var out Rendered
	switch kernelType {
	case TypeXray:
		content, err := renderXray(sorted)
		if err != nil {
			return Rendered{}, err
		}
		out.Files = []File{{Name: "config.json", Content: content}}
		out.Args = []string{"run", "-c", "{RUN_DIR}/config.json"}
	case TypeSingbox:
		content, err := renderSingbox(sorted, opts)
		if err != nil {
			return Rendered{}, err
		}
		out.Files = []File{{Name: "config.json", Content: content}}
		out.Args = []string{"run", "-c", "{RUN_DIR}/config.json"}
	case TypeSnell:
		r, err := renderSnell(sorted)
		if err != nil {
			return Rendered{}, err
		}
		out = r
	default:
		return Rendered{}, fmt.Errorf("暂不支持内核 %s", kernelType)
	}
	out.UserMarks = markedUsers(sorted)
	out.Hash = hashRendered(out)
	return out, nil
}

func hashRendered(r Rendered) string {
	h := sha256.New()
	for _, f := range r.Files {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write([]byte(f.Content))
		h.Write([]byte{0})
	}
	for _, a := range r.Args {
		h.Write([]byte(a))
		h.Write([]byte{0})
	}
	for _, p := range r.Processes {
		h.Write([]byte(p.Name + "\x00" + p.Bin + "\x00" + strings.Join(p.Args, "\x00")))
		h.Write([]byte{1})
	}
	for _, e := range r.Extras {
		h.Write([]byte(e))
		h.Write([]byte{2})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func marshalConfig(v interface{}) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// mergeExtra deep-merges a user supplied JSON object into dst. Objects merge
// recursively; any other value replaces the default.
func mergeExtra(dst map[string]interface{}, extra json.RawMessage) error {
	if len(extra) == 0 {
		return nil
	}
	var src map[string]interface{}
	if err := json.Unmarshal(extra, &src); err != nil {
		return fmt.Errorf("高级配置解析失败: %v", err)
	}
	deepMerge(dst, src)
	return nil
}

func deepMerge(dst, src map[string]interface{}) {
	for k, v := range src {
		srcMap, srcIsMap := v.(map[string]interface{})
		dstMap, dstIsMap := dst[k].(map[string]interface{})
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
			continue
		}
		dst[k] = v
	}
}
