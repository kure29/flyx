package handler

import (
	"net/http"
	"strconv"
	"time"

	"go-backend/internal/http/response"
)

// nodeTrafficMonth returns each node's traffic since the 1st of this month
// in the viewer's time zone, for the node list: {"<id>": [in, out]}.
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
	now := time.Now().In(time.FixedZone("", req.TZOffset*60))
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	traffic, err := h.repo.NodeTrafficSince(month.UnixMilli())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	out := make(map[string][2]int64, len(traffic))
	for id, v := range traffic {
		out[strconv.FormatInt(id, 10)] = v
	}
	response.WriteJSON(w, response.OK(out))
}
