package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"go-backend/internal/http/client"
	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

type createPeerShareRequest struct {
	Name           string `json:"name"`
	NodeID         int64  `json:"nodeId"`
	MaxBandwidth   int64  `json:"maxBandwidth"`
	ExpiryTime     int64  `json:"expiryTime"`
	PortRangeStart int    `json:"portRangeStart"`
	PortRangeEnd   int    `json:"portRangeEnd"`
	AllowedDomains string `json:"allowedDomains"`
	AllowedIPs     string `json:"allowedIps"`
}

type deletePeerShareRequest struct {
	ID int64 `json:"id"`
}

type resetPeerShareFlowRequest struct {
	ID int64 `json:"id"`
}

type updatePeerShareRequest struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	MaxBandwidth   int64  `json:"maxBandwidth"`
	ExpiryTime     int64  `json:"expiryTime"`
	PortRangeStart int    `json:"portRangeStart"`
	PortRangeEnd   int    `json:"portRangeEnd"`
	AllowedDomains string `json:"allowedDomains"`
	AllowedIPs     string `json:"allowedIps"`
}

type peerShareUsedPort struct {
	RuntimeID   int64  `json:"runtimeId"`
	Port        int    `json:"port"`
	Role        string `json:"role"`
	Protocol    string `json:"protocol"`
	ResourceKey string `json:"resourceKey"`
	Applied     int    `json:"applied"`
	UpdatedTime int64  `json:"updatedTime"`
}

type peerShareListItem struct {
	repo.PeerShare
	UsedPorts        []int               `json:"usedPorts"`
	UsedPortDetails  []peerShareUsedPort `json:"usedPortDetails"`
	ActiveRuntimeNum int                 `json:"activeRuntimeNum"`
}

type remoteUsageBindingItem struct {
	BindingID       int64  `json:"bindingId"`
	TunnelID        int64  `json:"tunnelId"`
	TunnelName      string `json:"tunnelName"`
	ChainType       int    `json:"chainType"`
	HopInx          int    `json:"hopInx"`
	AllocatedPort   int    `json:"allocatedPort"`
	ResourceKey     string `json:"resourceKey"`
	RemoteBindingID string `json:"remoteBindingId"`
	UpdatedTime     int64  `json:"updatedTime"`
}

type remoteUsageNodeItem struct {
	NodeID           int64                    `json:"nodeId"`
	NodeName         string                   `json:"nodeName"`
	RemoteURL        string                   `json:"remoteUrl"`
	ShareID          int64                    `json:"shareId"`
	PortRangeStart   int                      `json:"portRangeStart"`
	PortRangeEnd     int                      `json:"portRangeEnd"`
	MaxBandwidth     int64                    `json:"maxBandwidth"`
	CurrentFlow      int64                    `json:"currentFlow"`
	ExpiryTime       int64                    `json:"expiryTime"`
	UsedPorts        []int                    `json:"usedPorts"`
	Bindings         []remoteUsageBindingItem `json:"bindings"`
	ActiveBindingNum int                      `json:"activeBindingNum"`
	SyncError        string                   `json:"syncError,omitempty"`
}

func (h *Handler) federationShareList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	shares, err := h.repo.ListPeerShares()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	items := make([]peerShareListItem, 0, len(shares))
	for i := range shares {
		share := shares[i]
		runtimes, err := h.repo.ListActivePeerShareRuntimesByShareID(share.ID)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}

		usedSet := make(map[int]struct{}, len(runtimes))
		details := make([]peerShareUsedPort, 0, len(runtimes))
		for _, runtime := range runtimes {
			if runtime.Port > 0 {
				usedSet[runtime.Port] = struct{}{}
			}
			details = append(details, peerShareUsedPort{
				RuntimeID:   runtime.ID,
				Port:        runtime.Port,
				Role:        runtime.Role,
				Protocol:    runtime.Protocol,
				ResourceKey: runtime.ResourceKey,
				Applied:     runtime.Applied,
				UpdatedTime: runtime.UpdatedTime,
			})
		}

		usedPorts := make([]int, 0, len(usedSet))
		for port := range usedSet {
			usedPorts = append(usedPorts, port)
		}
		sort.Ints(usedPorts)

		sort.Slice(details, func(i, j int) bool {
			if details[i].Port == details[j].Port {
				return details[i].RuntimeID < details[j].RuntimeID
			}
			return details[i].Port < details[j].Port
		})

		items = append(items, peerShareListItem{
			PeerShare:        share,
			UsedPorts:        usedPorts,
			UsedPortDetails:  details,
			ActiveRuntimeNum: len(details),
		})
	}

	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) federationShareCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	var req createPeerShareRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}

	if req.Name == "" || req.NodeID == 0 {
		response.WriteJSON(w, response.ErrDefault("Name and NodeID are required"))
		return
	}

	if req.MaxBandwidth < 0 {
		response.WriteJSON(w, response.ErrDefault("Max bandwidth cannot be negative"))
		return
	}

	if req.ExpiryTime < 0 {
		response.WriteJSON(w, response.ErrDefault("Expiry time cannot be negative"))
		return
	}

	if req.PortRangeStart < 0 || req.PortRangeStart > 65535 || req.PortRangeEnd < 0 || req.PortRangeEnd > 65535 {
		response.WriteJSON(w, response.ErrDefault("Invalid port range"))
		return
	}

	if req.PortRangeStart > req.PortRangeEnd {
		response.WriteJSON(w, response.ErrDefault("Port range start cannot be greater than end"))
		return
	}

	allowedIPs, err := normalizePeerShareAllowedIPs(req.AllowedIPs)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	node, err := h.repo.GetNodeByID(req.NodeID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if node == nil {
		response.WriteJSON(w, response.ErrDefault("Node not found"))
		return
	}
	if node.IsRemote == 1 {
		response.WriteJSON(w, response.ErrDefault("Only local nodes can be shared"))
		return
	}

	now := time.Now().UnixMilli()
	token := randomToken(32)

	share := &repo.PeerShare{
		Name:           req.Name,
		NodeID:         req.NodeID,
		Token:          token,
		MaxBandwidth:   req.MaxBandwidth,
		ExpiryTime:     req.ExpiryTime,
		PortRangeStart: req.PortRangeStart,
		PortRangeEnd:   req.PortRangeEnd,
		IsActive:       1,
		CreatedTime:    now,
		UpdatedTime:    now,
		AllowedDomains: req.AllowedDomains,
		AllowedIPs:     allowedIPs,
	}

	if err := h.repo.CreatePeerShare(share); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) federationShareDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	var req deletePeerShareRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}

	share, err := h.repo.GetPeerShare(req.ID)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if share != nil {
		// Revoke new allocations before cleanup; failed deletions keep the
		// disabled share and its reservations available for retry.
		share.IsActive = 0
		share.UpdatedTime = time.Now().UnixMilli()
		if err := h.repo.UpdatePeerShare(share); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}

	if err := h.cleanupPeerShareRuntimes(req.ID); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	h.cleanupFederationTunnels(req.ID)

	if err := h.repo.DeletePeerShare(req.ID); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) federationShareResetFlow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	var req resetPeerShareFlowRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}
	if req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("Share ID is required"))
		return
	}

	share, err := h.repo.GetPeerShare(req.ID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if share == nil {
		response.WriteJSON(w, response.ErrDefault("Share not found"))
		return
	}

	if err := h.repo.ResetPeerShareCurrentFlow(req.ID, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) federationShareUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	var req updatePeerShareRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("Invalid JSON"))
		return
	}
	if req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("Share ID is required"))
		return
	}

	share, err := h.repo.GetPeerShare(req.ID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if share == nil {
		response.WriteJSON(w, response.ErrDefault("Share not found"))
		return
	}

	if req.Name == "" {
		response.WriteJSON(w, response.ErrDefault("Name is required"))
		return
	}

	if req.MaxBandwidth < 0 {
		response.WriteJSON(w, response.ErrDefault("Max bandwidth cannot be negative"))
		return
	}

	if req.ExpiryTime < 0 {
		response.WriteJSON(w, response.ErrDefault("Expiry time cannot be negative"))
		return
	}

	if req.PortRangeStart < 0 || req.PortRangeStart > 65535 || req.PortRangeEnd < 0 || req.PortRangeEnd > 65535 {
		response.WriteJSON(w, response.ErrDefault("Invalid port range"))
		return
	}

	if req.PortRangeStart > req.PortRangeEnd {
		response.WriteJSON(w, response.ErrDefault("Port range start cannot be greater than end"))
		return
	}

	allowedIPs, err := normalizePeerShareAllowedIPs(req.AllowedIPs)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	share.Name = req.Name
	share.MaxBandwidth = req.MaxBandwidth
	share.ExpiryTime = req.ExpiryTime
	share.PortRangeStart = req.PortRangeStart
	share.PortRangeEnd = req.PortRangeEnd
	share.AllowedDomains = req.AllowedDomains
	share.AllowedIPs = allowedIPs
	share.UpdatedTime = time.Now().UnixMilli()

	if err := h.repo.UpdatePeerShare(share); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) federationRemoteUsageList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	remoteNodes, err := h.repo.ListRemoteNodes()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	fc := client.NewFederationClient()
	localDomain := h.federationLocalDomain()

	items := make([]remoteUsageNodeItem, 0)
	for _, node := range remoteNodes {
		nodeID := node.ID
		nodeName := node.Name

		shareID, maxBandwidth, currentFlow, expiryTime, portRangeStart, portRangeEnd := parseRemoteShareUsageConfig(node.RemoteConfig.String)

		var syncError string
		url := strings.TrimSpace(node.RemoteURL.String)
		token := strings.TrimSpace(node.RemoteToken.String)
		if url != "" && token != "" {
			info, connectErr := fc.Connect(url, token, localDomain)
			if connectErr != nil {
				syncError = connectErr.Error()
			} else if info != nil {
				shareID = info.ShareID
				maxBandwidth = info.MaxBandwidth
				currentFlow = info.CurrentFlow
				expiryTime = info.ExpiryTime
				portRangeStart = info.PortRangeStart
				portRangeEnd = info.PortRangeEnd

				configData, _ := json.Marshal(map[string]interface{}{
					"shareId":        info.ShareID,
					"maxBandwidth":   info.MaxBandwidth,
					"currentFlow":    info.CurrentFlow,
					"expiryTime":     info.ExpiryTime,
					"portRangeStart": info.PortRangeStart,
					"portRangeEnd":   info.PortRangeEnd,
				})
				_ = h.repo.UpdateNodeRemoteConfig(nodeID, string(configData))
			}
		}

		bindingRows, err := h.repo.ListActiveBindingsForNode(nodeID)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		forwardPortRows, err := h.repo.ListActiveForwardPortsForNode(nodeID)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}

		usedSet := make(map[int]struct{})
		bindings := make([]remoteUsageBindingItem, 0, len(bindingRows)+len(forwardPortRows))
		for _, b := range bindingRows {
			bindings = append(bindings, remoteUsageBindingItem{
				BindingID:       b.ID,
				TunnelID:        b.TunnelID,
				TunnelName:      b.TunnelName,
				ChainType:       b.ChainType,
				HopInx:          b.HopInx,
				AllocatedPort:   b.AllocatedPort,
				ResourceKey:     b.ResourceKey,
				RemoteBindingID: b.RemoteBindingID,
				UpdatedTime:     b.UpdatedTime,
			})
			if b.AllocatedPort > 0 {
				usedSet[b.AllocatedPort] = struct{}{}
			}
		}
		for _, fp := range forwardPortRows {
			bindings = append(bindings, remoteUsageBindingItem{
				BindingID:       -fp.ForwardID,
				TunnelID:        fp.TunnelID,
				TunnelName:      fp.TunnelName,
				ChainType:       1,
				HopInx:          0,
				AllocatedPort:   fp.Port,
				ResourceKey:     fmt.Sprintf("forward:%d", fp.ForwardID),
				RemoteBindingID: "",
				UpdatedTime:     fp.UpdatedTime,
			})
			if fp.Port > 0 {
				usedSet[fp.Port] = struct{}{}
			}
		}

		sort.Slice(bindings, func(i, j int) bool {
			if bindings[i].AllocatedPort == bindings[j].AllocatedPort {
				return bindings[i].BindingID < bindings[j].BindingID
			}
			return bindings[i].AllocatedPort < bindings[j].AllocatedPort
		})

		usedPorts := make([]int, 0, len(usedSet))
		for port := range usedSet {
			usedPorts = append(usedPorts, port)
		}
		sort.Ints(usedPorts)

		items = append(items, remoteUsageNodeItem{
			NodeID:           nodeID,
			NodeName:         nodeName,
			RemoteURL:        url,
			ShareID:          shareID,
			PortRangeStart:   portRangeStart,
			PortRangeEnd:     portRangeEnd,
			MaxBandwidth:     maxBandwidth,
			CurrentFlow:      currentFlow,
			ExpiryTime:       expiryTime,
			UsedPorts:        usedPorts,
			Bindings:         bindings,
			ActiveBindingNum: len(bindings),
			SyncError:        syncError,
		})
	}

	response.WriteJSON(w, response.OK(items))
}

func remoteNodePortRange(node *nodeRecord) (int, int) {
	if node == nil || node.IsRemote != 1 || node.RemoteConfig == "" {
		return 0, 0
	}
	_, _, _, _, portRangeStart, portRangeEnd := parseRemoteShareUsageConfig(node.RemoteConfig)
	return portRangeStart, portRangeEnd
}

func validateRemoteNodePort(node *nodeRecord, port int) error {
	if node == nil || node.IsRemote != 1 || port <= 0 {
		return nil
	}
	start, end := remoteNodePortRange(node)
	if start <= 0 || end <= 0 {
		return nil
	}
	if port < start || port > end {
		return fmt.Errorf("远程服务器端口 %d 超出允许范围 %d-%d", port, start, end)
	}
	return nil
}

func parseRemoteShareUsageConfig(raw string) (int64, int64, int64, int64, int, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, 0, 0, 0, 0
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return 0, 0, 0, 0, 0, 0
	}

	shareID := asInt64(cfg["shareId"], 0)
	maxBandwidth := asInt64(cfg["maxBandwidth"], 0)
	currentFlow := asInt64(cfg["currentFlow"], 0)
	expiryTime := asInt64(cfg["expiryTime"], 0)
	portRangeStart := int(asInt64(cfg["portRangeStart"], 0))
	portRangeEnd := int(asInt64(cfg["portRangeEnd"], 0))
	return shareID, maxBandwidth, currentFlow, expiryTime, portRangeStart, portRangeEnd
}
