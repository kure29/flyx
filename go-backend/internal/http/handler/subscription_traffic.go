package handler

import (
	"net/http"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
)

// Subscription traffic history: quarter-hour rows (legacy rows are hourly)
// summed into the last 24 hours, 30 days or 12 months, in the viewer's
// time zone.

// subTrafficRetention keeps a little over a year, for the monthly view.
const subTrafficRetention = 400 * 24 * time.Hour

type subTrafficRequest struct {
	ID    int64  `json:"id"`
	Range string `json:"range"`
	// TZOffset is the viewer's offset from UTC in minutes (UTC+8 = 480).
	TZOffset int `json:"tzOffset"`
}

type subTrafficPoint struct {
	// Time is the unix ms the bucket starts at.
	Time int64 `json:"time"`
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type subTrafficView struct {
	Range     string            `json:"range"`
	Estimated bool              `json:"estimated"`
	Points    []subTrafficPoint `json:"points"`
	Up        int64             `json:"up"`
	Down      int64             `json:"down"`
	// Since is when the subscription's used traffic started counting (its
	// creation or last reset), for marking it on the chart.
	Since int64 `json:"since"`
}

// subTrafficBuckets returns the bucket starts of a range ending at now:
// 24 hours, 30 days or 12 months.
func subTrafficBuckets(rangeKey string, now time.Time) []time.Time {
	var out []time.Time
	switch rangeKey {
	case "day":
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		for i := 29; i >= 0; i-- {
			out = append(out, today.AddDate(0, 0, -i))
		}
	case "month":
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		for i := 11; i >= 0; i-- {
			out = append(out, month.AddDate(0, -i, 0))
		}
	default:
		hour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
		for i := 23; i >= 0; i-- {
			out = append(out, hour.Add(-time.Duration(i)*time.Hour))
		}
	}
	return out
}

// subTraffic sums interval overlaps. Legacy hours crossing a local boundary
// cannot be reconstructed; distribute them proportionally and mark the estimate.
func subTraffic(rows []model.SubscriptionTraffic, rangeKey string, now time.Time) subTrafficView {
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
				if row.UploadBytes != 0 || row.DownloadBytes != 0 {
					view.Estimated = true
				}
			}
			// Cumulative integer shares preserve every byte across adjacent
			// buckets and avoid overflow from multiplying the entire count.
			up := trafficShare(row.UploadBytes, right-row.Hour, duration) - trafficShare(row.UploadBytes, left-row.Hour, duration)
			down := trafficShare(row.DownloadBytes, right-row.Hour, duration) - trafficShare(row.DownloadBytes, left-row.Hour, duration)
			view.Points[i].Up += up
			view.Points[i].Down += down
			view.Up += up
			view.Down += down
		}
	}
	return view
}

func trafficShare(bytes, elapsed, duration int64) int64 {
	return bytes/duration*elapsed + bytes%duration*elapsed/duration
}

func (h *Handler) respondSubTraffic(w http.ResponseWriter, s *model.Subscription, req subTrafficRequest) {
	if req.TZOffset < -14*60 || req.TZOffset > 14*60 {
		req.TZOffset = 0
	}
	now := time.Now().In(time.FixedZone("", req.TZOffset*60))
	starts := subTrafficBuckets(req.Range, now)
	hours, err := h.repo.ListSubscriptionTraffic(s.ID, starts[0].Add(-time.Hour).UnixMilli())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	view := subTraffic(hours, req.Range, now)
	view.Since = subTrafficSince(s)
	response.WriteJSON(w, response.OK(view))
}

func (h *Handler) subscriptionTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req subTrafficRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	s, err := h.repo.GetSubscription(req.ID)
	if err != nil || s == nil {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	h.respondSubTraffic(w, s, req)
}

// userSubscriptionTraffic is the same history for one of the caller's own
// subscriptions.
func (h *Handler) userSubscriptionTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	uid, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("未登录"))
		return
	}
	var req subTrafficRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	s, err := h.repo.GetSubscription(req.ID)
	if err != nil || s == nil || s.UserID != uid {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	h.respondSubTraffic(w, s, req)
}
