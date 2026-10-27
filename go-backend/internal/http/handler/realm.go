package handler

import (
	"errors"
	"fmt"
	"go-backend/internal/store/repo"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/kernel"
	runtimenft "go-backend/internal/runtime/nftables"
	"go-backend/internal/store/model"
	"gorm.io/gorm"
)

// Tunnels whose engine is realm run their forwards with realm on the entry
// nodes instead of the agent's forwarder. realm is installed and supervised
// like a kernel (one per node, config = every realm forward on the node), so
// a forward change re-renders its nodes' realm config.

const tunnelEngineRealm = kernel.TypeRealm

func tunnelIsRealm(t *tunnelRecord) bool { return t != nil && t.Engine == tunnelEngineRealm }

// normalizeTunnelEngine checks a requested engine: "" or "realm".
func normalizeTunnelEngine(raw string) (string, error) {
	switch e := strings.ToLower(strings.TrimSpace(raw)); e {
	case "", "gost":
		return "", nil
	case tunnelEngineRealm:
		return e, nil
	default:
		return "", fmt.Errorf("不支持的转发引擎 %s", raw)
	}
}

// validateRealmTunnelTx: realm runs port forwards on the panel's own agents.
func (h *Handler) validateRealmTunnelTx(tx *gorm.DB, typeVal int, entryNodeIDs []int64) error {
	if typeVal != 1 {
		return errors.New("realm 引擎仅支持端口转发隧道")
	}
	for _, nodeID := range entryNodeIDs {
		node, err := h.repo.GetNodeRecordTx(tx, nodeID)
		if err != nil {
			return err
		}
		if node == nil {
			return errors.New("服务器不存在")
		}
		if node.IsRemote == 1 {
			return fmt.Errorf("服务器 %s 是远程服务器，不能使用 realm 引擎", node.Name)
		}
		if isNftablesForwardMode(node.ForwardMode) {
			return fmt.Errorf("服务器 %s 是 nftables 模式，不能使用 realm 引擎", node.Name)
		}
	}
	return nil
}

// realmIPHash maps the forward strategy: realm only balances by round robin
// or IP hash.
func realmIPHash(strategy string) bool { return strings.TrimSpace(strategy) == "hash" }

func realmListenAddr(node *nodeRecord, port int, bindIP string) string {
	if bindIP = strings.TrimSpace(bindIP); bindIP != "" {
		if _, _, err := net.SplitHostPort(bindIP); err == nil {
			return processServerAddress(bindIP)
		}
		return processServerAddress(net.JoinHostPort(strings.Trim(bindIP, "[]"), strconv.Itoa(port)))
	}
	return processServerAddress(fmt.Sprintf("%s:%d", node.TCPListenAddr, port))
}

// realmNodeEndpoints lists the realm forwards to run on a node. override
// forces a forward on or off: pause, resume and delete re-render before the
// forward's status changes in the database.
func (h *Handler) realmNodeEndpoints(nodeID int64, override map[int64]bool) ([]kernel.RealmEndpoint, []map[string]interface{}, error) {
	node, err := h.getNodeRecord(nodeID)
	if err != nil {
		return nil, nil, err
	}
	forwards, err := h.repo.ListRealmForwardsByNode(nodeID)
	if err != nil {
		return nil, nil, err
	}
	var endpoints []kernel.RealmEndpoint
	var ports []map[string]interface{}
	tunnels := map[int64]*tunnelRecord{}
	for i := range forwards {
		f := &forwards[i]
		tunnel, ok := tunnels[f.TunnelID]
		if !ok {
			tunnel, _ = h.repo.GetTunnelRecord(f.TunnelID)
			tunnels[f.TunnelID] = tunnel
		}
		run := f.Status == 1 && tunnel != nil && tunnel.Status == 1
		if v, ok := override[f.ID]; ok {
			run = v
		}
		if !run {
			continue
		}
		fps, err := h.listForwardPorts(f.ID)
		if err != nil {
			return nil, nil, err
		}
		receive, send := normalizeForwardProxyProtocol(f.ProxyProtocol, f.ProxyProtocolReceive, f.ProxyProtocolSend)
		for _, fp := range fps {
			if fp.NodeID != nodeID || fp.Port <= 0 {
				continue
			}
			e := kernel.RealmEndpoint{
				ForwardID:   f.ID,
				Listen:      realmListenAddr(node, fp.Port, fp.InIP),
				Remotes:     splitRemoteTargets(f.RemoteAddr),
				IPHash:      realmIPHash(f.Strategy),
				Interface:   strings.TrimSpace(node.InterfaceName),
				AcceptProxy: receive > 0,
				SendProxy:   send,
			}
			endpoints = append(endpoints, e)
			ports = append(ports, map[string]interface{}{
				"inboundId": kernel.RealmCounterID(f.ID), "port": fp.Port, "network": "tcp,udp",
			})
		}
	}
	return endpoints, ports, nil
}

// buildRealmApplyPayload renders a node's realm kernel.
func (h *Handler) buildRealmApplyPayload(k *model.NodeKernel, info kernel.Info, override map[int64]bool) (map[string]interface{}, string, error) {
	endpoints, ports, err := h.realmNodeEndpoints(k.NodeID, override)
	if err != nil {
		return nil, "", err
	}
	rendered, err := kernel.RenderRealm(endpoints)
	if err != nil {
		return nil, "", err
	}
	proxy := ""
	if enabled, proxyURL := h.getGithubProxyConfig(); enabled {
		proxy = proxyURL
	}
	download, err := kernel.ResolveDownload(info, k.Version, proxy)
	if err != nil {
		return nil, "", err
	}
	if ports == nil {
		ports = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"type":     k.KernelType,
		"version":  k.Version,
		"download": download,
		"files":    rendered.Files,
		"args":     rendered.Args,
		"hash":     rendered.Hash,
		"enabled":  k.Enabled == 1 && !rendered.Idle,
		"ports":    ports,
	}, rendered.Hash, nil
}

// ensureRealmKernel returns the node's realm kernel, registering it when the
// node needs one (create false only looks it up).
func (h *Handler) ensureRealmKernel(nodeID int64, create bool) (*model.NodeKernel, error) {
	k, err := h.repo.GetNodeKernelByType(nodeID, kernel.TypeRealm)
	if err != nil || (k == nil && !create) {
		return k, err
	}
	now := time.Now().UnixMilli()
	if k == nil {
		k = &model.NodeKernel{
			NodeID: nodeID, KernelType: kernel.TypeRealm, Version: kernel.RealmVersion,
			Status: kernelStatusPending, Enabled: 1, CreatedTime: now, UpdatedTime: now,
		}
		return k, h.repo.CreateNodeKernel(k)
	}
	if k.Version != kernel.RealmVersion {
		// A panel upgrade moved to a newer realm.
		if err := h.repo.UpdateNodeKernelFields(k.ID, map[string]interface{}{"version": kernel.RealmVersion, "updated_time": now}); err != nil {
			return nil, err
		}
		k.Version = kernel.RealmVersion
	}
	return k, nil
}

// syncRealmNodes re-renders realm on the nodes. Offline nodes are skipped
// with a warning; they catch up when the agent reconnects.
func (h *Handler) syncRealmNodes(nodeIDs []int64, override map[int64]bool) ([]string, error) {
	var warnings []string
	for _, nodeID := range uniqueInt64s(nodeIDs) {
		if nodeID <= 0 {
			continue
		}
		endpoints, _, err := h.realmNodeEndpoints(nodeID, override)
		if err != nil {
			return warnings, err
		}
		k, err := h.ensureRealmKernel(nodeID, len(endpoints) > 0)
		if err != nil {
			return warnings, err
		}
		if k == nil {
			continue
		}
		install := k.InstalledVersion == ""
		if err := h.applyKernelWith(k.ID, install, override); err != nil {
			if isNodeOfflineOrTimeoutError(err) {
				warnings = append(warnings, fmt.Sprintf("服务器 %s 不在线，realm 配置待服务器上线后下发", h.nodeLabel(nodeID)))
				continue
			}
			if strings.Contains(err.Error(), "不支持的内核类型") {
				return warnings, fmt.Errorf("服务器 %s 的 agent 版本过旧，不支持 realm，请先升级服务器", h.nodeLabel(nodeID))
			}
			return warnings, fmt.Errorf("服务器 %s realm 下发失败: %w", h.nodeLabel(nodeID), err)
		}
	}
	return warnings, nil
}

// syncRealmForward re-renders the realm nodes of one forward.
func (h *Handler) syncRealmForward(forward *forwardRecord, override map[int64]bool) ([]string, error) {
	ports, err := h.listForwardPorts(forward.ID)
	if err != nil {
		return nil, err
	}
	warnings, err := h.syncRealmNodes(forwardPortNodeIDs(ports), override)
	if err != nil {
		return warnings, err
	}
	if forward.SpeedID.Valid || forward.IPSpeedID.Valid || forward.MaxConn > 0 || forward.IPMaxConn > 0 {
		warnings = append(warnings, "realm 引擎不支持限速和连接数限制，这些设置不生效")
	}
	return warnings, nil
}

// controlRealmForward pauses, resumes or removes a realm forward.
func (h *Handler) controlRealmForward(forward *forwardRecord, commandType string) error {
	run := strings.EqualFold(strings.TrimSpace(commandType), "ResumeService")
	_, err := h.syncRealmForward(forward, map[int64]bool{forward.ID: run})
	return err
}

// switchTunnelEngine moves a tunnel's forwards between realm and the agent's
// forwarder on its old and new entry nodes.
func (h *Handler) switchTunnelEngine(tunnelID int64, fromRealm bool, oldEntryNodeIDs, newEntryNodeIDs []int64) {
	forwards, err := h.listForwardsByTunnel(tunnelID)
	if err != nil {
		return
	}
	nodes := append(append([]int64{}, oldEntryNodeIDs...), newEntryNodeIDs...)
	if fromRealm {
		// Free the ports first, then start the agent's services.
		if _, err := h.syncRealmNodes(nodes, nil); err != nil {
			log.Printf("realm engine switch tunnel_id=%d err=%v", tunnelID, err)
		}
		for i := range forwards {
			_ = h.syncForwardServices(&forwards[i], "UpdateService", true)
		}
		return
	}
	for i := range forwards {
		for _, nodeID := range uniqueInt64s(nodes) {
			_ = h.deleteForwardServicesOnNode(&forwards[i], nodeID)
		}
	}
	if _, err := h.syncRealmNodes(nodes, nil); err != nil {
		log.Printf("realm engine switch tunnel_id=%d err=%v", tunnelID, err)
	}
}

// applyRealmCounters credits realm forwards with the traffic the agent
// counted on their ports, the same way nftables forwards are accounted.
func (h *Handler) applyRealmCounters(nodeID int64, epoch string, counters []agentKernelCounter, now time.Time) {
	sequence, err := h.repo.NextCounterSnapshot(nodeID, "kernel")
	if err != nil {
		return
	}
	var batch flowUploadBatch
	var quotas map[int64]*model.UserQuotaView
	applied, err := h.repo.ApplyCounterSnapshot(nodeID, "kernel", sequence, epoch, func(tx *repo.Repository) error {
		var err error
		batch, quotas, err = (&Handler{repo: tx}).applyRealmCountersTx(nodeID, epoch, counters, now)
		return err
	})
	if err != nil {
		log.Printf("realm accounting failed: %v", err)
		return
	}
	if applied {
		h.enforceFlowUploadBatch(nodeID, batch, quotas)
	}
}
func (h *Handler) applyRealmCountersTx(nodeID int64, epoch string, counters []agentKernelCounter, now time.Time) (flowUploadBatch, map[int64]*model.UserQuotaView, error) {

	samples := make([]runtimenft.CounterSample, 0, len(counters))
	hashes := map[int64]string{}
	for _, c := range counters {
		fid, ok := kernel.RealmForwardID(c.InboundID)
		if !ok || c.Bytes < 0 {
			continue
		}
		dir := runtimenft.CounterDirectionToTarget
		if c.Dir == "out" {
			dir = runtimenft.CounterDirectionFromTarget
		} else if c.Dir != "in" {
			continue
		}
		samples = append(samples, runtimenft.CounterSample{ForwardID: fid, Direction: dir, Protocol: c.Proto, Bytes: uint64(c.Bytes)})
		// The agent's counters restart with its epoch.
		hashes[fid] = "realm:" + epoch
	}
	if len(samples) == 0 {
		return flowUploadBatch{}, nil, nil
	}
	return h.accountForwardCounterSamplesTx(nodeID, samples, hashes, now)
}
