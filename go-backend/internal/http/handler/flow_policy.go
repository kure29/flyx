package handler

import (
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"time"
)

type gostConfigSnapshot struct {
	Services []namedConfigItem `json:"services"`
	Chains   []namedConfigItem `json:"chains"`
	Limiters []namedConfigItem `json:"limiters"`
}

type namedConfigItem struct {
	Name    string `json:"name"`
	Limiter string `json:"limiter,omitempty"`
	Handler *struct {
		Chain string `json:"chain"`
	} `json:"handler,omitempty"`
}

func (h *Handler) processFlowItem(nodeID int64, item flowItem) {
	if h == nil || h.repo == nil || nodeID <= 0 {
		return
	}
	metas, err := h.repo.GetFlowUploadForwardMetas(collectFlowUploadForwardIDs([]flowItem{item}))
	if err != nil {
		log.Printf("flow metadata read failed node_id=%d err=%v", nodeID, err)
		return
	}
	if err := validateFlowUploadItems([]flowItem{item}); err != nil {
		log.Printf("flow upload rejected node_id=%d err=%v", nodeID, err)
		return
	}
	h.applyFlowUploadBatch(nodeID, h.buildNodeFlowUploadBatch(nodeID, []flowItem{item}, metas), time.Now())
}

func parseFlowServiceIDs(serviceName string) (int64, int64, int64, bool) {
	parts := strings.Split(serviceName, "_")
	if len(parts) < 3 {
		return 0, 0, 0, false
	}

	forwardID, err1 := strconv.ParseInt(parts[0], 10, 64)
	userID, err2 := strconv.ParseInt(parts[1], 10, 64)
	userTunnelID, err3 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || forwardID <= 0 || userID <= 0 {
		return 0, 0, 0, false
	}

	return forwardID, userID, userTunnelID, true
}

func parsePeerShareRuntimeServiceID(serviceName string) (int64, bool) {
	const prefix = "fed_svc_"
	if !strings.HasPrefix(serviceName, prefix) {
		return 0, false
	}
	raw := strings.TrimPrefix(serviceName, prefix)
	if raw == "" {
		return 0, false
	}
	parts := strings.SplitN(raw, "_", 2)
	runtimeID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || runtimeID <= 0 {
		return 0, false
	}
	return runtimeID, true
}

func parsePeerShareInfoFromFederationTunnelName(tunnelName string) (int64, int, bool) {
	tunnelName = strings.TrimSpace(tunnelName)
	if !strings.HasPrefix(tunnelName, "Share-") {
		return 0, 0, false
	}
	raw := strings.TrimPrefix(tunnelName, "Share-")
	idx := strings.Index(raw, "-Port-")
	if idx <= 0 {
		return 0, 0, false
	}
	shareID, err := strconv.ParseInt(raw[:idx], 10, 64)
	if err != nil || shareID <= 0 {
		return 0, 0, false
	}
	portValue := strings.TrimSpace(raw[idx+len("-Port-"):])
	port, err := strconv.Atoi(portValue)
	if err != nil || port <= 0 {
		return 0, 0, false
	}
	return shareID, port, true
}

func parsePeerShareIDFromFederationTunnelName(tunnelName string) (int64, bool) {
	tunnelName = strings.TrimSpace(tunnelName)
	if !strings.HasPrefix(tunnelName, "Share-") {
		return 0, false
	}
	raw := strings.TrimPrefix(tunnelName, "Share-")
	idx := strings.Index(raw, "-Port-")
	if idx <= 0 {
		return 0, false
	}
	shareID, err := strconv.ParseInt(raw[:idx], 10, 64)
	if err != nil || shareID <= 0 {
		return 0, false
	}
	return shareID, true
}

func (h *Handler) processPeerShareFlow(nodeID, runtimeID int64, item flowItem) {
	if h == nil || h.repo == nil || nodeID <= 0 || runtimeID <= 0 {
		return
	}
	runtime, err := h.repo.GetPeerShareRuntimeByID(runtimeID)
	if err != nil || runtime == nil || runtime.NodeID != nodeID || runtime.Status != 1 {
		return
	}
	h.addPeerShareFlow(nodeID, runtime.ShareID, item.D+item.U)
}

func (h *Handler) processPeerShareFlowFromForward(forwardID int64, nodeID int64, serviceName string, item flowItem) {
	if h == nil || h.repo == nil || forwardID <= 0 || nodeID <= 0 {
		return
	}
	// Prefer the reporting node's explicit shared ownership over a coincidentally
	// equal local forward ID. Never fall back to a service on another node.
	runtimes, err := h.repo.ListActiveForwardPeerShareRuntimesByNode(nodeID)
	if err != nil {
		return
	}
	for _, runtime := range runtimes {
		if normalizeForwardRuntimeServiceName(runtime.ServiceName) == normalizeForwardRuntimeServiceName(serviceName) {
			h.processPeerShareFlowByServiceName(nodeID, serviceName, item)
			return
		}
	}
	forward, err := h.getForwardRecord(forwardID)
	if err != nil || forward == nil {
		return
	}
	_, userID, _, ok := parseFlowServiceIDs(serviceName)
	if !ok || userID != forward.UserID {
		return
	}
	tunnelName, err := h.repo.GetTunnelName(forward.TunnelID)
	if err != nil {
		return
	}
	shareID, ok := parsePeerShareIDFromFederationTunnelName(tunnelName)
	if ok {
		h.addPeerShareFlow(nodeID, shareID, item.D+item.U)
	}
}

func normalizeForwardRuntimeServiceName(serviceName string) string {
	name := strings.TrimSpace(serviceName)
	if strings.HasSuffix(name, "_tcp") {
		return strings.TrimSuffix(name, "_tcp")
	}
	if strings.HasSuffix(name, "_udp") {
		return strings.TrimSuffix(name, "_udp")
	}
	return name
}

func (h *Handler) processPeerShareFlowByServiceName(nodeID int64, serviceName string, item flowItem) {
	if h == nil || h.repo == nil || nodeID <= 0 || strings.TrimSpace(serviceName) == "" {
		return
	}
	runtimes, err := h.repo.ListActiveForwardPeerShareRuntimesByNode(nodeID)
	if err != nil {
		return
	}
	var shareID int64
	for _, runtime := range runtimes {
		if normalizeForwardRuntimeServiceName(runtime.ServiceName) != normalizeForwardRuntimeServiceName(serviceName) {
			continue
		}
		if shareID != 0 {
			log.Printf("ambiguous peer share runtime service=%s node_id=%d", serviceName, nodeID)
			return
		}
		shareID = runtime.ShareID
	}
	if shareID > 0 {
		h.addPeerShareFlow(nodeID, shareID, item.D+item.U)
	}
}

func (h *Handler) enforcePeerShareFlowLimit(shareID int64) {
	if h == nil || h.repo == nil || shareID <= 0 {
		return
	}
	if err := h.cleanupPeerShareRuntimes(shareID); err != nil {
		log.Printf("peer share quota cleanup pending share_id=%d err=%v", shareID, err)
	}
}

func (h *Handler) cleanNodeConfigs(nodeID int64, rawConfig string) {
	if h == nil || h.repo == nil || nodeID <= 0 {
		return
	}
	if strings.TrimSpace(rawConfig) == "" {
		return
	}

	var snapshot gostConfigSnapshot
	if err := json.Unmarshal([]byte(rawConfig), &snapshot); err != nil {
		return
	}

	protection, err := h.loadForwardServiceProtection(nodeID)
	if err != nil {
		return
	}
	h.cleanOrphanedServicesWithProtection(nodeID, snapshot.Services, protection)
	// Dependencies are sent before services. A pending shared reservation may
	// therefore have chains/limiters that are not referenced in this snapshot yet.
	if protection.unbound {
		return
	}
	chainsInUse := make(map[string]struct{})
	limitersInUse := make(map[string]struct{})
	for _, service := range snapshot.Services {
		if service.Handler != nil {
			if chain := strings.TrimSpace(service.Handler.Chain); chain != "" {
				chainsInUse[chain] = struct{}{}
			}
		}
		for _, limiter := range strings.Split(service.Limiter, ",") {
			if limiter = strings.TrimSpace(limiter); limiter != "" {
				limitersInUse[limiter] = struct{}{}
			}
		}
	}
	// Keep dependencies referenced by the reported services, even when their
	// IDs belong to a different panel. Orphan dependencies can be collected on
	// the next report after their services have actually disappeared.
	h.cleanOrphanedChains(nodeID, snapshot.Chains, chainsInUse)
	h.cleanOrphanedLimiters(nodeID, snapshot.Limiters, limitersInUse)
}

type forwardServiceProtection struct {
	sharedNames map[string]struct{}
	unbound     bool
}

// Shared forward IDs belong to another panel and need not exist in our forward
// table. Use the same node-scoped ownership check for config and flow reports.
func (h *Handler) loadForwardServiceProtection(nodeID int64) (forwardServiceProtection, error) {
	protection := forwardServiceProtection{sharedNames: make(map[string]struct{})}
	// Read names and pending bindings in one snapshot, so a concurrent bind
	// cannot fall between two queries and disappear from both protections.
	runtimes, err := h.repo.ListActiveForwardPeerShareRuntimesByNode(nodeID)
	if err != nil {
		return protection, err
	}
	minUpdatedTime := time.Now().Add(-10 * time.Minute).UnixMilli()
	for _, runtime := range runtimes {
		serviceName := normalizeForwardRuntimeServiceName(runtime.ServiceName)
		if serviceName == "" {
			if runtime.Applied == 0 && runtime.UpdatedTime >= minUpdatedTime {
				protection.unbound = true
			}
			continue
		}
		protection.sharedNames[serviceName] = struct{}{}
	}
	resources, err := h.repo.ListPeerShareResourcesByNode(nodeID)
	if err != nil {
		return protection, err
	}
	for _, resource := range resources {
		if base := normalizeForwardRuntimeServiceName(resource.LegacyServiceBase); base != "" {
			protection.sharedNames[base] = struct{}{}
		}
	}
	return protection, nil
}

func (p forwardServiceProtection) preserves(serviceName string) bool {
	_, shared := p.sharedNames[normalizeForwardRuntimeServiceName(serviceName)]
	return shared || p.unbound
}

func (h *Handler) cleanOrphanedServices(nodeID int64, services []namedConfigItem) {
	if h == nil || h.repo == nil || nodeID <= 0 {
		return
	}
	protection, err := h.loadForwardServiceProtection(nodeID)
	if err != nil {
		// A failed ownership lookup must never authorize deletion.
		return
	}
	h.cleanOrphanedServicesWithProtection(nodeID, services, protection)
}

func (h *Handler) cleanOrphanedServicesWithProtection(nodeID int64, services []namedConfigItem, protection forwardServiceProtection) {
	for _, item := range services {
		name := strings.TrimSpace(item.Name)
		if name == "" || name == "web_api" {
			continue
		}
		if strings.HasPrefix(name, "fed_svc_") || strings.HasPrefix(name, "peer-share-") {
			continue
		}
		if _, ok := protection.sharedNames[normalizeForwardRuntimeServiceName(name)]; ok {
			continue
		}

		parts := strings.Split(name, "_")
		if len(parts) == 2 && parts[0] == "tunnel" {
			tunnelID, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil && tunnelID > 0 && !h.tunnelExists(tunnelID) {
				_, _ = h.sendNodeCommand(nodeID, "DeleteService", map[string]interface{}{"services": []string{name}}, false, true)
			}
			continue
		}

		if _, _, _, ok := parseFlowServiceIDs(name); ok {
			h.deleteOrphanedForwardService(nodeID, name, protection)
			continue
		}
		suffix := parts[len(parts)-1]

		switch suffix {
		case "tls", "kcp", "wss", "mtls", "mwss", "mtcp":
			tunnelID, err := strconv.ParseInt(parts[0], 10, 64)
			if err != nil || tunnelID <= 0 || h.tunnelExists(tunnelID) {
				continue
			}
			_, _ = h.sendNodeCommand(nodeID, "DeleteService", map[string]interface{}{"services": []string{name}}, false, true)
		case "tcp":
			if len(parts) < 4 {
				tunnelID, err := strconv.ParseInt(parts[0], 10, 64)
				if err == nil && tunnelID > 0 && !h.tunnelExists(tunnelID) {
					_, _ = h.sendNodeCommand(nodeID, "DeleteService", map[string]interface{}{"services": []string{name}}, false, true)
				}
				continue
			}
		}
	}
}

func (h *Handler) cleanOrphanedChains(nodeID int64, chains []namedConfigItem, inUse map[string]struct{}) {
	for _, item := range chains {
		name := strings.TrimSpace(item.Name)
		if name == "" || strings.HasPrefix(name, "fed_chain_") || strings.HasPrefix(name, "peer-share-") {
			continue
		}
		if _, ok := inUse[name]; ok {
			continue
		}

		idx := strings.LastIndex(name, "_")
		if idx <= 0 || idx >= len(name)-1 {
			continue
		}
		tunnelID, err := strconv.ParseInt(name[idx+1:], 10, 64)
		if err != nil || tunnelID <= 0 {
			continue
		}
		exists, err := h.repo.TunnelExists(tunnelID)
		if err != nil || exists {
			continue
		}
		_, _ = h.sendNodeCommand(nodeID, "DeleteChains", map[string]interface{}{"chain": name}, false, true)
	}
}

func (h *Handler) cleanOrphanedLimiters(nodeID int64, limiters []namedConfigItem, inUse map[string]struct{}) {
	for _, item := range limiters {
		name := strings.TrimSpace(item.Name)
		if name == "" || strings.HasPrefix(name, "peer-share-") {
			continue
		}
		// The panel no longer sets speed limits of its own: whatever limiter
		// no service uses is left over from before and goes.
		if _, ok := inUse[name]; ok {
			continue
		}
		_, _ = h.sendNodeCommand(nodeID, "DeleteLimiters", map[string]interface{}{"limiter": name}, false, true)
	}
}

func (h *Handler) tunnelExists(tunnelID int64) bool {
	ok, _ := h.repo.TunnelExists(tunnelID)
	return ok
}

func (h *Handler) forwardExists(forwardID int64) bool {
	ok, _ := h.repo.ForwardExists(forwardID)
	return ok
}

func (h *Handler) sendDeleteOrphanedForwardService(nodeID int64, serviceName string) {
	h.sendDeleteOrphanedForwardServices(nodeID, []string{serviceName})
}

func (h *Handler) sendDeleteOrphanedForwardServices(nodeID int64, serviceNames []string) {
	if h == nil || h.repo == nil || nodeID <= 0 || len(serviceNames) == 0 {
		return
	}
	protection, err := h.loadForwardServiceProtection(nodeID)
	if err != nil {
		return
	}
	seen := make(map[string]struct{}, len(serviceNames))
	for _, serviceName := range serviceNames {
		serviceName = normalizeForwardRuntimeServiceName(serviceName)
		if _, ok := seen[serviceName]; ok {
			continue
		}
		seen[serviceName] = struct{}{}
		h.deleteOrphanedForwardService(nodeID, serviceName, protection)
	}
}

func (h *Handler) deleteOrphanedForwardService(nodeID int64, serviceName string, protection forwardServiceProtection) {
	forwardID, _, _, ok := parseFlowServiceIDs(serviceName)
	if !ok || protection.preserves(serviceName) {
		return
	}
	parts := strings.Split(serviceName, "_")
	base := parts[0] + "_" + parts[1] + "_" + parts[2]
	// Parsing accepts legacy suffixes, while deletion targets the entire base
	// family. Verify the actual targets cannot include a protected share.
	if protection.preserves(base) {
		return
	}
	// Batch metadata can be missing after a read failure, or stale by the time
	// cleanup runs. Confirm absence before issuing a destructive command.
	exists, err := h.repo.ForwardExists(forwardID)
	if err != nil || exists {
		return
	}
	_, _ = h.sendNodeCommand(nodeID, "DeleteService", map[string]interface{}{
		"services": buildForwardServiceDeleteNames([]string{base}),
	}, false, true)
}
