package handler

import (
	"net/http"
	"strconv"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
)

// nodeTrafficMonth returns each node's traffic in its current period (since
// its reset day, the 1st by default), for the node list: {"<id>": [in, out]}.
func (h *Handler) nodeTrafficMonth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		TZOffset int `json:"tzOffset"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.TZOffset < -14*60 || req.TZOffset > 14*60 {
		req.TZOffset = 0
	}
	// Each server's own period (its reset day) in the panel's time zone, as
	// the status page counts it, with the admin's correction applied; the
	// viewer's offset is no longer used.
	nodes, err := h.repo.ListNodeTrafficPlans()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	traffic, err := h.periodTraffic(nodes, time.Now())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	out := make(map[string][2]int64, len(traffic))
	for id, v := range traffic {
		out[strconv.FormatInt(id, 10)] = [2]int64{v.Down, v.Up}
	}
	response.WriteJSON(w, response.OK(out))
}

type nodeTrafficHistoryRequest struct {
	NodeID   int64  `json:"nodeId"`
	Range    string `json:"range"`
	TZOffset int    `json:"tzOffset"`
}

// aggregateNodeTraffic sums node_traffic 15-minute (or legacy hourly) buckets
// into the viewer's local 24h / 30d / 12m buckets.
func aggregateNodeTraffic(rows []model.NodeTraffic, rangeKey string, now time.Time) subTrafficView {
	if rangeKey != "day" && rangeKey != "month" {
		rangeKey = "hour"
	}
	starts := subTrafficBuckets(rangeKey, now)
	end := starts[len(starts)-1].Add(time.Hour)
	if rangeKey == "day" {
		end = starts[len(starts)-1].AddDate(0, 0, 1)
	}
	if rangeKey == "month" {
		end = starts[len(starts)-1].AddDate(0, 1, 0)
	}
	view := subTrafficView{Range: rangeKey, Points: make([]subTrafficPoint, len(starts))}
	for i, start := range starts {
		view.Points[i].Time = start.UnixMilli()
	}
	for _, row := range rows {
		minutes := row.BucketMinutes
		if minutes <= 0 {
			minutes = 60
		}
		duration := int64(minutes) * int64(time.Minute/time.Millisecond)
		rowEnd := row.Hour + duration
		for i := range view.Points {
			start, finish := view.Points[i].Time, end.UnixMilli()
			if i+1 < len(view.Points) {
				finish = view.Points[i+1].Time
			}
			left, right := max(start, row.Hour), min(finish, rowEnd)
			if left >= right {
				continue
			}
			if left != row.Hour || right != rowEnd {
				if row.OutBytes != 0 || row.InBytes != 0 {
					view.Estimated = true
				}
			}
			up := trafficShare(row.OutBytes, right-row.Hour, duration) - trafficShare(row.OutBytes, left-row.Hour, duration)
			down := trafficShare(row.InBytes, right-row.Hour, duration) - trafficShare(row.InBytes, left-row.Hour, duration)
			view.Points[i].Up += up
			view.Points[i].Down += down
			view.Up += up
			view.Down += down
		}
	}
	return view
}

// nodeTrafficHistory returns bucketed historical server traffic (hour/day/month)
// for a single node (nodeId > 0) or aggregated across all servers (nodeId == 0).
func (h *Handler) nodeTrafficHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req nodeTrafficHistoryRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.NodeID < 0 {
		response.WriteJSON(w, response.ErrDefault("服务器不存在"))
		return
	}
	if req.TZOffset < -14*60 || req.TZOffset > 14*60 {
		req.TZOffset = 0
	}

	var since int64
	if req.NodeID > 0 {
		node, err := h.repo.GetNodeByID(req.NodeID)
		if err != nil || node == nil {
			response.WriteJSON(w, response.ErrDefault("服务器不存在"))
			return
		}
		since = trafficPeriodStart(node.TrafficResetDay, time.Now()).UnixMilli()
	}

	now := time.Now().In(time.FixedZone("", req.TZOffset*60))
	starts := subTrafficBuckets(req.Range, now)
	rows, err := h.repo.ListNodeTrafficSince(req.NodeID, starts[0].Add(-time.Hour).UnixMilli())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	view := aggregateNodeTraffic(rows, req.Range, now)
	view.Since = since
	response.WriteJSON(w, response.OK(view))
}
