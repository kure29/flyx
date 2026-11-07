package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/store/model"
)

const (
	notifyEventUserQuota   = "user_quota"
	notifyEventDailyReport = "daily_report"

	notifyConfigDailyHour     = "notify_daily_hour"
	notifyConfigDailyLast     = "notify_daily_last"
	notifyConfigDailySnapshot = "notify_daily_inbound_total"

	notifyFlowWarnRatio  = 0.9
	notifyCheckInterval  = 10 * time.Minute
	defaultDailyHour     = 9
	notifyUserExpiryDays = 3
)

func formatBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

// alertOnce records key and reports whether it is new.
func (n *notifier) alertOnce(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.alerted[key] {
		return false
	}
	n.alerted[key] = true
	return true
}

// clearAlerts forgets keys with the prefix, so the alert can fire again
// (e.g. after a user's traffic was reset).
func (n *notifier) clearAlerts(prefix string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for k := range n.alerted {
		if strings.HasPrefix(k, prefix) {
			delete(n.alerted, k)
		}
	}
}

func expiryText(expMs int64, now time.Time) (string, bool) {
	exp := time.UnixMilli(expMs)
	left := exp.Sub(now)
	if left <= 0 {
		return exp.Format("2006-01-02 15:04") + "（已到期）", true
	}
	return fmt.Sprintf("%s（剩余约 %d 天）", exp.Format("2006-01-02 15:04"), int(left.Hours()/24)+1), false
}

// checkUserReminders reports shared subscriptions near their traffic limit
// or expiry.
func (h *Handler) checkUserReminders(now time.Time) {
	if h.notifier == nil {
		return
	}
	window := time.Duration(notifyUserExpiryDays) * 24 * time.Hour
	if subs, err := h.repo.ListSubscriptions(); err == nil {
		for _, s := range subs {
			if isShare(&s) && s.Enabled == 1 {
				h.checkSubQuota(&s)
			}
			if !isShare(&s) || s.Enabled != 1 || s.ExpireTime == 0 || time.UnixMilli(s.ExpireTime).Sub(now) > window {
				continue
			}
			if !h.notifier.alertOnce(fmt.Sprintf("sub:%d:%d", s.ID, s.ExpireTime)) {
				continue
			}
			text, expired := expiryText(s.ExpireTime, now)
			title := "分享订阅即将到期"
			if expired {
				title = "分享订阅已到期，已断开"
			}
			h.notify(notifyEventUserQuota, title, fmt.Sprintf("订阅：%s\n到期：%s", s.Name, text))
		}
	}
}

// checkSubQuota reports a share subscription near or at its traffic limit.
func (h *Handler) checkSubQuota(s *model.Subscription) {
	key := fmt.Sprintf("subflow:%d:", s.ID)
	used, limit := subUsed(s), s.TrafficLimit
	switch {
	case limit <= 0:
	case used >= limit:
		if h.notifier.alertOnce(key + "full") {
			h.notifier.alertOnce(key + "warn")
			h.notify(notifyEventUserQuota, "分享订阅流量已用完，已断开",
				fmt.Sprintf("订阅：%s\n已用：%s / %s", s.Name, formatBytes(used), formatBytes(limit)))
		}
	case float64(used) >= float64(limit)*notifyFlowWarnRatio:
		if h.notifier.alertOnce(key + "warn") {
			h.notify(notifyEventUserQuota, "分享订阅流量即将用完",
				fmt.Sprintf("订阅：%s\n已用：%s / %s（%.0f%%）", s.Name, formatBytes(used), formatBytes(limit),
					float64(used)*100/float64(limit)))
		}
	default:
		h.notifier.clearAlerts(key)
	}
}

// dailyReportHour is the configured hour (0-23) in the panel's time zone.
func (h *Handler) dailyReportHour() int {
	cfg, err := h.repo.GetConfigByName(notifyConfigDailyHour)
	if err != nil || cfg == nil || strings.TrimSpace(cfg.Value) == "" {
		return defaultDailyHour
	}
	v, err := strconv.Atoi(strings.TrimSpace(cfg.Value))
	if err != nil || v < 0 || v > 23 {
		return defaultDailyHour
	}
	return v
}

// maybeDailyReport sends the summary once a day at the configured hour.
// The last sent date is stored so restarts don't repeat it.
func (h *Handler) maybeDailyReport(now time.Time) {
	if h.notifier == nil || now.Hour() != h.dailyReportHour() {
		return
	}
	if _, _, ok := h.notifyConfig(notifyEventDailyReport); !ok {
		return
	}
	today := now.Format("2006-01-02")
	if cfg, err := h.repo.GetConfigByName(notifyConfigDailyLast); err == nil && cfg != nil && cfg.Value == today {
		return
	}
	_ = h.repo.UpsertConfig(notifyConfigDailyLast, today, now.UnixMilli())
	h.notify(notifyEventDailyReport, "每日汇总 "+today, h.dailyReportText(now))
}

func (h *Handler) dailyReportText(now time.Time) string {
	var lines []string
	if online, total, err := h.repo.CountNodesOnline(); err == nil && total > 0 {
		lines = append(lines, fmt.Sprintf("服务器在线：%d / %d", online, total))
	}
	if kernels, err := h.repo.ListNodeKernels(0); err == nil && len(kernels) > 0 {
		bad := 0
		for _, k := range kernels {
			if k.Status == kernelStatusError {
				bad++
			}
		}
		lines = append(lines, fmt.Sprintf("内核出错：%d / %d", bad, len(kernels)))
	}
	if h.reachProbes != nil {
		h.reachProbes.mu.RLock()
		down, checked := 0, 0
		for _, p := range h.reachProbes.results {
			if p.Skipped {
				continue
			}
			checked++
			if !p.OK {
				down++
			}
		}
		h.reachProbes.mu.RUnlock()
		if checked > 0 {
			lines = append(lines, fmt.Sprintf("节点连不上：%d / %d", down, checked))
		}
	}
	if total, err := h.repo.SumInboundTraffic(); err == nil {
		text := "入站流量（累计）：" + formatBytes(total)
		if cfg, err := h.repo.GetConfigByName(notifyConfigDailySnapshot); err == nil && cfg != nil {
			if prev, err := strconv.ParseInt(cfg.Value, 10, 64); err == nil && total >= prev {
				text = "入站流量（过去一天）：" + formatBytes(total-prev)
			}
		}
		lines = append(lines, text)
		_ = h.repo.UpsertConfig(notifyConfigDailySnapshot, strconv.FormatInt(total, 10), now.UnixMilli())
	}
	if flow, err := h.repo.SumStatisticsFlowSince(now.Add(-24 * time.Hour).UnixMilli()); err == nil && flow > 0 {
		lines = append(lines, "转发流量（过去 24 小时）："+formatBytes(flow))
	}
	if subs, err := h.repo.ListSubscriptions(); err == nil {
		active, soon, total := 0, 0, 0
		for i := range subs {
			if !isShare(&subs[i]) {
				continue
			}
			total++
			if subActive(&subs[i], now) {
				active++
				if subs[i].ExpireTime > 0 && time.UnixMilli(subs[i].ExpireTime).Sub(now) <= notifyUserExpiryDays*24*time.Hour {
					soon++
				}
			}
		}
		if total > 0 {
			lines = append(lines, fmt.Sprintf("分享订阅：%d 个有效，%d 个 3 天内到期", active, soon))
		}
	}
	if len(lines) == 0 {
		return "暂无数据"
	}
	return strings.Join(lines, "\n")
}

// runNotifyLoop runs the periodic reminders: node and user expiry,
// traffic limits and the daily summary.
func (h *Handler) runNotifyLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(notifyCheckInterval)
	defer ticker.Stop()
	for {
		now := time.Now()
		h.checkNodeExpiry(now)
		h.checkUserReminders(now)
		h.maybeDailyReport(now)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
