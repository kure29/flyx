package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/client"
	"go-backend/internal/store/model"
	"go-backend/internal/ws"
)

var errForwardNotFound = errors.New("forward not found")

type forwardRecord = model.ForwardRecord
type tunnelRecord = model.TunnelRecord
type forwardPortRecord = model.ForwardPortRecord
type nodeRecord = model.NodeRecord

type chainNodeRecord = model.ChainNodeRecord

const (
	defaultNodeCommandTimeout  = 6 * time.Second
	diagnosisCommandTimeout    = 30 * time.Second
	diagnosisRequestTimeout    = 2 * time.Minute
	diagnosisCommandTimeoutMsg = "诊断超时（30秒）"
	diagnosisRequestTimeoutMsg = "诊断超时（2分钟）"
)

func (h *Handler) resolveForwardAccess(r *http.Request, forwardID int64) (*forwardRecord, int64, int, error) {
	userID, roleID, err := userRoleFromRequest(r)
	if err != nil {
		return nil, 0, 0, err
	}
	forward, err := h.ensureForwardAccessByActor(userID, roleID, forwardID)
	if err != nil {
		return nil, userID, roleID, err
	}
	return forward, userID, roleID, nil
}

func (h *Handler) ensureForwardAccessByActor(actorUserID int64, actorRole int, forwardID int64) (*forwardRecord, error) {
	forward, err := h.getForwardRecord(forwardID)
	if err != nil {
		return nil, err
	}
	if actorRole != 0 && forward.UserID != actorUserID {
		return nil, errForwardNotFound
	}
	return forward, nil
}

func (h *Handler) ensureTunnelPermission(userID int64, roleID int, tunnelID int64) error {
	if roleID == 0 {
		return nil
	}
	ok, err := h.repo.UserTunnelExistsByUserAndTunnel(userID, tunnelID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("你没有该隧道的权限")
	}
	return nil
}

func (h *Handler) getForwardRecord(forwardID int64) (*forwardRecord, error) {
	fr, err := h.repo.GetForwardRecord(forwardID)
	if err != nil {
		return nil, err
	}
	if fr == nil {
		return nil, errForwardNotFound
	}
	return fr, nil
}

func (h *Handler) getTunnelRecord(tunnelID int64) (*tunnelRecord, error) {
	tr, err := h.repo.GetTunnelRecord(tunnelID)
	if err != nil {
		return nil, err
	}
	if tr == nil {
		return nil, errors.New("隧道不存在")
	}
	return tr, nil
}

func (h *Handler) listForwardsByTunnel(tunnelID int64) ([]forwardRecord, error) {
	return h.repo.ListForwardsByTunnel(tunnelID)
}

func (h *Handler) listForwardPorts(forwardID int64) ([]forwardPortRecord, error) {
	return h.repo.ListForwardPorts(forwardID)
}

func (h *Handler) getNodeRecord(nodeID int64) (*nodeRecord, error) {
	n, err := h.repo.GetNodeRecord(nodeID)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, errors.New("服务器不存在")
	}
	return n, nil
}

func (h *Handler) resolveUserTunnelAndLimiter(userID, tunnelID int64) (int64, *int64, *int, error) {
	info, err := h.repo.ResolveUserTunnelAndLimiter(userID, tunnelID)
	if err != nil {
		return 0, nil, nil, err
	}
	if info == nil {
		return 0, nil, nil, nil
	}
	return info.UserTunnelID, info.LimiterID, info.Speed, nil
}

func (h *Handler) listUserTunnelIDs(userID, tunnelID int64) ([]int64, error) {
	return h.repo.ListUserTunnelIDs(userID, tunnelID)
}

func (h *Handler) listUserTunnelIDsByUser(userID int64) ([]int64, error) {
	return h.repo.ListUserTunnelIDsByUser(userID)
}

func (h *Handler) applyNodeProtocolChange(nodeID int64, httpVal, tlsVal, socksVal int) error {
	_, err := h.sendNodeCommand(nodeID, "SetProtocol", map[string]interface{}{
		"http":  httpVal,
		"tls":   tlsVal,
		"socks": socksVal,
	}, false, false)
	return err
}

func (h *Handler) sendNodeCommand(nodeID int64, commandType string, data interface{}, tolerateExists bool, tolerateNotFound bool) (ws.CommandResult, error) {
	return h.sendNodeCommandWithTimeout(nodeID, commandType, data, defaultNodeCommandTimeout, tolerateExists, tolerateNotFound)
}

func (h *Handler) sendNodeCommandWithTimeout(nodeID int64, commandType string, data interface{}, timeout time.Duration, tolerateExists bool, tolerateNotFound bool) (ws.CommandResult, error) {
	var (
		result ws.CommandResult
		err    error
	)
	if timeout <= 0 {
		timeout = defaultNodeCommandTimeout
	}

	node, nodeErr := h.getNodeRecord(nodeID)
	if nodeErr == nil && node != nil && node.IsRemote == 1 {
		result, err = h.sendRemoteNodeCommandWithTimeout(node, commandType, data, timeout)
	} else {
		result, err = h.wsServer.SendCommand(nodeID, commandType, data, timeout)
	}
	if err == nil {
		return result, nil
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if tolerateExists {
		if isAlreadyExistsMessage(msg) {
			return result, nil
		}
	}
	if tolerateNotFound {
		if strings.Contains(msg, "not found") || strings.Contains(msg, "不存在") {
			return result, nil
		}
	}
	return result, err
}

func (h *Handler) sendRemoteNodeCommand(node *nodeRecord, commandType string, data interface{}) (ws.CommandResult, error) {
	return h.sendRemoteNodeCommandWithTimeout(node, commandType, data, 0)
}

func (h *Handler) sendRemoteNodeCommandWithTimeout(node *nodeRecord, commandType string, data interface{}, timeout time.Duration) (ws.CommandResult, error) {
	if node == nil {
		return ws.CommandResult{}, errors.New("服务器不存在")
	}
	remoteURL := strings.TrimSpace(node.RemoteURL)
	remoteToken := strings.TrimSpace(node.RemoteToken)
	if remoteURL == "" || remoteToken == "" {
		return ws.CommandResult{}, errors.New("远程服务器缺少共享配置")
	}

	fc := client.NewFederationClient()
	if timeout > 0 {
		fc = client.NewFederationClientWithTimeout(timeout)
	}
	res, err := fc.Command(remoteURL, remoteToken, h.federationLocalDomain(), client.RuntimeNodeCommandRequest{
		CommandType: commandType,
		Data:        data,
	})
	if err != nil {
		return ws.CommandResult{}, err
	}
	if res == nil {
		return ws.CommandResult{}, errors.New("远程服务器未返回命令结果")
	}

	result := ws.CommandResult{
		Type:    res.Type,
		Success: res.Success,
		Message: res.Message,
		Data:    res.Data,
	}
	if !result.Success {
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "命令执行失败"
		}
		return result, errors.New(msg)
	}
	return result, nil
}
