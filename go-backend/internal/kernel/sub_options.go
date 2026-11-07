package kernel

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// SubOptions is a subscription's processing pipeline. The steps run in a
// fixed order: filter → rename → flag → prefix → dedupe → sort → script;
// region groups are added when the profile is rendered.
type SubOptions struct {
	// Lines keeps "relay" or "direct" endpoints only; "" keeps both.
	// "prefer_relay" hides each inbound's direct endpoint when that same
	// inbound has a relay, before applying the other filters.
	Lines string
	// Regions keeps endpoints whose region (see RegionOf) is listed;
	// RegionOther stands for names without a recognized region.
	Regions []string
	// Protocols keeps endpoints using one of these protocols.
	Protocols []string
	// Include / Exclude match names: /regex/, or keywords split by "|".
	Include string
	Exclude string
	// Rename rules, one per line: "find => replace". A find wrapped in
	// slashes (/.../) is a regular expression; an empty replace deletes.
	Rename string
	// AddFlag puts the region's flag emoji in front of names lacking one.
	AddFlag bool
	Prefix  string
	// Dedupe drops endpoints pointing at the same address, port and
	// protocol as an earlier one.
	Dedupe bool
	// Sort is "" (panel order) or "name".
	Sort string
	// RegionGroups adds one auto-select group per detected region.
	RegionGroups bool
	// ChainLandingNames / ChainRelayNames pick nodes by their name before
	// the pipeline (Endpoint.Original): landing nodes are dialed through
	// the relay group (see markChain). ChainLanding / ChainRelay are the
	// older name expressions (like Include), used when no names are picked.
	ChainLandingNames []string
	ChainRelayNames   []string
	ChainLanding      string
	ChainRelay        string
	// ChainLandingSources / ChainRelaySources take every node of these
	// remote sources, including ones a later refresh brings, and
	// ChainLandingOwn every node of the panel's own. ChainNoneNames keeps
	// nodes they would take out of the chain; picked names win over all.
	ChainLandingSources []int64
	ChainRelaySources   []int64
	ChainLandingOwn     bool
	ChainNoneNames      []string
	// ChainSourceFilters narrows a chain source to the nodes whose original
	// name matches (a filter expression, like Include); ChainDropUnused
	// leaves a chain source's nodes without a role out of the subscription.
	ChainSourceFilters map[int64]string
	ChainDropUnused    bool
	// ChainHideRelays keeps relay nodes out of the node lists and groups:
	// they are only in the relay group.
	ChainHideRelays bool
	// Script is JavaScript run after the other steps (see sub_script.go).
	Script string
	// Rules adds routing rules, DNS and the service groups in RuleGroups
	// (see sub_rules.go) to Mihomo, sing-box and Surge profiles; it implies
	// RegionGroups there.
	Rules      bool
	RuleGroups []string
	// SingboxSnell puts Snell nodes in sing-box profiles: the client is
	// sing-box 1.14+ (see SingboxClientHasSnell).
	SingboxSnell bool
	// Templates are the user's own profiles by template kind
	// (TemplateClash, ...; see sub_template.go). A format with a template
	// renders into it instead of the built-in profile and rules.
	Templates map[string]string
	// Notices are placeholder nodes put first, after the other steps (see
	// sub_notice.go): info nodes, or why the subscription stopped working.
	Notices []string
}

// RegionOther is the filter value for names without a recognized region.
const RegionOther = "其他"

// RegionNames lists the region groups in display order.
func RegionNames() []string {
	out := make([]string, len(regions))
	for i, r := range regions {
		out[i] = r.name
	}
	return out
}

type matcher struct {
	re    *regexp.Regexp
	words []string
}

// ParseMatcher parses a filter expression: /regex/, or keywords separated
// by "|" (any of them, case-insensitive). Empty matches nothing.
func ParseMatcher(expr string) (*matcher, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}
	if len(expr) > 2 && strings.HasPrefix(expr, "/") && strings.HasSuffix(expr, "/") {
		re, err := regexp.Compile(expr[1 : len(expr)-1])
		if err != nil {
			return nil, fmt.Errorf("正则表达式错误：%s", expr)
		}
		return &matcher{re: re}, nil
	}
	m := &matcher{}
	for _, w := range strings.Split(expr, "|") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			m.words = append(m.words, w)
		}
	}
	return m, nil
}

func (m *matcher) match(name string) bool {
	if m.re != nil {
		return m.re.MatchString(name)
	}
	lower := strings.ToLower(name)
	for _, w := range m.words {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// EndpointProtocol is the protocol clients use for the endpoint.
func EndpointProtocol(e Endpoint) string {
	if e.Proxy != nil {
		if p := ProxyProtocol(e.Proxy); p != "" {
			return p
		}
		return e.Proxy.Type()
	}
	return e.Inbound.Protocol
}

// EndpointRegion is the endpoint's region group, or RegionOther.
func EndpointRegion(e Endpoint) string {
	if r := RegionOf(e.Name); r != "" {
		return r
	}
	return RegionOther
}

// Validate checks the expressions in the options.
func (o SubOptions) Validate() error {
	if _, err := ParseRenameRules(o.Rename); err != nil {
		return err
	}
	if _, err := ParseMatcher(o.Include); err != nil {
		return fmt.Errorf("包含：%v", err)
	}
	if _, err := ParseMatcher(o.Exclude); err != nil {
		return fmt.Errorf("排除：%v", err)
	}
	if _, err := ParseMatcher(o.ChainLanding); err != nil {
		return fmt.Errorf("落地节点：%v", err)
	}
	if _, err := ParseMatcher(o.ChainRelay); err != nil {
		return fmt.Errorf("中转节点：%v", err)
	}
	for _, expr := range o.ChainSourceFilters {
		if _, err := ParseMatcher(expr); err != nil {
			return fmt.Errorf("订阅源筛选：%v", err)
		}
	}
	return ValidateSubScript(o.Script)
}

// FilterEndpoints applies the filter step only.
func FilterEndpoints(endpoints []Endpoint, opts SubOptions) []Endpoint {
	include, _ := ParseMatcher(opts.Include)
	exclude, _ := ParseMatcher(opts.Exclude)
	regionSet := toSet(opts.Regions)
	protoSet := toSet(opts.Protocols)
	relayInbounds := map[int64]bool{}
	if opts.Lines == "prefer_relay" {
		for _, e := range endpoints {
			if !e.Direct && e.Inbound.ID > 0 {
				relayInbounds[e.Inbound.ID] = true
			}
		}
	}
	out := make([]Endpoint, 0, len(endpoints))
	for _, e := range endpoints {
		switch {
		case opts.Lines == "relay" && e.Direct, opts.Lines == "direct" && !e.Direct:
			continue
		case opts.Lines == "prefer_relay" && e.Direct && relayInbounds[e.Inbound.ID]:
			continue
		case len(regionSet) > 0 && !regionSet[EndpointRegion(e)]:
			continue
		case len(protoSet) > 0 && !protoSet[EndpointProtocol(e)]:
			continue
		case include != nil && !include.match(e.Name):
			continue
		case exclude != nil && exclude.match(e.Name):
			continue
		}
		out = append(out, e)
	}
	return out
}

func toSet(list []string) map[string]bool {
	if len(list) == 0 {
		return nil
	}
	m := make(map[string]bool, len(list))
	for _, v := range list {
		m[v] = true
	}
	return m
}

type renameRule struct {
	re   *regexp.Regexp
	find string
	repl string
}

// ParseRenameRules parses the rule text and reports the first bad line.
func ParseRenameRules(text string) ([]renameRule, error) {
	var rules []renameRule
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=>", 2)
		find := strings.TrimSpace(parts[0])
		repl := ""
		if len(parts) == 2 {
			repl = strings.TrimSpace(parts[1])
		}
		if find == "" {
			return nil, fmt.Errorf("第 %d 行：缺少要替换的内容", i+1)
		}
		rule := renameRule{find: find, repl: repl}
		if len(find) > 2 && strings.HasPrefix(find, "/") && strings.HasSuffix(find, "/") {
			re, err := regexp.Compile(find[1 : len(find)-1])
			if err != nil {
				return nil, fmt.Errorf("第 %d 行：正则表达式错误", i+1)
			}
			rule.re = re
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ApplySubOptions runs the pipeline. Invalid expressions are skipped
// (they are validated when saved).
func ApplySubOptions(endpoints []Endpoint, opts SubOptions) []Endpoint {
	return markChain(ProcessNodes(endpoints, opts), opts)
}

// ProcessNodes runs the node steps of the pipeline (filter, rename, flag,
// prefix, dedupe, sort, script) without the chain: a node group's part.
// Original keeps the name from before, which the chain picks nodes by.
func ProcessNodes(endpoints []Endpoint, opts SubOptions) []Endpoint {
	endpoints = FilterEndpoints(endpoints, opts)
	rules, _ := ParseRenameRules(opts.Rename)
	out := make([]Endpoint, 0, len(endpoints))
	seen := map[string]bool{}
	for _, e := range endpoints {
		if e.Original == "" {
			e.Original = e.Name
		}
		name := e.Name
		for _, r := range rules {
			if r.re != nil {
				name = r.re.ReplaceAllString(name, r.repl)
			} else {
				name = strings.ReplaceAll(name, r.find, r.repl)
			}
		}
		name = strings.TrimSpace(name)
		if name == "" {
			name = strings.TrimSpace(e.Name)
		}
		if opts.AddFlag {
			name = withFlag(name, e.Name)
		}
		if opts.Dedupe {
			key := strings.ToLower(strings.Trim(e.Host, "[]")) + "|" + fmt.Sprint(e.Port) + "|" + EndpointProtocol(e)
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		e.Name = opts.Prefix + name
		out = append(out, e)
	}
	if opts.Sort == "name" {
		sort.SliceStable(out, func(i, j int) bool { return NaturalLess(out[i].Name, out[j].Name) })
	}
	// A failing script leaves the nodes as the other steps made them; the
	// editor shows the error (SubScriptError).
	if scripted, err := ApplySubScript(out, opts.Script); err == nil {
		out = scripted
	}
	return out
}

// SubScriptError is the error the subscription's script gives on these
// endpoints, if any.
func SubScriptError(endpoints []Endpoint, opts SubOptions) error {
	if strings.TrimSpace(opts.Script) == "" {
		return nil
	}
	script := opts.Script
	opts.Script = ""
	_, err := ApplySubScript(ApplySubOptions(endpoints, opts), script)
	return err
}

// NaturalLess compares names with digit runs compared numerically, so
// "香港 2" sorts before "香港 10".
func NaturalLess(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if isDigit(ra[i]) && isDigit(rb[j]) {
			si := i
			for i < len(ra) && isDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && isDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	return len(ra)-i < len(rb)-j
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

type region struct {
	name     string
	keywords []string
}

// regions are matched in order; keywords are matched case-insensitively,
// short ASCII codes only as whole words.
var regions = []region{
	{"🇭🇰 香港", []string{"🇭🇰", "香港", "港", "hong kong", "hongkong", "hk"}},
	{"🇹🇼 台湾", []string{"🇹🇼", "台湾", "台灣", "taiwan", "tw"}},
	{"🇯🇵 日本", []string{"🇯🇵", "日本", "东京", "大阪", "japan", "tokyo", "osaka", "jp"}},
	{"🇸🇬 新加坡", []string{"🇸🇬", "新加坡", "狮城", "singapore", "sg"}},
	{"🇰🇷 韩国", []string{"🇰🇷", "韩国", "韓國", "首尔", "korea", "seoul", "kr"}},
	{"🇺🇸 美国", []string{"🇺🇸", "美国", "美國", "洛杉矶", "圣何塞", "united states", "los angeles", "us", "usa"}},
	{"🇬🇧 英国", []string{"🇬🇧", "英国", "伦敦", "united kingdom", "london", "uk", "gb"}},
	{"🇩🇪 德国", []string{"🇩🇪", "德国", "法兰克福", "germany", "frankfurt", "de"}},
	{"🇫🇷 法国", []string{"🇫🇷", "法国", "巴黎", "france", "paris", "fr"}},
	{"🇳🇱 荷兰", []string{"🇳🇱", "荷兰", "阿姆斯特丹", "netherlands", "amsterdam", "nl"}},
	{"🇨🇦 加拿大", []string{"🇨🇦", "加拿大", "canada", "ca"}},
	{"🇦🇺 澳大利亚", []string{"🇦🇺", "澳大利亚", "澳洲", "悉尼", "australia", "sydney", "au"}},
	{"🇷🇺 俄罗斯", []string{"🇷🇺", "俄罗斯", "莫斯科", "russia", "moscow", "ru"}},
	{"🇮🇳 印度", []string{"🇮🇳", "印度", "india", "in"}},
	{"🇹🇷 土耳其", []string{"🇹🇷", "土耳其", "turkey", "tr"}},
	{"🇻🇳 越南", []string{"🇻🇳", "越南", "vietnam", "vn"}},
	{"🇹🇭 泰国", []string{"🇹🇭", "泰国", "thailand", "th"}},
	{"🇵🇭 菲律宾", []string{"🇵🇭", "菲律宾", "philippines", "ph"}},
	{"🇲🇾 马来西亚", []string{"🇲🇾", "马来西亚", "malaysia", "my"}},
	{"🇦🇷 阿根廷", []string{"🇦🇷", "阿根廷", "argentina", "ar"}},
	{"🇧🇷 巴西", []string{"🇧🇷", "巴西", "brazil", "br"}},
	{"🇦🇪 阿联酋", []string{"🇦🇪", "阿联酋", "迪拜", "dubai", "uae"}},
}

var wordSplit = regexp.MustCompile(`[^a-z0-9]+`)

// RegionOf returns the region group name for an endpoint name, or "".
func RegionOf(name string) string {
	lower := strings.ToLower(name)
	words := map[string]bool{}
	for _, w := range wordSplit.Split(lower, -1) {
		if w != "" {
			words[w] = true
		}
	}
	for _, r := range regions {
		for _, k := range r.keywords {
			ascii := true
			for _, c := range k {
				if c > 127 {
					ascii = false
					break
				}
			}
			if ascii && !strings.Contains(k, " ") && len(k) <= 3 {
				if words[k] {
					return r.name
				}
				continue
			}
			if strings.Contains(lower, k) {
				return r.name
			}
		}
	}
	return ""
}

type regionGroup struct {
	name    string
	members []string
}

// regionGroups groups names by region, in the order of the region table.
// Regions with no nodes are left out.
func regionGroups(names []string) []regionGroup {
	byRegion := map[string][]string{}
	for _, n := range names {
		if r := RegionOf(n); r != "" {
			byRegion[r] = append(byRegion[r], n)
		}
	}
	var out []regionGroup
	for _, r := range regions {
		if m := byRegion[r.name]; len(m) > 0 {
			out = append(out, regionGroup{name: r.name, members: m})
		}
	}
	return out
}

// withFlag prefixes the region flag (from the new name, else the original)
// unless the name already carries a flag.
func withFlag(name, original string) string {
	if hasFlag(name) {
		return name
	}
	region := RegionOf(name)
	if region == "" {
		region = RegionOf(original)
	}
	if region == "" {
		return name
	}
	flag := strings.Fields(region)[0]
	return flag + " " + name
}

// hasFlag reports whether s contains a regional-indicator pair (a flag).
func hasFlag(s string) bool {
	prev := false
	for _, r := range s {
		ri := r >= 0x1F1E6 && r <= 0x1F1FF
		if ri && prev {
			return true
		}
		prev = ri
	}
	return false
}
