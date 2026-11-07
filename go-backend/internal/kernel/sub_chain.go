package kernel

// Chained proxies in subscriptions (client side): landing nodes carry a
// dialer-proxy (Mihomo), detour (sing-box) or underlying-proxy (Surge) set
// to the relay group, so the client reaches a landing node through whichever
// relay node is selected there. Base64 share links cannot express a chain.

const (
	// ChainLandingGroup holds the exit nodes; it heads the node selection.
	ChainLandingGroup = "落地节点"
	// ChainRelayGroup holds the nodes landing nodes are dialed through.
	ChainRelayGroup = "中转节点"

	ChainRoleLanding = "landing"
	ChainRoleRelay   = "relay"
)

// markChain tags the endpoints the chain options match. A name matching
// both is a landing node: it must not relay for itself. Nothing is tagged
// unless both sides match a node. With ChainDropUnused, the nodes of chain
// sources that got no role are left out.
func markChain(endpoints []Endpoint, opts SubOptions) []Endpoint {
	isLanding, isRelay := chainMatchers(opts)
	if isLanding == nil || isRelay == nil {
		return endpoints
	}
	for i := range endpoints {
		e := &endpoints[i]
		switch {
		case isLanding(*e):
			e.ChainRole = ChainRoleLanding
		case isRelay(*e):
			e.ChainRole = ChainRoleRelay
		}
	}
	if _, _, ok := chainGroups(endpoints); !ok {
		for i := range endpoints {
			endpoints[i].ChainRole = ""
		}
		return endpoints
	}
	if !opts.ChainDropUnused {
		return endpoints
	}
	sources := idSet(append(append([]int64{}, opts.ChainLandingSources...), opts.ChainRelaySources...))
	kept := endpoints[:0]
	for _, e := range endpoints {
		if e.ChainRole != "" || !sources[e.SourceID] {
			kept = append(kept, e)
		}
	}
	return kept
}

// chainMatchers tell landing and relay nodes apart: by the picked names,
// sources and own nodes when there are any, else by the name expressions.
// Nil when unset.
func chainMatchers(opts SubOptions) (func(Endpoint) bool, func(Endpoint) bool) {
	if len(opts.ChainLandingNames) > 0 || len(opts.ChainRelayNames) > 0 || len(opts.ChainLandingSources) > 0 ||
		len(opts.ChainRelaySources) > 0 || opts.ChainLandingOwn {
		landing, relay, none := toSet(opts.ChainLandingNames), toSet(opts.ChainRelayNames), toSet(opts.ChainNoneNames)
		landingSrc, relaySrc := idSet(opts.ChainLandingSources), idSet(opts.ChainRelaySources)
		filters := map[int64]*matcher{}
		for id, expr := range opts.ChainSourceFilters {
			if m, err := ParseMatcher(expr); err == nil && m != nil {
				filters[id] = m
			}
		}
		// A picked name decides; otherwise the node's source (narrowed by
		// its filter) or ownership.
		byGroup := func(e Endpoint) bool {
			if landing[e.Original] || relay[e.Original] || none[e.Original] {
				return false
			}
			m := filters[e.SourceID]
			return m == nil || m.match(e.Original)
		}
		return func(e Endpoint) bool {
				return landing[e.Original] || byGroup(e) && (landingSrc[e.SourceID] || opts.ChainLandingOwn && e.Proxy == nil && e.SourceID == 0)
			}, func(e Endpoint) bool {
				return relay[e.Original] || byGroup(e) && relaySrc[e.SourceID]
			}
	}
	landing, _ := ParseMatcher(opts.ChainLanding)
	relay, _ := ParseMatcher(opts.ChainRelay)
	if landing == nil || relay == nil {
		return nil, nil
	}
	return func(e Endpoint) bool { return landing.match(e.Name) || landing.match(e.Original) },
		func(e Endpoint) bool { return relay.match(e.Name) || relay.match(e.Original) }
}

// idSet holds the non-zero ids.
func idSet(ids []int64) map[int64]bool {
	out := map[int64]bool{}
	for _, id := range ids {
		if id > 0 {
			out[id] = true
		}
	}
	return out
}

// listed reports whether a node goes into the node lists and groups: with
// ChainHideRelays a chain's relay nodes are only in the relay group.
func listed(e Endpoint, chain bool, opts SubOptions) bool {
	return !chain || !opts.ChainHideRelays || e.ChainRole != ChainRoleRelay
}

// chainGroups returns the landing and relay names among the endpoints a
// format could render; the chain applies only when both are non-empty.
func chainGroups(endpoints []Endpoint) (landing, relay []string, ok bool) {
	for _, e := range endpoints {
		switch e.ChainRole {
		case ChainRoleLanding:
			landing = append(landing, e.Name)
		case ChainRoleRelay:
			relay = append(relay, e.Name)
		}
	}
	return landing, relay, len(landing) > 0 && len(relay) > 0
}

func toInterfaces(names []string) []interface{} {
	out := make([]interface{}, len(names))
	for i, n := range names {
		out[i] = n
	}
	return out
}
