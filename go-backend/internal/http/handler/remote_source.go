package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

// Remote sources are subscription URLs, or uploaded subscription files,
// whose nodes the panel keeps as imported nodes (SourceID set). A refresh
// (or a new upload) updates them in place, so relays and subscriptions
// that use them keep working.

var remoteSourceCheckInterval = 10 * time.Minute

// remoteSourceMu serializes refreshes (the loop and the refresh button).
var remoteSourceMu sync.Mutex

type remoteSourceView struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	URL           string `json:"url"`
	Kind          string `json:"kind"`
	Role          string `json:"role"`
	IntervalHours int    `json:"intervalHours"`
	RelayOnly     bool   `json:"relayOnly"`
	Enabled       bool   `json:"enabled"`
	LastFetch     int64  `json:"lastFetch"`
	LastError     string `json:"lastError"`
	NodeCount     int    `json:"nodeCount"`
	Userinfo      string `json:"userinfo"`
	UserAgent     string `json:"userAgent"`
}

func remoteSourceViewOf(s *model.RemoteSource) remoteSourceView {
	return remoteSourceView{
		ID: s.ID, Name: s.Name, URL: s.URL, Kind: sourceKind(s), Role: s.Role, IntervalHours: s.IntervalHours, RelayOnly: s.RelayOnly == 1,
		Enabled: s.Enabled == 1, LastFetch: s.LastFetch, LastError: s.LastError, NodeCount: s.NodeCount,
		Userinfo: s.Userinfo, UserAgent: s.UserAgent,
	}
}

func sourceKind(s *model.RemoteSource) string {
	if s.Kind == model.RemoteSourceFile {
		return model.RemoteSourceFile
	}
	return model.RemoteSourceURL
}

// remoteSyncResult is what one refresh changed.
type remoteSyncResult struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
	// Kept are unmatched nodes retained because they are referenced or
	// because this refresh did not provide a complete, successfully saved set.
	Kept   int      `json:"kept"`
	Errors []string `json:"errors"`
}

func (r remoteSyncResult) summary() string {
	parts := []string{fmt.Sprintf("新增 %d", r.Added), fmt.Sprintf("更新 %d", r.Updated), fmt.Sprintf("移除 %d", r.Removed)}
	if r.Kept > 0 {
		parts = append(parts, fmt.Sprintf("保留 %d 个旧节点", r.Kept))
	}
	return strings.Join(parts, "，")
}

func sameImported(row *model.NodeInbound, node kernel.ImportedNode, extra string) bool {
	return importedName(row) == truncateRunes(node.Name, 100) && row.Listen == node.Host && row.Port == node.Port && row.Extra == extra
}

// importedName is the node's name in its source; rows refreshed before
// ImportedName existed still carry it as their name.
func importedName(row *model.NodeInbound) string {
	if row.ImportedName != "" {
		return row.ImportedName
	}
	return row.Name
}

// syncRemoteSource fetches the source (a file source takes the uploaded
// content) and brings its imported nodes in line: nodes are matched by name
// first (providers keep names when they move a server), then by address.
func (h *Handler) syncRemoteSource(ctx context.Context, src *model.RemoteSource, upload string) (remoteSyncResult, error) {
	remoteSourceMu.Lock()
	defer remoteSourceMu.Unlock()
	res := remoteSyncResult{Errors: []string{}}
	now := time.Now().UnixMilli()
	fail := func(err error) (remoteSyncResult, error) {
		_ = h.repo.UpdateRemoteSourceStatus(src.ID, map[string]interface{}{"last_fetch": now, "last_error": truncate(err.Error(), 500)})
		return res, err
	}

	body, userinfo := upload, ""
	if sourceKind(src) == model.RemoteSourceFile {
		if strings.TrimSpace(upload) == "" {
			return fail(fmt.Errorf("文件订阅源不能自动更新，请重新上传文件"))
		}
	} else {
		var err error
		if body, userinfo, err = h.fetchSubscriptionInfo(ctx, src.URL, src.UserAgent); err != nil {
			return fail(fmt.Errorf("获取订阅失败：%v", err))
		}
	}
	nodes, parseErrs := kernel.ParseImport(body)
	if len(nodes) == 0 {
		msg := "订阅里没有可用的节点"
		if len(parseErrs) > 0 {
			msg += "：" + parseErrs[0]
		}
		// An empty answer is more likely a provider hiccup than a plan
		// without servers: keep the current nodes.
		return fail(fmt.Errorf("%s", msg))
	}
	if len(nodes) > maxImportNodes {
		nodes = nodes[:maxImportNodes]
		res.Errors = append(res.Errors, fmt.Sprintf("只保留前 %d 个节点", maxImportNodes))
	}
	res.Errors = append(res.Errors, parseErrs...)

	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		return fail(err)
	}
	var mine []*model.NodeInbound
	byName := map[string][]*model.NodeInbound{}
	byAddr := map[string][]*model.NodeInbound{}
	for i := range rows {
		row := &rows[i]
		if row.SourceID != src.ID || !isExternalInbound(row) {
			continue
		}
		mine = append(mine, row)
		byName[importedName(row)] = append(byName[importedName(row)], row)
		byAddr[externalKey(row.Listen, row.Port)] = append(byAddr[externalKey(row.Listen, row.Port)], row)
	}
	used := map[int64]bool{}
	take := func(list []*model.NodeInbound) *model.NodeInbound {
		for _, row := range list {
			if !used[row.ID] {
				used[row.ID] = true
				return row
			}
		}
		return nil
	}

	for _, node := range nodes {
		fresh, err := externalRow(node, src.RelayOnly == 1, now)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s：%v", node.Name, err))
			continue
		}
		fresh.SourceID, fresh.ImportedName = src.ID, fresh.Name
		row := take(byName[fresh.Name])
		if row == nil {
			row = take(byAddr[externalKey(node.Host, node.Port)])
		}
		if row == nil {
			if err := h.repo.CreateNodeInbound(&fresh); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s：%v", node.Name, err))
				continue
			}
			res.Added++
			continue
		}
		if sameImported(row, node, fresh.Extra) {
			continue
		}
		moved := row.Listen != fresh.Listen || row.Port != fresh.Port
		// A name the user changed is kept; otherwise it follows the source.
		if row.Name == importedName(row) {
			row.Name = fresh.Name
		}
		row.ImportedName = fresh.Name
		row.Protocol, row.Listen, row.Port = fresh.Protocol, fresh.Listen, fresh.Port
		row.ShareHost, row.Extra, row.UpdatedTime = fresh.ShareHost, fresh.Extra, now
		if err := h.repo.SaveNodeInbound(row); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s：%v", node.Name, err))
			continue
		}
		res.Updated++
		if moved {
			for _, p := range h.retargetRelays(row) {
				res.Errors = append(res.Errors, row.Name+"："+p)
			}
		}
		for _, p := range h.applyChainReferrers(row.ID) {
			res.Errors = append(res.Errors, row.Name+"："+p)
		}
	}

	// A failed parse, truncated input or failed write cannot establish that
	// an unmatched node was removed by the provider. Keep its ID and references.
	complete := len(res.Errors) == 0
	for _, row := range mine {
		if used[row.ID] {
			continue
		}
		if !complete {
			res.Kept++
			continue
		}
		if relays, err := h.repo.ListInboundRelays(row.ID); err == nil && len(relays) > 0 {
			res.Kept++
			continue
		}
		if len(h.chainReferrers(row.ID)) > 0 {
			res.Kept++
			continue
		}
		if err := h.repo.DeleteNodeInbound(row.ID); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s：%v", row.Name, err))
			continue
		}
		res.Removed++
	}

	count := 0
	if rows, err := h.repo.ListNodeInbounds(); err == nil {
		for _, row := range rows {
			if row.SourceID == src.ID {
				count++
			}
		}
	}
	lastError := ""
	if len(res.Errors) > 0 {
		lastError = "订阅未完整同步：" + truncateRunes(strings.Join(res.Errors, "；"), 450)
	}
	_ = h.repo.UpdateRemoteSourceStatus(src.ID, map[string]interface{}{
		"last_fetch": now, "last_error": lastError, "node_count": count, "userinfo": userinfo,
	})
	if lastError != "" {
		return res, fmt.Errorf("%s；%s", res.summary(), lastError)
	}
	return res, nil
}

func remoteSourceDue(s *model.RemoteSource, now time.Time) bool {
	if s.Enabled != 1 || s.IntervalHours <= 0 || sourceKind(s) == model.RemoteSourceFile {
		return false
	}
	return now.Sub(time.UnixMilli(s.LastFetch)) >= time.Duration(s.IntervalHours)*time.Hour
}

func (h *Handler) refreshDueRemoteSources(ctx context.Context, now time.Time) {
	sources, err := h.repo.ListRemoteSources()
	if err != nil {
		return
	}
	for i := range sources {
		s := &sources[i]
		if !remoteSourceDue(s, now) {
			continue
		}
		if _, err := h.syncRemoteSource(ctx, s, ""); err != nil {
			log.Printf("remote source %d refresh failed: %v", s.ID, err)
		}
	}
}

func (h *Handler) runRemoteSourceLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(remoteSourceCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		h.refreshDueRemoteSources(ctx, time.Now())
		h.refreshDueTemplates(ctx, time.Now())
	}
}

func (h *Handler) remoteSourceList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	sources, err := h.repo.ListRemoteSources()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	out := make([]remoteSourceView, 0, len(sources))
	for i := range sources {
		out = append(out, remoteSourceViewOf(&sources[i]))
	}
	response.WriteJSON(w, response.OK(out))
}

// remoteSourceSave creates or edits a source; a new one, a changed URL or
// an uploaded file is synced right away.
func (h *Handler) remoteSourceSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
		Kind string `json:"kind"`
		Role string `json:"role"`
		// Content is an uploaded file (file sources).
		Content       string `json:"content"`
		IntervalHours int    `json:"intervalHours"`
		RelayOnly     bool   `json:"relayOnly"`
		Enabled       *bool  `json:"enabled"`
		// UserAgent is sent when fetching; "" uses the default.
		UserAgent string `json:"userAgent"`
	}
	// A JSON-escaped file can be larger than the file itself.
	if err := decodeJSON(http.MaxBytesReader(w, r.Body, 2*maxSubscriptionBytes), &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误或文件超过 5MB"))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		response.WriteJSON(w, response.ErrDefault("请填写名称（最多 100 个字）"))
		return
	}
	if req.Kind != model.RemoteSourceFile {
		req.Kind = model.RemoteSourceURL
	}
	if req.Role != "relay" && req.Role != "landing" {
		req.Role = ""
	}
	if len(req.Content) > maxSubscriptionBytes {
		response.WriteJSON(w, response.ErrDefault("文件超过 5MB"))
		return
	}
	if req.Kind == model.RemoteSourceURL && !isSubscriptionURL(req.URL) {
		response.WriteJSON(w, response.ErrDefault("请填写 http(s) 订阅地址"))
		return
	}
	req.UserAgent = strings.TrimSpace(req.UserAgent)
	if len(req.UserAgent) > 255 || strings.ContainsFunc(req.UserAgent, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		response.WriteJSON(w, response.ErrDefault("User-Agent 不正确（一行文字，最多 255 个字符）"))
		return
	}
	if req.IntervalHours < 0 || req.IntervalHours > 24*7 {
		response.WriteJSON(w, response.ErrDefault("更新间隔不正确"))
		return
	}
	now := time.Now().UnixMilli()
	src := &model.RemoteSource{CreatedTime: now, Enabled: 1}
	if req.ID > 0 {
		var err error
		if src, err = h.repo.GetRemoteSource(req.ID); err != nil || src == nil {
			response.WriteJSON(w, response.ErrDefault("订阅源不存在"))
			return
		}
	}
	if req.Kind == model.RemoteSourceFile {
		req.URL, req.IntervalHours, req.UserAgent = "", 0, ""
		if strings.TrimSpace(req.Content) == "" && (src.ID == 0 || sourceKind(src) != model.RemoteSourceFile) {
			response.WriteJSON(w, response.ErrDefault("请选择订阅文件"))
			return
		}
	} else {
		req.Content = ""
	}
	fetch := src.ID == 0 || src.URL != req.URL || sourceKind(src) != req.Kind || req.Content != "" ||
		(req.Kind == model.RemoteSourceURL && src.UserAgent != req.UserAgent)
	src.Name, src.URL, src.IntervalHours, src.Kind, src.Role = req.Name, req.URL, req.IntervalHours, req.Kind, req.Role
	src.UserAgent = req.UserAgent
	src.RelayOnly = boolToInt(req.RelayOnly)
	if req.Enabled != nil {
		src.Enabled = boolToInt(*req.Enabled)
	}
	src.UpdatedTime = now
	if err := h.repo.SaveRemoteSource(src); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if !fetch {
		src, _ = h.repo.GetRemoteSource(src.ID)
		response.WriteJSON(w, response.OK(remoteSourceViewOf(src)))
		return
	}
	h.respondRemoteSync(w, r.Context(), src, req.Content)
}

func (h *Handler) respondRemoteSync(w http.ResponseWriter, ctx context.Context, src *model.RemoteSource, upload string) {
	res, err := h.syncRemoteSource(ctx, src, upload)
	saved, _ := h.repo.GetRemoteSource(src.ID)
	if saved == nil {
		saved = src
	}
	data := map[string]interface{}{"source": remoteSourceViewOf(saved), "result": res}
	if err != nil {
		response.WriteJSON(w, response.R{Code: -1, Msg: err.Error(), Data: data, TS: time.Now().UnixMilli()})
		return
	}
	response.WriteJSON(w, response.R{Code: 0, Msg: res.summary(), Data: data, TS: time.Now().UnixMilli()})
}

func (h *Handler) remoteSourceRefresh(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	src, err := h.repo.GetRemoteSource(id)
	if err != nil || src == nil {
		response.WriteJSON(w, response.ErrDefault("订阅源不存在"))
		return
	}
	if sourceKind(src) == model.RemoteSourceFile {
		response.WriteJSON(w, response.ErrDefault("文件订阅源请在编辑里重新上传文件"))
		return
	}
	h.respondRemoteSync(w, r.Context(), src, "")
}

// remoteSourceDelete removes a source and, unless keepNodes is set, its
// nodes (with their relays).
func (h *Handler) remoteSourceDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID        int64 `json:"id"`
		KeepNodes bool  `json:"keepNodes"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	remoteSourceMu.Lock()
	defer remoteSourceMu.Unlock()
	if req.KeepNodes {
		if err := h.repo.DetachSourceInbounds(req.ID); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	} else if rows, err := h.repo.ListNodeInbounds(); err == nil {
		for i := range rows {
			if rows[i].SourceID != req.ID {
				continue
			}
			if msg := h.chainInUseMessage(&rows[i]); msg != "" {
				response.WriteJSON(w, response.ErrDefault(msg+"，或删除时保留节点"))
				return
			}
		}
		for _, row := range rows {
			if row.SourceID != req.ID {
				continue
			}
			h.removeInboundRelays(row.ID)
			if err := h.repo.DeleteNodeInbound(row.ID); err != nil {
				response.WriteJSON(w, response.Err(-2, err.Error()))
				return
			}
		}
	}
	if err := h.repo.DeleteRemoteSource(req.ID); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}
