package handler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/client"
	runtimenft "go-backend/internal/runtime/nftables"
	"go-backend/internal/store/model"
)

type diagnosisTarget struct {
	Address string
	IP      string
	Port    int
}

type diagnosisWorkItem struct {
	fromNodeID   int64
	targetIP     string
	targetPort   int
	description  string
	metadata     map[string]interface{}
	toNode       chainNodeRecord
	hasChainHop  bool
	ipPreference string
	protocol     string
}

type diagnosisExecOptions struct {
	commandTimeout time.Duration
	pingTimeoutMS  int
	pingCount      int
	timeoutMessage string
}

type diagnosisProgress struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Success   int `json:"success"`
	Failed    int `json:"failed"`
}

type diagnosisItemEmitter func(index int, item map[string]interface{}, progress diagnosisProgress)

func (h *Handler) buildDiagnosisStreamStartItems(workItems []diagnosisWorkItem) []map[string]interface{} {
	if len(workItems) == 0 {
		return []map[string]interface{}{}
	}

	nodeCache := map[int64]*nodeRecord{}
	items := make([]map[string]interface{}, 0, len(workItems))
	for _, workItem := range workItems {
		targetIP := strings.TrimSpace(workItem.targetIP)
		targetPort := workItem.targetPort
		if workItem.hasChainHop {
			fromNode, _ := h.cachedNode(nodeCache, workItem.fromNodeID)
			targetNode, err := h.cachedNode(nodeCache, workItem.toNode.NodeID)
			if err == nil {
				resolvedIP, resolvedPort, resolveErr := resolveChainProbeTarget(fromNode, targetNode, workItem.toNode.Port, workItem.ipPreference, workItem.toNode.ConnectIP)
				if resolveErr == nil {
					targetIP = resolvedIP
					targetPort = resolvedPort
				}
			}
		}
		if targetPort <= 0 {
			targetPort = 443
		}

		nodeName := fmt.Sprintf("node_%d", workItem.fromNodeID)
		if node, err := h.cachedNode(nodeCache, workItem.fromNodeID); err == nil && strings.TrimSpace(node.Name) != "" {
			nodeName = node.Name
		}

		item := map[string]interface{}{
			"success":     false,
			"diagnosing":  true,
			"description": workItem.description,
			"nodeName":    nodeName,
			"nodeId":      strconv.FormatInt(workItem.fromNodeID, 10),
			"targetIp":    targetIP,
			"targetPort":  targetPort,
			"message":     "诊断中...",
		}
		for key, value := range workItem.metadata {
			item[key] = value
		}
		items = append(items, item)
	}

	return items
}

const diagnosisMaxConcurrency = 8

func (h *Handler) diagnoseForwardRuntime(ctx context.Context, forward *forwardRecord) (map[string]interface{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if payload, handled, err := h.diagnoseNftablesForwardRuntime(forward); handled || err != nil {
		return payload, err
	}
	forwardName, workItems, err := h.prepareForwardDiagnosis(forward)
	if err != nil {
		return nil, err
	}

	results := h.runDiagnosisWorkItems(ctx, workItems, nil)

	payload := map[string]interface{}{
		"forwardName": forwardName,
		"timestamp":   time.Now().UnixMilli(),
		"results":     results,
	}
	return payload, nil
}

func (h *Handler) diagnoseNftablesForwardRuntime(forward *forwardRecord) (map[string]interface{}, bool, error) {
	if forward == nil {
		return nil, false, errForwardNotFound
	}
	nftMode, entryNodeIDs, err := h.tunnelUsesNftables(forward.TunnelID)
	if err != nil {
		return nil, false, err
	}
	if !nftMode {
		return nil, false, nil
	}
	if len(entryNodeIDs) == 0 {
		return nil, true, errors.New("nftables 转发缺少入口服务器")
	}
	targets, err := resolveDiagnosisTargets(forward.RemoteAddr)
	if err != nil {
		return nil, true, err
	}
	results, err := h.buildNftablesForwardDiagnosisResults(forward, entryNodeIDs[0], targets)
	if err != nil {
		return nil, true, err
	}
	payload := map[string]interface{}{
		"forwardName": forward.Name,
		"timestamp":   time.Now().UnixMilli(),
		"results":     results,
	}
	return payload, true, nil
}

func (h *Handler) buildNftablesForwardDiagnosisResults(forward *forwardRecord, nodeID int64, targets []diagnosisTarget) ([]map[string]interface{}, error) {
	if h == nil || h.repo == nil {
		return nil, errors.New("handler not initialized")
	}
	node, err := h.getNodeRecord(nodeID)
	if err != nil {
		return nil, err
	}
	bindings, err := h.repo.ListNftRuleBindingsByNode(nodeID)
	if err != nil {
		return nil, err
	}
	var binding *model.NftRuleBinding
	for i := range bindings {
		if bindings[i].ForwardID == forward.ID {
			binding = &bindings[i]
			break
		}
	}
	target := diagnosisTarget{}
	if len(targets) > 0 {
		target = targets[0]
	}

	status := "missing"
	message := "nftables 规则未下发"
	success := false
	inPort := 0
	protocols := ""
	targetAddr := strings.TrimSpace(forward.RemoteAddr)
	ruleHash := ""
	if binding != nil {
		status = strings.ToLower(strings.TrimSpace(binding.Status))
		inPort = binding.InPort
		protocols = strings.TrimSpace(binding.Protocols)
		targetAddr = strings.TrimSpace(binding.TargetAddr)
		ruleHash = strings.TrimSpace(binding.RuleHash)
		if status == "" {
			status = "pending"
		}
		if status == runtimenft.StatusApplied {
			success = true
			message = "nftables 规则已下发"
		} else if strings.TrimSpace(binding.LastError) != "" {
			message = binding.LastError
		} else {
			message = "nftables 规则未完成下发"
		}
	}

	packetLoss := 100
	if success {
		packetLoss = 0
	}
	result := map[string]interface{}{
		"success":       success,
		"nodeName":      node.Name,
		"nodeId":        strconv.FormatInt(nodeID, 10),
		"targetIp":      target.IP,
		"targetPort":    target.Port,
		"description":   fmt.Sprintf("nftables规则(%s)->目标(%s)", node.Name, defaultString(target.Address, targetAddr)),
		"averageTime":   0,
		"packetLoss":    packetLoss,
		"message":       message,
		"fromChainType": 1,
		"forwardMode":   "nftables",
		"nftRuleStatus": status,
		"nftRuleHash":   ruleHash,
		"inPort":        inPort,
		"protocols":     protocols,
		"targetAddr":    targetAddr,
	}
	return []map[string]interface{}{result}, nil
}

func (h *Handler) prepareForwardDiagnosis(forward *forwardRecord) (string, []diagnosisWorkItem, error) {
	if forward == nil {
		return "", nil, errForwardNotFound
	}
	targets, err := resolveDiagnosisTargets(forward.RemoteAddr)
	if err != nil {
		return "", nil, err
	}

	tunnel, err := h.getTunnelRecord(forward.TunnelID)
	if err != nil {
		return "", nil, err
	}

	chainRows, err := h.listChainNodesForTunnel(forward.TunnelID)
	if err != nil {
		return "", nil, err
	}
	if len(chainRows) == 0 {
		return "", nil, errors.New("隧道配置不完整")
	}

	ipPreference := h.repo.GetTunnelIPPreference(forward.TunnelID)
	protocol := strings.ToLower(strings.TrimSpace(tunnel.Protocol))

	inNodes, chainHops, outNodes := splitChainNodeGroups(chainRows)
	workItems := make([]diagnosisWorkItem, 0, len(chainRows)*2+len(targets))

	switch tunnel.Type {
	case 1:
		for _, inNode := range inNodes {
			for _, target := range targets {
				description := fmt.Sprintf("入口(%s)->目标(%s)", inNode.NodeName, target.Address)
				workItems = append(workItems, diagnosisWorkItem{
					fromNodeID:  inNode.NodeID,
					targetIP:    target.IP,
					targetPort:  target.Port,
					description: description,
					protocol:    "tcp",
					metadata: map[string]interface{}{
						"fromChainType": 1,
					},
				})
			}
		}
	case 2:
		for _, inNode := range inNodes {
			if len(chainHops) > 0 {
				for _, firstNode := range chainHops[0] {
					description := fmt.Sprintf("入口(%s)->第1跳(%s)", inNode.NodeName, firstNode.NodeName)
					workItems = append(workItems, diagnosisWorkItem{
						fromNodeID:   inNode.NodeID,
						toNode:       firstNode,
						hasChainHop:  true,
						ipPreference: ipPreference,
						description:  description,
						protocol:     defaultString(strings.ToLower(strings.TrimSpace(firstNode.Protocol)), protocol),
						metadata: map[string]interface{}{
							"fromChainType": 1,
							"toChainType":   2,
							"toInx":         firstNode.Inx,
						},
					})
				}
			} else {
				for _, outNode := range outNodes {
					description := fmt.Sprintf("入口(%s)->出口(%s)", inNode.NodeName, outNode.NodeName)
					workItems = append(workItems, diagnosisWorkItem{
						fromNodeID:   inNode.NodeID,
						toNode:       outNode,
						hasChainHop:  true,
						ipPreference: ipPreference,
						description:  description,
						protocol:     defaultString(strings.ToLower(strings.TrimSpace(outNode.Protocol)), protocol),
						metadata: map[string]interface{}{
							"fromChainType": 1,
							"toChainType":   3,
						},
					})
				}
			}
		}

		for i, hop := range chainHops {
			for _, currentNode := range hop {
				if i+1 < len(chainHops) {
					for _, nextNode := range chainHops[i+1] {
						description := fmt.Sprintf("第%d跳(%s)->第%d跳(%s)", i+1, currentNode.NodeName, i+2, nextNode.NodeName)
						workItems = append(workItems, diagnosisWorkItem{
							fromNodeID:   currentNode.NodeID,
							toNode:       nextNode,
							hasChainHop:  true,
							ipPreference: ipPreference,
							description:  description,
							protocol:     defaultString(strings.ToLower(strings.TrimSpace(nextNode.Protocol)), protocol),
							metadata: map[string]interface{}{
								"fromChainType": 2,
								"fromInx":       currentNode.Inx,
								"toChainType":   2,
								"toInx":         nextNode.Inx,
							},
						})
					}
				} else {
					for _, outNode := range outNodes {
						description := fmt.Sprintf("第%d跳(%s)->出口(%s)", i+1, currentNode.NodeName, outNode.NodeName)
						workItems = append(workItems, diagnosisWorkItem{
							fromNodeID:   currentNode.NodeID,
							toNode:       outNode,
							hasChainHop:  true,
							ipPreference: ipPreference,
							description:  description,
							protocol:     defaultString(strings.ToLower(strings.TrimSpace(outNode.Protocol)), protocol),
							metadata: map[string]interface{}{
								"fromChainType": 2,
								"fromInx":       currentNode.Inx,
								"toChainType":   3,
							},
						})
					}
				}
			}
		}

		for _, outNode := range outNodes {
			for _, target := range targets {
				description := fmt.Sprintf("出口(%s)->目标(%s)", outNode.NodeName, target.Address)
				workItems = append(workItems, diagnosisWorkItem{
					fromNodeID:  outNode.NodeID,
					targetIP:    target.IP,
					targetPort:  target.Port,
					description: description,
					protocol:    "tcp",
					metadata: map[string]interface{}{
						"fromChainType": 3,
					},
				})
			}
		}
	default:
		for _, inNode := range inNodes {
			for _, target := range targets {
				description := fmt.Sprintf("入口(%s)->目标(%s)", inNode.NodeName, target.Address)
				workItems = append(workItems, diagnosisWorkItem{
					fromNodeID:  inNode.NodeID,
					targetIP:    target.IP,
					targetPort:  target.Port,
					description: description,
					protocol:    "tcp",
					metadata: map[string]interface{}{
						"fromChainType": 1,
					},
				})
			}
		}
	}

	return forward.Name, workItems, nil
}

func (h *Handler) diagnoseTunnelRuntime(ctx context.Context, tunnelID int64) (map[string]interface{}, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	tunnelName, tunnelType, workItems, err := h.prepareTunnelDiagnosis(tunnelID)
	if err != nil {
		return nil, err
	}

	results := h.runDiagnosisWorkItems(ctx, workItems, nil)

	payload := map[string]interface{}{
		"tunnelName": tunnelName,
		"tunnelType": tunnelType,
		"timestamp":  time.Now().UnixMilli(),
		"results":    results,
	}
	return payload, nil
}

func (h *Handler) prepareTunnelDiagnosis(tunnelID int64) (string, string, []diagnosisWorkItem, error) {
	tunnel, err := h.getTunnelRecord(tunnelID)
	if err != nil {
		return "", "", nil, err
	}

	tunnelName, err := h.repo.GetTunnelName(tunnelID)
	if err != nil {
		return "", "", nil, err
	}
	if tunnelName == "" {
		return "", "", nil, errors.New("隧道不存在")
	}

	chainRows, err := h.listChainNodesForTunnel(tunnelID)
	if err != nil {
		return "", "", nil, err
	}
	if len(chainRows) == 0 {
		return "", "", nil, errors.New("隧道配置不完整")
	}

	ipPreference := h.repo.GetTunnelIPPreference(tunnelID)
	protocol := strings.ToLower(strings.TrimSpace(tunnel.Protocol))
	probeTarget := effectiveTunnelProbeTargetValues(tunnel.ProbeTargetHost, tunnel.ProbeTargetPort)
	inNodes, chainHops, outNodes := splitChainNodeGroups(chainRows)
	workItems := make([]diagnosisWorkItem, 0, len(chainRows)*2)

	switch tunnel.Type {
	case 1:
		for _, inNode := range inNodes {
			description := fmt.Sprintf("入口(%s)->外网", inNode.NodeName)
			workItems = append(workItems, diagnosisWorkItem{
				fromNodeID:  inNode.NodeID,
				targetIP:    probeTarget.Host,
				targetPort:  probeTarget.Port,
				description: description,
				protocol:    "tcp",
				metadata: map[string]interface{}{
					"fromChainType": 1,
				},
			})
		}
	case 2:
		for _, inNode := range inNodes {
			if len(chainHops) > 0 {
				for _, firstNode := range chainHops[0] {
					description := fmt.Sprintf("入口(%s)->第1跳(%s)", inNode.NodeName, firstNode.NodeName)
					workItems = append(workItems, diagnosisWorkItem{
						fromNodeID:   inNode.NodeID,
						toNode:       firstNode,
						hasChainHop:  true,
						ipPreference: ipPreference,
						description:  description,
						protocol:     defaultString(strings.ToLower(strings.TrimSpace(firstNode.Protocol)), protocol),
						metadata: map[string]interface{}{
							"fromChainType": 1,
							"toChainType":   2,
							"toInx":         firstNode.Inx,
						},
					})
				}
			} else {
				for _, outNode := range outNodes {
					description := fmt.Sprintf("入口(%s)->出口(%s)", inNode.NodeName, outNode.NodeName)
					workItems = append(workItems, diagnosisWorkItem{
						fromNodeID:   inNode.NodeID,
						toNode:       outNode,
						hasChainHop:  true,
						ipPreference: ipPreference,
						description:  description,
						protocol:     defaultString(strings.ToLower(strings.TrimSpace(outNode.Protocol)), protocol),
						metadata: map[string]interface{}{
							"fromChainType": 1,
							"toChainType":   3,
						},
					})
				}
			}
		}

		for i, hop := range chainHops {
			for _, currentNode := range hop {
				if i+1 < len(chainHops) {
					for _, nextNode := range chainHops[i+1] {
						description := fmt.Sprintf("第%d跳(%s)->第%d跳(%s)", i+1, currentNode.NodeName, i+2, nextNode.NodeName)
						workItems = append(workItems, diagnosisWorkItem{
							fromNodeID:   currentNode.NodeID,
							toNode:       nextNode,
							hasChainHop:  true,
							ipPreference: ipPreference,
							description:  description,
							protocol:     defaultString(strings.ToLower(strings.TrimSpace(nextNode.Protocol)), protocol),
							metadata: map[string]interface{}{
								"fromChainType": 2,
								"fromInx":       currentNode.Inx,
								"toChainType":   2,
								"toInx":         nextNode.Inx,
							},
						})
					}
				} else {
					for _, outNode := range outNodes {
						description := fmt.Sprintf("第%d跳(%s)->出口(%s)", i+1, currentNode.NodeName, outNode.NodeName)
						workItems = append(workItems, diagnosisWorkItem{
							fromNodeID:   currentNode.NodeID,
							toNode:       outNode,
							hasChainHop:  true,
							ipPreference: ipPreference,
							description:  description,
							protocol:     defaultString(strings.ToLower(strings.TrimSpace(outNode.Protocol)), protocol),
							metadata: map[string]interface{}{
								"fromChainType": 2,
								"fromInx":       currentNode.Inx,
								"toChainType":   3,
							},
						})
					}
				}
			}
		}

		for _, outNode := range outNodes {
			description := fmt.Sprintf("出口(%s)->外网", outNode.NodeName)
			workItems = append(workItems, diagnosisWorkItem{
				fromNodeID:  outNode.NodeID,
				targetIP:    probeTarget.Host,
				targetPort:  probeTarget.Port,
				description: description,
				protocol:    "tcp",
				metadata: map[string]interface{}{
					"fromChainType": 3,
				},
			})
		}
	default:
		for _, inNode := range inNodes {
			description := fmt.Sprintf("入口(%s)->外网", inNode.NodeName)
			workItems = append(workItems, diagnosisWorkItem{
				fromNodeID:  inNode.NodeID,
				targetIP:    probeTarget.Host,
				targetPort:  probeTarget.Port,
				description: description,
				protocol:    "tcp",
				metadata: map[string]interface{}{
					"fromChainType": 1,
				},
			})
		}
	}

	tunnelType := map[bool]string{true: "端口转发", false: "隧道转发"}[tunnel.Type == 1]
	return tunnelName, tunnelType, workItems, nil
}

func splitChainNodeGroups(rows []chainNodeRecord) ([]chainNodeRecord, [][]chainNodeRecord, []chainNodeRecord) {
	inNodes := make([]chainNodeRecord, 0)
	outNodes := make([]chainNodeRecord, 0)
	chainByInx := map[int64][]chainNodeRecord{}
	hopOrder := make([]int64, 0)

	for _, row := range rows {
		switch row.ChainType {
		case 1:
			inNodes = append(inNodes, row)
		case 2:
			if _, ok := chainByInx[row.Inx]; !ok {
				hopOrder = append(hopOrder, row.Inx)
			}
			chainByInx[row.Inx] = append(chainByInx[row.Inx], row)
		case 3:
			outNodes = append(outNodes, row)
		}
	}

	sort.Slice(hopOrder, func(i, j int) bool { return hopOrder[i] < hopOrder[j] })
	chainHops := make([][]chainNodeRecord, 0, len(hopOrder))
	for _, inx := range hopOrder {
		chainHops = append(chainHops, chainByInx[inx])
	}

	return inNodes, chainHops, outNodes
}

func resolveDiagnosisTargets(remoteAddr string) ([]diagnosisTarget, error) {
	rawTargets := splitRemoteTargets(remoteAddr)
	if len(rawTargets) == 0 {
		return nil, errors.New("目标地址不能为空")
	}

	targets := make([]diagnosisTarget, 0, len(rawTargets))
	for _, raw := range rawTargets {
		ip, port, err := parseTargetAddress(raw)
		if err != nil {
			continue
		}
		targets = append(targets, diagnosisTarget{Address: raw, IP: ip, Port: port})
	}
	if len(targets) == 0 {
		return nil, errors.New("目标地址格式错误")
	}
	return targets, nil
}

func diagnosisContextMessage(ctx context.Context) string {
	if ctx == nil {
		return diagnosisRequestTimeoutMsg
	}
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return diagnosisRequestTimeoutMsg
	case context.Canceled:
		return "诊断已取消"
	default:
		return diagnosisRequestTimeoutMsg
	}
}

func diagnosisExecOptionsFromContext(ctx context.Context) diagnosisExecOptions {
	timeout := diagnosisCommandTimeout
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				remaining = 100 * time.Millisecond
			}
			if remaining < timeout {
				timeout = remaining
			}
		}
	}
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	pingTimeoutMS := int(timeout / time.Millisecond)
	if pingTimeoutMS <= 0 {
		pingTimeoutMS = 100
	}
	return diagnosisExecOptions{
		commandTimeout: timeout,
		pingTimeoutMS:  pingTimeoutMS,
		timeoutMessage: diagnosisContextMessage(ctx),
	}
}

func newDiagnosisTimeoutItem(workItem diagnosisWorkItem, message string) map[string]interface{} {
	targetPort := workItem.targetPort
	if targetPort <= 0 {
		targetPort = workItem.toNode.Port
	}
	item := newDiagnosisResultItem(workItem.fromNodeID, workItem.targetIP, targetPort, workItem.description, workItem.metadata)
	item["success"] = false
	if strings.TrimSpace(message) == "" {
		message = diagnosisCommandTimeoutMsg
	}
	item["message"] = message
	return item
}

func (h *Handler) executeDiagnosisWorkItem(workItem diagnosisWorkItem, options diagnosisExecOptions) map[string]interface{} {
	single := make([]map[string]interface{}, 0, 1)
	nodeCache := map[int64]*nodeRecord{}
	if workItem.hasChainHop {
		h.appendChainHopDiagnosis(&single, nodeCache, workItem.fromNodeID, workItem.toNode, workItem.description, workItem.metadata, workItem.ipPreference, workItem.protocol, options)
	} else {
		h.appendPathDiagnosis(&single, nodeCache, workItem.fromNodeID, workItem.targetIP, workItem.targetPort, workItem.description, workItem.metadata, workItem.protocol, options)
	}

	if len(single) == 0 {
		return newDiagnosisTimeoutItem(workItem, "诊断任务未返回结果")
	}
	return single[0]
}

func (h *Handler) runDiagnosisWorkItems(ctx context.Context, workItems []diagnosisWorkItem, emitter diagnosisItemEmitter) []map[string]interface{} {
	if ctx == nil {
		ctx = context.Background()
	}
	results := make([]map[string]interface{}, len(workItems))
	if len(workItems) == 0 {
		return results
	}

	workerLimit := diagnosisMaxConcurrency
	if workerLimit < 1 {
		workerLimit = 1
	}
	if workerLimit > len(workItems) {
		workerLimit = len(workItems)
	}

	type diagnosisWorkResult struct {
		index int
		item  map[string]interface{}
	}

	jobs := make(chan int)
	resultCh := make(chan diagnosisWorkResult, len(workItems))

	var wg sync.WaitGroup
	for i := 0; i < workerLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				select {
				case <-ctx.Done():
					resultCh <- diagnosisWorkResult{index: index, item: newDiagnosisTimeoutItem(workItems[index], diagnosisContextMessage(ctx))}
					continue
				default:
				}
				options := diagnosisExecOptionsFromContext(ctx)
				resultCh <- diagnosisWorkResult{index: index, item: h.executeDiagnosisWorkItem(workItems[index], options)}
			}
		}()
	}

enqueueLoop:
	for i := 0; i < len(workItems); i++ {
		select {
		case <-ctx.Done():
			message := diagnosisContextMessage(ctx)
			for j := i; j < len(workItems); j++ {
				resultCh <- diagnosisWorkResult{index: j, item: newDiagnosisTimeoutItem(workItems[j], message)}
			}
			break enqueueLoop
		case jobs <- i:
		}
	}
	close(jobs)
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	progress := diagnosisProgress{Total: len(workItems)}
	for result := range resultCh {
		results[result.index] = result.item
		progress.Completed++
		if asBool(result.item["success"], false) {
			progress.Success++
		} else {
			progress.Failed++
		}
		if emitter != nil {
			emitter(result.index, result.item, progress)
		}
	}

	for i := range results {
		if results[i] == nil {
			results[i] = newDiagnosisTimeoutItem(workItems[i], diagnosisCommandTimeoutMsg)
		}
	}
	return results
}

func (h *Handler) cachedNode(nodeCache map[int64]*nodeRecord, nodeID int64) (*nodeRecord, error) {
	if node, ok := nodeCache[nodeID]; ok {
		return node, nil
	}
	node, err := h.getNodeRecord(nodeID)
	if err != nil {
		return nil, err
	}
	nodeCache[nodeID] = node
	return node, nil
}

func newDiagnosisResultItem(fromNodeID int64, targetIP string, targetPort int, description string, metadata map[string]interface{}) map[string]interface{} {
	item := map[string]interface{}{
		"nodeName":    fmt.Sprintf("node_%d", fromNodeID),
		"nodeId":      strconv.FormatInt(fromNodeID, 10),
		"targetIp":    targetIP,
		"targetPort":  targetPort,
		"description": description,
		"averageTime": 0,
		"packetLoss":  100,
	}
	for k, v := range metadata {
		item[k] = v
	}
	return item
}

func (h *Handler) appendFailedDiagnosis(results *[]map[string]interface{}, nodeCache map[int64]*nodeRecord, fromNodeID int64, targetIP string, targetPort int, description string, metadata map[string]interface{}, message string) {
	item := newDiagnosisResultItem(fromNodeID, targetIP, targetPort, description, metadata)
	if node, err := h.cachedNode(nodeCache, fromNodeID); err == nil {
		item["nodeName"] = node.Name
	}
	if strings.TrimSpace(message) == "" {
		message = "连接失败"
	}
	item["success"] = false
	item["message"] = message
	*results = append(*results, item)
}

func (h *Handler) appendPathDiagnosis(results *[]map[string]interface{}, nodeCache map[int64]*nodeRecord, fromNodeID int64, targetIP string, targetPort int, description string, metadata map[string]interface{}, protocol string, options diagnosisExecOptions) {
	item := newDiagnosisResultItem(fromNodeID, targetIP, targetPort, description, metadata)

	fromNode, err := h.cachedNode(nodeCache, fromNodeID)
	if err != nil {
		item["success"] = false
		item["message"] = err.Error()
		*results = append(*results, item)
		return
	}
	item["nodeName"] = fromNode.Name

	var (
		pingData map[string]interface{}
		pingErr  error
	)
	if fromNode.IsRemote == 1 {
		pingData, pingErr = h.pingViaRemoteNode(fromNode, targetIP, targetPort, protocol, options)
	} else {
		pingData, pingErr = h.pingViaNode(fromNodeID, targetIP, targetPort, protocol, options)
	}
	if pingErr != nil {
		item["success"] = false
		item["message"] = pingErr.Error()
		*results = append(*results, item)
		return
	}

	success := asBool(pingData["success"], false)
	item["success"] = success
	item["averageTime"] = asFloat(pingData["averageTime"], 0)
	item["packetLoss"] = asFloat(pingData["packetLoss"], 100)

	message := strings.TrimSpace(asString(pingData["message"]))
	if success {
		if message == "" {
			message = "连接成功"
		}
	} else {
		if message == "" {
			message = strings.TrimSpace(asString(pingData["errorMessage"]))
		}
		if message == "" {
			message = "连接失败"
		}
	}
	item["message"] = message
	*results = append(*results, item)
}

func (h *Handler) appendChainHopDiagnosis(results *[]map[string]interface{}, nodeCache map[int64]*nodeRecord, fromNodeID int64, toNode chainNodeRecord, description string, metadata map[string]interface{}, ipPreference string, protocol string, options diagnosisExecOptions) {
	fromNode, _ := h.cachedNode(nodeCache, fromNodeID)
	targetNode, err := h.cachedNode(nodeCache, toNode.NodeID)
	if err != nil {
		h.appendFailedDiagnosis(results, nodeCache, fromNodeID, "", 0, description, metadata, err.Error())
		return
	}
	targetIP, targetPort, err := resolveChainProbeTarget(fromNode, targetNode, toNode.Port, ipPreference, toNode.ConnectIP)
	if err != nil {
		h.appendFailedDiagnosis(results, nodeCache, fromNodeID, strings.Trim(strings.TrimSpace(targetNode.ServerIP), "[]"), toNode.Port, description, metadata, err.Error())
		return
	}
	h.appendPathDiagnosis(results, nodeCache, fromNodeID, targetIP, targetPort, description, metadata, protocol, options)
}

func resolveChainProbeTarget(fromNode, targetNode *nodeRecord, preferredPort int, ipPreference string, connectIp string) (string, int, error) {
	if targetNode == nil {
		return "", 0, errors.New("目标服务器不存在")
	}
	host, err := selectTunnelDialHost(fromNode, targetNode, ipPreference, connectIp)
	if err != nil {
		host = strings.Trim(strings.TrimSpace(targetNode.ServerIP), "[]")
	}
	if host == "" {
		return "", 0, errors.New("目标服务器地址为空")
	}
	port := preferredPort
	if port <= 0 {
		port = firstPortFromRange(targetNode.PortRange)
	}
	if port <= 0 {
		port = 443
	}
	return host, port, nil
}

func firstPortFromRange(portRange string) int {
	portRange = strings.TrimSpace(portRange)
	if portRange == "" {
		return 0
	}
	first := strings.Split(portRange, ",")[0]
	first = strings.TrimSpace(first)
	if strings.Contains(first, "-") {
		parts := strings.SplitN(first, "-", 2)
		if len(parts) != 2 {
			return 0
		}
		p, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || p <= 0 {
			return 0
		}
		return p
	}
	p, err := strconv.Atoi(first)
	if err != nil || p <= 0 {
		return 0
	}
	return p
}

func (h *Handler) listChainNodesForTunnel(tunnelID int64) ([]chainNodeRecord, error) {
	return h.repo.ListChainNodesForTunnel(tunnelID)
}

func (h *Handler) tcpPingViaNode(nodeID int64, ip string, port int, options diagnosisExecOptions) (map[string]interface{}, error) {
	if options.commandTimeout <= 0 {
		options.commandTimeout = diagnosisCommandTimeout
	}
	if options.pingTimeoutMS <= 0 {
		options.pingTimeoutMS = int(diagnosisCommandTimeout / time.Millisecond)
	}
	pingCount := options.pingCount
	if pingCount <= 0 {
		pingCount = 4
	}
	res, err := h.sendTCPPing(nodeID, map[string]interface{}{
		"ip":      ip,
		"port":    port,
		"count":   pingCount,
		"timeout": options.pingTimeoutMS,
	}, options.commandTimeout)
	if err != nil {
		return nil, err
	}
	if res.Data == nil {
		return nil, errors.New("服务器未返回诊断数据")
	}
	return res.Data, nil
}

func (h *Handler) tcpPingViaRemoteNode(node *nodeRecord, ip string, port int, options diagnosisExecOptions) (map[string]interface{}, error) {
	if node == nil {
		return nil, errors.New("服务器不存在")
	}
	remoteURL := strings.TrimSpace(node.RemoteURL)
	remoteToken := strings.TrimSpace(node.RemoteToken)
	if remoteURL == "" || remoteToken == "" {
		return nil, errors.New("远程服务器缺少共享配置")
	}
	if options.commandTimeout <= 0 {
		options.commandTimeout = diagnosisCommandTimeout
	}
	if options.pingTimeoutMS <= 0 {
		options.pingTimeoutMS = int(diagnosisCommandTimeout / time.Millisecond)
	}
	pingCount := options.pingCount
	if pingCount <= 0 {
		pingCount = 4
	}

	fc := client.NewFederationClientWithTimeout(options.commandTimeout)
	return fc.Diagnose(remoteURL, remoteToken, h.federationLocalDomain(), client.RuntimeDiagnoseRequest{
		IP:       strings.TrimSpace(ip),
		Port:     port,
		Count:    pingCount,
		Timeout:  options.pingTimeoutMS,
		Protocol: "tcp",
	})
}

func (h *Handler) udpPingViaRemoteNode(node *nodeRecord, ip string, port int, options diagnosisExecOptions) (map[string]interface{}, error) {
	if node == nil {
		return nil, errors.New("服务器不存在")
	}
	remoteURL := strings.TrimSpace(node.RemoteURL)
	remoteToken := strings.TrimSpace(node.RemoteToken)
	if remoteURL == "" || remoteToken == "" {
		return nil, errors.New("远程服务器缺少共享配置")
	}
	if options.commandTimeout <= 0 {
		options.commandTimeout = diagnosisCommandTimeout
	}
	if options.pingTimeoutMS <= 0 {
		options.pingTimeoutMS = int(diagnosisCommandTimeout / time.Millisecond)
	}

	fc := client.NewFederationClientWithTimeout(options.commandTimeout)
	return fc.Diagnose(remoteURL, remoteToken, h.federationLocalDomain(), client.RuntimeDiagnoseRequest{
		IP:       strings.TrimSpace(ip),
		Port:     port,
		Count:    4,
		Timeout:  options.pingTimeoutMS,
		Protocol: "udp",
	})
}

func isUDPBasedProtocol(protocol string) bool {
	p := strings.ToLower(strings.TrimSpace(protocol))
	return p == "kcp" || p == "udp" || p == "quic"
}

func (h *Handler) pingViaNode(nodeID int64, ip string, port int, protocol string, options diagnosisExecOptions) (map[string]interface{}, error) {
	if isUDPBasedProtocol(protocol) {
		return h.udpPingViaNode(nodeID, ip, port, options)
	}
	return h.tcpPingViaNode(nodeID, ip, port, options)
}

func (h *Handler) pingViaRemoteNode(node *nodeRecord, ip string, port int, protocol string, options diagnosisExecOptions) (map[string]interface{}, error) {
	if isUDPBasedProtocol(protocol) {
		return h.udpPingViaRemoteNode(node, ip, port, options)
	}
	return h.tcpPingViaRemoteNode(node, ip, port, options)
}

func (h *Handler) udpPingViaNode(nodeID int64, ip string, port int, options diagnosisExecOptions) (map[string]interface{}, error) {
	if options.commandTimeout <= 0 {
		options.commandTimeout = diagnosisCommandTimeout
	}
	if options.pingTimeoutMS <= 0 {
		options.pingTimeoutMS = int(diagnosisCommandTimeout / time.Millisecond)
	}
	res, err := h.sendNodeCommandWithTimeout(nodeID, "UdpPing", map[string]interface{}{
		"ip":      ip,
		"port":    port,
		"count":   4,
		"timeout": options.pingTimeoutMS,
	}, options.commandTimeout, false, false)
	if err != nil {
		return nil, err
	}
	if res.Data == nil {
		return nil, errors.New("服务器未返回诊断数据")
	}
	return res.Data, nil
}

func splitRemoteTargets(remoteAddr string) []string {
	parts := strings.Split(remoteAddr, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, processServerAddress(part))
	}
	return out
}

func parseTargetAddress(addr string) (string, int, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", 0, errors.New("empty address")
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		idx := strings.LastIndex(addr, ":")
		if idx <= 0 || idx >= len(addr)-1 {
			return "", 0, err
		}
		host = strings.TrimSpace(addr[:idx])
		portStr = strings.TrimSpace(addr[idx+1:])
	}
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, errors.New("invalid port")
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return "", 0, errors.New("invalid host")
	}
	return host, port, nil
}
