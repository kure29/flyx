package kernel

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

const (
	OutboundIPAuto     = "auto"
	OutboundIPv4Only   = "ipv4_only"
	OutboundIPv6Only   = "ipv6_only"
	OutboundPreferIPv4 = "prefer_ipv4"
	OutboundPreferIPv6 = "prefer_ipv6"
	ipv4ResolverTag    = "flyx-ipv4-resolver"
	ipv6ResolverTag    = "flyx-ipv6-resolver"
)

// NormalizeOutboundIPStrategy keeps old configurations automatic and rejects
// unsupported policies instead of silently falling back to a dual-stack exit.
func NormalizeOutboundIPStrategy(kernelType, strategy string) (string, error) {
	strategy = strings.TrimSpace(strategy)
	if strategy == "" {
		strategy = OutboundIPAuto
	}
	if strategy != OutboundIPAuto && strategy != OutboundIPv4Only && strategy != OutboundIPv6Only && !isOutboundIPPreference(strategy) {
		return "", fmt.Errorf("未知出站 IP 策略 %q", strategy)
	}
	if strategy != OutboundIPAuto && kernelType != TypeXray && kernelType != TypeSingbox {
		return "", fmt.Errorf("出站 IP 策略需要 Xray 或 sing-box 内核")
	}
	return strategy, nil
}

// Extra JSON may customize an inbound, but changing its routing identity or
// detouring into another inbound would bypass an inbound-scoped IP policy.
func ValidateOutboundIPExtra(kernelType, strategy string, extra json.RawMessage) error {
	policy, err := NormalizeOutboundIPStrategy(kernelType, strategy)
	if err != nil {
		return err
	}
	if policy == OutboundIPAuto || len(extra) == 0 {
		return nil
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(extra, &fields); err != nil {
		return fmt.Errorf("高级配置必须是 JSON 对象")
	}
	for _, key := range []string{"tag", "detour"} {
		if _, exists := fields[key]; exists {
			return fmt.Errorf("指定出站 IP 策略时，高级配置不能覆盖 %s 字段", key)
		}
	}
	return nil
}

// applyOutboundIPPolicies runs after chain/split/user rules have been built.
// Only scoped direct outbounds are cloned; their user accounting marks remain
// intact and automatic inbounds keep using the original shared outbounds.
func applyOutboundIPPolicies(kernelType string, cfg obj, inbounds []Inbound, preferGoDNS bool) error {
	var restricted []Inbound
	for _, in := range inbounds {
		strategy, err := NormalizeOutboundIPStrategy(kernelType, in.OutboundIPStrategy)
		if err != nil {
			return err
		}
		if strategy != OutboundIPAuto {
			in.OutboundIPStrategy = strategy
			restricted = append(restricted, in)
		}
	}
	if len(restricted) == 0 {
		return nil
	}
	routeKey, inboundKey, outboundKey, userKey := "route", "inbound", "outbound", "auth_user"
	if kernelType == TypeXray {
		routeKey, inboundKey, outboundKey, userKey = "routing", "inboundTag", "outboundTag", "user"
	}
	route, _ := cfg[routeKey].(obj)
	if route == nil {
		route = obj{}
		cfg[routeKey] = route
	}
	rules, _ := route["rules"].([]interface{})
	outbounds := cfg["outbounds"].([]interface{})
	byTag := map[string]obj{}
	for _, value := range outbounds {
		ob := value.(obj)
		if tag, ok := ob["tag"].(string); ok {
			byTag[tag] = ob
		}
	}
	var fallback []interface{}
	var resolvers []interface{}
	resolverAdded := map[string]bool{}
	// blockBefore[i] is a strict-mode rule that rejects the other family
	// right before rules[i], a rule that leaves through a direct outbound.
	blockBefore := map[int]obj{}
	// Proxy outbounds whose literal server is outside the inbound's family:
	// their rules reject, like a chain whose target is gone.
	unreachable := map[string]bool{}
	for _, in := range restricted {
		preferred := isOutboundIPPreference(in.OutboundIPStrategy)
		family, blockedFamily := 4, 6
		blockedCIDR, bindAddress, bindKey := "::/0", "0.0.0.0", "inet4_bind_address"
		domainStrategy, resolverTag := "ForceIPv4", ipv4ResolverTag
		if in.OutboundIPStrategy == OutboundIPv6Only || in.OutboundIPStrategy == OutboundPreferIPv6 {
			family, blockedFamily = 6, 4
			blockedCIDR, bindAddress, bindKey = "0.0.0.0/0", "::", "inet6_bind_address"
			domainStrategy, resolverTag = "ForceIPv6", ipv6ResolverTag
		}
		tagPrefix := fmt.Sprintf("ipv%d-", family)
		if preferred {
			tagPrefix = "prefer-" + tagPrefix
		}
		tags := singboxInboundTags(in)
		if kernelType == TypeXray {
			tags = []string{in.Tag()}
		} else {
			if !resolverAdded[resolverTag] {
				resolver := obj{"type": "local", "tag": resolverTag}
				if preferGoDNS {
					resolver["prefer_go"] = true
				}
				resolvers = append(resolvers, resolver)
				resolverAdded[resolverTag] = true
			}
		}
		patch := func(ob obj) error {
			if err := validateOutboundIPServers(in.OutboundIPStrategy, ob); err != nil {
				return fmt.Errorf("入站 %d：%w", in.ID, err)
			}
			if kernelType == TypeXray {
				stream, _ := ob["streamSettings"].(obj)
				if stream == nil {
					stream = obj{}
					ob["streamSettings"] = stream
				}
				sockopt, _ := stream["sockopt"].(obj)
				if sockopt == nil {
					sockopt = obj{}
					stream["sockopt"] = sockopt
				}
				if preferred {
					// Both A and AAAA must reach the connection racer. IPv4v6 /
					// IPv6v4 strategies only fall back when DNS returns no IPs.
					sockopt["domainStrategy"] = "ForceIP"
					sockopt["happyEyeballs"] = obj{"prioritizeIPv6": family == 6, "tryDelayMs": 250, "interleave": 1, "maxConcurrentTry": 4}
				} else {
					sockopt["domainStrategy"] = domainStrategy
					ob["sendThrough"] = bindAddress
					// Older Xray UDP packet writers resolve domains through
					// freedom settings rather than transport sockopt.
					if ob["protocol"] == "freedom" {
						settings, _ := ob["settings"].(obj)
						if settings == nil {
							settings = obj{}
							ob["settings"] = settings
						}
						settings["domainStrategy"] = domainStrategy
					}
				}
			} else if ob["detour"] == nil {
				ob["domain_resolver"] = obj{"server": resolverTag, "strategy": in.OutboundIPStrategy}
				if preferred {
					ob["fallback_delay"] = "250ms"
				} else {
					ob[bindKey] = bindAddress
				}
			}
			return nil
		}
		// The policy covers the local socket only: traffic sent on to a
		// next hop may reach either family there, so strict modes reject the
		// other family only on the way out through a direct outbound.
		familyBlock := func(rule obj) obj {
			block := obj{}
			for k, v := range rule {
				block[k] = v
			}
			if kernelType != TypeXray {
				delete(block, outboundKey)
				block["ip_version"], block["action"] = blockedFamily, "reject"
				return block
			}
			block[outboundKey] = "block"
			ips, ok := rule["ip"].([]string)
			if !ok {
				block["ip"] = []string{blockedCIDR}
				return block
			}
			// A rule matching listed CIDRs only needs its other-family ones.
			var other []string
			for _, cidr := range ips {
				if ip, _, err := net.ParseCIDR(cidr); err == nil && (ip.To4() == nil) == (blockedFamily == 6) {
					other = append(other, cidr)
				}
			}
			if len(other) == 0 {
				return nil
			}
			block["ip"] = other
			return block
		}
		// Chain/split proxy outbounds are already unique to this inbound.
		for tag, ob := range byTag {
			if tag == chainTag(in) || strings.HasPrefix(tag, chainTag(in)+"-") {
				if err := patch(ob); err != nil {
					unreachable[tag] = true
				}
			}
		}
		clones := map[string]string{}
		directTag := func(sourceTag string) string {
			if tag := clones[sourceTag]; tag != "" {
				return tag
			}
			original := byTag[sourceTag]
			if original == nil {
				return sourceTag
			}
			if original["type"] != "direct" && original["protocol"] != "freedom" {
				return sourceTag
			}
			// Round-trip through JSON before modifying nested sockopt maps.
			data, _ := json.Marshal(original)
			var copy obj
			_ = json.Unmarshal(data, &copy)
			tag := tagPrefix + in.Tag() + "-" + sourceTag
			copy["tag"] = tag
			_ = patch(copy) // A direct outbound contains no upstream server.
			outbounds = append(outbounds, copy)
			clones[sourceTag] = tag
			return tag
		}
		for i, value := range rules {
			rule := value.(obj)
			if !containsInboundTag(rule[inboundKey], tags) {
				continue
			}
			tag, ok := rule[outboundKey].(string)
			if !ok {
				continue
			}
			if unreachable[tag] {
				if kernelType == TypeXray {
					rule[outboundKey] = "block"
				} else {
					delete(rule, outboundKey)
					rule["action"] = "reject"
				}
				continue
			}
			if direct := directTag(tag); direct != tag {
				rule[outboundKey] = direct
				if !preferred {
					if block := familyBlock(rule); block != nil {
						blockBefore[i] = block
					}
				}
			}
		}
		if in.Via == nil {
			if !preferred {
				rule := obj{inboundKey: tags}
				if kernelType == TypeXray {
					rule["type"] = "field"
				}
				fallback = append(fallback, familyBlock(rule))
			}
			for _, id := range in.countedUsers() {
				user := interface{}([]string{User{ID: id}.Name()})
				if kernelType == TypeXray {
					user = xrayUserMatch(id)
				}
				rule := obj{inboundKey: tags, userKey: user, outboundKey: directTag(userOutboundTag(id))}
				if kernelType == TypeXray {
					rule["type"] = "field"
				}
				fallback = append(fallback, rule)
			}
			rule := obj{inboundKey: tags, outboundKey: directTag("direct")}
			if kernelType == TypeXray {
				rule["type"] = "field"
			}
			fallback = append(fallback, rule)
		}
	}
	// User-wide rules must stay after the new inbound-scoped fallbacks.
	var preprocess, scoped, shared []interface{}
	for i, value := range rules {
		rule := value.(obj)
		if rule["action"] == "sniff" {
			preprocess = append(preprocess, value)
		} else if rule[inboundKey] == nil {
			shared = append(shared, value)
		} else {
			if block := blockBefore[i]; block != nil {
				scoped = append(scoped, block)
			}
			scoped = append(scoped, value)
		}
	}
	route["rules"] = append(append(append(preprocess, scoped...), fallback...), shared...)
	if kernelType == TypeSingbox {
		route["final"] = "direct"
		// Added to the servers already there (the default local resolver);
		// automatic/shared dialers keep an existing default, else get the
		// first policy resolver so they are never ambiguous.
		dns, _ := cfg["dns"].(obj)
		if dns == nil {
			dns = obj{}
			cfg["dns"] = dns
		}
		servers, _ := dns["servers"].([]interface{})
		dns["servers"] = append(servers, resolvers...)
		if _, ok := route["default_domain_resolver"]; !ok {
			route["default_domain_resolver"] = resolvers[0].(obj)["tag"]
		}
	}
	cfg["outbounds"] = outbounds
	if kernelType == TypeXray {
		applyXrayUDPPreferences(cfg, restricted)
	}
	return nil
}

func isOutboundIPPreference(strategy string) bool {
	return strategy == OutboundPreferIPv4 || strategy == OutboundPreferIPv6
}

func containsInboundTag(value interface{}, tags []string) bool {
	values, _ := value.([]string)
	for _, candidate := range values {
		for _, tag := range tags {
			if candidate == tag {
				return true
			}
		}
	}
	return false
}

// ValidateOutboundIPServer rejects literal next hops outside the selected IP
// family. Domains are resolved by the kernel with the same strict policy.
func ValidateOutboundIPServer(strategy, address string) error {
	ip := net.ParseIP(strings.Trim(address, "[]"))
	if ip == nil || strategy == "" || strategy == OutboundIPAuto {
		return nil
	}
	if strategy == OutboundIPv4Only && ip.To4() == nil {
		return fmt.Errorf("仅 IPv4 出站无法连接 IPv6 地址，请改用 IPv4 地址或域名")
	}
	if strategy == OutboundIPv6Only && ip.To4() != nil {
		return fmt.Errorf("仅 IPv6 出站无法连接 IPv4 地址，请改用 IPv6 地址或域名")
	}
	return nil
}

// Only server address fields are checked, not client/listen addresses.
func validateOutboundIPServers(strategy string, value interface{}) error {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			if key == "server" || key == "address" {
				if address, ok := child.(string); ok {
					if err := ValidateOutboundIPServer(strategy, address); err != nil {
						return err
					}
				}
			}
			if err := validateOutboundIPServers(strategy, child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range v {
			if err := validateOutboundIPServers(strategy, child); err != nil {
				return err
			}
		}
	}
	return nil
}
