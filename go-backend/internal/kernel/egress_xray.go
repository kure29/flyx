package kernel

import (
	"encoding/json"
	"strings"
)

// Xray's Happy Eyeballs only races TCP connections. Direct and Shadowsocks
// UDP need separate outbounds so both the initial dial and packet writer use
// the preferred DNS family without pre-resolving TCP to a single family.
// UDP silence cannot reliably indicate a failed connection; no packets are
// replayed to a second address just because the destination has not replied.
func applyXrayUDPPreferences(cfg obj, inbounds []Inbound) {
	policies := map[string]string{}
	for _, in := range inbounds {
		if isOutboundIPPreference(in.OutboundIPStrategy) {
			policies[in.Tag()] = in.OutboundIPStrategy
		}
	}
	if len(policies) == 0 {
		return
	}
	outbounds := cfg["outbounds"].([]interface{})
	byTag := map[string]obj{}
	for _, value := range outbounds {
		ob := value.(obj)
		byTag[ob["tag"].(string)] = ob
	}
	routing := cfg["routing"].(obj)
	var rules []interface{}
	cloned := map[string]string{}
	for _, value := range routing["rules"].([]interface{}) {
		rule := value.(obj)
		var policy string
		for tag, candidate := range policies {
			if containsInboundTag(rule["inboundTag"], []string{tag}) {
				policy = candidate
				break
			}
		}
		sourceTag, _ := rule["outboundTag"].(string)
		ob := byTag[sourceTag]
		network, _ := rule["network"].(string)
		udp := network == "" || strings.Contains(network, "udp")
		if policy != "" && ob != nil && (ob["protocol"] == "freedom" || ob["protocol"] == "shadowsocks") && udp {
			tag := cloned[sourceTag]
			if tag == "" {
				data, _ := json.Marshal(ob)
				var copy obj
				_ = json.Unmarshal(data, &copy)
				tag = sourceTag + "-udp"
				copy["tag"] = tag
				strategy := "ForceIPv4v6"
				if policy == OutboundPreferIPv6 {
					strategy = "ForceIPv6v4"
				}
				if ob["protocol"] == "freedom" {
					settings, _ := copy["settings"].(obj)
					if settings == nil {
						settings = obj{}
						copy["settings"] = settings
					}
					settings["domainStrategy"] = strategy
				}
				sockopt := copy["streamSettings"].(obj)["sockopt"].(obj)
				sockopt["domainStrategy"] = strategy
				delete(sockopt, "happyEyeballs")
				outbounds = append(outbounds, copy)
				cloned[sourceTag] = tag
			}
			copy := obj{}
			for key, value := range rule {
				copy[key] = value
			}
			copy["network"], copy["outboundTag"] = "udp", tag
			rules = append(rules, copy)
		}
		rules = append(rules, rule)
	}
	routing["rules"] = rules
	cfg["outbounds"] = outbounds
}
