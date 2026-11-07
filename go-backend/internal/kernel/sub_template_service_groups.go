package kernel

import "fmt"

// TemplateServiceGroup describes both the generated selector and its default
// policy. Node/filter groups never point back at service groups, so selectors
// cannot introduce cycles.
type TemplateServiceGroup struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Target  string   `json:"target"`
	Members []string `json:"members"`
}

func TemplateBuilderServiceGroups(kind string, b TemplateBuilder) []TemplateServiceGroup {
	groups := []TemplateServiceGroup{}
	if b.ServiceGroups != nil && !*b.ServiceGroups {
		return groups
	}
	used := map[string]bool{builderMain: true, "DIRECT": true, "REJECT": true, "direct": true}
	base := []string{builderMain}
	for _, g := range b.Groups {
		used[g.Name] = true
		base = append(base, g.Name)
	}
	base = append(base, "DIRECT")
	if kind != TemplateSingbox {
		base = append(base, "REJECT")
	}
	add := func(key, name, target string) {
		original := name
		for i := 1; used[name]; i++ {
			name = fmt.Sprintf("%s · 分流 %d", original, i)
		}
		used[name] = true
		members, seen := []string{}, map[string]bool{}
		for _, item := range append([]string{target}, base...) {
			if !seen[item] {
				members = append(members, item)
				seen[item] = true
			}
		}
		groups = append(groups, TemplateServiceGroup{Key: key, Name: name, Target: target, Members: members})
	}
	categories := builderCategories()
	for _, r := range b.Rules {
		if r.Type != "category" {
			continue
		}
		// Modern sing-box rejects with a route action; it has no block
		// outbound that a selector can pick. Keep explicit reject rules fixed.
		if kind == TemplateSingbox && r.Target == "REJECT" {
			continue
		}
		if c, ok := categories[r.Value]; ok {
			add(r.Value, c.Name, r.Target)
		}
	}
	add("__final", "漏网之鱼", b.Final)
	return groups
}

func builderServiceTargets(groups []TemplateServiceGroup) map[string]string {
	targets := map[string]string{}
	for _, g := range groups {
		targets[g.Key] = g.Name
	}
	return targets
}

func builderRoutedRule(r TemplateBuilderRule, targets map[string]string) TemplateBuilderRule {
	if r.Type == "category" && targets[r.Value] != "" {
		r.Target = targets[r.Value]
	}
	return r
}

func builderFinal(b TemplateBuilder, targets map[string]string) string {
	if name := targets["__final"]; name != "" {
		return name
	}
	return b.Final
}
