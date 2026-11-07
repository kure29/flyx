package repo

import (
	"database/sql"
	"errors"
	"net"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"go-backend/internal/store/model"
)

func (r *Repository) ListForwards() ([]map[string]interface{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}

	type fwdRow struct {
		ID                   int64
		UserID               int64
		UserName             string
		Name                 string
		TunnelID             int64
		TunnelName           string
		TrafficRatio         float64
		RemoteAddr           string
		Strategy             string
		InFlow               int64
		OutFlow              int64
		CreatedTime          int64
		Status               int
		Inx                  int
		SpeedID              sql.NullInt64
		MaxConn              int
		IPMaxConn            int
		IPSpeedID            sql.NullInt64
		IPSpeedLimitName     string
		ProxyProtocol        int
		ProxyProtocolReceive int
		ProxyProtocolSend    int
	}

	var rows []fwdRow
	err := r.db.Model(&model.Forward{}).
		Select("forward.id, forward.user_id, forward.user_name, forward.name, forward.tunnel_id, COALESCE(tunnel.name, '') AS tunnel_name, COALESCE(tunnel.traffic_ratio, 1.0) AS traffic_ratio, forward.remote_addr, COALESCE(forward.strategy, 'fifo') AS strategy, forward.in_flow, forward.out_flow, forward.created_time, forward.status, forward.inx, forward.speed_id, forward.max_conn, forward.ip_max_conn, forward.ip_speed_id, COALESCE(ip_speed_limit.name, '') AS ip_speed_limit_name, forward.proxy_protocol, forward.proxy_protocol_receive, forward.proxy_protocol_send").
		Joins("LEFT JOIN tunnel ON tunnel.id = forward.tunnel_id").
		Joins("LEFT JOIN speed_limit AS ip_speed_limit ON ip_speed_limit.id = forward.ip_speed_id").
		Order("forward.inx ASC, forward.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		proxyProtocolReceive, proxyProtocolSend := normalizeForwardProxyProtocol(row.ProxyProtocol, row.ProxyProtocolReceive, row.ProxyProtocolSend)
		inIP, inPort, err := resolveForwardIngress(r.db, row.ID, row.TunnelID)
		if err != nil {
			return nil, err
		}
		item := map[string]interface{}{
			"id": row.ID, "userId": row.UserID, "userName": row.UserName,
			"name": row.Name, "tunnelId": row.TunnelID, "tunnelName": row.TunnelName,
			"tunnelTrafficRatio": row.TrafficRatio,
			"inIp":               nullableForwardIngress(inIP), "inPort": nullableInt64(inPort),
			"remoteAddr": row.RemoteAddr, "strategy": row.Strategy,
			"inFlow": row.InFlow, "outFlow": row.OutFlow,
			"createdTime": row.CreatedTime, "status": row.Status, "inx": int64(row.Inx),
			"maxConn":              row.MaxConn,
			"ipMaxConn":            row.IPMaxConn,
			"proxyProtocol":        row.ProxyProtocol,
			"proxyProtocolReceive": proxyProtocolReceive,
			"proxyProtocolSend":    proxyProtocolSend,
		}
		if row.SpeedID.Valid {
			item["speedId"] = row.SpeedID.Int64
		}
		if row.IPSpeedID.Valid {
			item["ipSpeedId"] = row.IPSpeedID.Int64
		}
		if strings.TrimSpace(row.IPSpeedLimitName) != "" {
			item["ipSpeedLimitName"] = row.IPSpeedLimitName
		}
		items = append(items, item)
	}
	return items, nil
}

// ─── Helper Functions ────────────────────────────────────────────────

func resolveForwardIngress(db *gorm.DB, forwardID int64, tunnelID int64) (string, sql.NullInt64, error) {
	var tunnelInIP sql.NullString
	db.Model(&model.Tunnel{}).Select("in_ip").Where("id = ?", tunnelID).Limit(1).Scan(&tunnelInIP)

	type fpRow struct {
		Port     sql.NullInt64
		ServerIP sql.NullString
		InIP     sql.NullString
	}
	var fpRows []fpRow
	err := db.Model(&model.ForwardPort{}).
		Select("forward_port.port, node.server_ip, forward_port.in_ip").
		Joins("LEFT JOIN node ON node.id = forward_port.node_id").
		Where("forward_port.forward_id = ?", forwardID).
		Order("forward_port.id ASC").
		Find(&fpRows).Error
	if err != nil {
		return "", sql.NullInt64{}, err
	}

	ports := make([]int64, 0)
	entries := make([]string, 0)
	seenPorts := make(map[int64]struct{})
	seenPairs := make(map[string]struct{})

	for _, row := range fpRows {
		if !row.Port.Valid {
			continue
		}
		if _, ok := seenPorts[row.Port.Int64]; !ok {
			seenPorts[row.Port.Int64] = struct{}{}
			ports = append(ports, row.Port.Int64)
		}

		var ip string
		if row.InIP.Valid && strings.TrimSpace(row.InIP.String) != "" {
			ip = strings.TrimSpace(row.InIP.String)
		} else if row.ServerIP.Valid && strings.TrimSpace(row.ServerIP.String) != "" {
			ip = strings.TrimSpace(row.ServerIP.String)
		}

		if ip != "" {
			pair := formatForwardIngressAddress(ip, row.Port.Int64)
			if _, ok := seenPairs[pair]; !ok {
				seenPairs[pair] = struct{}{}
				entries = append(entries, pair)
			}
		}
	}

	if len(ports) == 0 {
		return "", sql.NullInt64{}, nil
	}

	inPort := sql.NullInt64{Int64: ports[0], Valid: true}

	return strings.Join(entries, ","), inPort, nil
}

func formatForwardIngressAddress(host string, port int64) string {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 {
		return ""
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	return net.JoinHostPort(host, strconv.FormatInt(port, 10))
}

func nullableForwardIngress(v string) interface{} {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return v
}
