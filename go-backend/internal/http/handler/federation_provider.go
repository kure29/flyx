package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
	"go-backend/internal/ws"
)

type federationRuntimeReservePortRequest struct {
	ResourceKey   string `json:"resourceKey"`
	Protocol      string `json:"protocol"`
	RequestedPort int    `json:"requestedPort"`
}

type federationRuntimeTarget struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

type federationRuntimeApplyRoleRequest struct {
	ReservationID string                    `json:"reservationId"`
	ResourceKey   string                    `json:"resourceKey"`
	Role          string                    `json:"role"`
	Protocol      string                    `json:"protocol"`
	Strategy      string                    `json:"strategy"`
	Targets       []federationRuntimeTarget `json:"targets"`
}

type federationRuntimeReleaseRoleRequest struct {
	BindingID     string `json:"bindingId"`
	ReservationID string `json:"reservationId"`
	ResourceKey   string `json:"resourceKey"`
}

func federationRuntimeChainName(bindingID string) string {
	bindingID = strings.TrimSpace(bindingID)
	if bindingID == "" {
		return ""
	}
	return "fed_chain_" + bindingID
}

func buildFederationMiddleChainConfig(chainName string, runtimeID int64, protocol, strategy string, targets []federationRuntimeTarget, interfaceName string) (map[string]interface{}, error) {
	chainName = strings.TrimSpace(chainName)
	if chainName == "" {
		return nil, fmt.Errorf("chain name is required")
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("targets are required for middle role")
	}
	protocol = defaultString(protocol, "tls")
	nodeItems := make([]map[string]interface{}, 0, len(targets))
	for i, target := range targets {
		host := strings.TrimSpace(target.Host)
		if host == "" || target.Port <= 0 {
			return nil, fmt.Errorf("Invalid target")
		}
		targetProtocol := defaultString(target.Protocol, protocol)
		connector := map[string]interface{}{
			"type": "relay",
		}
		if isTCPTunnelProtocol(targetProtocol) {
			connector["metadata"] = map[string]interface{}{
				"nodelay":               true,
				"mux.keepaliveInterval": "15s",
				"mux.keepaliveTimeout":  "45s",
			}
		}
		if isKCPTunnelProtocol(targetProtocol) {
			connector["metadata"] = map[string]interface{}{
				"connectTimeout": "30s",
			}
		}
		nodeItems = append(nodeItems, map[string]interface{}{
			"name":      fmt.Sprintf("node_%d", i+1),
			"addr":      processServerAddress(fmt.Sprintf("%s:%d", host, target.Port)),
			"connector": connector,
			"dialer":    buildTunnelDialerConfig(targetProtocol),
		})
	}

	chainData := map[string]interface{}{
		"name": chainName,
		"hops": []map[string]interface{}{
			{
				"name": fmt.Sprintf("hop_%d", runtimeID),
				"selector": map[string]interface{}{
					"strategy":    runtimeTunnelStrategy(strategy),
					"maxFails":    1,
					"failTimeout": int64(600000000000),
				},
				"nodes": nodeItems,
			},
		},
	}
	if strings.TrimSpace(interfaceName) != "" {
		hops := chainData["hops"].([]map[string]interface{})
		hops[0]["interface"] = interfaceName
	}
	return chainData, nil
}

func updateChainPayload(chainName string, chainData map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"chain": chainName,
		"data":  chainData,
	}
}

type federationRuntimeDiagnoseRequest struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Count    int    `json:"count"`
	Timeout  int    `json:"timeout"`
	Protocol string `json:"protocol"`
}

type federationRuntimeCommandRequest struct {
	CommandType string      `json:"commandType"`
	Data        interface{} `json:"data"`
}

func buildFederationServiceConfig(serviceName, addr, protocol, role, chainName string, targetCount int, interfaceName string) map[string]interface{} {
	service := map[string]interface{}{
		"name": serviceName,
		"addr": addr,
		"handler": map[string]interface{}{
			"type": "relay",
		},
		"listener": buildTunnelListenerConfig(protocol),
	}
	if isTCPTunnelProtocol(protocol) {
		service["handler"].(map[string]interface{})["metadata"] = map[string]interface{}{
			"nodelay":               true,
			"mux.keepaliveInterval": "15s",
			"mux.keepaliveTimeout":  "45s",
		}
	}
	if isKCPTunnelProtocol(protocol) {
		service["handler"].(map[string]interface{})["metadata"] = map[string]interface{}{
			"connectTimeout": "30s",
		}
	}
	if role == "middle" {
		service["handler"].(map[string]interface{})["chain"] = chainName
		if targetCount > 1 {
			service["handler"].(map[string]interface{})["retries"] = targetCount - 1
		}
	}
	if role == "exit" && strings.TrimSpace(interfaceName) != "" {
		service["metadata"] = map[string]interface{}{"interface": interfaceName}
	}
	return service
}

func (h *Handler) federationRuntimeReservePort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	token := extractBearerToken(r)
	share, err := h.repo.GetPeerShareByToken(token)
	if err != nil || share == nil {
		response.WriteJSON(w, response.Err(401, "Unauthorized"))
		return
	}

	var req federationRuntimeReservePortRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}
	req.ResourceKey = strings.TrimSpace(req.ResourceKey)
	if req.ResourceKey == "" {
		response.WriteJSON(w, response.ErrDefault("resourceKey is required"))
		return
	}

	peerRoleRuntimeMu.Lock()
	defer peerRoleRuntimeMu.Unlock()
	share, err = h.repo.GetPeerShare(share.ID)
	if err != nil || share == nil || share.IsActive != 1 || (share.ExpiryTime > 0 && share.ExpiryTime <= time.Now().UnixMilli()) {
		response.WriteJSON(w, response.Err(403, "Share is unavailable"))
		return
	}

	if isPeerShareFlowExceeded(share) {
		response.WriteJSON(w, response.Err(403, "Share traffic limit exceeded"))
		return
	}

	existing, err := h.repo.GetPeerShareRuntimeByResourceKey(share.ID, req.ResourceKey)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if existing != nil && existing.ReleasePending != 0 {
		response.WriteJSON(w, response.ErrDefault("Runtime release is pending"))
		return
	}
	if existing != nil && existing.Status == 1 {
		response.WriteJSON(w, response.OK(map[string]interface{}{
			"reservationId": existing.ReservationID,
			"allocatedPort": existing.Port,
			"bindingId":     existing.BindingID,
		}))
		return
	}

	allocatedPort, err := h.pickPeerSharePort(share, req.RequestedPort)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	now := time.Now().UnixMilli()
	if existing != nil {
		existing.ReservationID = randomToken(24)
		existing.Protocol = defaultString(req.Protocol, "tls")
		existing.Port = allocatedPort
		existing.BindingID = ""
		existing.Role = ""
		existing.ChainName = ""
		existing.ServiceName = ""
		existing.Strategy = "round"
		existing.Target = ""
		existing.Applied = 0
		existing.Status = 1
		existing.UpdatedTime = now
		if err := h.repo.UpdatePeerShareRuntime(existing); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		response.WriteJSON(w, response.OK(map[string]interface{}{
			"reservationId": existing.ReservationID,
			"allocatedPort": existing.Port,
			"bindingId":     existing.BindingID,
		}))
		return
	}

	runtime := &repo.PeerShareRuntime{
		ShareID:       share.ID,
		NodeID:        share.NodeID,
		ReservationID: randomToken(24),
		ResourceKey:   req.ResourceKey,
		BindingID:     "",
		Role:          "",
		ChainName:     "",
		ServiceName:   "",
		Protocol:      defaultString(req.Protocol, "tls"),
		Strategy:      "round",
		Port:          allocatedPort,
		Target:        "",
		Applied:       0,
		Status:        1,
		CreatedTime:   now,
		UpdatedTime:   now,
	}
	if err := h.repo.CreatePeerShareRuntime(runtime); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"reservationId": runtime.ReservationID,
		"allocatedPort": runtime.Port,
		"bindingId":     runtime.BindingID,
	}))
}

func (h *Handler) federationRuntimeApplyRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	token := extractBearerToken(r)
	share, err := h.repo.GetPeerShareByToken(token)
	if err != nil || share == nil {
		response.WriteJSON(w, response.Err(401, "Unauthorized"))
		return
	}

	var req federationRuntimeApplyRoleRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	if req.Role != "middle" && req.Role != "exit" {
		response.WriteJSON(w, response.ErrDefault("Invalid role"))
		return
	}

	peerRoleRuntimeMu.Lock()
	defer peerRoleRuntimeMu.Unlock()
	share, err = h.repo.GetPeerShare(share.ID)
	if err != nil || share == nil || share.IsActive != 1 || (share.ExpiryTime > 0 && share.ExpiryTime <= time.Now().UnixMilli()) {
		response.WriteJSON(w, response.ErrDefault("Share is unavailable"))
		return
	}

	var runtime *repo.PeerShareRuntime
	if strings.TrimSpace(req.ReservationID) != "" {
		runtime, err = h.repo.GetPeerShareRuntimeByReservationID(share.ID, strings.TrimSpace(req.ReservationID))
	} else {
		runtime, err = h.repo.GetPeerShareRuntimeByResourceKey(share.ID, strings.TrimSpace(req.ResourceKey))
	}
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if runtime == nil || runtime.Status == 0 {
		response.WriteJSON(w, response.ErrDefault("Reservation not found"))
		return
	}

	if runtime.ReleasePending != 0 {
		response.WriteJSON(w, response.ErrDefault("Runtime release is pending"))
		return
	}
	if isPeerShareFlowExceeded(share) {
		response.WriteJSON(w, response.Err(403, "Share traffic limit exceeded"))
		return
	}
	if runtime.Role != "" && runtime.Role != req.Role {
		response.WriteJSON(w, response.ErrDefault("Runtime role cannot change without release"))
		return
	}
	if share.PortRangeStart > 0 && share.PortRangeEnd > 0 && (runtime.Port < share.PortRangeStart || runtime.Port > share.PortRangeEnd) {
		response.WriteJSON(w, response.ErrDefault("Reserved port is outside share range"))
		return
	}
	if strings.TrimSpace(runtime.BindingID) == "" {
		runtime.BindingID = randomToken(24)
	}
	runtime.Role = req.Role
	runtime.ServiceName = fmt.Sprintf("fed_svc_%d", runtime.ID)
	runtime.ChainName = ""
	if req.Role == "middle" {
		runtime.ChainName = federationRuntimeChainName(runtime.BindingID)
	}
	runtime.Protocol = defaultString(req.Protocol, runtime.Protocol)
	runtime.Strategy = defaultString(req.Strategy, "round")
	targetBytes, _ := json.Marshal(req.Targets)
	runtime.Target = string(targetBytes)
	if err := h.applyPeerShareRoleRuntime(runtime); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"bindingId":     runtime.BindingID,
		"reservationId": runtime.ReservationID,
		"allocatedPort": runtime.Port,
	}))
}

func (h *Handler) federationRuntimeReleaseRole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	token := extractBearerToken(r)
	share, err := h.repo.GetPeerShareByToken(token)
	if err != nil || share == nil {
		response.WriteJSON(w, response.Err(401, "Unauthorized"))
		return
	}

	var req federationRuntimeReleaseRoleRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}

	var runtime *repo.PeerShareRuntime
	if strings.TrimSpace(req.BindingID) != "" {
		runtime, err = h.repo.GetPeerShareRuntimeByBindingID(share.ID, strings.TrimSpace(req.BindingID))
	} else if strings.TrimSpace(req.ReservationID) != "" {
		runtime, err = h.repo.GetPeerShareRuntimeByReservationID(share.ID, strings.TrimSpace(req.ReservationID))
	} else if strings.TrimSpace(req.ResourceKey) != "" {
		runtime, err = h.repo.GetPeerShareRuntimeByResourceKey(share.ID, strings.TrimSpace(req.ResourceKey))
	} else {
		response.WriteJSON(w, response.ErrDefault("bindingId or reservationId or resourceKey is required"))
		return
	}
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if runtime == nil {
		response.WriteJSON(w, response.OKEmpty())
		return
	}

	if err := h.releasePeerShareRuntime(runtime); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) federationRuntimeDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	token := extractBearerToken(r)
	share, err := h.repo.GetPeerShareByToken(token)
	if err != nil || share == nil {
		response.WriteJSON(w, response.Err(401, "Unauthorized"))
		return
	}

	var req federationRuntimeDiagnoseRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}

	req.IP = strings.TrimSpace(req.IP)
	if req.IP == "" || req.Port <= 0 || req.Port > 65535 {
		response.WriteJSON(w, response.ErrDefault("Invalid target"))
		return
	}
	if req.Count <= 0 {
		req.Count = 4
	}
	if req.Timeout <= 0 || req.Timeout > int(diagnosisCommandTimeout/time.Millisecond) {
		req.Timeout = int(diagnosisCommandTimeout / time.Millisecond)
	}
	commandTimeout := time.Duration(req.Timeout) * time.Millisecond
	if commandTimeout <= 0 || commandTimeout > diagnosisCommandTimeout {
		commandTimeout = diagnosisCommandTimeout
	}

	data := map[string]interface{}{
		"ip":      req.IP,
		"port":    req.Port,
		"count":   req.Count,
		"timeout": req.Timeout,
	}
	var res ws.CommandResult
	if isUDPBasedProtocol(req.Protocol) {
		res, err = h.sendNodeCommandWithTimeout(share.NodeID, "UdpPing", data, commandTimeout, false, false)
	} else {
		res, err = h.sendTCPPing(share.NodeID, data, commandTimeout)
	}
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if res.Data == nil {
		response.WriteJSON(w, response.ErrDefault("Node did not return diagnosis data"))
		return
	}

	response.WriteJSON(w, response.OK(res.Data))
}

func (h *Handler) federationRuntimeCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	token := extractBearerToken(r)
	share, err := h.repo.GetPeerShareByToken(token)
	if err != nil || share == nil {
		response.WriteJSON(w, response.Err(401, "Unauthorized"))
		return
	}

	var req federationRuntimeCommandRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}
	cmd := strings.TrimSpace(req.CommandType)
	if cmd == "" {
		response.WriteJSON(w, response.ErrDefault("commandType is required"))
		return
	}
	if !isFederationRuntimeCommandAllowed(cmd) {
		response.WriteJSON(w, response.ErrDefault("command not allowed"))
		return
	}

	h.peerResourceMu.Lock()
	defer h.peerResourceMu.Unlock()
	// Recheck after acquiring the mutation lock: an earlier authentication
	// decision cannot authorize a recreation after quota/expiry cleanup.
	share, err = h.repo.GetPeerShare(share.ID)
	if err != nil || share == nil {
		response.WriteJSON(w, response.ErrDefault("share ownership unavailable"))
		return
	}
	if isFederationServiceCommand(cmd) {
		if err := validateFederationCommandPorts(share, req.Data); err != nil {
			response.WriteJSON(w, response.Err(403, err.Error()))
			return
		}
	}

	_, action := federationResourceCommandKind(cmd)
	if action != "delete" && (share.IsActive != 1 || (share.ExpiryTime > 0 && share.ExpiryTime <= time.Now().UnixMilli()) || isPeerShareFlowExceeded(share)) {
		response.WriteJSON(w, response.Err(403, "share is inactive, expired, or over quota"))
		return
	}
	if strings.EqualFold(cmd, "tcpping") {
		res, err := h.sendTCPPing(share.NodeID, req.Data, defaultNodeCommandTimeout)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		response.WriteJSON(w, response.OK(res))
		return
	}
	items, err := h.preparePeerResourceCommand(share, cmd, req.Data)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	var result interface{} = map[string]interface{}{"success": true}
	for _, item := range items {
		res, err := h.applyPeerShareResource(item)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		result = res
	}
	response.WriteJSON(w, response.OK(result))
}

type federationForwardServiceBinding struct {
	Name string
	Port int
}

func extractFederationServiceEntries(data interface{}) []map[string]interface{} {
	if data == nil {
		return nil
	}

	if entries := asMapSlice(data); len(entries) > 0 {
		return entries
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil
	}

	if entries := asMapSlice(dataMap["services"]); len(entries) > 0 {
		return entries
	}

	return nil
}

func parseFederationForwardServiceBindings(data interface{}) []federationForwardServiceBinding {
	serviceList := extractFederationServiceEntries(data)
	bindings := make([]federationForwardServiceBinding, 0, len(serviceList))
	for _, svcMap := range serviceList {
		name := normalizeForwardRuntimeServiceName(asString(svcMap["name"]))
		originalName := name
		if shareID, original, ok := parsePeerShareServiceName(asString(svcMap["name"])); ok {
			originalName = normalizeForwardRuntimeServiceName(original)
			name = peerShareResourceName(shareID, "service", originalName)
		}
		if name == "" {
			continue
		}
		if _, _, _, ok := parseFlowServiceIDs(originalName); !ok {
			continue
		}
		addr := strings.TrimSpace(asString(svcMap["addr"]))
		if addr == "" {
			continue
		}
		_, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			continue
		}
		bindings = append(bindings, federationForwardServiceBinding{Name: name, Port: port})
	}
	return bindings
}

func parseFederationForwardServiceNamesForRelease(data interface{}) []string {
	names := make(map[string]struct{})
	appendName := func(raw string) {
		name := normalizeForwardRuntimeServiceName(raw)
		if name == "" {
			return
		}
		if _, _, _, ok := parseFlowServiceIDs(name); !ok {
			return
		}
		names[name] = struct{}{}
	}

	for _, svcMap := range extractFederationServiceEntries(data) {
		appendName(asString(svcMap["name"]))
	}

	if dataMap, ok := data.(map[string]interface{}); ok {
		for _, item := range asAnySlice(dataMap["services"]) {
			appendName(asString(item))
		}
	}

	for _, item := range asAnySlice(data) {
		appendName(asString(item))
	}

	if len(names) == 0 {
		return nil
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (h *Handler) bindPeerShareForwardRuntimeServices(share *repo.PeerShare, data interface{}) error {
	if h == nil || h.repo == nil || share == nil {
		return nil
	}
	bindings := parseFederationForwardServiceBindings(data)
	if len(bindings) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()
	for _, binding := range bindings {
		runtime, err := h.repo.GetActiveForwardPeerShareRuntimeByPort(share.ID, binding.Port)
		if err != nil {
			return err
		}
		if runtime == nil {
			runtime, err = h.repo.GetActiveForwardPeerShareRuntimeByServiceName(share.ID, binding.Name)
			if err != nil {
				return err
			}
		}
		if runtime == nil {
			if err := h.repo.CreatePeerShareRuntime(&repo.PeerShareRuntime{
				ShareID:       share.ID,
				NodeID:        share.NodeID,
				ReservationID: randomToken(24),
				ResourceKey:   fmt.Sprintf("forward-runtime:%d:%s:%d:%s", share.ID, binding.Name, binding.Port, randomToken(8)),
				BindingID:     "",
				Role:          "forward",
				ChainName:     "",
				ServiceName:   binding.Name,
				Protocol:      "tcp",
				Strategy:      "fifo",
				Port:          binding.Port,
				Target:        "",
				Applied:       1,
				Status:        1,
				CreatedTime:   now,
				UpdatedTime:   now,
			}); err != nil {
				return err
			}
			continue
		}
		if runtime.ReleasePending != 0 {
			return fmt.Errorf("runtime release is pending")
		}
		if runtime.ServiceName == binding.Name && runtime.Applied == 1 && runtime.Port == binding.Port && runtime.Status == 1 {
			continue
		}
		runtime.ServiceName = binding.Name
		runtime.Port = binding.Port
		runtime.Applied = 1
		runtime.Status = 1
		runtime.UpdatedTime = now
		if strings.TrimSpace(runtime.Protocol) == "" {
			runtime.Protocol = "tcp"
		}
		if strings.TrimSpace(runtime.Strategy) == "" {
			runtime.Strategy = "fifo"
		}
		if err := h.repo.UpdatePeerShareRuntime(runtime); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) releasePeerShareForwardRuntimeServices(share *repo.PeerShare, data interface{}) {
	if h == nil || h.repo == nil || share == nil {
		return
	}
	names := parseFederationForwardServiceNamesForRelease(data)
	if len(names) == 0 {
		return
	}

	now := time.Now().UnixMilli()
	for _, name := range names {
		_ = h.repo.MarkForwardPeerShareRuntimeReleasedByServiceName(share.ID, name, now)
	}
}

func isFederationRuntimeCommandAllowed(commandType string) bool {
	switch strings.ToLower(strings.TrimSpace(commandType)) {
	case "addservice", "updateservice", "deleteservice", "pauseservice", "resumeservice", "addchains", "updatechains", "deletechains", "addlimiters", "updatelimiters", "deletelimiters", "addclimiters", "updateclimiters", "deleteclimiters", "tcpping":
		return true
	default:
		return false
	}
}

func isFederationServiceCommand(commandType string) bool {
	switch strings.ToLower(strings.TrimSpace(commandType)) {
	case "addservice", "updateservice":
		return true
	default:
		return false
	}
}

func validateFederationCommandPorts(share *repo.PeerShare, data interface{}) error {
	if share == nil || (share.PortRangeStart <= 0 && share.PortRangeEnd <= 0) {
		return nil
	}

	serviceList := extractFederationServiceEntries(data)
	if len(serviceList) == 0 {
		return nil
	}
	for _, svcMap := range serviceList {
		addr := asString(svcMap["addr"])
		if addr == "" {
			continue
		}
		_, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("invalid service address: %s", addr)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 {
			return fmt.Errorf("invalid port in service address: %s", addr)
		}
		if port < share.PortRangeStart || port > share.PortRangeEnd {
			return fmt.Errorf("port %d out of allowed range %d-%d", port, share.PortRangeStart, share.PortRangeEnd)
		}
	}

	return nil
}

func (h *Handler) pickPeerSharePort(share *repo.PeerShare, requestedPort int) (int, error) {
	if share == nil {
		return 0, fmt.Errorf("share not found")
	}
	if share.PortRangeStart <= 0 || share.PortRangeEnd <= 0 || share.PortRangeEnd < share.PortRangeStart {
		return 0, fmt.Errorf("No available port")
	}

	used := make(map[int]struct{})

	nodePorts, err := h.repo.ListUsedPortsOnNode(share.NodeID)
	if err != nil {
		return 0, err
	}
	for _, p := range nodePorts {
		used[p] = struct{}{}
	}

	ports, err := h.repo.ListActivePeerShareRuntimePorts(share.ID, share.NodeID)
	if err != nil {
		return 0, err
	}
	for _, p := range ports {
		if p > 0 {
			used[p] = struct{}{}
		}
	}

	if requestedPort > 0 {
		if requestedPort < share.PortRangeStart || requestedPort > share.PortRangeEnd {
			return 0, fmt.Errorf("Port out of range")
		}
		if _, ok := used[requestedPort]; ok {
			return 0, fmt.Errorf("No available port")
		}
		return requestedPort, nil
	}

	for p := share.PortRangeStart; p <= share.PortRangeEnd; p++ {
		if _, ok := used[p]; ok {
			continue
		}
		return p, nil
	}

	return 0, fmt.Errorf("No available port")
}
