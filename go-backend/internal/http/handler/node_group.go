package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

// Node groups: reusable node lists for subscriptions (like Sub-Store's
// collections). A group picks inbounds and remote sources and processes
// their nodes (filter, rename, flag, prefix, dedupe, sort, script); a
// subscription lists the nodes of its groups in order and adds the chain,
// rules and templates.

// groupPipeline is the JSON node processing stored per node group.
type groupPipeline struct {
	Lines     string   `json:"lines"`
	Regions   []string `json:"regions"`
	Protocols []string `json:"protocols"`
	Include   string   `json:"include"`
	Exclude   string   `json:"exclude"`
	Rename    string   `json:"rename"`
	AddFlag   bool     `json:"addFlag"`
	Prefix    string   `json:"prefix"`
	Dedupe    bool     `json:"dedupe"`
	Sort      string   `json:"sort"`
	Script    string   `json:"script"`
}

func (p groupPipeline) options() kernel.SubOptions {
	return kernel.SubOptions{
		Lines: p.Lines, Regions: p.Regions, Protocols: p.Protocols, Include: p.Include, Exclude: p.Exclude,
		Rename: p.Rename, AddFlag: p.AddFlag, Prefix: p.Prefix, Dedupe: p.Dedupe, Sort: p.Sort, Script: p.Script,
	}
}

func (p *groupPipeline) normalize() error {
	p.Rename = strings.TrimSpace(strings.ReplaceAll(p.Rename, "\r\n", "\n"))
	p.Include = strings.TrimSpace(p.Include)
	p.Exclude = strings.TrimSpace(p.Exclude)
	p.Script = strings.TrimSpace(strings.ReplaceAll(p.Script, "\r\n", "\n"))
	if len(p.Rename) > 10000 || len(p.Include) > 1000 || len(p.Exclude) > 1000 {
		return errText("规则太长")
	}
	if len([]rune(p.Prefix)) > 50 {
		return errText("前缀最多 50 个字")
	}
	if p.Sort != "name" {
		p.Sort = ""
	}
	if p.Lines != "relay" && p.Lines != "direct" && p.Lines != "prefer_relay" {
		p.Lines = ""
	}
	if p.Regions == nil {
		p.Regions = []string{}
	}
	if p.Protocols == nil {
		p.Protocols = []string{}
	}
	return p.options().Validate()
}

func parseGroupPipeline(raw string) groupPipeline {
	var p groupPipeline
	_ = json.Unmarshal([]byte(raw), &p)
	if p.Regions == nil {
		p.Regions = []string{}
	}
	if p.Protocols == nil {
		p.Protocols = []string{}
	}
	return p
}

// subContext is what building subscriptions needs: the inbounds and the
// node groups by id.
type subContext struct {
	rows   []model.NodeInbound
	groups map[int64]*model.NodeGroup
}

func (h *Handler) loadSubContext() (*subContext, error) {
	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		return nil, err
	}
	return h.subContextFor(rows)
}

// subContextFor uses the given inbounds (e.g. narrowed to one).
func (h *Handler) subContextFor(rows []model.NodeInbound) (*subContext, error) {
	if err := h.repo.GroupLegacySubscriptions(); err != nil {
		return nil, err
	}
	list, err := h.repo.ListNodeGroups()
	if err != nil {
		return nil, err
	}
	groups := make(map[int64]*model.NodeGroup, len(list))
	for i := range list {
		groups[list[i].ID] = &list[i]
	}
	return &subContext{rows: rows, groups: groups}, nil
}

// groupInboundIDs resolves a group's sources against the inbounds.
func (c *subContext) groupInboundIDs(g *model.NodeGroup) []int64 {
	if g.SourceAll == 1 {
		ids := make([]int64, 0, len(c.rows))
		for _, r := range c.rows {
			ids = append(ids, r.ID)
		}
		return ids
	}
	known := make(map[int64]bool, len(c.rows))
	for _, r := range c.rows {
		known[r.ID] = true
	}
	var ids []int64
	picked := map[int64]bool{}
	for _, id := range parseIDList(g.InboundIDs) {
		if known[id] && !picked[id] {
			picked[id] = true
			ids = append(ids, id)
		}
	}
	sources := map[int64]bool{}
	for _, id := range parseIDList(g.SourceIDs) {
		sources[id] = true
	}
	for _, r := range c.rows {
		if r.SourceID != 0 && sources[r.SourceID] && !picked[r.ID] {
			picked[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// subGroups are the subscription's node groups that still exist, in order.
func (c *subContext) subGroups(s *model.Subscription) []*model.NodeGroup {
	var out []*model.NodeGroup
	for _, id := range repo.ParseGroupIDs(s.GroupIDs) {
		if g := c.groups[id]; g != nil {
			out = append(out, g)
		}
	}
	return out
}

// inboundIDs are the inbounds of all the subscription's groups: where its
// share credentials must be accepted.
func (c *subContext) inboundIDs(s *model.Subscription) []int64 {
	var ids []int64
	seen := map[int64]bool{}
	for _, g := range c.subGroups(s) {
		for _, id := range c.groupInboundIDs(g) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// groupNodes are a group's processed nodes; for a share subscription they
// carry its credentials where the protocol allows.
func (h *Handler) groupNodes(c *subContext, g *model.NodeGroup, s *model.Subscription) []kernel.Endpoint {
	return kernel.ProcessNodes(h.groupSource(c, g, s), parseGroupPipeline(g.Options).options())
}

// groupSource are a group's nodes before its processing.
func (h *Handler) groupSource(c *subContext, g *model.NodeGroup, s *model.Subscription) []kernel.Endpoint {
	want := map[int64]bool{}
	for _, id := range c.groupInboundIDs(g) {
		want[id] = true
	}
	selected := make([]model.NodeInbound, 0, len(want))
	for _, row := range c.rows {
		if want[row.ID] {
			selected = append(selected, row)
		}
	}
	endpoints, _ := h.inboundEndpoints(selected)
	if s != nil && isShare(s) {
		ku := kernelUser(s)
		for i := range endpoints {
			if endpoints[i].Proxy == nil {
				endpoints[i].Inbound = endpoints[i].Inbound.ForUser(ku)
			}
		}
	}
	return endpoints
}

// subNodes are the nodes of a subscription's groups, in order, before its
// chain, rules and templates.
func (h *Handler) subNodes(c *subContext, s *model.Subscription) []kernel.Endpoint {
	var out []kernel.Endpoint
	for _, g := range c.subGroups(s) {
		out = append(out, h.groupNodes(c, g, s)...)
	}
	return out
}

// subscriptionsUsingGroup lists the subscriptions that draw from a group.
func subscriptionsUsingGroup(subs []model.Subscription, id int64) []model.Subscription {
	var out []model.Subscription
	for _, s := range subs {
		for _, gid := range repo.ParseGroupIDs(s.GroupIDs) {
			if gid == id {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// shareInboundIDs are the inbounds whose kernels carry these share
// subscriptions' credentials.
func shareInboundIDs(c *subContext, subs []model.Subscription) []int64 {
	var ids []int64
	for i := range subs {
		if isShare(&subs[i]) {
			ids = append(ids, c.inboundIDs(&subs[i])...)
		}
	}
	return ids
}

// ---- API ----

type nodeGroupRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type nodeGroupView struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	SourceAll  bool          `json:"sourceAll"`
	InboundIDs []int64       `json:"inboundIds"`
	SourceIDs  []int64       `json:"sourceIds"`
	Options    groupPipeline `json:"options"`
	NodeCount  int           `json:"nodeCount"`
	// Subscriptions draw from the group.
	Subscriptions []nodeGroupRef `json:"subscriptions"`
}

func (h *Handler) nodeGroupView(c *subContext, g *model.NodeGroup, subs []model.Subscription) nodeGroupView {
	known := make(map[int64]bool, len(c.rows))
	for _, r := range c.rows {
		known[r.ID] = true
	}
	inbounds, sources := []int64{}, []int64{}
	if g.SourceAll != 1 {
		for _, id := range parseIDList(g.InboundIDs) {
			if known[id] {
				inbounds = append(inbounds, id)
			}
		}
		sources = append(sources, parseIDList(g.SourceIDs)...)
	}
	refs := []nodeGroupRef{}
	for _, s := range subscriptionsUsingGroup(subs, g.ID) {
		refs = append(refs, nodeGroupRef{ID: s.ID, Name: s.Name})
	}
	return nodeGroupView{
		ID: g.ID, Name: g.Name, SourceAll: g.SourceAll == 1, InboundIDs: inbounds, SourceIDs: sources,
		Options: parseGroupPipeline(g.Options), NodeCount: len(h.groupNodes(c, g, nil)), Subscriptions: refs,
	}
}

func (h *Handler) nodeGroupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	c, err := h.loadSubContext()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	subs, err := h.repo.ListSubscriptions()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	list, err := h.repo.ListNodeGroups()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	items := make([]nodeGroupView, 0, len(list))
	for i := range list {
		items = append(items, h.nodeGroupView(c, &list[i], subs))
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"items":   items,
		"regions": append(kernel.RegionNames(), kernel.RegionOther),
	}))
}

type nodeGroupRequest struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	SourceAll  bool          `json:"sourceAll"`
	InboundIDs []int64       `json:"inboundIds"`
	SourceIDs  []int64       `json:"sourceIds"`
	Options    groupPipeline `json:"options"`
}

// validate checks the request and keeps the inbounds that exist.
func (req *nodeGroupRequest) validate(rows []model.NodeInbound) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		return errText("请填写名称（最多 100 个字）")
	}
	if err := req.Options.normalize(); err != nil {
		return err
	}
	known := make(map[int64]bool, len(rows))
	for _, r := range rows {
		known[r.ID] = true
	}
	var ids []int64
	for _, id := range req.InboundIDs {
		if known[id] {
			ids = append(ids, id)
		}
	}
	req.InboundIDs = ids
	var sources []int64
	for _, id := range req.SourceIDs {
		if id > 0 {
			sources = append(sources, id)
		}
	}
	req.SourceIDs = sources
	if !req.SourceAll && len(ids) == 0 && len(sources) == 0 {
		return errText("请至少选择一个入站或订阅源，或选择全部")
	}
	return nil
}

func (req *nodeGroupRequest) draft() *model.NodeGroup {
	opts, _ := json.Marshal(req.Options)
	return &model.NodeGroup{ID: req.ID, Name: req.Name, SourceAll: boolToInt(req.SourceAll),
		InboundIDs: formatIDList(req.InboundIDs), SourceIDs: formatIDList(req.SourceIDs), Options: string(opts)}
}

func (h *Handler) nodeGroupSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req nodeGroupRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	c, err := h.loadSubContext()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := req.validate(c.rows); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	subs, err := h.repo.ListSubscriptions()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	now := time.Now().UnixMilli()
	g := req.draft()
	var push []int64
	if req.ID > 0 {
		old := c.groups[req.ID]
		if old == nil {
			response.WriteJSON(w, response.ErrDefault("节点组不存在"))
			return
		}
		g.Inx, g.CreatedTime = old.Inx, old.CreatedTime
		// The subscriptions drawing from it may gain or lose inbounds:
		// their credentials move with them.
		users := subscriptionsUsingGroup(subs, g.ID)
		push = shareInboundIDs(c, users)
		c.groups[g.ID] = g
		push = append(push, shareInboundIDs(c, users)...)
	} else {
		g.Inx, g.CreatedTime = len(c.groups)+1, now
	}
	g.UpdatedTime = now
	if err := h.repo.SaveNodeGroup(g); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	c.groups[g.ID] = g
	h.respondAfterSubPush(w, push, h.nodeGroupView(c, g, subs))
}

func (h *Handler) nodeGroupDelete(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	subs, err := h.repo.ListSubscriptions()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if users := subscriptionsUsingGroup(subs, id); len(users) > 0 {
		names := make([]string, 0, len(users))
		for _, s := range users {
			names = append(names, "「"+s.Name+"」")
		}
		response.WriteJSON(w, response.ErrDefault("订阅"+strings.Join(names, "、")+"正在使用这个节点组，请先在订阅里取消选择"))
		return
	}
	if err := h.repo.DeleteNodeGroup(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeGroupOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if err := h.repo.UpdateNodeGroupOrder(req.IDs); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

// nodeGroupPreview runs an (unsaved) group's processing and lists the
// resulting nodes, plus how many the filters dropped.
func (h *Handler) nodeGroupPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req nodeGroupRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "preview"
	}
	c, err := h.loadSubContext()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := req.validate(c.rows); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	g := req.draft()
	source := h.groupSource(c, g, nil)
	opts := req.Options.options()
	result := kernel.ProcessNodes(source, opts)
	scriptError := ""
	if err := kernel.SubScriptError(source, opts); err != nil {
		scriptError = err.Error()
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"items":       previewItems(result),
		"total":       len(source),
		"dropped":     len(source) - len(result),
		"scriptError": scriptError,
	}))
}

func previewItems(nodes []kernel.Endpoint) []subPreviewItem {
	items := make([]subPreviewItem, 0, len(nodes))
	for _, e := range nodes {
		line := "relay"
		if e.Direct {
			line = "direct"
		}
		items = append(items, subPreviewItem{Name: e.Name, Original: e.Original, Region: kernel.RegionOf(e.Name),
			Protocol: kernel.EndpointProtocol(e), Line: line, Chain: e.ChainRole, SourceID: e.SourceID, Own: e.Proxy == nil && e.SourceID == 0})
	}
	return items
}
