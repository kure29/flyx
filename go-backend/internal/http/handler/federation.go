package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/client"
	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

type federationTunnelRequest struct {
	Protocol   string `json:"protocol"`
	RemotePort int    `json:"remotePort"`
	Target     string `json:"target"`
}

type nodeImportRequest struct {
	RemoteURL string `json:"remoteUrl"`
	Token     string `json:"token"`
}

func (h *Handler) nodeImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	var req nodeImportRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}

	if req.RemoteURL == "" || req.Token == "" {
		response.WriteJSON(w, response.ErrDefault("Remote URL and Token are required"))
		return
	}
	rURL, err := url.Parse(req.RemoteURL)
	if err != nil || (rURL.Scheme != "http" && rURL.Scheme != "https") {
		response.WriteJSON(w, response.ErrDefault("Invalid Remote URL format"))
		return
	}
	if err := IsSafeRemoteAddr(rURL.Host); err != nil {
		response.WriteJSON(w, response.Err(403, "禁止将远程服务器地址设置为内部网络"))
		return
	}

	domainCfg, _ := h.repo.GetConfigByName("panel_domain")
	localDomain := ""
	if domainCfg != nil {
		localDomain = domainCfg.Value
	}

	fc := client.NewFederationClient()
	info, err := fc.Connect(req.RemoteURL, req.Token, localDomain)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "Failed to connect: "+err.Error()))
		return
	}

	// Prepare config json for local storage (metadata about limits)
	configData := map[string]interface{}{
		"shareId":        info.ShareID,
		"maxBandwidth":   info.MaxBandwidth,
		"currentFlow":    info.CurrentFlow,
		"expiryTime":     info.ExpiryTime,
		"portRangeStart": info.PortRangeStart,
		"portRangeEnd":   info.PortRangeEnd,
	}
	configBytes, _ := json.Marshal(configData)

	portRange := "0"
	if info.PortRangeStart > 0 && info.PortRangeEnd >= info.PortRangeStart {
		portRange = fmt.Sprintf("%d-%d", info.PortRangeStart, info.PortRangeEnd)
	}

	inx := h.repo.NextIndex("node")
	now := time.Now().UnixMilli()

	if err = h.repo.CreateRemoteNode(
		fmt.Sprintf("%s (Remote)", info.NodeName),
		randomToken(16),
		info.ServerIP,
		portRange,
		now,
		info.Status,
		inx,
		req.RemoteURL,
		req.Token,
		string(configBytes),
	); err != nil {
		response.WriteJSON(w, response.Err(-2, "Database error: "+err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) federationTunnelCreate(w http.ResponseWriter, r *http.Request) {
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
	if isPeerShareFlowExceeded(share) {
		response.WriteJSON(w, response.Err(403, "Share traffic limit exceeded"))
		return
	}

	var req federationTunnelRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}

	if req.RemotePort < share.PortRangeStart || req.RemotePort > share.PortRangeEnd {
		response.WriteJSON(w, response.Err(403, "Port out of range"))
		return
	}

	usedPorts, err := h.repo.ListUsedPortsOnNode(share.NodeID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	for _, port := range usedPorts {
		if port == req.RemotePort {
			response.WriteJSON(w, response.Err(403, "Port already in use"))
			return
		}
	}

	runtimeOnPort, err := h.repo.GetActiveForwardPeerShareRuntimeByPort(share.ID, req.RemotePort)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if runtimeOnPort != nil {
		response.WriteJSON(w, response.Err(403, "Port already in use"))
		return
	}
	existsOnNodePort, err := h.repo.ExistsActivePeerShareRuntimeOnNodePort(share.NodeID, req.RemotePort)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if existsOnNodePort {
		response.WriteJSON(w, response.Err(403, "Port already in use"))
		return
	}

	now := time.Now().UnixMilli()
	tunnelID, err := h.repo.CreateFederationTunnel(
		fmt.Sprintf("Share-%d-Port-%d", share.ID, req.RemotePort),
		1,
		req.Protocol,
		now,
		share.NodeID,
		req.RemotePort,
	)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	runtime := &repo.PeerShareRuntime{
		ShareID:       share.ID,
		NodeID:        share.NodeID,
		ReservationID: randomToken(24),
		ResourceKey:   fmt.Sprintf("federation-forward-%d-%d-%d", share.ID, tunnelID, req.RemotePort),
		BindingID:     "",
		Role:          "forward",
		ChainName:     "",
		ServiceName:   "",
		Protocol:      defaultString(req.Protocol, "tcp"),
		Strategy:      "fifo",
		Port:          req.RemotePort,
		Target:        strings.TrimSpace(req.Target),
		Applied:       0,
		Status:        1,
		CreatedTime:   now,
		UpdatedTime:   now,
	}
	if err := h.repo.CreatePeerShareRuntime(runtime); err != nil {
		_ = h.deleteTunnelByID(tunnelID)
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"tunnelId": tunnelID,
	}))
}

func (h *Handler) syncRemoteNodeStatuses(items []map[string]interface{}) {
	type remoteEntry struct {
		index       int
		remoteURL   string
		remoteToken string
	}

	var remotes []remoteEntry
	for i, item := range items {
		isRemote, _ := item["isRemote"].(int)
		if isRemote != 1 {
			continue
		}
		url, _ := item["remoteUrl"].(string)
		token, _ := item["remoteToken"].(string)
		url = strings.TrimSpace(url)
		token = strings.TrimSpace(token)
		if url == "" || token == "" {
			continue
		}
		remotes = append(remotes, remoteEntry{index: i, remoteURL: url, remoteToken: token})
	}
	if len(remotes) == 0 {
		return
	}

	localDomain := h.federationLocalDomain()
	fc := client.NewFederationClientWithTimeout(5 * time.Second)

	type syncResult struct {
		index     int
		status    int
		syncError string
	}

	results := make([]syncResult, len(remotes))
	var wg sync.WaitGroup
	for i, entry := range remotes {
		wg.Add(1)
		go func(idx int, e remoteEntry) {
			defer wg.Done()
			info, err := fc.Connect(e.remoteURL, e.remoteToken, localDomain)
			if err != nil {
				errMsg := err.Error()
				if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "Invalid token") || strings.Contains(errMsg, "Unauthorized") {
					results[idx] = syncResult{index: e.index, status: 0, syncError: "provider_share_deleted"}
				} else if strings.Contains(errMsg, "403") || strings.Contains(errMsg, "Share is disabled") {
					results[idx] = syncResult{index: e.index, status: 0, syncError: "provider_share_disabled"}
				} else if strings.Contains(errMsg, "Share expired") {
					results[idx] = syncResult{index: e.index, status: 0, syncError: "provider_share_expired"}
				} else {
					results[idx] = syncResult{index: e.index, status: 0, syncError: errMsg}
				}
			} else {
				results[idx] = syncResult{index: e.index, status: info.Status, syncError: ""}
			}
		}(i, entry)
	}
	wg.Wait()

	for _, r := range results {
		items[r.index]["status"] = r.status
		if r.syncError != "" {
			items[r.index]["syncError"] = r.syncError
		}
	}
}

func (h *Handler) cleanupPeerShareRuntimes(shareID int64) error {
	if h == nil || h.repo == nil || shareID <= 0 {
		return nil
	}
	if err := h.releasePeerShareResources(shareID); err != nil {
		return err
	}
	runtimes, err := h.repo.ListActivePeerShareRuntimesByShareID(shareID)
	if err != nil {
		return err
	}
	var cleanupErr error
	for i := range runtimes {
		if err := h.releasePeerShareRuntime(&runtimes[i]); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func (h *Handler) cleanupFederationTunnels(shareID int64) {
	if h == nil || h.repo == nil || shareID <= 0 {
		return
	}
	namePrefix := fmt.Sprintf("Share-%d-Port-", shareID)
	tunnelIDs, err := h.repo.ListTunnelIDsByNamePrefix(namePrefix)
	if err != nil || len(tunnelIDs) == 0 {
		return
	}

	for _, tid := range tunnelIDs {
		_ = h.deleteTunnelByID(tid)
	}
}
