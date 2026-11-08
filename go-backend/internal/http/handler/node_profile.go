package handler

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
)

// A server's profile beyond how it is reached: its group, public note,
// price, traffic plan and whether the status page shows it. Each field is
// changed only when the request carries it, so older clients and the quick
// 价格与到期 dialog leave the others alone.

const (
	maxNodeGroupRunes = 50
	maxPublicNoteRune = 200
	maxNodePrice      = 1e9
	maxTrafficQuotaGB = 10_000_000 // 10 EB
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// applyNodeProfileRequest stores the profile fields present in req.
func (h *Handler) applyNodeProfileRequest(nodeID int64, req map[string]interface{}) error {
	updates, err := nodeProfileUpdates(req)
	if err != nil {
		return err
	}
	if len(updates) > 0 {
		if err := h.repo.UpdateNodeColumns(nodeID, updates); err != nil {
			return err
		}
	}
	if v, ok := req["statusVisible"]; ok {
		if err := h.setNodeStatusVisible(nodeID, asBool(v, true)); err != nil {
			return err
		}
	}
	if v, ok := req["trafficCorrection"]; ok && v != nil {
		c, ok := v.(map[string]interface{})
		if !ok {
			return errors.New("流量校正格式错误")
		}
		if err := h.correctNodeTraffic(nodeID, c, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

// nodeProfileUpdates checks the profile fields present in req and returns
// the columns they set; it is also run before a server is saved, so a bad
// value refuses the whole save.
func nodeProfileUpdates(req map[string]interface{}) (map[string]interface{}, error) {
	updates := map[string]interface{}{}
	if v, ok := req["group"]; ok {
		group := strings.TrimSpace(asString(v))
		if utf8.RuneCountInString(group) > maxNodeGroupRunes {
			return nil, fmt.Errorf("分组最多 %d 个字", maxNodeGroupRunes)
		}
		updates["group_name"] = group
	}
	if v, ok := req["publicNote"]; ok {
		note := strings.TrimSpace(asString(v))
		if utf8.RuneCountInString(note) > maxPublicNoteRune {
			return nil, fmt.Errorf("公开备注最多 %d 个字", maxPublicNoteRune)
		}
		updates["public_note"] = note
	}
	if err := billingUpdates(req, updates); err != nil {
		return nil, err
	}
	if v, ok := req["trafficQuotaGb"]; ok {
		quota := asInt64(v, -1)
		if quota < 0 || quota > maxTrafficQuotaGB {
			return nil, errors.New("每月流量额度应为不小于 0 的整数（GB），0 为不限")
		}
		updates["traffic_quota_gb"] = quota
	}
	if v, ok := req["trafficCalc"]; ok {
		calc, err := normalizeTrafficCalc(asString(v))
		if err != nil {
			return nil, err
		}
		updates["traffic_calc"] = calc
	}
	if v, ok := req["trafficResetDay"]; ok {
		day := asInt(v, -1)
		if day < 0 || day > 31 {
			return nil, errors.New("每月重置日应在 1 到 31 之间")
		}
		updates["traffic_reset_day"] = day
	}
	if v, ok := req["trafficCorrection"]; ok && v != nil {
		if _, ok := v.(map[string]interface{}); !ok {
			return nil, errors.New("流量校正格式错误")
		}
	}
	return updates, nil
}

// billingUpdates adds the price, currency, renewal cycle and expiry present
// in req to updates.
func billingUpdates(req map[string]interface{}, updates map[string]interface{}) error {
	if v, ok := req["price"]; ok {
		price := asFloat(v, -1)
		if v == nil || asString(v) == "" {
			price = 0
		}
		if price < 0 || price > maxNodePrice || math.IsNaN(price) {
			return errors.New("价格应为不小于 0 的数字，0 或留空为免费")
		}
		updates["price"] = math.Round(price*100) / 100
	}
	if v, ok := req["currency"]; ok {
		cur := strings.ToUpper(strings.TrimSpace(asString(v)))
		if cur != "" && !currencyCode.MatchString(cur) {
			return errors.New("货币请填三位字母代码，例如 CNY、USD")
		}
		updates["currency"] = cur
	}
	if v, ok := req["renewalCycle"]; ok {
		updates["renewal_cycle"] = nullableText(normalizeNodeRenewalCycle(asString(v)))
	}
	if v, ok := req["expiryTime"]; ok {
		updates["expiry_time"] = nullableUnixMilli(asInt64(v, 0))
		updates["expiry_reminder_dismissed"] = 0
	}
	return nil
}

func normalizeTrafficCalc(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "sum":
		return model.TrafficCalcSum, nil
	case model.TrafficCalcMax:
		return model.TrafficCalcMax, nil
	case model.TrafficCalcUp:
		return model.TrafficCalcUp, nil
	case model.TrafficCalcDown:
		return model.TrafficCalcDown, nil
	}
	return "", errors.New("流量计算方式应为 sum、max、up 或 down")
}

// nodeBilling saves only a server's price and renewal (the 价格与到期 dialog).
func (h *Handler) nodeBilling(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	id := asInt64(req["id"], 0)
	if node, err := h.repo.GetNodeByID(id); err != nil || node == nil {
		response.WriteJSON(w, response.ErrDefault("服务器不存在"))
		return
	}
	updates := map[string]interface{}{}
	if err := billingUpdates(req, updates); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if len(updates) > 0 {
		updates["updated_time"] = time.Now().UnixMilli()
		if err := h.repo.UpdateNodeColumns(id, updates); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	response.WriteJSON(w, response.OKEmpty())
}

// ---- status page visibility ----

// setNodeStatusVisible shows or hides a server on the status page; the
// choice lives in the status page's hidden-server list (设置 → 状态页).
func (h *Handler) setNodeStatusVisible(nodeID int64, visible bool) error {
	cfg, err := h.repo.GetConfigsByNames([]string{statusPageHiddenNodesKey})
	if err != nil {
		return err
	}
	hidden := parseStatusHiddenNodes(cfg[statusPageHiddenNodesKey])
	if hidden[nodeID] == !visible {
		return nil
	}
	if visible {
		delete(hidden, nodeID)
	} else {
		hidden[nodeID] = true
	}
	ids := make([]int64, 0, len(hidden))
	for id := range hidden {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	if err := h.repo.UpsertConfig(statusPageHiddenNodesKey, strings.Join(parts, ","), time.Now().UnixMilli()); err != nil {
		return err
	}
	h.resetStatusCaches()
	return nil
}

// resetStatusCaches makes the next status page request rebuild its answer.
func (h *Handler) resetStatusCaches() {
	if c := h.statusCache; c != nil {
		c.mu.Lock()
		c.body, c.at = statusBody{}, time.Time{}
		c.mu.Unlock()
	}
	if c := h.statusHistory; c != nil {
		c.mu.Lock()
		c.gate = statusGate{}
		c.mu.Unlock()
	}
}

// ---- traffic periods ----

// trafficPeriodStart is when the traffic period containing now began: the
// latest reset day (1-31, 0 = the 1st) at midnight in now's location; a
// month shorter than the reset day resets on its last day.
func trafficPeriodStart(resetDay int, now time.Time) time.Time {
	if resetDay < 1 || resetDay > 31 {
		resetDay = 1
	}
	at := func(year int, month time.Month) time.Time {
		last := time.Date(year, month+1, 0, 0, 0, 0, 0, now.Location()).Day()
		return time.Date(year, month, min(resetDay, last), 0, 0, 0, 0, now.Location())
	}
	start := at(now.Year(), now.Month())
	if start.After(now) {
		prev := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0)
		start = at(prev.Year(), prev.Month())
	}
	return start
}

// nodeTraffic is a server's traffic in its current period, corrected.
type nodeTraffic struct {
	Up, Down int64
	Start    time.Time
}

// periodTraffic returns each node's traffic (sent = up, received = down)
// since the start of its own period, with the admin's correction applied.
func (h *Handler) periodTraffic(nodes []model.Node, now time.Time) (map[int64]nodeTraffic, error) {
	out := make(map[int64]nodeTraffic, len(nodes))
	totals := map[int64]map[int64][2]int64{} // period start -> node -> [in, out]
	for _, n := range nodes {
		start := trafficPeriodStart(n.TrafficResetDay, now)
		ms := start.UnixMilli()
		sums, ok := totals[ms]
		if !ok {
			var err error
			if sums, err = h.repo.NodeTrafficSince(ms); err != nil {
				return nil, err
			}
			totals[ms] = sums
		}
		t := sums[n.ID]
		v := nodeTraffic{Up: t[1], Down: t[0], Start: start}
		if n.TrafficAdjustPeriod == ms {
			v.Up = max(v.Up+n.TrafficAdjustUp, 0)
			v.Down = max(v.Down+n.TrafficAdjustDown, 0)
		}
		out[n.ID] = v
	}
	return out, nil
}

// correctNodeTraffic sets what the server has used this period (GB, as the
// admin read it from the provider): the difference to what the panel
// counted is kept as an offset for the period.
func (h *Handler) correctNodeTraffic(nodeID int64, c map[string]interface{}, now time.Time) error {
	node, err := h.repo.GetNodeByID(nodeID)
	if err != nil || node == nil {
		return errors.New("服务器不存在")
	}
	gb := func(key string) (int64, bool, error) {
		v, ok := c[key]
		if !ok || v == nil || asString(v) == "" {
			return 0, false, nil
		}
		f := asFloat(v, -1)
		if f < 0 || f > maxTrafficQuotaGB || math.IsNaN(f) {
			return 0, false, errors.New("校正的流量应为不小于 0 的数字（GB）")
		}
		return int64(math.Round(f * (1 << 30))), true, nil
	}
	up, setUp, err := gb("upGb")
	if err != nil {
		return err
	}
	down, setDown, err := gb("downGb")
	if err != nil {
		return err
	}
	if !setUp && !setDown {
		return nil
	}
	n := *node
	n.TrafficAdjustUp, n.TrafficAdjustDown, n.TrafficAdjustPeriod = 0, 0, 0
	raw, err := h.periodTraffic([]model.Node{n}, now.In(time.Local))
	if err != nil {
		return err
	}
	counted := raw[nodeID]
	updates := map[string]interface{}{"traffic_adjust_period": counted.Start.UnixMilli()}
	// A direction left blank keeps its current correction for the period.
	keep := node.TrafficAdjustPeriod == counted.Start.UnixMilli()
	updates["traffic_adjust_up"] = map[bool]int64{true: node.TrafficAdjustUp, false: 0}[keep]
	updates["traffic_adjust_down"] = map[bool]int64{true: node.TrafficAdjustDown, false: 0}[keep]
	if setUp {
		updates["traffic_adjust_up"] = up - counted.Up
	}
	if setDown {
		updates["traffic_adjust_down"] = down - counted.Down
	}
	if err := h.repo.UpdateNodeColumns(nodeID, updates); err != nil {
		return err
	}
	h.resetStatusCaches()
	return nil
}
