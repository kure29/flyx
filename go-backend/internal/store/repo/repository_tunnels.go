package repo

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"go-backend/internal/store/model"
)

// TunnelListenOption is what a forward on a tunnel may listen on: the
// addresses of its entry servers (or the tunnel's own entry address when they
// have none), as the admin forward form offers them.
type TunnelListenOption struct {
	EntryCount int
	IPs        []string
}

// TunnelListenOptions returns the listen choices of the given tunnels.
func (r *Repository) TunnelListenOptions(tunnelIDs []int64) (map[int64]TunnelListenOption, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	out := make(map[int64]TunnelListenOption, len(tunnelIDs))
	if len(tunnelIDs) == 0 {
		return out, nil
	}
	type entry struct {
		TunnelID int64
		NodeID   int64
	}
	var entries []entry
	if err := r.db.Model(&model.ChainTunnel{}).Select("tunnel_id, node_id").
		Where("tunnel_id IN ? AND chain_type = ?", tunnelIDs, "1").
		Order("inx ASC, id ASC").Find(&entries).Error; err != nil {
		return nil, err
	}
	nodeIDs := make([]int64, 0, len(entries))
	for _, e := range entries {
		nodeIDs = append(nodeIDs, e.NodeID)
	}
	nodes := map[int64]model.Node{}
	if len(nodeIDs) > 0 {
		var rows []model.Node
		if err := r.db.Select("id, server_ip, server_ip_v4, server_ip_v6, extra_ips").
			Where("id IN ?", nodeIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, n := range rows {
			nodes[n.ID] = n
		}
	}
	type tunnelRow struct {
		ID   int64
		InIP *string
	}
	var tunnels []tunnelRow
	if err := r.db.Model(&model.Tunnel{}).Select("id, in_ip").Where("id IN ?", tunnelIDs).Find(&tunnels).Error; err != nil {
		return nil, err
	}
	tunnelInIP := map[int64]string{}
	for _, t := range tunnels {
		if t.InIP != nil {
			tunnelInIP[t.ID] = *t.InIP
		}
	}

	collect := func(dst *[]string, seen map[string]bool, values ...string) {
		for _, v := range values {
			for _, part := range strings.Split(v, ",") {
				if p := strings.TrimSpace(part); p != "" && !seen[p] {
					seen[p] = true
					*dst = append(*dst, p)
				}
			}
		}
	}
	seen := map[int64]map[string]bool{}
	for _, id := range tunnelIDs {
		out[id] = TunnelListenOption{IPs: []string{}}
		seen[id] = map[string]bool{}
	}
	for _, e := range entries {
		opt := out[e.TunnelID]
		opt.EntryCount++
		if n, ok := nodes[e.NodeID]; ok {
			collect(&opt.IPs, seen[e.TunnelID], n.ServerIPV4.String, n.ServerIPV6.String, n.ServerIP, n.ExtraIPs.String)
		}
		out[e.TunnelID] = opt
	}
	// Only when the entry servers have no address: the tunnel's own entry
	// address may be a NAT public address or a domain no server can bind.
	for id, opt := range out {
		if len(opt.IPs) == 0 {
			collect(&opt.IPs, seen[id], tunnelInIP[id])
			out[id] = opt
		}
	}
	return out, nil
}

func (r *Repository) ListEnabledTunnelSummaries() ([]map[string]interface{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}

	type row struct {
		ID   int64
		Name string
	}
	var rows []row
	err := r.db.Model(&model.Tunnel{}).Select("id, name").Where("status = 1").Order("inx ASC, id ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}

	tunnelIDs := make([]int64, 0, len(rows))
	for _, rw := range rows {
		tunnelIDs = append(tunnelIDs, rw.ID)
	}
	portRangeMap := r.getTunnelEntryPortRanges(tunnelIDs)

	items := make([]map[string]interface{}, 0, len(rows))
	for _, rw := range rows {
		item := map[string]interface{}{"id": rw.ID, "name": rw.Name}
		if pr, ok := portRangeMap[rw.ID]; ok {
			item["portRangeMin"] = pr.min
			item["portRangeMax"] = pr.max
		}
		items = append(items, item)
	}
	return items, nil
}

type tunnelPortRange struct {
	min int
	max int
}

func (r *Repository) getTunnelEntryPortRanges(tunnelIDs []int64) map[int64]tunnelPortRange {
	result := make(map[int64]tunnelPortRange)
	if len(tunnelIDs) == 0 {
		return result
	}

	type entryNode struct {
		TunnelID int64
		NodeID   int64
	}
	var entries []entryNode
	r.db.Model(&model.ChainTunnel{}).
		Select("tunnel_id, node_id").
		Where("tunnel_id IN (?) AND chain_type = ?", tunnelIDs, "1").
		Find(&entries)

	nodeIDs := make([]int64, 0, len(entries))
	nodeSet := make(map[int64]struct{})
	for _, e := range entries {
		if _, exists := nodeSet[e.NodeID]; !exists {
			nodeSet[e.NodeID] = struct{}{}
			nodeIDs = append(nodeIDs, e.NodeID)
		}
	}

	type nodePort struct {
		ID   int64
		Port string
	}
	var nodePorts []nodePort
	if len(nodeIDs) > 0 {
		r.db.Model(&model.Node{}).Select("id, port").Where("id IN (?)", nodeIDs).Find(&nodePorts)
	}

	nodePortMap := make(map[int64]string)
	for _, np := range nodePorts {
		nodePortMap[np.ID] = np.Port
	}

	for _, e := range entries {
		portSpec := nodePortMap[e.NodeID]
		if portSpec == "" {
			continue
		}
		minP, maxP := parsePortRangeMinMax(portSpec)
		if minP <= 0 || maxP <= 0 {
			continue
		}
		pr, exists := result[e.TunnelID]
		if !exists {
			result[e.TunnelID] = tunnelPortRange{min: minP, max: maxP}
		} else {
			if minP < pr.min {
				pr.min = minP
			}
			if maxP > pr.max {
				pr.max = maxP
			}
			result[e.TunnelID] = pr
		}
	}
	return result
}

func parsePortRangeMinMax(input string) (int, int) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, 0
	}
	minPort, maxPort := 0, 0
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			r := strings.SplitN(part, "-", 2)
			if len(r) != 2 {
				continue
			}
			start, end := parseIntPort(r[0]), parseIntPort(r[1])
			if start <= 0 || end <= 0 {
				continue
			}
			if end < start {
				start, end = end, start
			}
			if minPort == 0 || start < minPort {
				minPort = start
			}
			if maxPort == 0 || end > maxPort {
				maxPort = end
			}
			continue
		}
		p := parseIntPort(part)
		if p <= 0 {
			continue
		}
		if minPort == 0 || p < minPort {
			minPort = p
		}
		if maxPort == 0 || p > maxPort {
			maxPort = p
		}
	}
	return minPort, maxPort
}

func parseIntPort(s string) int {
	var p int
	fmt.Sscanf(strings.TrimSpace(s), "%d", &p)
	return p
}

func (r *Repository) ListTunnels() ([]map[string]interface{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}

	var tunnels []model.Tunnel
	if err := r.db.Order("inx ASC, id ASC").Find(&tunnels).Error; err != nil {
		return nil, err
	}

	tunnelMap := make(map[int64]map[string]interface{})
	orderedIDs := make([]int64, 0, len(tunnels))

	for _, t := range tunnels {
		tunnelMap[t.ID] = map[string]interface{}{
			"id": t.ID, "inx": t.Inx, "name": t.Name,
			"type": t.Type, "flow": t.Flow, "trafficRatio": t.TrafficRatio,
			"status": t.Status, "createdTime": t.CreatedTime,
			"inIp":            nullableString(t.InIP),
			"ipPreference":    t.IPPreference,
			"probeTargetHost": t.ProbeTargetHost,
			"probeTargetPort": t.ProbeTargetPort,
			"engine":          t.Engine,
			"inNodeId":        make([]map[string]interface{}, 0),
			"outNodeId":       make([]map[string]interface{}, 0),
			"chainNodes":      make([][]map[string]interface{}, 0),
		}
		orderedIDs = append(orderedIDs, t.ID)
	}

	// Build node IP map
	nodeIPMap := map[int64]string{}
	var nodeList []model.Node
	if err := r.db.Select("id, server_ip").Find(&nodeList).Error; err == nil {
		for _, n := range nodeList {
			nodeIPMap[n.ID] = n.ServerIP
		}
	}

	// Load chain tunnels
	var chains []model.ChainTunnel
	if err := r.db.Order("tunnel_id ASC, chain_type ASC, inx ASC, id ASC").Find(&chains).Error; err != nil {
		return nil, err
	}

	chainBucket := map[int64]map[int][]map[string]interface{}{}
	inNodeIPs := map[int64][]string{}

	for _, c := range chains {
		t, ok := tunnelMap[c.TunnelID]
		if !ok {
			continue
		}

		chainTypeInt := 0
		fmt.Sscanf(c.ChainType, "%d", &chainTypeInt)

		inx := int64(0)
		if c.Inx.Valid {
			inx = c.Inx.Int64
		}

		nodeObj := map[string]interface{}{
			"nodeId":    c.NodeID,
			"chainType": chainTypeInt,
			"inx":       inx,
		}
		if c.Protocol.Valid {
			nodeObj["protocol"] = c.Protocol.String
		}
		if c.Strategy.Valid {
			nodeObj["strategy"] = c.Strategy.String
		}
		if c.ConnectIP.Valid {
			nodeObj["connectIp"] = c.ConnectIP.String
		}

		switch chainTypeInt {
		case 1:
			t["inNodeId"] = append(t["inNodeId"].([]map[string]interface{}), nodeObj)
			if ip, ok := nodeIPMap[c.NodeID]; ok && ip != "" {
				inNodeIPs[c.TunnelID] = append(inNodeIPs[c.TunnelID], ip)
			}
		case 2:
			if _, ok := chainBucket[c.TunnelID]; !ok {
				chainBucket[c.TunnelID] = map[int][]map[string]interface{}{}
			}
			chainBucket[c.TunnelID][int(inx)] = append(chainBucket[c.TunnelID][int(inx)], nodeObj)
		case 3:
			t["outNodeId"] = append(t["outNodeId"].([]map[string]interface{}), nodeObj)
		}
	}

	for tunnelID, groups := range chainBucket {
		t := tunnelMap[tunnelID]
		if t == nil {
			continue
		}
		keys := make([]int, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		ordered := make([][]map[string]interface{}, 0, len(keys))
		for _, k := range keys {
			ordered = append(ordered, groups[k])
		}
		t["chainNodes"] = ordered

		if s, ok := t["inIp"].(string); !ok || strings.TrimSpace(s) == "" {
			if ips := inNodeIPs[tunnelID]; len(ips) > 0 {
				t["inIp"] = strings.Join(ips, ",")
			}
		}
	}

	result := make([]map[string]interface{}, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if t, ok := tunnelMap[id]; ok {
			result = append(result, t)
		}
	}
	return result, nil
}
