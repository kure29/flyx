package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
	runtimenft "go-backend/internal/runtime/nftables"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"

	"gorm.io/gorm"
)

type nftablesRuntimeManager interface {
	SetBlocked(context.Context, runtimenft.SSHConfig, int, bool) error
	Test(ctx context.Context, cfg runtimenft.SSHConfig) error
	Reconcile(ctx context.Context, cfg runtimenft.SSHConfig, plan runtimenft.NodePlan) (runtimenft.ApplyResult, error)
	Clear(ctx context.Context, cfg runtimenft.SSHConfig) error
	CollectCounters(ctx context.Context, cfg runtimenft.SSHConfig) ([]runtimenft.CounterSample, error)
}

func isNftablesForwardMode(mode string) bool {
	return strings.EqualFold(strings.TrimSpace(mode), runtimenft.ModeNftables)
}

func (h *Handler) nodeUsesNftables(nodeID int64) (bool, error) {
	return h.nodeUsesNftablesTx(nil, nodeID)
}

func (h *Handler) nodeUsesNftablesTx(tx *gorm.DB, nodeID int64) (bool, error) {
	if h == nil || h.repo == nil {
		return false, errors.New("handler not initialized")
	}
	var (
		mode string
		err  error
	)
	if tx != nil {
		mode, err = h.repo.GetNodeForwardModeTx(tx, nodeID)
	} else {
		mode, err = h.repo.GetNodeForwardMode(nodeID)
	}
	if err != nil {
		return false, err
	}
	return isNftablesForwardMode(mode), nil
}

func (h *Handler) tunnelUsesNftables(tunnelID int64) (bool, []int64, error) {
	entryNodeIDs, err := h.tunnelEntryNodeIDs(tunnelID)
	if err != nil {
		return false, nil, err
	}
	for _, nodeID := range entryNodeIDs {
		ok, modeErr := h.nodeUsesNftables(nodeID)
		if modeErr != nil {
			return false, nil, modeErr
		}
		if ok {
			return true, entryNodeIDs, nil
		}
	}
	return false, entryNodeIDs, nil
}

func (h *Handler) validateNftablesForwardRequest(tunnel *tunnelRecord, remoteAddr string, entryNodeIDs []int64) error {
	if tunnel == nil {
		return errors.New("隧道不存在")
	}
	if tunnel.Type != 1 {
		return errors.New("nftables 服务器仅支持直连隧道")
	}
	if len(entryNodeIDs) != 1 {
		return errors.New("nftables 服务器仅支持单入口隧道")
	}
	target, err := runtimenft.ParseSingleTarget(remoteAddr)
	if err != nil {
		return err
	}
	if net.ParseIP(strings.Trim(strings.TrimSpace(target.Host), "[]")) == nil {
		return errors.New("nftables 服务器仅支持 IP 目标地址")
	}
	return nil
}

func sshConfigFromModel(cfg *model.NodeSSHConfig) (runtimenft.SSHConfig, error) {
	if cfg == nil {
		return runtimenft.SSHConfig{}, errors.New("服务器缺少 SSH 配置")
	}
	if strings.TrimSpace(cfg.Host) == "" || strings.TrimSpace(cfg.Username) == "" {
		return runtimenft.SSHConfig{}, errors.New("服务器 SSH 配置不完整")
	}
	return runtimenft.SSHConfig{
		Host:       strings.TrimSpace(cfg.Host),
		Port:       cfg.Port,
		Username:   strings.TrimSpace(cfg.Username),
		AuthType:   strings.TrimSpace(cfg.AuthType),
		Password:   cfg.Password.String,
		PrivateKey: cfg.PrivateKey.String,
		Passphrase: cfg.Passphrase.String,
		SudoMode:   strings.TrimSpace(cfg.SudoMode),
	}, nil
}

func (h *Handler) validateNftablesTunnelState(entryNodeIDs []int64) error {
	return h.validateNftablesTunnelStateTx(nil, entryNodeIDs)
}

func (h *Handler) validateNftablesTunnelStateTx(tx *gorm.DB, entryNodeIDs []int64) error {
	if h == nil || h.repo == nil {
		return errors.New("handler not initialized")
	}
	for _, nodeID := range entryNodeIDs {
		isNft, err := h.nodeUsesNftablesTx(tx, nodeID)
		if err != nil {
			return err
		}
		if !isNft {
			continue
		}
		var cfg *model.NodeSSHConfig
		if tx != nil {
			cfg, err = h.repo.GetNodeSSHConfigTx(tx, nodeID)
		} else {
			cfg, err = h.repo.GetNodeSSHConfig(nodeID)
		}
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("nftables 服务器缺少 SSH 配置")
			}
			return err
		}
		sshCfg, err := sshConfigFromModel(cfg)
		if err != nil {
			return err
		}
		if h.nftablesManager == nil {
			return errors.New("nftables manager not initialized")
		}
		if err := h.nftablesManager.Test(context.Background(), sshCfg); err != nil {
			return fmt.Errorf("nftables 服务器能力校验失败: %w", err)
		}
	}
	return nil
}

func (h *Handler) buildNftablesNodePlan(nodeID int64) (runtimenft.NodePlan, *model.NodeSSHConfig, error) {
	cfg, err := h.repo.GetNodeSSHConfig(nodeID)
	if err != nil {
		return runtimenft.NodePlan{}, nil, err
	}
	forwards, err := h.repo.ListNftablesForwardsByNode(nodeID)
	if err != nil {
		return runtimenft.NodePlan{}, nil, err
	}
	userIDs := make([]int64, 0, len(forwards))
	for _, f := range forwards {
		userIDs = append(userIDs, f.UserID)
	}
	quotas, err := h.repo.ListUserQuotaViewsByUserIDs(userIDs, time.Now())
	if err != nil {
		return runtimenft.NodePlan{}, nil, err
	}
	plan := runtimenft.NodePlan{NodeID: nodeID, Rules: make([]runtimenft.Rule, 0, len(forwards))}
	for i := range forwards {
		forward := &forwards[i]
		tunnel, err := h.getTunnelRecord(forward.TunnelID)
		if err != nil || tunnel == nil || tunnel.Status != 1 {
			continue
		}
		entryNodeIDs, err := h.tunnelEntryNodeIDs(forward.TunnelID)
		if err != nil {
			return runtimenft.NodePlan{}, nil, err
		}
		if len(entryNodeIDs) != 1 || entryNodeIDs[0] != nodeID {
			continue
		}
		if err := h.validateNftablesForwardRequest(tunnel, forward.RemoteAddr, entryNodeIDs); err != nil {
			return runtimenft.NodePlan{}, nil, err
		}
		ports, err := h.listForwardPorts(forward.ID)
		if err != nil {
			return runtimenft.NodePlan{}, nil, err
		}
		for _, fp := range ports {
			if fp.NodeID != nodeID {
				continue
			}
			target, err := runtimenft.ParseSingleTarget(forward.RemoteAddr)
			if err != nil {
				return runtimenft.NodePlan{}, nil, err
			}
			plan.Rules = append(plan.Rules, runtimenft.Rule{
				ForwardID:  forward.ID,
				InPort:     fp.Port,
				BindIP:     strings.TrimSpace(fp.InIP),
				TargetHost: target.Host,
				TargetPort: target.Port,
				Protocols:  []string{"tcp", "udp"},
				Paused:     forward.Status != 1 || (quotas[forward.UserID] != nil && (quotas[forward.UserID].DisabledByQuota == 1 || isUserQuotaExceeded(quotas[forward.UserID]))),
			})
		}
	}
	return plan, cfg, nil
}

func (h *Handler) syncNftablesNode(nodeID int64) error {
	h.nftablesControlMu.Lock()
	defer h.nftablesControlMu.Unlock()
	return h.syncNftablesNodeLocked(nodeID)
}

func (h *Handler) syncNftablesNodeLocked(nodeID int64) error {
	if h == nil || h.repo == nil {
		return errors.New("handler not initialized")
	}
	plan, cfgModel, err := h.buildNftablesNodePlan(nodeID)
	if err != nil {
		return err
	}
	sshCfg, err := sshConfigFromModel(cfgModel)
	if err != nil {
		return err
	}
	// Restore blocks before NAT rules, including after a node restart.
	for _, rule := range plan.Rules {
		if rule.Paused {
			if err := h.nftablesManager.SetBlocked(context.Background(), sshCfg, rule.InPort, true); err != nil {
				return err
			}
		}
	}
	result, err := h.nftablesManager.Reconcile(context.Background(), sshCfg, plan)
	now := time.Now().UnixMilli()
	if err != nil {
		bindings, _ := h.repo.ListNftRuleBindingsByNode(nodeID)
		for _, binding := range bindings {
			_ = h.repo.MarkNftRuleBindingError(binding.ForwardID, nodeID, err.Error(), now)
		}
		return err
	}
	for _, rule := range plan.Rules {
		if !rule.Paused {
			if err := h.nftablesManager.SetBlocked(context.Background(), sshCfg, rule.InPort, false); err != nil {
				return err
			}
		}
	}
	activeForwardIDs := make(map[int64]struct{}, len(plan.Rules))
	for _, rule := range plan.Rules {
		activeForwardIDs[rule.ForwardID] = struct{}{}
		hash := result.Hashes[rule.ForwardID]
		_ = h.repo.UpsertNftRuleBinding(modelToRuleBindingInput(nodeID, rule, hash), now)
	}
	bindings, _ := h.repo.ListNftRuleBindingsByNode(nodeID)
	for _, binding := range bindings {
		if _, ok := activeForwardIDs[binding.ForwardID]; ok {
			continue
		}
		_ = h.repo.DeleteNftRuleBindingsByForward(binding.ForwardID)
	}
	return nil
}

func modelToRuleBindingInput(nodeID int64, rule runtimenft.Rule, hash string) repo.NftRuleBindingInput {
	return repo.NftRuleBindingInput{
		ForwardID:  rule.ForwardID,
		NodeID:     nodeID,
		InPort:     rule.InPort,
		Protocols:  strings.Join(rule.Protocols, ","),
		TargetAddr: fmt.Sprintf("%s:%d", rule.TargetHost, rule.TargetPort),
		BindIP:     rule.BindIP,
		RuleHash:   hash,
		Status:     runtimenft.StatusApplied,
	}
}

func (h *Handler) nftablesNodeIDFromRequest(r *http.Request, w http.ResponseWriter) (int64, bool) {
	nodeID := asInt64FromBodyKey(r, w, "nodeId")
	if nodeID <= 0 {
		return 0, false
	}
	return nodeID, true
}

func (h *Handler) loadNftablesSSHConfig(nodeID int64) (runtimenft.SSHConfig, error) {
	cfg, err := h.repo.GetNodeSSHConfig(nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return runtimenft.SSHConfig{}, errors.New("nftables 服务器缺少 SSH 配置")
		}
		return runtimenft.SSHConfig{}, err
	}
	return sshConfigFromModel(cfg)
}

func (h *Handler) clearNftablesNode(nodeID int64) error {
	h.nftablesControlMu.Lock()
	defer h.nftablesControlMu.Unlock()
	if h == nil || h.repo == nil {
		return errors.New("handler not initialized")
	}
	sshCfg, err := h.loadNftablesSSHConfig(nodeID)
	if err != nil {
		return err
	}
	if h.nftablesManager == nil {
		return errors.New("nftables manager not initialized")
	}
	if err := h.nftablesManager.Clear(context.Background(), sshCfg); err != nil {
		return err
	}
	bindings, listErr := h.repo.ListNftRuleBindingsByNode(nodeID)
	if listErr != nil {
		return listErr
	}
	for _, binding := range bindings {
		if err := h.repo.DeleteNftRuleBindingsByForward(binding.ForwardID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) reconcileNftablesNodeByRequest(nodeID int64) error {
	usesNft, err := h.nodeUsesNftables(nodeID)
	if err != nil {
		return err
	}
	if !usesNft {
		return errors.New("服务器未启用 nftables 转发模式")
	}
	return h.syncNftablesNode(nodeID)
}

func (h *Handler) nodeNftablesTest(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := h.nftablesNodeIDFromRequest(r, w)
	if !ok {
		return
	}
	usesNft, err := h.nodeUsesNftables(nodeID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if !usesNft {
		response.WriteJSON(w, response.ErrDefault("服务器未启用 nftables 转发模式"))
		return
	}
	sshCfg, err := h.loadNftablesSSHConfig(nodeID)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if h.nftablesManager == nil {
		response.WriteJSON(w, response.Err(-2, "nftables manager not initialized"))
		return
	}
	if err := h.nftablesManager.Test(context.Background(), sshCfg); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeNftablesReconcile(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := h.nftablesNodeIDFromRequest(r, w)
	if !ok {
		return
	}
	if err := h.reconcileNftablesNodeByRequest(nodeID); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeNftablesClear(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := h.nftablesNodeIDFromRequest(r, w)
	if !ok {
		return
	}
	usesNft, err := h.nodeUsesNftables(nodeID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if !usesNft {
		response.WriteJSON(w, response.ErrDefault("服务器未启用 nftables 转发模式"))
		return
	}
	if err := h.clearNftablesNode(nodeID); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) controlNftablesForward(forward *forwardRecord, nodes []int64, command string) error {
	h.nftablesControlMu.Lock()
	defer h.nftablesControlMu.Unlock()
	if h.nftablesManager == nil {
		return errors.New("nftables manager not initialized")
	}
	blocked := command == "PauseService" || command == "DeleteService"
	if !blocked && command != "ResumeService" {
		return fmt.Errorf("unsupported nftables command: %s", command)
	}
	ports, err := h.listForwardPorts(forward.ID)
	if err != nil {
		return err
	}
	if len(nodes) != 1 || len(ports) == 0 {
		return errors.New("nftables forward has no valid entry port")
	}
	cfg, err := h.loadNftablesSSHConfig(nodes[0])
	if err != nil {
		return err
	}
	if !blocked {
		bindings, err := h.repo.ListNftRuleBindingsByNode(nodes[0])
		if err != nil {
			return err
		}
		found := false
		for _, binding := range bindings {
			if binding.ForwardID == forward.ID && binding.Status == runtimenft.StatusApplied {
				found = true
			}
		}
		if !found {
			if err := h.syncNftablesNodeLocked(nodes[0]); err != nil {
				return err
			}
		}
	}
	for _, port := range ports {
		if port.NodeID != nodes[0] {
			continue
		}
		if err := h.nftablesManager.SetBlocked(context.Background(), cfg, port.Port, blocked); err != nil {
			return err
		}
	}
	status := 1
	if blocked {
		status = 0
	}
	return h.repo.UpdateForwardStatus(forward.ID, status, time.Now().UnixMilli())
}
