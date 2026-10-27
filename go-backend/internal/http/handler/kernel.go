package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

const (
	kernelApplyTimeout   = 5 * time.Minute
	kernelCommandTimeout = 20 * time.Second

	kernelStatusPending = "pending"
	kernelStatusRunning = "running"
	kernelStatusStopped = "stopped"
	kernelStatusError   = "error"
	// kernelStatusOffline is shown, never stored: the node is offline, so
	// the last known state may be stale (e.g. the server was reinstalled).
	kernelStatusOffline = "offline"
)

// kernelStatusOnNode is the status to show for a kernel: what the agent
// last reported, unless the node is offline and the report is out of date.
func kernelStatusOnNode(status string, node *model.Node) string {
	// Imported nodes have no kernel (and no status) to override.
	if status == "" || status == kernelStatusPending {
		return status
	}
	if node == nil || node.Status != 1 {
		return kernelStatusOffline
	}
	return status
}

var kernelStatusInterval = 60 * time.Second

// kernelApplyLocks serializes renders and dispatches per kernel so two edits
// in quick succession cannot reach the agent out of order.
var kernelApplyLocks sync.Map

func lockKernelApply(kernelID int64) func() {
	v, _ := kernelApplyLocks.LoadOrStore(kernelID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

type kernelView struct {
	ID               int64  `json:"id"`
	NodeID           int64  `json:"nodeId"`
	NodeName         string `json:"nodeName"`
	NodeOnline       bool   `json:"nodeOnline"`
	KernelType       string `json:"kernelType"`
	Version          string `json:"version"`
	InstalledVersion string `json:"installedVersion"`
	Status           string `json:"status"`
	LastError        string `json:"lastError"`
	Synced           bool   `json:"synced"`
	Enabled          bool   `json:"enabled"`
	InboundCount     int    `json:"inboundCount"`
	UpdatedTime      int64  `json:"updatedTime"`
}

type inboundView struct {
	ID           int64  `json:"id"`
	KernelID     int64  `json:"kernelId"`
	KernelType   string `json:"kernelType"`
	KernelStatus string `json:"kernelStatus"`
	External     bool   `json:"external"`
	// ImportedSummary / ImportedParams describe imported nodes, whose
	// parameters live in the stored proxy rather than the kernel fields.
	ImportedSummary  string             `json:"importedSummary,omitempty"`
	ImportedParams   [][2]string        `json:"importedParams,omitempty"`
	NodeID           int64              `json:"nodeId"`
	NodeName         string             `json:"nodeName"`
	NodeServerIP     string             `json:"nodeServerIp"`
	Name             string             `json:"name"`
	Protocol         string             `json:"protocol"`
	Listen           string             `json:"listen"`
	Port             int                `json:"port"`
	Network          string             `json:"network"`
	Settings         kernel.Settings    `json:"settings"`
	Transport        kernel.Transport   `json:"transport"`
	Security         kernelSecurityView `json:"security"`
	Extra            string             `json:"extra"`
	ShareAddressMode string             `json:"shareAddressMode"`
	ShareAddress     string             `json:"shareAddress"`
	ShareHost        string             `json:"shareHost"`
	Remark           string             `json:"remark"`
	RelayOnly        bool               `json:"relayOnly"`
	RelayCount       int                `json:"relayCount"`
	AcceptProxy      bool               `json:"acceptProxyProtocol"`
	Whitelist        bool               `json:"relayWhitelist"`
	WhitelistExtra   string             `json:"whitelistExtra"`
	Enabled          bool               `json:"enabled"`
	UploadBytes      int64              `json:"uploadBytes"`
	DownloadBytes    int64              `json:"downloadBytes"`
	CreatedTime      int64              `json:"createdTime"`
	UpdatedTime      int64              `json:"updatedTime"`
	// Probe is the last reachability check made from the panel server.
	Probe *reachProbe `json:"probe,omitempty"`
	// SourceID / SourceName: the remote subscription it came from.
	SourceID   int64  `json:"sourceId,omitempty"`
	SourceName string `json:"sourceName,omitempty"`
	// OutboundID is the inbound this one's traffic is chained through (0 =
	// direct); OutboundMissing flags a target that was deleted.
	OutboundID      int64  `json:"outboundId"`
	OutboundName    string `json:"outboundName,omitempty"`
	OutboundMissing bool   `json:"outboundMissing,omitempty"`
	// SplitRules are the inbound's 分流 rules.
	SplitRules         []kernel.SplitRule `json:"splitRules"`
	OutboundIPStrategy string             `json:"outboundIpStrategy"`
	Tags               []string           `json:"tags"`
}

// kernelSecurityView hides the self-signed private material and DNS API
// credentials from list output; saving without them keeps the stored ones.
type kernelSecurityView struct {
	kernel.Security
	CertPEM       string `json:"certPem,omitempty"`
	KeyPEM        string `json:"keyPem,omitempty"`
	ACMEDNSKey    string `json:"acmeDnsKey,omitempty"`
	ACMEDNSSecret string `json:"acmeDnsSecret,omitempty"`
	ACMEDNSSet    bool   `json:"acmeDnsSet,omitempty"`
}

type inboundRequest struct {
	OutboundIPStrategy  *string            `json:"outboundIpStrategy"`
	ID                  int64              `json:"id"`
	NodeID              int64              `json:"nodeId"`
	KernelType          string             `json:"kernelType"`
	Name                string             `json:"name"`
	Protocol            string             `json:"protocol"`
	Listen              string             `json:"listen"`
	Port                int                `json:"port"`
	Settings            kernel.Settings    `json:"settings"`
	Transport           kernel.Transport   `json:"transport"`
	Security            kernel.Security    `json:"security"`
	Extra               string             `json:"extra"`
	ShareAddressMode    *string            `json:"shareAddressMode"`
	ShareHost           string             `json:"shareHost"`
	Remark              string             `json:"remark"`
	RelayOnly           bool               `json:"relayOnly"`
	AcceptProxyProtocol bool               `json:"acceptProxyProtocol"`
	RelayWhitelist      bool               `json:"relayWhitelist"`
	WhitelistExtra      string             `json:"whitelistExtra"`
	OutboundID          int64              `json:"outboundId"`
	SplitRules          []kernel.SplitRule `json:"splitRules"`
	Enabled             *bool              `json:"enabled"`
}

// ---- conversion ----

func inboundFromModel(row model.NodeInbound) (kernel.Inbound, error) {
	in := kernel.Inbound{ID: row.ID, Protocol: row.Protocol, Listen: row.Listen, Port: row.Port,
		AcceptProxyProtocol: row.AcceptProxyProtocol == 1, OutboundIPStrategy: row.OutboundIPStrategy}
	if err := unmarshalOptional(row.Settings, &in.Settings); err != nil {
		return in, fmt.Errorf("入站 %d settings 损坏: %v", row.ID, err)
	}
	if err := unmarshalOptional(row.Transport, &in.Transport); err != nil {
		return in, fmt.Errorf("入站 %d transport 损坏: %v", row.ID, err)
	}
	if err := unmarshalOptional(row.Security, &in.Security); err != nil {
		return in, fmt.Errorf("入站 %d security 损坏: %v", row.ID, err)
	}
	if strings.TrimSpace(row.Extra) != "" {
		in.Extra = json.RawMessage(row.Extra)
	}
	return in, nil
}

func unmarshalOptional(raw string, out interface{}) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), out)
}

func applyInboundToModel(in kernel.Inbound, row *model.NodeInbound) error {
	settings, err := json.Marshal(in.Settings)
	if err != nil {
		return err
	}
	transport, err := json.Marshal(in.Transport)
	if err != nil {
		return err
	}
	security, err := json.Marshal(in.Security)
	if err != nil {
		return err
	}
	row.Protocol = in.Protocol
	row.Listen = in.Listen
	row.Port = in.Port
	row.Settings = string(settings)
	row.Transport = string(transport)
	row.Security = string(security)
	row.Extra = string(in.Extra)
	row.OutboundIPStrategy = in.OutboundIPStrategy
	return nil
}

// ---- kernel endpoints ----

func (h *Handler) kernelCapabilities(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"kernels":   kernel.Catalog(),
		"ssMethods": kernel.SSMethods(),
		"defaults": map[string]interface{}{
			"realitySni":  kernel.DefaultRealitySNI,
			"fingerprint": kernel.DefaultFingerprint,
			"ssMethod":    kernel.DefaultSSMethod,
			"flow":        kernel.FlowVision,
		},
		"splitCategories": kernel.SplitCategories,
	}))
}

func (h *Handler) kernelList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		NodeID int64 `json:"nodeId"`
	}
	_ = decodeJSON(r.Body, &req)

	views, err := h.kernelViews(req.NodeID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(views))
}

func (h *Handler) kernelViews(nodeID int64) ([]kernelView, error) {
	kernels, err := h.repo.ListNodeKernels(nodeID)
	if err != nil {
		return nil, err
	}
	inbounds, err := h.repo.ListNodeInbounds()
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int)
	for _, in := range inbounds {
		counts[in.KernelID]++
	}
	nodes := make(map[int64]*model.Node)
	views := make([]kernelView, 0, len(kernels))
	for _, k := range kernels {
		node, ok := nodes[k.NodeID]
		if !ok {
			node, _ = h.repo.GetNodeByID(k.NodeID)
			nodes[k.NodeID] = node
		}
		v := kernelView{
			ID:               k.ID,
			NodeID:           k.NodeID,
			KernelType:       k.KernelType,
			Version:          k.Version,
			InstalledVersion: k.InstalledVersion,
			Status:           kernelStatusOnNode(k.Status, node),
			LastError:        k.LastError,
			Synced:           k.ConfigHash != "" && k.ConfigHash == k.AppliedHash,
			Enabled:          k.Enabled == 1,
			InboundCount:     counts[k.ID],
			UpdatedTime:      k.UpdatedTime,
		}
		if node != nil {
			v.NodeName = node.Name
			v.NodeOnline = node.Status == 1
		}
		views = append(views, v)
	}
	return views, nil
}

func (h *Handler) kernelNodeEligible(nodeID int64) (*model.Node, error) {
	node, err := h.repo.GetNodeByID(nodeID)
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, errors.New("服务器不存在")
	}
	if node.IsRemote == 1 {
		return nil, errors.New("远程服务器不支持部署内核")
	}
	if isNftablesForwardMode(node.ForwardMode) {
		return nil, errors.New("nftables 模式服务器不支持部署内核")
	}
	return node, nil
}

func (h *Handler) kernelInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		NodeID     int64  `json:"nodeId"`
		KernelType string `json:"kernelType"`
		Version    string `json:"version"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if _, err := h.kernelNodeEligible(req.NodeID); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	info, ok := kernel.Lookup(req.KernelType)
	if !ok || !info.Implemented || info.Relay {
		response.WriteJSON(w, response.ErrDefault("暂不支持该内核"))
		return
	}
	version := normalizeKernelVersion(req.Version)
	if version == "" {
		response.WriteJSON(w, response.ErrDefault("版本号格式错误"))
		return
	}
	// Kernels with a closed version list (Snell) store the concrete version
	// so the binary directory and protocol checks agree with what is pinned.
	resolved, err := kernel.ResolveVersion(info, version)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if len(info.Versions) > 0 {
		version = resolved
	}

	now := time.Now().UnixMilli()
	k, err := h.repo.GetNodeKernelByType(req.NodeID, info.Type)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if k != nil && (info.Type == kernel.TypeSnell || info.Type == kernel.TypeSingbox) {
		if err := h.checkKernelVersionFits(k.ID, info.Type, version); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	if k == nil {
		k = &model.NodeKernel{
			NodeID:      req.NodeID,
			KernelType:  info.Type,
			Version:     version,
			Status:      kernelStatusPending,
			Enabled:     1,
			CreatedTime: now,
			UpdatedTime: now,
		}
		if err := h.repo.CreateNodeKernel(k); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	} else if err := h.repo.UpdateNodeKernelFields(k.ID, map[string]interface{}{
		"version": version, "enabled": 1, "updated_time": now,
	}); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	if err := h.applyKernel(k.ID, true); err != nil {
		response.WriteJSON(w, response.ErrDefault("内核已登记，但下发失败: "+err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

// normalizeKernelVersion accepts "latest" or a dotted version, returning ""
// for anything that could inject into a download URL.
func normalizeKernelVersion(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" || strings.EqualFold(v, "latest") {
		return "latest"
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9') && c != '.' && c != '-' && !(c >= 'a' && c <= 'z') {
			return ""
		}
	}
	if len(v) > 40 {
		return ""
	}
	return v
}

func (h *Handler) kernelControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID     int64  `json:"id"`
		Action string `json:"action"`
		Force  bool   `json:"force"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	k, err := h.repo.GetNodeKernel(req.ID)
	if err != nil || k == nil {
		response.WriteJSON(w, response.ErrDefault("内核不存在"))
		return
	}
	now := time.Now().UnixMilli()

	switch req.Action {
	case "start", "stop":
		enabled := 0
		if req.Action == "start" {
			enabled = 1
		}
		if err := h.repo.UpdateNodeKernelFields(k.ID, map[string]interface{}{"enabled": enabled, "updated_time": now}); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if err := h.applyKernel(k.ID, false); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	case "restart":
		unlock := lockKernelApply(k.ID)
		_, err := h.sendNodeCommandWithTimeout(k.NodeID, "KernelControl", map[string]interface{}{
			"type": k.KernelType, "action": "restart",
		}, kernelCommandTimeout, false, false)
		unlock()
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		h.refreshNodeKernelStatus(k.NodeID)
	case "remove":
		unlock := lockKernelApply(k.ID)
		_, err := h.sendNodeCommandWithTimeout(k.NodeID, "KernelControl", map[string]interface{}{
			"type": k.KernelType, "action": "remove",
		}, kernelCommandTimeout, false, true)
		if err != nil && !req.Force {
			unlock()
			response.WriteJSON(w, response.ErrDefault("服务器卸载失败: "+err.Error()+"（可强制删除面板记录）"))
			return
		}
		if rows, lerr := h.repo.ListNodeInboundsByKernel(k.ID); lerr == nil {
			for _, row := range rows {
				h.removeInboundRelays(row.ID)
			}
		}
		err = h.repo.DeleteNodeKernel(k.ID)
		unlock()
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	default:
		response.WriteJSON(w, response.ErrDefault("未知操作"))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) kernelLogs(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	k, err := h.repo.GetNodeKernel(id)
	if err != nil || k == nil {
		response.WriteJSON(w, response.ErrDefault("内核不存在"))
		return
	}
	res, err := h.sendNodeCommandWithTimeout(k.NodeID, "KernelStatus", map[string]interface{}{
		"logs": k.KernelType,
	}, kernelCommandTimeout, false, false)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	var status agentKernelStatus
	if err := remarshal(res.Data, &status); err != nil {
		response.WriteJSON(w, response.ErrDefault("服务器返回数据格式错误"))
		return
	}
	for _, ks := range status.Kernels {
		if ks.Type == k.KernelType {
			response.WriteJSON(w, response.OK(ks))
			return
		}
	}
	response.WriteJSON(w, response.OK(agentKernelState{Type: k.KernelType}))
}

// ---- inbound endpoints ----

func (h *Handler) inboundList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	kernels, err := h.repo.ListNodeKernels(0)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	relays, err := h.repo.ListInboundRelays(0)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	relayCounts := make(map[int64]int)
	for _, rel := range relays {
		relayCounts[rel.InboundID]++
	}
	kernelByID := make(map[int64]model.NodeKernel, len(kernels))
	for _, k := range kernels {
		kernelByID[k.ID] = k
	}
	nodes := make(map[int64]*model.Node)
	sourceNames := map[int64]string{}
	if sources, err := h.repo.ListRemoteSources(); err == nil {
		for _, s := range sources {
			sourceNames[s.ID] = s.Name
		}
	}
	names := make(map[int64]string, len(rows))
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	views := make([]inboundView, 0, len(rows))
	for _, row := range rows {
		in, err := inboundFromModel(row)
		if err != nil {
			log.Printf("kernel: %v", err)
		}
		node, ok := nodes[row.NodeID]
		if !ok {
			node, _ = h.repo.GetNodeByID(row.NodeID)
			nodes[row.NodeID] = node
		}
		k := kernelByID[row.KernelID]
		v := inboundView{
			ID:                 row.ID,
			KernelID:           row.KernelID,
			KernelType:         k.KernelType,
			KernelStatus:       kernelStatusOnNode(k.Status, node),
			External:           isExternalInbound(&row),
			NodeID:             row.NodeID,
			Name:               row.Name,
			Protocol:           row.Protocol,
			Listen:             row.Listen,
			Port:               row.Port,
			Network:            in.NetworkOn(k.KernelType),
			Settings:           in.Settings,
			Transport:          in.Transport,
			Security:           kernelSecurityView{Security: in.Security, ACMEDNSSet: in.Security.ACMEDNSKey != ""},
			Extra:              row.Extra,
			ShareHost:          row.ShareHost,
			ShareAddressMode:   inboundShareMode(&row),
			Remark:             row.Remark,
			RelayOnly:          row.RelayOnly == 1,
			RelayCount:         relayCounts[row.ID],
			AcceptProxy:        row.AcceptProxyProtocol == 1,
			Whitelist:          row.RelayWhitelist == 1,
			WhitelistExtra:     row.WhitelistExtra,
			Enabled:            row.Enabled == 1,
			UploadBytes:        row.UploadBytes,
			DownloadBytes:      row.DownloadBytes,
			CreatedTime:        row.CreatedTime,
			UpdatedTime:        row.UpdatedTime,
			SourceID:           row.SourceID,
			SourceName:         sourceNames[row.SourceID],
			OutboundID:         row.OutboundID,
			SplitRules:         rowSplitRules(row),
			OutboundIPStrategy: row.OutboundIPStrategy,
			Tags:               splitTags(row.Tags),
		}
		v.ShareAddress = inboundShareAddress(&row, nodes[row.NodeID])
		if v.SplitRules == nil {
			v.SplitRules = []kernel.SplitRule{}
		}
		if row.OutboundID != 0 {
			name, ok := names[row.OutboundID]
			v.OutboundName, v.OutboundMissing = name, !ok
		}
		if node != nil {
			v.NodeName = node.Name
			v.NodeServerIP = node.ServerIP
		}
		if p := importedProxy(&row); p != nil {
			v.ImportedSummary = kernel.DescribeProxy(p)
			v.ImportedParams = kernel.ProxyParams(p)
			v.Extra = ""
		}
		v.Probe = h.inboundProbeView(&row)
		views = append(views, v)
	}
	response.WriteJSON(w, response.OK(views))
}

func (h *Handler) inboundCreate(w http.ResponseWriter, r *http.Request) {
	h.inboundSave(w, r, false)
}

func (h *Handler) inboundUpdate(w http.ResponseWriter, r *http.Request) {
	h.inboundSave(w, r, true)
}

func (h *Handler) inboundSave(w http.ResponseWriter, r *http.Request, update bool) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req inboundRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		response.WriteJSON(w, response.ErrDefault("名称不能为空且不超过 100 字"))
		return
	}
	req.ShareHost = strings.TrimSpace(req.ShareHost)
	if len(req.ShareHost) > 255 {
		response.WriteJSON(w, response.ErrDefault("分享地址过长"))
		return
	}

	now := time.Now().UnixMilli()
	var row model.NodeInbound
	var k *model.NodeKernel
	if update {
		existing, err := h.repo.GetNodeInbound(req.ID)
		if err != nil || existing == nil {
			response.WriteJSON(w, response.ErrDefault("节点服务不存在"))
			return
		}
		row = *existing
		if isExternalInbound(&row) {
			response.WriteJSON(w, response.ErrDefault("导入的节点请在“编辑”里修改地址和密码"))
			return
		}
		k, err = h.repo.GetNodeKernel(row.KernelID)
		if err != nil || k == nil {
			response.WriteJSON(w, response.ErrDefault("内核不存在"))
			return
		}
		// Keep secrets the client did not resend (the list hides PEM).
		old, _ := inboundFromModel(row)
		if req.Security.Type == old.Security.Type && req.Security.CertMode == old.Security.CertMode &&
			req.Security.CertPEM == "" && req.Security.KeyPEM == "" {
			req.Security.CertPEM, req.Security.KeyPEM = old.Security.CertPEM, old.Security.KeyPEM
		}
		if req.Security.CertMode == kernel.CertModeACME && req.Security.ACMEDNS == old.Security.ACMEDNS &&
			req.Security.ACMEDNSKey == "" && req.Security.ACMEDNSSecret == "" {
			req.Security.ACMEDNSKey, req.Security.ACMEDNSSecret = old.Security.ACMEDNSKey, old.Security.ACMEDNSSecret
		}
	} else {
		if _, err := h.kernelNodeEligible(req.NodeID); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		var err error
		k, err = h.repo.GetNodeKernelByType(req.NodeID, strings.ToLower(strings.TrimSpace(req.KernelType)))
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if k == nil {
			response.WriteJSON(w, response.ErrDefault("请先在该服务器安装内核"))
			return
		}
		row = model.NodeInbound{KernelID: k.ID, NodeID: k.NodeID, CreatedTime: now, Enabled: 1}
	}

	mode := inboundShareMode(&row)
	if req.ShareAddressMode != nil {
		mode = strings.TrimSpace(*req.ShareAddressMode)
	}
	if req.ShareAddressMode == nil {
		if req.ShareHost != "" {
			mode = "custom"
		} else if mode == "custom" {
			mode = "auto"
		}
	}
	node, err := h.repo.GetNodeByID(row.NodeID)
	if err != nil || node == nil {
		response.WriteJSON(w, response.ErrDefault("服务器不存在"))
		return
	}
	if err := validateShareAddress(mode, req.ShareHost, node); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	row.ShareAddressMode = mode
	if mode != "custom" {
		req.ShareHost = ""
	}

	in := kernel.Inbound{
		OutboundIPStrategy: row.OutboundIPStrategy,
		ID:                 row.ID,
		Protocol:           req.Protocol,
		Listen:             req.Listen,
		Port:               req.Port,
		Settings:           req.Settings,
		Transport:          req.Transport,
		Security:           req.Security,
		Extra:              json.RawMessage(strings.TrimSpace(req.Extra)),

		AcceptProxyProtocol: req.AcceptProxyProtocol,
	}
	if req.OutboundIPStrategy != nil {
		in.OutboundIPStrategy = *req.OutboundIPStrategy
	}
	if err := validateOutboundIPVersion(k.KernelType, k.Version, in.OutboundIPStrategy); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	allowExtra, err := kernel.ParseAllowList(req.WhitelistExtra)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if in.Port == 0 {
		port, err := h.pickFreeInboundPort(row.NodeID, row.ID)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		in.Port = port
	}
	if err := kernel.Normalize(k.KernelType, &in); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if in.Security.CertMode == kernel.CertModeACME && versionBelow(k.Version, 1, 14) {
		response.WriteJSON(w, response.ErrDefault(fmt.Sprintf("自动申请证书需要 sing-box 1.14 及以上，服务器安装的是 %s", k.Version)))
		return
	}
	row.OutboundIPStrategy = in.OutboundIPStrategy
	prevTargets := outboundTargets(row)
	if err := h.setInboundRouting(&row, k.KernelType, k.Version, req.OutboundID, req.SplitRules); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if in.Protocol == kernel.ProtocolSnell && k.KernelType == kernel.TypeSingbox && versionBelow(k.Version, 1, 14) {
		response.WriteJSON(w, response.ErrDefault(fmt.Sprintf("sing-box 内置 Snell 需要 1.14 及以上，服务器安装的是 %s", k.Version)))
		return
	}
	if in.Protocol == kernel.ProtocolSnell && k.KernelType == kernel.TypeSnell {
		if err := snellServes(k.Version, in.Settings.SnellVersion); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	if msg, err := h.inboundPortConflict(row.NodeID, in, row.ID); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	} else if msg != "" {
		response.WriteJSON(w, response.ErrDefault(msg))
		return
	}

	if err := applyInboundToModel(in, &row); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	row.Name = req.Name
	row.ShareHost = req.ShareHost
	row.Remark = strings.TrimSpace(req.Remark)
	prevAcceptProxy := row.AcceptProxyProtocol
	row.RelayOnly = boolToInt(req.RelayOnly)
	row.AcceptProxyProtocol = boolToInt(req.AcceptProxyProtocol)
	if row.AcceptProxyProtocol == 1 {
		// Clients cannot speak PROXY protocol, so direct access stops working.
		row.RelayOnly = 1
	}
	row.RelayWhitelist = boolToInt(req.RelayWhitelist)
	row.WhitelistExtra = strings.Join(allowExtra, ", ")
	if req.Enabled != nil {
		row.Enabled = boolToInt(*req.Enabled)
	}
	row.UpdatedTime = now

	if update {
		err = h.repo.SaveNodeInbound(&row)
	} else {
		err = h.repo.CreateNodeInbound(&row)
	}
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	var warnings []string
	if update && prevAcceptProxy != row.AcceptProxyProtocol {
		warnings = h.syncRelayProxyProtocol(row)
	}
	if err := h.applyKernel(k.ID, false); err != nil {
		warnings = append([]string{"下发到服务器失败: " + err.Error()}, warnings...)
	}
	if update {
		warnings = append(warnings, h.applyChainReferrers(row.ID)...)
	}
	warnings = append(warnings, h.applyChangedTargets(prevTargets, row)...)
	if len(warnings) > 0 {
		response.WriteJSON(w, response.R{Code: 0, Msg: "已保存，但" + strings.Join(warnings, "；"), TS: now, Data: map[string]interface{}{"id": row.ID}})
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"id": row.ID}))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (h *Handler) pickFreeInboundPort(nodeID, excludeInboundID int64) (int, error) {
	for i := 0; i < 64; i++ {
		port := 20000 + rand.Intn(40000)
		msg, err := h.inboundPortConflict(nodeID, kernel.Inbound{Port: port}, excludeInboundID)
		if err != nil {
			return 0, err
		}
		if msg == "" {
			return port, nil
		}
	}
	return 0, errors.New("未找到可用端口，请手动指定")
}

// portSpan is a port range an inbound occupies on its node.
type portSpan struct {
	lo, hi int
	what   string
}

func (s portSpan) String() string {
	if s.lo == s.hi {
		return fmt.Sprintf("%s %d", s.what, s.lo)
	}
	return fmt.Sprintf("%s %d-%d", s.what, s.lo, s.hi)
}

func (s portSpan) overlaps(o portSpan) bool { return s.lo <= o.hi && o.lo <= s.hi }

// inboundSpans lists the ports an inbound needs: its listen port, the
// loopback backend of a ShadowTLS front, and the port-hopping range.
func inboundSpans(in kernel.Inbound) []portSpan {
	spans := []portSpan{{in.Port, in.Port, "端口"}}
	if bp := in.Settings.BackendPort; bp > 0 {
		spans = append(spans, portSpan{bp, bp, "后端端口"})
	}
	if lo, hi := in.HopRange(); lo > 0 {
		spans = append(spans, portSpan{lo, hi, "端口跳跃范围"})
	}
	return spans
}

// inboundPortConflict describes the first clash between the ports an
// inbound needs and what already runs on the node ("" when there is none).
func (h *Handler) inboundPortConflict(nodeID int64, in kernel.Inbound, excludeInboundID int64) (string, error) {
	spans := inboundSpans(in)
	for _, sp := range spans {
		occ, err := h.repo.NodePortRangeOccupant(nodeID, sp.lo, sp.hi, excludeInboundID)
		if err != nil {
			return "", err
		}
		if occ != "" {
			return fmt.Sprintf("%s 已被%s占用", sp, occ), nil
		}
	}
	for _, sp := range spans {
		if sp.lo == sp.hi {
			continue
		}
		// A hop range inside the node's forwarding range would capture the
		// UDP traffic of forwards created there later.
		spec, err := h.repo.GetNodePortRange(nodeID)
		if err != nil {
			return "", err
		}
		if lo, hi := parsePortRangeMinMax(spec); lo > 0 && sp.overlaps(portSpan{lo: lo, hi: hi}) {
			return fmt.Sprintf("%s 与服务器的转发端口段 %s 重叠", sp, spec), nil
		}
	}
	others, err := h.repo.ListNodeInboundsByNode(nodeID)
	if err != nil {
		return "", err
	}
	for _, row := range others {
		if row.ID == excludeInboundID {
			continue
		}
		other, err := inboundFromModel(row)
		if err != nil {
			continue
		}
		// Listen ports were checked by the query above; compare the rest.
		for _, os := range inboundSpans(other)[1:] {
			for _, sp := range spans {
				if sp.overlaps(os) {
					return fmt.Sprintf("%s 与入站「%s」的%s 冲突", sp, row.Name, os), nil
				}
			}
		}
	}
	return "", nil
}

func (h *Handler) inboundDelete(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	row, err := h.repo.GetNodeInbound(id)
	if err != nil || row == nil {
		response.WriteJSON(w, response.ErrDefault("节点服务不存在"))
		return
	}
	if msg := h.chainInUseMessage(row); msg != "" {
		response.WriteJSON(w, response.ErrDefault(msg))
		return
	}
	h.removeInboundRelays(id)
	if err := h.repo.DeleteNodeInbound(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	for _, target := range outboundTargets(*row) {
		h.applyWhitelistedTarget(target)
	}
	if isExternalInbound(row) {
		response.WriteJSON(w, response.OKEmpty())
		return
	}
	if err := h.applyKernel(row.KernelID, false); err != nil {
		response.WriteJSON(w, response.R{Code: 0, Msg: "已删除，但下发到服务器失败: " + err.Error(), TS: time.Now().UnixMilli()})
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

// inboundBatchDelete removes several inbounds and their relays, then pushes
// each affected kernel's config once instead of once per inbound.
func (h *Handler) inboundBatchDelete(w http.ResponseWriter, r *http.Request) {
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	s := 0
	f := 0
	failures := make([]batchFailureDetail, 0)
	var targetIDs []int64
	kernelIDs := make([]int64, 0)
	seenKernel := make(map[int64]bool)
	for _, id := range ids {
		row, err := h.repo.GetNodeInbound(id)
		if err != nil || row == nil {
			f++
			failures = appendBatchFailureReason(failures, id, "", "节点服务不存在")
			continue
		}
		if msg := h.chainInUseMessage(row); msg != "" && !batchDeletesReferrers(ids, h.chainReferrers(id)) {
			f++
			failures = appendBatchFailureReason(failures, id, row.Name, msg)
			continue
		}
		h.removeInboundRelays(id)
		if err := h.repo.DeleteNodeInbound(id); err != nil {
			f++
			failures = appendBatchFailure(failures, id, row.Name, err)
			continue
		}
		s++
		if !isExternalInbound(row) && !seenKernel[row.KernelID] {
			seenKernel[row.KernelID] = true
			kernelIDs = append(kernelIDs, row.KernelID)
		}
		targetIDs = append(targetIDs, outboundTargets(*row)...)
	}
	msg := "操作成功"
	for _, kernelID := range kernelIDs {
		if err := h.applyKernel(kernelID, false); err != nil {
			msg = "已删除，但下发到服务器失败: " + err.Error()
		}
	}
	for _, targetID := range targetIDs {
		h.applyWhitelistedTarget(targetID)
	}
	response.WriteJSON(w, response.R{
		Code: 0,
		Msg:  msg,
		Data: batchOperationResult{SuccessCount: s, FailCount: f, Failures: failures},
		TS:   time.Now().UnixMilli(),
	})
}

// inboundBatchToggle enables or disables several inbounds, then pushes each
// affected kernel's config once.
func (h *Handler) inboundBatchToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		IDs     []int64 `json:"ids"`
		Enabled bool    `json:"enabled"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if len(req.IDs) == 0 {
		response.WriteJSON(w, response.ErrDefault("ids不能为空"))
		return
	}
	s := 0
	f := 0
	failures := make([]batchFailureDetail, 0)
	kernelIDs := make([]int64, 0)
	seenKernel := make(map[int64]bool)
	now := time.Now().UnixMilli()
	for _, id := range req.IDs {
		row, err := h.repo.GetNodeInbound(id)
		if err != nil || row == nil {
			f++
			failures = appendBatchFailureReason(failures, id, "", "节点服务不存在")
			continue
		}
		if (row.Enabled == 1) == req.Enabled {
			s++
			continue
		}
		row.Enabled = boolToInt(req.Enabled)
		row.UpdatedTime = now
		if err := h.repo.SaveNodeInbound(row); err != nil {
			f++
			failures = appendBatchFailure(failures, id, row.Name, err)
			continue
		}
		s++
		if !isExternalInbound(row) && !seenKernel[row.KernelID] {
			seenKernel[row.KernelID] = true
			kernelIDs = append(kernelIDs, row.KernelID)
		}
	}
	msg := "操作成功"
	for _, kernelID := range kernelIDs {
		if err := h.applyKernel(kernelID, false); err != nil {
			msg = "已保存，但下发到服务器失败: " + err.Error()
		}
	}
	response.WriteJSON(w, response.R{
		Code: 0,
		Msg:  msg,
		Data: batchOperationResult{SuccessCount: s, FailCount: f, Failures: failures},
		TS:   time.Now().UnixMilli(),
	})
}

func (h *Handler) inboundToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID      int64 `json:"id"`
		Enabled bool  `json:"enabled"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	row, err := h.repo.GetNodeInbound(req.ID)
	if err != nil || row == nil {
		response.WriteJSON(w, response.ErrDefault("节点服务不存在"))
		return
	}
	row.Enabled = boolToInt(req.Enabled)
	row.UpdatedTime = time.Now().UnixMilli()
	if err := h.repo.SaveNodeInbound(row); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if isExternalInbound(row) {
		response.WriteJSON(w, response.OKEmpty())
		return
	}
	if err := h.applyKernel(row.KernelID, false); err != nil {
		response.WriteJSON(w, response.R{Code: 0, Msg: "已保存，但下发到服务器失败: " + err.Error(), TS: row.UpdatedTime})
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) inboundResetTraffic(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	if err := h.repo.ResetNodeInboundTraffic(id, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) inboundRegenerate(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	row, err := h.repo.GetNodeInbound(id)
	if err != nil || row == nil {
		response.WriteJSON(w, response.ErrDefault("节点服务不存在"))
		return
	}
	if isExternalInbound(row) {
		response.WriteJSON(w, response.ErrDefault("导入的节点由原服务器管理，不能在面板里更换密钥"))
		return
	}
	k, err := h.repo.GetNodeKernel(row.KernelID)
	if err != nil || k == nil {
		response.WriteJSON(w, response.ErrDefault("内核不存在"))
		return
	}
	in, err := inboundFromModel(*row)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := kernel.Regenerate(k.KernelType, &in); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := applyInboundToModel(in, row); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	row.UpdatedTime = time.Now().UnixMilli()
	if err := h.repo.SaveNodeInbound(row); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.applyKernel(k.ID, false); err != nil {
		response.WriteJSON(w, response.R{Code: 0, Msg: "已重新生成，但下发到服务器失败: " + err.Error(), TS: row.UpdatedTime})
		return
	}
	if warnings := h.applyChainReferrers(row.ID); len(warnings) > 0 {
		response.WriteJSON(w, response.R{Code: 0, Msg: "已重新生成，但" + strings.Join(warnings, "；"), TS: row.UpdatedTime})
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

// ---- dispatch ----

type agentKernelState struct {
	Type       string   `json:"type"`
	Running    bool     `json:"running"`
	Enabled    bool     `json:"enabled"`
	Pid        int      `json:"pid"`
	Version    string   `json:"version"`
	ConfigHash string   `json:"configHash"`
	Error      string   `json:"error"`
	Restarts   int      `json:"restarts"`
	StartedAt  int64    `json:"startedAt"`
	Logs       []string `json:"logs,omitempty"`
}

type agentKernelCounter struct {
	InboundID int64  `json:"inboundId"`
	Proto     string `json:"proto"`
	Dir       string `json:"dir"`
	Bytes     int64  `json:"bytes"`
}

type agentUserCounter struct {
	ID    int64  `json:"id"`
	Dir   string `json:"dir"`
	Bytes int64  `json:"bytes"`
}

type agentKernelStatus struct {
	Kernels      []agentKernelState   `json:"kernels"`
	Counters     []agentKernelCounter `json:"counters"`
	Users        []agentUserCounter   `json:"users"`
	Epoch        string               `json:"epoch"`
	NftAvailable bool                 `json:"nftAvailable"`
	NftError     string               `json:"nftError"`
}

func remarshal(in interface{}, out interface{}) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// buildKernelApplyPayload renders the kernel's enabled inbounds into the
// KernelApply command body.
func (h *Handler) buildKernelApplyPayload(k *model.NodeKernel) (map[string]interface{}, string, error) {
	return h.buildKernelApplyPayloadWith(k, nil)
}

// buildKernelApplyPayloadWith takes realm forwards to force on or off
// (realm.go).
func (h *Handler) buildKernelApplyPayloadWith(k *model.NodeKernel, realmOverride map[int64]bool) (map[string]interface{}, string, error) {
	info, ok := kernel.Lookup(k.KernelType)
	if !ok || !info.Implemented {
		return nil, "", fmt.Errorf("暂不支持内核 %s", k.KernelType)
	}
	if info.Relay {
		return h.buildRealmApplyPayload(k, info, realmOverride)
	}
	rows, err := h.repo.ListNodeInboundsByKernel(k.ID)
	if err != nil {
		return nil, "", err
	}
	inbounds := make([]kernel.Inbound, 0, len(rows))
	ports := make([]map[string]interface{}, 0, len(rows))
	users := h.activeUsersByInbound(time.Now())
	for _, row := range rows {
		if row.Enabled != 1 {
			continue
		}
		if err := validateOutboundIPVersion(k.KernelType, k.Version, row.OutboundIPStrategy); err != nil {
			return nil, "", err
		}
		in, err := inboundFromModel(row)
		if err != nil {
			return nil, "", err
		}
		if in.SupportsUsers() {
			in.Users = users[in.ID]
		}
		if in.Via, err = h.inboundVia(row); err != nil {
			return nil, "", err
		}
		if in.Split, err = h.inboundSplit(row); err != nil {
			return nil, "", err
		}
		inbounds = append(inbounds, in)
		port := map[string]interface{}{"inboundId": in.ID, "port": in.Port, "network": in.NetworkOn(k.KernelType)}
		if lo, hi := in.HopRange(); lo > 0 {
			port["hopFrom"], port["hopTo"] = lo, hi
		}
		if row.RelayWhitelist == 1 {
			port["allow"] = h.inboundAllowList(row)
		}
		ports = append(ports, port)
	}
	proxy := ""
	if enabled, proxyURL := h.getGithubProxyConfig(); enabled {
		proxy = proxyURL
	}
	rendered, err := kernel.RenderWith(k.KernelType, inbounds, kernel.RenderOptions{
		RuleSetMirror:      proxy,
		SingboxHTTPClient:  kernelCaps(k.Version).SingboxHTTPClient,
		SingboxSnell:       kernelCaps(k.Version).SingboxSnell,
		SingboxPreferGoDNS: kernelCaps(k.Version).SingboxPreferGoDNS,
	})
	if err != nil {
		return nil, "", err
	}
	download, err := kernel.ResolveDownload(info, k.Version, proxy)
	if err != nil {
		return nil, "", err
	}
	payload := map[string]interface{}{
		"type":     k.KernelType,
		"version":  k.Version,
		"download": download,
		"files":    rendered.Files,
		"args":     rendered.Args,
		"hash":     rendered.Hash,
		"enabled":  k.Enabled == 1 && !rendered.Idle,
		"ports":    ports,
	}
	if len(rendered.UserMarks) > 0 {
		payload["userMarks"] = rendered.UserMarks
	}
	if k.KernelType == kernel.TypeXray {
		// The agent keeps the 分流 data files next to Xray up to date.
		payload["geoData"] = kernel.XrayGeoData(proxy)
	}
	if len(rendered.Processes) > 0 {
		payload["processes"] = rendered.Processes
	}
	if len(rendered.Extras) > 0 {
		extras := make([]map[string]interface{}, 0, len(rendered.Extras))
		for _, name := range rendered.Extras {
			if name != kernel.ShadowTLSExtra {
				return nil, "", fmt.Errorf("未知的附加程序 %s", name)
			}
			d, err := kernel.ExtraDownload(kernel.ShadowTLSDownload(), kernel.ShadowTLSVersion, proxy)
			if err != nil {
				return nil, "", err
			}
			extras = append(extras, map[string]interface{}{"name": name, "version": kernel.ShadowTLSVersion, "download": d})
		}
		payload["extras"] = extras
	}
	return payload, rendered.Hash, nil
}

type kernelVersionItem struct {
	Version     string `json:"version"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

var kernelVersionCache = struct {
	sync.Mutex
	at    map[string]time.Time
	items map[string][]kernelVersionItem
}{at: map[string]time.Time{}, items: map[string][]kernelVersionItem{}}

// kernelReleaseVersions lists recent releases of a kernel from GitHub,
// cached for 30 minutes. Kernels with a pinned list return that list.
func kernelReleaseVersions(info kernel.Info) ([]kernelVersionItem, error) {
	if len(info.Versions) > 0 {
		out := make([]kernelVersionItem, 0, len(info.Versions))
		for _, v := range info.Versions {
			out = append(out, kernelVersionItem{Version: v})
		}
		return out, nil
	}
	if info.ReleaseRepo == "" {
		return nil, nil
	}
	kernelVersionCache.Lock()
	defer kernelVersionCache.Unlock()
	if at, ok := kernelVersionCache.at[info.Type]; ok && time.Since(at) < 30*time.Minute {
		return kernelVersionCache.items[info.Type], nil
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/repos/%s/releases?per_page=30", githubAPIBase, info.ReleaseRepo))
	if err != nil {
		return nil, fmt.Errorf("获取版本列表失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取版本列表失败: GitHub 返回 %d", resp.StatusCode)
	}
	releases, err := decodeReleases(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("解析版本列表失败: %v", err)
	}
	out := make([]kernelVersionItem, 0, len(releases))
	for _, r := range releases {
		if r.Draft {
			continue
		}
		v := normalizeKernelVersion(r.TagName)
		if v == "" || v == "latest" {
			continue
		}
		out = append(out, kernelVersionItem{Version: v, Prerelease: r.Prerelease, PublishedAt: r.PublishedAt})
	}
	kernelVersionCache.at[info.Type] = time.Now()
	kernelVersionCache.items[info.Type] = out
	return out, nil
}

// decodeReleases reads GitHub's release list one release at a time. Releases
// with many assets (sing-box ships dozens per release) can exceed the size
// limit; the releases read before the cut are kept, newest first.
func decodeReleases(r io.Reader) ([]githubRelease, error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil {
		return nil, err
	} else if tok != json.Delim('[') {
		return nil, fmt.Errorf("不是版本列表")
	}
	var out []githubRelease
	for dec.More() {
		var rel githubRelease
		if err := dec.Decode(&rel); err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, err
		}
		out = append(out, rel)
	}
	return out, nil
}

func (h *Handler) kernelVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		KernelType string `json:"kernelType"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	info, ok := kernel.Lookup(req.KernelType)
	if !ok || !info.Implemented {
		response.WriteJSON(w, response.ErrDefault("未知内核"))
		return
	}
	items, err := kernelReleaseVersions(info)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

// versionBelow reports whether a concrete "x.y[.z]" version is older than
// major.minor (or major.minor.patch); "latest" and unparsable versions are not.
func versionBelow(version string, major, minor int, patch ...int) bool {
	parts := strings.SplitN(strings.TrimPrefix(version, "v"), ".", 3)
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if ma != major {
		return ma < major
	}
	if mi != minor {
		return mi < minor
	}
	if len(patch) == 0 {
		return false
	}
	if len(parts) < 3 {
		return patch[0] > 0
	}
	pa, err := strconv.Atoi(parts[2])
	return err == nil && pa < patch[0]
}

// snellMajor returns the protocol major version a Snell binary serves.
func snellMajor(version string) int {
	switch {
	case strings.HasPrefix(version, "4."):
		return 4
	case strings.HasPrefix(version, "6."):
		return 6
	}
	return 5
}

// snellServes reports whether a snell-server version accepts clients of a
// protocol version: 5.x also serves v4, while 6.x serves v6 only.
func snellServes(version string, client int) error {
	major := snellMajor(version)
	switch {
	case major == 6 && client != 6:
		return fmt.Errorf("Snell %s 只接受 v6 客户端：请把入站改成 v6，或安装 Snell 5.0.1", version)
	case client > major:
		return fmt.Errorf("服务器安装的是 Snell %s，不支持 v%d 客户端", version, client)
	}
	return nil
}

// checkKernelVersionFits refuses to install a kernel version that cannot
// serve an existing inbound: a Snell binary older than the protocol version
// the inbound advertises, or a sing-box before 1.14 under inbounds that need
// it (built-in Snell, ACME certificates).
func (h *Handler) checkKernelVersionFits(kernelID int64, kernelType, version string) error {
	rows, err := h.repo.ListNodeInboundsByKernel(kernelID)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := validateOutboundIPVersion(kernelType, version, row.OutboundIPStrategy); err != nil {
			return fmt.Errorf("入站 %s：%v", row.Name, err)
		}
		in, err := inboundFromModel(row)
		if err != nil {
			continue
		}
		switch kernelType {
		case kernel.TypeSnell:
			if err := snellServes(version, in.Settings.SnellVersion); err != nil {
				return fmt.Errorf("入站 %s 使用 Snell v%d，不能安装 %s", row.Name, in.Settings.SnellVersion, version)
			}
		case kernel.TypeSingbox:
			if !versionBelow(version, 1, 14) {
				continue
			}
			if in.Protocol == kernel.ProtocolSnell {
				return fmt.Errorf("入站 %s 使用 sing-box 内置 Snell，需要 1.14 及以上", row.Name)
			}
			if in.Security.CertMode == kernel.CertModeACME {
				return fmt.Errorf("入站 %s 自动申请证书，需要 sing-box 1.14 及以上", row.Name)
			}
			for _, id := range outboundTargets(row) {
				if t, err := h.repo.GetNodeInbound(id); err == nil && t != nil && t.Protocol == kernel.ProtocolSnell {
					return fmt.Errorf("入站 %s 经 Snell 节点 %s 出站，需要 sing-box 1.14 及以上", row.Name, t.Name)
				}
			}
		}
	}
	return nil
}

// applyKernel pushes the current configuration of a kernel to its node. An
// offline node is not an error for persistence: the kernel stays pending and
// is reconciled when the agent reconnects.
func (h *Handler) applyKernel(kernelID int64, install bool) error {
	return h.applyKernelWith(kernelID, install, nil)
}

func (h *Handler) applyKernelWith(kernelID int64, install bool, realmOverride map[int64]bool) error {
	unlock := lockKernelApply(kernelID)
	defer unlock()

	k, err := h.repo.GetNodeKernel(kernelID)
	if err != nil {
		return err
	}
	if k == nil {
		return errors.New("内核不存在")
	}
	payload, hash, err := h.buildKernelApplyPayloadWith(k, realmOverride)
	now := time.Now().UnixMilli()
	if err != nil {
		_ = h.repo.UpdateNodeKernelFields(k.ID, map[string]interface{}{
			"status": kernelStatusError, "last_error": err.Error(), "updated_time": now,
		})
		return err
	}
	_ = h.repo.UpdateNodeKernelFields(k.ID, map[string]interface{}{"config_hash": hash, "updated_time": now})

	timeout := kernelCommandTimeout
	if install || k.InstalledVersion == "" || k.Version != "latest" && !strings.Contains(k.InstalledVersion, k.Version) {
		timeout = kernelApplyTimeout
	}
	res, err := h.sendNodeCommandWithTimeout(k.NodeID, "KernelApply", payload, timeout, false, false)
	now = time.Now().UnixMilli()
	if err != nil {
		status := kernelStatusError
		if strings.Contains(err.Error(), "不在线") {
			status = kernelStatusPending
		}
		_ = h.repo.UpdateNodeKernelFields(k.ID, map[string]interface{}{
			"status": status, "last_error": err.Error(), "updated_time": now,
		})
		return err
	}

	var state agentKernelState
	_ = remarshal(res.Data, &state)
	fields := map[string]interface{}{
		"applied_hash": hash,
		"status":       kernelStateStatus(state, k.Enabled == 1),
		"last_error":   state.Error,
		"updated_time": now,
	}
	if state.Version != "" {
		fields["installed_version"] = truncate(state.Version, 100)
	}
	return h.repo.UpdateNodeKernelFields(k.ID, fields)
}

func kernelStateStatus(state agentKernelState, enabled bool) string {
	switch {
	case state.Running:
		return kernelStatusRunning
	case !enabled:
		return kernelStatusStopped
	case state.Error != "":
		return kernelStatusError
	default:
		return kernelStatusStopped
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// reconcileNodeKernels re-applies every kernel of a node, e.g. after the agent
// reconnects. The agent skips work when the config hash is unchanged.
func (h *Handler) reconcileNodeKernels(nodeID int64) {
	kernels, err := h.repo.ListNodeKernels(nodeID)
	if err != nil {
		return
	}
	for _, k := range kernels {
		// A reconnecting agent may have to download the kernel again (the
		// server was reinstalled), so allow an install's time.
		if err := h.applyKernel(k.ID, true); err != nil {
			log.Printf("kernel reconcile failed node_id=%d kernel=%s err=%v", nodeID, k.KernelType, err)
		}
	}
	// Also runs when the panel has no kernel left on the node, so kernels
	// deleted while it was offline get removed.
	h.pollNodeKernels(nodeID, true)
}

// refreshNodeKernelStatus polls one node for kernel state and traffic.
func (h *Handler) refreshNodeKernelStatus(nodeID int64) {
	h.pollNodeKernels(nodeID, false)
}

func (h *Handler) pollNodeKernels(nodeID int64, evenIfNone bool) {
	kernels, err := h.repo.ListNodeKernels(nodeID)
	if err != nil || (len(kernels) == 0 && !evenIfNone) {
		return
	}
	sequence, err := h.repo.NextCounterSnapshot(nodeID, "kernel")
	if err != nil {
		log.Printf("counter sequence failed: %v", err)
		return
	}

	res, err := h.sendNodeCommandWithTimeout(nodeID, "KernelStatus", map[string]interface{}{}, kernelCommandTimeout, false, false)
	if err != nil {
		return // offline, or an agent without kernel support
	}
	var status agentKernelStatus
	if err := remarshal(res.Data, &status); err != nil {
		log.Printf("kernel status decode failed node_id=%d err=%v", nodeID, err)
		return
	}

	now := time.Now().UnixMilli()
	byType := make(map[string]agentKernelState, len(status.Kernels))
	for _, s := range status.Kernels {
		byType[s.Type] = s
	}
	known := make(map[string]struct{}, len(kernels))
	for _, k := range kernels {
		known[k.KernelType] = struct{}{}
	}
	for t := range byType {
		if _, ok := known[t]; ok {
			continue
		}
		// Deleted from the panel while the node was offline: the agent
		// restored it from its local state, so remove it now.
		if _, err := h.sendNodeCommandWithTimeout(nodeID, "KernelControl", map[string]interface{}{
			"type": t, "action": "remove",
		}, kernelCommandTimeout, false, true); err != nil {
			log.Printf("kernel orphan removal failed node_id=%d kernel=%s err=%v", nodeID, t, err)
		}
	}
	for _, k := range kernels {
		s, ok := byType[k.KernelType]
		if !ok || s.ConfigHash != k.ConfigHash {
			// The agent lost a config it had acknowledged (reinstall, state
			// wipe): push it again. A failed apply is left for the user or
			// the next reconnect instead of retrying a download every poll.
			if k.AppliedHash != k.ConfigHash || k.ConfigHash == "" {
				continue
			}
			go func(id int64) {
				if err := h.applyKernel(id, true); err != nil {
					log.Printf("kernel drift re-apply failed kernel_id=%d err=%v", id, err)
				}
			}(k.ID)
			continue
		}
		newStatus := kernelStateStatus(s, k.Enabled == 1)
		if newStatus == kernelStatusError && k.Status != kernelStatusError {
			msg := fmt.Sprintf("服务器：%s\n内核：%s", h.nodeLabel(nodeID), k.KernelType)
			if s.Error != "" {
				msg += "\n错误：" + truncate(s.Error, 300)
			}
			h.notifyForNode(nodeID, notifyEventKernelError, "内核运行出错", msg)
		}
		fields := map[string]interface{}{
			"status":       newStatus,
			"last_error":   s.Error,
			"applied_hash": s.ConfigHash,
			"updated_time": now,
		}
		if s.Version != "" {
			fields["installed_version"] = truncate(s.Version, 100)
		}
		_ = h.repo.UpdateNodeKernelFields(k.ID, fields)
	}

	if !status.NftAvailable {
		return
	}
	if len(status.Counters) == 0 && len(status.Users) == 0 {
		return
	}
	samples := make([]repo.InboundCounterSample, 0, len(status.Counters))
	var realmCounters []agentKernelCounter
	for _, c := range status.Counters {
		if _, ok := kernel.RealmForwardID(c.InboundID); ok {
			realmCounters = append(realmCounters, c)
			continue
		}
		samples = append(samples, repo.InboundCounterSample{
			InboundID: c.InboundID,
			Protocol:  c.Proto,
			Direction: c.Dir,
			Epoch:     status.Epoch,
			Bytes:     c.Bytes,
		})
	}
	var batch flowUploadBatch
	var quotas map[int64]*model.UserQuotaView
	applied, err := h.repo.ApplyCounterSnapshot(nodeID, "kernel", sequence, status.Epoch, func(tx *repo.Repository) error {
		users := make([]repo.SubscriptionCounterSample, 0, len(status.Users))
		for _, c := range status.Users {
			users = append(users, repo.SubscriptionCounterSample{SubscriptionID: c.ID, Direction: c.Dir, Bytes: c.Bytes})
		}
		if err := tx.ApplySubscriptionCounterSamples(nodeID, status.Epoch, users, now); err != nil {
			return err
		}
		if err := tx.ApplyInboundCounterSamples(samples, now); err != nil {
			return err
		}
		var err error
		batch, quotas, err = (&Handler{repo: tx}).applyRealmCountersTx(nodeID, status.Epoch, realmCounters, time.UnixMilli(now))
		return err
	})
	if err != nil {
		log.Printf("kernel traffic apply failed node_id=%d err=%v", nodeID, err)
		return
	}
	if applied {
		h.enforceFlowUploadBatch(nodeID, batch, quotas)
		if len(status.Users) > 0 {
			h.syncSubscriptions(time.UnixMilli(now))
		}
	}

}

func (h *Handler) runKernelStatusLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(kernelStatusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		nodeIDs, err := h.repo.ListKernelNodeIDs()
		if err != nil {
			log.Printf("kernel status node lookup failed: %v", err)
			continue
		}
		for _, nodeID := range nodeIDs {
			node, err := h.repo.GetNodeByID(nodeID)
			if err != nil || node == nil || node.Status != 1 {
				continue
			}
			h.refreshNodeKernelStatus(nodeID)
		}
	}
}
