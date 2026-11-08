package handler

import (
	"net/http"
	"strconv"
	"time"

	"go-backend/internal/http/response"
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
