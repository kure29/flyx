package handler

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

type relayView struct {
	ID            int64               `json:"id"`
	InboundID     int64               `json:"inboundId"`
	ForwardID     int64               `json:"forwardId"`
	Name          string              `json:"name"`
	ShareHost     string              `json:"shareHost"`
	Managed       bool                `json:"managed"`
	Mode          string              `json:"mode"` // "port" or "tunnel"
	ForwardName   string              `json:"forwardName"`
	ForwardStatus int                 `json:"forwardStatus"`
	ForwardExists bool                `json:"forwardExists"`
	TunnelID      int64               `json:"tunnelId"`
	TunnelName    string              `json:"tunnelName"`
	RemoteAddr    string              `json:"remoteAddr"`
	Entries       []repo.ForwardEntry `json:"entries"`
	CreatedTime   int64               `json:"createdTime"`
	// Latency is the line's last measurement (relay_latency.go).
	Latency *relayLatency `json:"latency,omitempty"`
}

type shareEntry struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // "direct" or "relay"
	Host string `json:"host"`
	Port int    `json:"port"`
	URI  string `json:"uri"`
	// Surge is the Surge [Proxy] line; for Snell it is the only export.
	Surge string `json:"surge"`
	// Conf is the WireGuard client config (wg-quick format).
	Conf string `json:"conf,omitempty"`
}

func relayMode(tunnelType int) string {
	if tunnelType == 2 {
		return "tunnel"
	}
	return "port"
}

func isLoopbackListen(listen string) bool {
	ip := net.ParseIP(listen)
	return ip != nil && ip.IsLoopback()
}

func isWildcardListen(listen string) bool {
	ip := net.ParseIP(listen)
	return ip != nil && ip.IsUnspecified()
}

// ---- relays ----

func (h *Handler) relayViews(inboundID int64) ([]relayView, error) {
	rows, err := h.repo.ListInboundRelays(inboundID)
	if err != nil {
		return nil, err
	}
	out := make([]relayView, 0, len(rows))
	for _, rw := range rows {
		v := relayView{
			ID:            rw.ID,
			InboundID:     rw.InboundID,
			ForwardID:     rw.ForwardID,
			Name:          rw.Name,
			ShareHost:     rw.ShareHost,
			Managed:       rw.Managed == 1,
			Mode:          relayMode(rw.TunnelType),
			ForwardName:   rw.ForwardName,
			ForwardStatus: rw.ForwardStatus,
			ForwardExists: rw.ForwardExists,
			TunnelID:      rw.TunnelID,
			TunnelName:    rw.TunnelName,
			RemoteAddr:    rw.RemoteAddr,
			Entries:       []repo.ForwardEntry{},
			CreatedTime:   rw.CreatedTime,
			Latency:       h.relayLatencyView(rw.ID),
		}
		if rw.ForwardExists {
			if entries, err := h.repo.ForwardEntries(rw.ForwardID, rw.TunnelID); err == nil && entries != nil {
				v.Entries = entries
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (h *Handler) inboundRelayList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		InboundID int64 `json:"inboundId"`
	}
	_ = decodeJSON(r.Body, &req)
	views, err := h.relayViews(req.InboundID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(views))
}

// relayTarget decides where the relay forward dials: the inbound's local
// address when a tunnel exits on the landing node itself, otherwise the
// landing node's public address (port forwards, and tunnels exiting on
// another node such as an IX hop in front of the landing node).
func (h *Handler) relayTarget(row *model.NodeInbound, tunnel *tunnelRecord) (string, error) {
	port := strconv.Itoa(row.Port)
	if tunnel.Type == 2 {
		exits, err := h.repo.TunnelExitNodeIDs(tunnel.ID)
		if err != nil {
			return "", err
		}
		onLanding := false
		for _, id := range exits {
			if id == row.NodeID {
				onLanding = true
			}
		}
		if onLanding {
			if len(exits) > 1 {
				return "", errors.New("出口为节点所在服务器的隧道只能有这一个出口服务器")
			}
			host := row.Listen
			if isWildcardListen(host) {
				host = "127.0.0.1"
			}
			return net.JoinHostPort(host, port), nil
		}
	}

	if isLoopbackListen(row.Listen) {
		return "", errors.New("入站只监听本机，只能通过出口为节点所在服务器的隧道转发对接")
	}
	host := row.Listen
	if isWildcardListen(host) {
		node, err := h.repo.GetNodeByID(row.NodeID)
		if err != nil || node == nil {
			return "", errors.New("节点不存在")
		}
		host = strings.TrimSpace(node.ServerIP)
		if v4 := strings.TrimSpace(node.ServerIPV4.String); host == "" && v4 != "" {
			host = v4
		}
	}
	if host == "" {
		return "", errors.New("节点没有可用地址")
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), port), nil
}

func (h *Handler) inboundRelayCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		InboundID int64  `json:"inboundId"`
		TunnelID  int64  `json:"tunnelId"`
		InPort    int    `json:"inPort"`
		Name      string `json:"name"`
		ShareHost string `json:"shareHost"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	userID, _, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	row, err := h.repo.GetNodeInbound(req.InboundID)
	if err != nil || row == nil {
		response.WriteJSON(w, response.ErrDefault("入站不存在"))
		return
	}
	tunnel, err := h.getTunnelRecord(req.TunnelID)
	if err != nil || tunnel == nil {
		response.WriteJSON(w, response.ErrDefault("隧道不存在"))
		return
	}
	if tunnel.Status != 1 {
		response.WriteJSON(w, response.ErrDefault("隧道已禁用"))
		return
	}
	remoteAddr, err := h.relayTarget(row, tunnel)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	tunnelName, _ := h.repo.GetTunnelName(tunnel.ID)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = fmt.Sprintf("%s → %s", firstNonEmpty(tunnelName, "中转"), row.Name)
	}
	if len([]rune(name)) > 100 {
		response.WriteJSON(w, response.ErrDefault("名称不超过 100 字"))
		return
	}
	shareHost := strings.TrimSpace(req.ShareHost)
	if len(shareHost) > 255 {
		response.WriteJSON(w, response.ErrDefault("分享地址过长"))
		return
	}

	ppSend := 0
	if row.AcceptProxyProtocol == 1 {
		ppSend = relayProxyProtocolVersion
	}
	forwardID, err := h.createRelayForward(userID, name, tunnel, remoteAddr, req.InPort, ppSend)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	rel := &model.NodeInboundRelay{
		InboundID:   row.ID,
		ForwardID:   forwardID,
		Name:        name,
		ShareHost:   shareHost,
		Managed:     1,
		CreatedTime: time.Now().UnixMilli(),
	}
	if err := h.repo.CreateInboundRelay(rel); err != nil {
		if fwd, ferr := h.getForwardRecord(forwardID); ferr == nil && fwd != nil {
			_ = h.removeForwardWithServices(fwd)
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	h.refreshInboundWhitelist(row.ID)
	response.WriteJSON(w, response.OK(map[string]interface{}{"id": rel.ID, "forwardId": forwardID}))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// createRelayForward mirrors the admin path of forwardCreate: same port
// validation, persistence and runtime sync, without user-facing quota checks.
func (h *Handler) createRelayForward(userID int64, name string, tunnel *tunnelRecord, remoteAddr string, inPort int, proxyProtocolSend int) (int64, error) {
	entryNodes, err := h.tunnelEntryNodeIDs(tunnel.ID)
	if err != nil {
		return 0, err
	}
	if len(entryNodes) == 0 {
		return 0, errors.New("隧道没有入口服务器")
	}
	isNftTunnel, _, err := h.tunnelUsesNftables(tunnel.ID)
	if err != nil {
		return 0, err
	}
	if isNftTunnel {
		if err := h.validateNftablesForwardRequest(tunnel, remoteAddr, entryNodes); err != nil {
			return 0, err
		}
	}
	port := inPort
	if port <= 0 {
		port = h.pickTunnelPort(tunnel.ID)
	}
	if port <= 0 || port > 65535 {
		return 0, errors.New("没有可用的入口端口")
	}
	for _, nodeID := range entryNodes {
		node, nodeErr := h.getNodeRecord(nodeID)
		if nodeErr != nil {
			continue
		}
		if err := validateRemoteNodePort(node, port); err != nil {
			return 0, err
		}
		if err := validateLocalNodePort(node, port); err != nil {
			return 0, err
		}
		if err := h.validateForwardPortAvailability(node, port, 0); err != nil {
			return 0, err
		}
	}

	now := time.Now().UnixMilli()
	userName := h.repo.GetUsernameByID(userID)
	if userName == "" {
		userName = "admin"
	}
	forwardID, err := h.repo.CreateForwardTx(userID, userName, name, tunnel.ID, remoteAddr, "fifo", now,
		h.repo.NextIndex("forward"), entryNodes, port, "", nil, 0, 0, nil, 0, 0, proxyProtocolSend)
	if err != nil {
		return 0, err
	}
	created, err := h.getForwardRecord(forwardID)
	if err != nil {
		_ = h.deleteForwardByID(forwardID)
		return 0, err
	}
	if isNftTunnel {
		if err := h.reconcileNftablesNodeByRequest(entryNodes[0]); err != nil {
			_ = h.deleteForwardByID(forwardID)
			return 0, err
		}
		return forwardID, nil
	}
	if err := h.syncForwardServices(created, "UpdateService", true); err != nil {
		_ = h.deleteForwardByID(forwardID)
		return 0, err
	}
	return forwardID, nil
}

// removeForwardWithServices mirrors forwardDelete for a forward the panel
// manages on behalf of an inbound.
func (h *Handler) removeForwardWithServices(forward *forwardRecord) error {
	var nftNodeID int64
	if nftMode, entryNodeIDs, err := h.tunnelUsesNftables(forward.TunnelID); err != nil {
		return err
	} else if nftMode {
		if len(entryNodeIDs) > 0 {
			nftNodeID = entryNodeIDs[0]
		}
	} else if err := h.controlForwardServices(forward, "DeleteService", true); err != nil {
		return err
	}
	if err := h.deleteForwardByID(forward.ID); err != nil {
		return err
	}
	if nftNodeID > 0 {
		return h.reconcileNftablesNodeByRequest(nftNodeID)
	}
	return nil
}

// inboundRelayUpdate renames a relay and changes its share host. Both only
// feed share links and subscriptions, so nothing is pushed to the nodes.
func (h *Handler) inboundRelayUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		ShareHost string `json:"shareHost"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	rel, err := h.repo.GetInboundRelay(req.ID)
	if err != nil || rel == nil {
		response.WriteJSON(w, response.ErrDefault("中转不存在"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		response.WriteJSON(w, response.ErrDefault("名称不能为空"))
		return
	}
	if len([]rune(name)) > 100 {
		response.WriteJSON(w, response.ErrDefault("名称不超过 100 字"))
		return
	}
	shareHost := strings.TrimSpace(req.ShareHost)
	if len(shareHost) > 255 {
		response.WriteJSON(w, response.ErrDefault("分享地址过长"))
		return
	}
	if err := h.repo.UpdateInboundRelay(rel.ID, name, shareHost); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) inboundRelayDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID    int64 `json:"id"`
		Force bool  `json:"force"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	rel, err := h.repo.GetInboundRelay(req.ID)
	if err != nil || rel == nil {
		response.WriteJSON(w, response.ErrDefault("中转不存在"))
		return
	}
	if err := h.removeRelay(rel, req.Force); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	h.refreshInboundWhitelist(rel.InboundID)
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) removeRelay(rel *model.NodeInboundRelay, force bool) error {
	if rel.Managed == 1 {
		if fwd, err := h.getForwardRecord(rel.ForwardID); err == nil && fwd != nil {
			if err := h.removeForwardWithServices(fwd); err != nil {
				if !force {
					return fmt.Errorf("删除中转转发失败: %v（可强制删除）", err)
				}
				_ = h.deleteForwardByID(fwd.ID)
			}
		}
	}
	return h.repo.DeleteInboundRelay(rel.ID)
}

// removeInboundRelays deletes the forwards the panel created for an inbound.
func (h *Handler) removeInboundRelays(inboundID int64) {
	rels, err := h.repo.ListInboundRelays(inboundID)
	if err != nil {
		return
	}
	for _, rel := range rels {
		rel := rel.NodeInboundRelay
		if err := h.removeRelay(&rel, true); err != nil {
			log.Printf("relay cleanup failed relay_id=%d err=%v", rel.ID, err)
		}
	}
}

// ---- share links & subscription ----

// inboundEndpoints lists every address clients can use for the inbounds:
// the landing node directly (unless relay-only or local-only) and each
// relay entry.
func (h *Handler) inboundEndpoints(rows []model.NodeInbound) ([]kernel.Endpoint, []string) {
	relays, err := h.relayViews(0)
	if err != nil {
		log.Printf("relay lookup failed: %v", err)
	}
	byInbound := make(map[int64][]relayView)
	for _, rel := range relays {
		byInbound[rel.InboundID] = append(byInbound[rel.InboundID], rel)
	}
	nodes := make(map[int64]*model.Node)
	postQuantum := make(map[int64]bool)

	var endpoints []kernel.Endpoint
	var kinds []string
	for _, row := range rows {
		if row.Enabled != 1 {
			continue
		}
		in, err := inboundFromModel(row)
		if err != nil {
			continue
		}
		node, ok := nodes[row.NodeID]
		if !ok {
			node, _ = h.repo.GetNodeByID(row.NodeID)
			nodes[row.NodeID] = node
		}
		pq, ok := postQuantum[row.KernelID]
		if !ok && row.KernelID > 0 {
			k, _ := h.repo.GetNodeKernel(row.KernelID)
			pq = kernelSupportsPostQuantumReality(k)
			postQuantum[row.KernelID] = pq
		}
		proxy := importedProxy(&row)
		if row.RelayOnly != 1 && !isLoopbackListen(row.Listen) {
			host := inboundShareAddress(&row, node)
			if host != "" {
				endpoints = append(endpoints, kernel.Endpoint{Name: row.Name, Host: host, Port: row.Port, Inbound: in, Direct: true, Proxy: proxy, SourceID: row.SourceID, PostQuantumReality: pq})
				kinds = append(kinds, "direct")
			}
		}
		for _, rel := range byInbound[row.ID] {
			if !rel.ForwardExists || rel.ForwardStatus != 1 {
				continue
			}
			for i, entry := range rel.Entries {
				name := rel.Name
				if len(rel.Entries) > 1 {
					name = fmt.Sprintf("%s %d", rel.Name, i+1)
				}
				host := entry.Host
				if rel.ShareHost != "" {
					host = rel.ShareHost
				}
				// A relay forwards TCP; REALITY still ends at the landing kernel.
				endpoints = append(endpoints, kernel.Endpoint{Name: name, Host: host, Port: entry.Port, Inbound: in, Proxy: proxy, SourceID: row.SourceID, PostQuantumReality: pq})
				kinds = append(kinds, "relay")
			}
		}
	}
	return endpoints, kinds
}

func (h *Handler) inboundShare(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	row, err := h.repo.GetNodeInbound(id)
	if err != nil || row == nil {
		response.WriteJSON(w, response.ErrDefault("入站不存在"))
		return
	}
	enabled := *row
	enabled.Enabled = 1 // share links are useful to prepare even while disabled
	endpoints, kinds := h.inboundEndpoints([]model.NodeInbound{enabled})
	out := make([]shareEntry, 0, len(endpoints))
	for i, e := range endpoints {
		uri, _ := kernel.ShareURI(e)
		surge, _ := kernel.SurgeLine(e)
		conf, _ := kernel.WireGuardConf(e)
		out = append(out, shareEntry{Name: e.Name, Kind: kinds[i], Host: e.Host, Port: e.Port, URI: uri, Surge: surge, Conf: conf})
	}
	response.WriteJSON(w, response.OK(out))
}

// openSubscription serves GET /api/v1/open_api/sub/{token}. It is outside
// JWT auth; the 128-bit token is the credential and is compared in constant
// time.
func (h *Handler) openSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/open_api/sub/"), "/")
	if len(token) != 32 {
		http.NotFound(w, r)
		return
	}
	sub, err := h.repo.GetSubscriptionByToken(token)
	if err != nil || sub == nil || !constantTimeEqual(token, sub.Token) {
		http.NotFound(w, r)
		return
	}
	now := time.Now()

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = kernel.FormatForUserAgent(r.UserAgent())
	}
	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if only := r.URL.Query().Get("inbound"); only != "" {
		id, _ := strconv.ParseInt(only, 10, 64)
		filtered := rows[:0]
		for _, row := range rows {
			if row.ID == id {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	c, err := h.subContextFor(rows)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// A share subscription reports its own traffic and limit; the owner's
	// reports the selected inbounds' traffic.
	var up, down, total int64
	expire := sub.ExpireTime / 1000
	if isShare(sub) {
		up, down, total = sub.UploadBytes, sub.DownloadBytes, sub.TrafficLimit
	} else {
		want := map[int64]bool{}
		for _, id := range c.inboundIDs(sub) {
			want[id] = true
		}
		for _, row := range rows {
			if want[row.ID] {
				up += row.UploadBytes
				down += row.DownloadBytes
			}
		}
	}

	// A link that stopped working still answers with a profile: one node
	// named after the reason. An error would leave the client on its old
	// profile with no hint why.
	var body, contentType string
	if reason := subInactiveReason(sub, now); reason != "" {
		body, contentType, err = kernel.Subscription(format, nil, kernel.SubOptions{Notices: []string{reason}})
	} else {
		p := parsePipeline(sub.Options)
		opts := h.subOptions(p)
		opts.SingboxSnell = kernel.SingboxClientHasSnell(r.UserAgent())
		if p.InfoNodes {
			opts.Notices = kernel.InfoNotices(sub.ExpireTime, up+down, total)
		}
		body, contentType, err = kernel.Subscription(format, h.subNodes(c, sub), opts)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", up, down, total, expire))
	w.Header().Set("Profile-Update-Interval", "12")
	// Clients use the file name as the profile name.
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(sub.Name))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(body))
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// installedKernelVersion is a concrete release such as "25.10.15".
var installedKernelVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+(\.[0-9]+)?$`)

// kernelSupportsPostQuantumReality: Xray negotiates X25519MLKEM768 in REALITY
// from 25.5.16 on. Mihomo needs server support for it, so an unknown installed
// version does not count.
func kernelSupportsPostQuantumReality(k *model.NodeKernel) bool {
	if k == nil || k.KernelType != kernel.TypeXray {
		return false
	}
	version := strings.TrimSpace(k.InstalledVersion)
	return installedKernelVersion.MatchString(version) && !versionBelow(version, 25, 5, 16)
}
