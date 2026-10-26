package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

// External inbounds are proxy nodes the panel does not deploy (built by a
// script, or on a machine without the agent). They are stored as inbounds
// without a kernel or node; Listen holds the node's public address so relays
// and subscriptions treat them like any other inbound.

// maxImportNodes bounds one import; subscriptions rarely carry more.
const maxImportNodes = 500

// maxSubscriptionBytes bounds a fetched subscription body.
const maxSubscriptionBytes = 5 << 20

func isExternalInbound(row *model.NodeInbound) bool {
	return row != nil && row.KernelID == 0
}

// importedProxy returns the stored Mihomo-schema proxy of an imported node,
// or nil for nodes imported before the proxy format (plain ss inbounds).
func importedProxy(row *model.NodeInbound) kernel.Proxy {
	if !isExternalInbound(row) || strings.TrimSpace(row.Extra) == "" {
		return nil
	}
	var p kernel.Proxy
	if err := json.Unmarshal([]byte(row.Extra), &p); err != nil || len(p) == 0 {
		return nil
	}
	return p
}

type externalImportResult struct {
	Created []int64  `json:"created"`
	Errors  []string `json:"errors"`
}

// inboundImport creates imported nodes from share links, a base64 / Clash /
// sing-box / Surge subscription body, or a subscription URL to fetch.
func (h *Handler) inboundImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		Links     string `json:"links"`
		RelayOnly bool   `json:"relayOnly"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	text := strings.TrimSpace(req.Links)
	if text == "" {
		response.WriteJSON(w, response.ErrDefault("请粘贴分享链接、订阅内容或订阅地址"))
		return
	}
	if isSubscriptionURL(text) {
		body, err := h.fetchSubscription(r.Context(), text)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault("获取订阅失败："+err.Error()))
			return
		}
		text = body
	}
	nodes, parseErrs := kernel.ParseImport(text)
	if len(nodes) > maxImportNodes {
		response.WriteJSON(w, response.ErrDefault(fmt.Sprintf("一次最多导入 %d 个节点", maxImportNodes)))
		return
	}

	existing, err := h.repo.ListNodeInbounds()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	seen := make(map[string]bool)
	for _, row := range existing {
		if isExternalInbound(&row) {
			seen[externalKey(row.Listen, row.Port)] = true
		}
	}

	res := externalImportResult{Created: []int64{}, Errors: append([]string{}, parseErrs...)}
	now := time.Now().UnixMilli()
	for _, node := range nodes {
		key := externalKey(node.Host, node.Port)
		if seen[key] {
			res.Errors = append(res.Errors, fmt.Sprintf("%s：%s 已经导入过", node.Name, key))
			continue
		}
		seen[key] = true
		row, err := externalRow(node, req.RelayOnly, now)
		if err == nil {
			err = h.repo.CreateNodeInbound(&row)
		}
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s：%v", node.Name, err))
			continue
		}
		res.Created = append(res.Created, row.ID)
	}
	if len(res.Created) == 0 {
		msg := "没有导入任何节点"
		if len(res.Errors) > 0 {
			msg += "：" + res.Errors[0]
		}
		response.WriteJSON(w, response.R{Code: -1, Msg: msg, Data: res, TS: now})
		return
	}
	response.WriteJSON(w, response.OK(res))
}

func externalRow(node kernel.ImportedNode, relayOnly bool, now int64) (model.NodeInbound, error) {
	raw, err := json.Marshal(node.Proxy)
	if err != nil {
		return model.NodeInbound{}, err
	}
	return model.NodeInbound{
		Name:        truncateRunes(node.Name, 100),
		Protocol:    kernel.ProxyProtocol(node.Proxy),
		Listen:      node.Host,
		Port:        node.Port,
		ShareHost:   node.Host,
		Extra:       string(raw),
		RelayOnly:   boolToInt(relayOnly),
		Enabled:     1,
		CreatedTime: now,
		UpdatedTime: now,
	}, nil
}

func isSubscriptionURL(text string) bool {
	if strings.ContainsAny(text, " \n\t") {
		return false
	}
	u, err := url.Parse(text)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// fetchSubscription downloads a subscription. The Mihomo user agent makes
// most providers return a Clash profile, which carries every parameter.
func (h *Handler) fetchSubscription(ctx context.Context, rawURL string) (string, error) {
	body, _, err := h.fetchSubscriptionInfo(ctx, rawURL, "")
	return body, err
}

// defaultFetchUserAgent is the Mihomo user agent downloads send unless a
// source sets its own.
const defaultFetchUserAgent = "clash.meta/1.19.0"

// fetchSubscriptionInfo is fetchSubscription plus the provider's
// Subscription-Userinfo header (plan traffic and expiry), sent with
// userAgent ("" for the default). It may not reach internal addresses
// unless allowed (see fetchClient).
func (h *Handler) fetchSubscriptionInfo(ctx context.Context, rawURL, userAgent string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	if userAgent == "" {
		userAgent = defaultFetchUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := fetchClient(h.fetchAllowLocal(), 0).Do(req)
	if err != nil {
		return "", "", fetchErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(body) > maxSubscriptionBytes {
		return "", "", errors.New("订阅内容超过 5MB")
	}
	return string(body), truncate(strings.TrimSpace(resp.Header.Get("Subscription-Userinfo")), 255), nil
}

// inboundExternalUpdate edits an imported node: its name, address and
// options, and optionally replaces its parameters with a new share link.
func (h *Handler) inboundExternalUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		Method    string `json:"method"`
		Password  string `json:"password"`
		Link      string `json:"link"`
		Remark    string `json:"remark"`
		RelayOnly bool   `json:"relayOnly"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	row, err := h.repo.GetNodeInbound(req.ID)
	if err != nil || row == nil || !isExternalInbound(row) {
		response.WriteJSON(w, response.ErrDefault("导入的节点不存在"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 100 {
		response.WriteJSON(w, response.ErrDefault("名称不能为空，且不超过 100 字"))
		return
	}
	host := strings.Trim(strings.TrimSpace(req.Host), "[]")
	if err := kernel.ValidateImportHost(host); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		response.WriteJSON(w, response.ErrDefault("端口不正确"))
		return
	}

	proxy := importedProxy(row)
	if link := strings.TrimSpace(req.Link); link != "" {
		nodes, errs := kernel.ParseImport(link)
		if len(nodes) != 1 {
			msg := "请只粘贴一个节点"
			if len(nodes) == 0 && len(errs) > 0 {
				msg = errs[0]
			}
			response.WriteJSON(w, response.ErrDefault(msg))
			return
		}
		proxy = nodes[0].Proxy
		// The address fields follow the new link unless the user changed them.
		if host == row.Listen && req.Port == row.Port {
			host, req.Port = nodes[0].Host, nodes[0].Port
		}
	}
	if proxy != nil {
		proxy["server"], proxy["port"] = host, req.Port
		raw, err := json.Marshal(proxy)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		row.Extra = string(raw)
		row.Protocol = kernel.ProxyProtocol(proxy)
		row.Listen, row.Port = host, req.Port
	} else {
		// Nodes imported before the proxy format: plain Shadowsocks.
		method := strings.ToLower(strings.TrimSpace(req.Method))
		if !containsMethod(method) {
			response.WriteJSON(w, response.ErrDefault("不支持的加密方式 "+method))
			return
		}
		if req.Password == "" {
			response.WriteJSON(w, response.ErrDefault("密码不能为空"))
			return
		}
		in, err := inboundFromModel(*row)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		in.Listen, in.Port = host, req.Port
		in.Settings.Method, in.Settings.Password = method, req.Password
		if err := applyInboundToModel(in, row); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	// Remember the source's name before a rename so refreshes keep the new one.
	if row.SourceID != 0 && row.ImportedName == "" {
		row.ImportedName = row.Name
	}
	row.Name = name
	row.ShareHost = host
	row.Remark = strings.TrimSpace(req.Remark)
	row.RelayOnly = boolToInt(req.RelayOnly)
	row.UpdatedTime = time.Now().UnixMilli()
	if err := h.repo.SaveNodeInbound(row); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	// Relay forwards dial the stored address; point them at the new one.
	if problems := h.retargetRelays(row); len(problems) > 0 {
		response.WriteJSON(w, response.R{Code: 0, Msg: "已保存，但部分中转更新失败: " + strings.Join(problems, "；"), TS: row.UpdatedTime})
		return
	}
	// So do kernels chained through the node.
	if warnings := h.applyChainReferrers(row.ID); len(warnings) > 0 {
		response.WriteJSON(w, response.R{Code: 0, Msg: "已保存，但" + strings.Join(warnings, "；"), TS: row.UpdatedTime})
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func externalKey(host string, port int) string {
	return net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port))
}

func containsMethod(method string) bool {
	for _, m := range kernel.SSMethods() {
		if m == method {
			return true
		}
	}
	return false
}

// retargetRelays updates the remote address of every managed relay forward
// of an inbound after its address changed.
func (h *Handler) retargetRelays(row *model.NodeInbound) []string {
	relays, err := h.repo.ListInboundRelays(row.ID)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, rel := range relays {
		if !rel.ForwardExists || rel.Managed != 1 {
			continue
		}
		fwd, err := h.getForwardRecord(rel.ForwardID)
		if err != nil || fwd == nil {
			continue
		}
		tunnel, err := h.getTunnelRecord(fwd.TunnelID)
		if err != nil || tunnel == nil {
			continue
		}
		target, err := h.relayTarget(row, tunnel)
		if err != nil {
			problems = append(problems, rel.Name+": "+err.Error())
			continue
		}
		if target == fwd.RemoteAddr {
			continue
		}
		if err := h.repo.UpdateForward(fwd.ID, fwd.Name, fwd.TunnelID, target, fwd.Strategy, time.Now().UnixMilli(),
			fwd.SpeedID, fwd.MaxConn, fwd.IPMaxConn, fwd.IPSpeedID, 0, fwd.ProxyProtocolReceive, fwd.ProxyProtocolSend); err != nil {
			problems = append(problems, rel.Name+": "+err.Error())
			continue
		}
		updated, err := h.getForwardRecord(fwd.ID)
		if err != nil || updated == nil {
			continue
		}
		if err := h.syncForwardServices(updated, "UpdateService", true); err != nil {
			problems = append(problems, rel.Name+": "+err.Error())
		}
	}
	return problems
}

func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
