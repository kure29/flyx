package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-backend/internal/http/client"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

type federationRuntimeReleaseRef struct {
	RemoteURL     string
	RemoteToken   string
	BindingID     string
	ReservationID string
	ResourceKey   string
}

func federationRuntimeResourceKey(tunnelID int64, nodeID int64, chainType int, hopInx int) string {
	return fmt.Sprintf("tunnel:%d:node:%d:type:%d:hop:%d", tunnelID, nodeID, chainType, hopInx)
}

func remoteShareIDFromConfig(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return 0
	}
	return asInt64(cfg["shareId"], 0)
}

func (h *Handler) federationLocalDomain() string {
	cfg, _ := h.repo.GetConfigByName("panel_domain")
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Value)
}

func (h *Handler) applyFederationRuntime(state *tunnelCreateState, localDomain string) ([]repo.FederationTunnelBinding, []federationRuntimeReleaseRef, error) {
	bindings := make([]repo.FederationTunnelBinding, 0)
	releaseRefs := make([]federationRuntimeReleaseRef, 0)
	if h == nil || state == nil {
		return bindings, releaseRefs, nil
	}
	fc := client.NewFederationClient()
	now := time.Now().UnixMilli()

	for outIdx := range state.OutNodes {
		outNode := state.OutNodes[outIdx]
		node := state.Nodes[outNode.NodeID]
		if node == nil || node.IsRemote != 1 {
			continue
		}
		remoteURL := strings.TrimSpace(node.RemoteURL)
		remoteToken := strings.TrimSpace(node.RemoteToken)
		if remoteURL == "" || remoteToken == "" {
			return bindings, releaseRefs, fmt.Errorf("远程服务器 %s 缺少共享配置", nodeDisplayName(node))
		}

		resourceKey := federationRuntimeResourceKey(state.TunnelID, outNode.NodeID, 3, 0)
		reserveReq := client.RuntimeReservePortRequest{
			ResourceKey:   resourceKey,
			Protocol:      defaultString(outNode.Protocol, "tls"),
			RequestedPort: outNode.Port,
		}
		reserveRes, err := fc.ReservePort(remoteURL, remoteToken, localDomain, reserveReq)
		if err != nil && reserveReq.RequestedPort > 0 {
			reserveReq.RequestedPort = 0
			reserveRes, err = fc.ReservePort(remoteURL, remoteToken, localDomain, reserveReq)
		}
		if err != nil {
			return bindings, releaseRefs, fmt.Errorf("远程服务器 %s 端口分配失败: %w", nodeDisplayName(node), err)
		}

		releaseRefs = append(releaseRefs, federationRuntimeReleaseRef{
			RemoteURL: remoteURL, RemoteToken: remoteToken, BindingID: reserveRes.BindingID,
			ReservationID: reserveRes.ReservationID, ResourceKey: resourceKey,
		})

		state.OutNodes[outIdx].Port = reserveRes.AllocatedPort
		outNode = state.OutNodes[outIdx]

		applyReq := client.RuntimeApplyRoleRequest{
			ReservationID: reserveRes.ReservationID,
			ResourceKey:   resourceKey,
			Role:          "exit",
			Protocol:      defaultString(outNode.Protocol, "tls"),
			Strategy:      defaultString(outNode.Strategy, "round"),
		}
		applyRes, err := fc.ApplyRole(remoteURL, remoteToken, localDomain, applyReq)
		if err != nil {
			return bindings, releaseRefs, fmt.Errorf("远程服务器 %s 运行时下发失败: %w", nodeDisplayName(node), err)
		}
		if applyRes.AllocatedPort > 0 {
			state.OutNodes[outIdx].Port = applyRes.AllocatedPort
			outNode = state.OutNodes[outIdx]
		}

		bindings = append(bindings, repo.FederationTunnelBinding{
			TunnelID:        state.TunnelID,
			NodeID:          outNode.NodeID,
			ChainType:       3,
			HopInx:          0,
			RemoteURL:       remoteURL,
			ResourceKey:     resourceKey,
			RemoteBindingID: defaultString(applyRes.BindingID, reserveRes.BindingID),
			AllocatedPort:   outNode.Port,
			Status:          1,
			CreatedTime:     now,
			UpdatedTime:     now,
		})
		releaseRefs[len(releaseRefs)-1].BindingID = defaultString(applyRes.BindingID, reserveRes.BindingID)
	}

	for hopIdx := len(state.ChainHops) - 1; hopIdx >= 0; hopIdx-- {
		for nodeIdx := range state.ChainHops[hopIdx] {
			chainNode := state.ChainHops[hopIdx][nodeIdx]
			node := state.Nodes[chainNode.NodeID]
			if node == nil || node.IsRemote != 1 {
				continue
			}
			remoteURL := strings.TrimSpace(node.RemoteURL)
			remoteToken := strings.TrimSpace(node.RemoteToken)
			if remoteURL == "" || remoteToken == "" {
				return bindings, releaseRefs, fmt.Errorf("远程服务器 %s 缺少共享配置", nodeDisplayName(node))
			}

			resourceKey := federationRuntimeResourceKey(state.TunnelID, chainNode.NodeID, 2, hopIdx+1)
			reserveReq := client.RuntimeReservePortRequest{
				ResourceKey:   resourceKey,
				Protocol:      defaultString(chainNode.Protocol, "tls"),
				RequestedPort: chainNode.Port,
			}
			reserveRes, err := fc.ReservePort(remoteURL, remoteToken, localDomain, reserveReq)
			if err != nil && reserveReq.RequestedPort > 0 {
				reserveReq.RequestedPort = 0
				reserveRes, err = fc.ReservePort(remoteURL, remoteToken, localDomain, reserveReq)
			}
			if err != nil {
				return bindings, releaseRefs, fmt.Errorf("远程服务器 %s 端口分配失败: %w", nodeDisplayName(node), err)
			}

			releaseRefs = append(releaseRefs, federationRuntimeReleaseRef{
				RemoteURL: remoteURL, RemoteToken: remoteToken, BindingID: reserveRes.BindingID,
				ReservationID: reserveRes.ReservationID, ResourceKey: resourceKey,
			})

			state.ChainHops[hopIdx][nodeIdx].Port = reserveRes.AllocatedPort
			chainNode = state.ChainHops[hopIdx][nodeIdx]

			nextTargets := state.OutNodes
			if hopIdx+1 < len(state.ChainHops) {
				nextTargets = state.ChainHops[hopIdx+1]
			} else {
				nextTargets = h.orderBestExitTargets(state.TunnelID, chainNode.NodeID, nextTargets)
			}
			applyTargets := make([]client.RuntimeTarget, 0, len(nextTargets))
			for _, target := range nextTargets {
				targetNode := state.Nodes[target.NodeID]
				if targetNode == nil {
					return bindings, releaseRefs, errors.New("服务器不存在")
				}
				host, hostErr := selectTunnelDialHost(node, targetNode, state.IPPreference, target.ConnectIP)
				if hostErr != nil {
					return bindings, releaseRefs, hostErr
				}
				if target.Port <= 0 {
					return bindings, releaseRefs, errors.New("服务器端口不能为空")
				}
				applyTargets = append(applyTargets, client.RuntimeTarget{
					Host:     host,
					Port:     target.Port,
					Protocol: defaultString(target.Protocol, "tls"),
				})
			}

			applyReq := client.RuntimeApplyRoleRequest{
				ReservationID: reserveRes.ReservationID,
				ResourceKey:   resourceKey,
				Role:          "middle",
				Protocol:      defaultString(chainNode.Protocol, "tls"),
				Strategy:      runtimeStrategyForTargets(chainNode, nextTargets),
				Targets:       applyTargets,
			}
			applyRes, err := fc.ApplyRole(remoteURL, remoteToken, localDomain, applyReq)
			if err != nil {
				return bindings, releaseRefs, fmt.Errorf("远程服务器 %s 运行时下发失败: %w", nodeDisplayName(node), err)
			}
			if applyRes.AllocatedPort > 0 {
				state.ChainHops[hopIdx][nodeIdx].Port = applyRes.AllocatedPort
				chainNode = state.ChainHops[hopIdx][nodeIdx]
			}

			bindings = append(bindings, repo.FederationTunnelBinding{
				TunnelID:        state.TunnelID,
				NodeID:          chainNode.NodeID,
				ChainType:       2,
				HopInx:          hopIdx + 1,
				RemoteURL:       remoteURL,
				ResourceKey:     resourceKey,
				RemoteBindingID: defaultString(applyRes.BindingID, reserveRes.BindingID),
				AllocatedPort:   chainNode.Port,
				Status:          1,
				CreatedTime:     now,
				UpdatedTime:     now,
			})
			releaseRefs[len(releaseRefs)-1].BindingID = defaultString(applyRes.BindingID, reserveRes.BindingID)
		}
	}

	return bindings, releaseRefs, nil
}

// releaseFederationRuntimeRefs is used after the caller has rolled back its
// transaction. Failed releases survive process restarts in a separate queue.
func (h *Handler) releaseFederationRuntimeRefs(refs []federationRuntimeReleaseRef) error {
	if h == nil || len(refs) == 0 {
		return nil
	}
	fc := client.NewFederationClient()
	localDomain := h.federationLocalDomain()
	var failures []error
	for i := len(refs) - 1; i >= 0; i-- {
		ref := refs[i]
		pending := &model.FederationPendingRelease{
			RemoteURL: ref.RemoteURL, RemoteToken: ref.RemoteToken,
			BindingID: ref.BindingID, ReservationID: ref.ReservationID, ResourceKey: ref.ResourceKey,
		}
		if err := h.repo.SavePendingFederationRelease(pending); err != nil {
			failures = append(failures, fmt.Errorf("保存共享运行时清理任务失败: %w", err))
			continue
		}
		req := client.RuntimeReleaseRoleRequest{
			BindingID: ref.BindingID, ReservationID: ref.ReservationID, ResourceKey: ref.ResourceKey,
		}
		if err := fc.ReleaseRole(ref.RemoteURL, ref.RemoteToken, localDomain, req); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := h.repo.DeletePendingFederationRelease(pending.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (h *Handler) cleanupFederationRuntime(tunnelID int64) error {
	if h == nil || tunnelID <= 0 {
		return nil
	}
	bindings, err := h.repo.ListFederationTunnelBindingsForCleanup(tunnelID)
	if err != nil {
		return err
	}
	var failures []error
	for _, b := range bindings {
		// Persist intent before sending the request: timeouts can occur after
		// the peer has already acted, and must remain safely retryable.
		if err := h.repo.MarkFederationTunnelBindingPendingRelease(b.ID); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := h.releaseFederationTunnelBinding(b); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (h *Handler) releaseFederationTunnelBinding(b repo.FederationTunnelBinding) error {
	node, err := h.repo.GetNodeByID(b.NodeID)
	if err != nil {
		return err
	}
	if node == nil {
		return fmt.Errorf("共享服务器 %d 不存在，保留待清理绑定", b.NodeID)
	}
	remoteURL := strings.TrimSpace(b.RemoteURL)
	if remoteURL == "" {
		remoteURL = strings.TrimSpace(node.RemoteURL.String)
	}
	remoteToken := strings.TrimSpace(node.RemoteToken.String)
	if remoteURL == "" || remoteToken == "" {
		return fmt.Errorf("共享服务器 %d 缺少连接配置，保留待清理绑定", b.NodeID)
	}
	req := client.RuntimeReleaseRoleRequest{
		BindingID: strings.TrimSpace(b.RemoteBindingID), ResourceKey: strings.TrimSpace(b.ResourceKey),
	}
	if err := client.NewFederationClient().ReleaseRole(remoteURL, remoteToken, h.federationLocalDomain(), req); err != nil {
		return fmt.Errorf("共享服务器 %d 清理失败: %w", b.NodeID, err)
	}
	return h.repo.DeleteFederationTunnelBinding(b.ID)
}

// retryPendingFederationRuntimeCleanup never touches active bindings.
func (h *Handler) retryPendingFederationRuntimeCleanup() error {
	bindings, err := h.repo.ListPendingFederationTunnelBindings()
	if err != nil {
		return err
	}
	var failures []error
	for _, binding := range bindings {
		if err := h.releaseFederationTunnelBinding(binding); err != nil {
			failures = append(failures, err)
		}
	}
	pending, err := h.repo.ListPendingFederationReleases()
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	fc := client.NewFederationClient()
	for _, item := range pending {
		req := client.RuntimeReleaseRoleRequest{
			BindingID: item.BindingID, ReservationID: item.ReservationID, ResourceKey: item.ResourceKey,
		}
		if err := fc.ReleaseRole(item.RemoteURL, item.RemoteToken, h.federationLocalDomain(), req); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := h.repo.DeletePendingFederationRelease(item.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
