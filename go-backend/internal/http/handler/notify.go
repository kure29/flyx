package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
)

// Notification events. An empty notify_events config enables all of them.
const (
	notifyEventNodeOffline = "node_offline"
	notifyEventInboundDown = "inbound_down"
	notifyEventKernelError = "kernel_error"
	notifyEventNodeExpiry  = "node_expiry"
	notifyEventTest        = "test"
)

const (
	notifyConfigEnabled     = "notify_enabled"
	notifyConfigEvents      = "notify_events"
	notifyConfigTGToken     = "notify_telegram_bot_token"
	notifyConfigTGChatID    = "notify_telegram_chat_id"
	notifyConfigTGAPIBase   = "notify_telegram_api_base"
	notifyConfigWebhookURL  = "notify_webhook_url"
	defaultTelegramAPIBase  = "https://api.telegram.org"
	notifySendTimeout       = 10 * time.Second
	notifyExpiryWindow      = 3 * 24 * time.Hour
	notifyInboundDownStreak = 2
)

// nodeOfflineGrace keeps short reconnects (agent restart, upgrade) quiet.
var nodeOfflineGrace = 90 * time.Second

type notifySettings struct {
	TelegramToken   string `json:"telegramToken"`
	TelegramChatID  string `json:"telegramChatId"`
	TelegramAPIBase string `json:"telegramApiBase"`
	WebhookURL      string `json:"webhookUrl"`
}

func (s notifySettings) empty() bool {
	return (s.TelegramToken == "" || s.TelegramChatID == "") && s.WebhookURL == ""
}

type notifyMessage struct {
	Event   string `json:"event"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Time    int64  `json:"time"`
	App     string `json:"app"`
}

// notifier tracks alert state so each problem is reported once and its
// recovery once.
type notifier struct {
	mu             sync.Mutex
	offlineTimers  map[int64]*time.Timer
	offlineAlerted map[int64]bool
	expiryAlerted  map[int64]int64
	// alerted holds keys of reminders already sent (traffic, expiry).
	alerted map[string]bool
	// muted holds incidents whose alert was not sent, so their recovery stays
	// quiet too.
	muted  map[string]bool
	client *http.Client
}

func newNotifier() *notifier {
	return &notifier{
		offlineTimers:  make(map[int64]*time.Timer),
		offlineAlerted: make(map[int64]bool),
		expiryAlerted:  make(map[int64]int64),
		alerted:        make(map[string]bool),
		muted:          make(map[string]bool),
		client:         &http.Client{Timeout: notifySendTimeout},
	}
}

// notifyConfig reads the saved settings; ok is false when notifications
// are off or the event is not selected.
func (h *Handler) notifyConfig(event string) (notifySettings, string, bool) {
	return h.notifyConfigForNode(0, event)
}

// Server overrides replace the event default, while the global master switch
// and delivery channels still apply to every notification.
func (h *Handler) notifyConfigForNode(nodeID int64, event string) (notifySettings, string, bool) {
	cfg, err := h.repo.ListConfigs()
	if err != nil {
		return notifySettings{}, "", false
	}
	app := strings.TrimSpace(cfg["app_name"])
	if app == "" {
		app = "FLYX"
	}
	settings := notifySettings{
		TelegramToken:   strings.TrimSpace(cfg[notifyConfigTGToken]),
		TelegramChatID:  strings.TrimSpace(cfg[notifyConfigTGChatID]),
		TelegramAPIBase: strings.TrimSpace(cfg[notifyConfigTGAPIBase]),
		WebhookURL:      strings.TrimSpace(cfg[notifyConfigWebhookURL]),
	}
	if cfg[notifyConfigEnabled] != "true" || settings.empty() {
		return settings, app, false
	}
	selected := true
	if events := strings.TrimSpace(cfg[notifyConfigEvents]); events != "" {
		selected = false
		for _, e := range strings.Split(events, ",") {
			if strings.TrimSpace(e) == event {
				selected = true
				break
			}
		}
	}
	if nodeID > 0 && isNodeNotifyEvent(event) {
		// A node that cannot be read keeps the global selection.
		if node, err := h.repo.GetNodeByID(nodeID); err == nil && node != nil {
			if enabled, exists := parseNodeNotifyEvents(node.NotifyEvents)[event]; exists {
				selected = enabled
			}
		}
	}
	return settings, app, selected
}

// notify sends an event in the background when it is enabled.
func (h *Handler) notify(event, title, message string) {
	h.notifyForNode(0, event, title, message)
}

// notifyForNode reports whether the event was enabled and sent.
func (h *Handler) notifyForNode(nodeID int64, event, title, message string) bool {
	if h == nil || h.repo == nil || h.notifier == nil {
		return false
	}
	settings, app, ok := h.notifyConfigForNode(nodeID, event)
	if !ok {
		return false
	}
	h.sendNotify(settings, app, event, title, message)
	return true
}

func (h *Handler) sendNotify(settings notifySettings, app, event, title, message string) {
	msg := notifyMessage{Event: event, Title: title, Message: message, Time: time.Now().UnixMilli(), App: app}
	go func() {
		if err := h.notifier.send(context.Background(), settings, msg); err != nil {
			log.Printf("notify %s failed: %v", event, err)
		}
	}()
}

// notifyIncident sends the alert of an incident and remembers when it was not
// sent, so incidentAlerted skips the matching recovery.
func (h *Handler) notifyIncident(nodeID int64, event string, keys []string, title, message string) {
	sent := h.notifyForNode(nodeID, event, title, message)
	if h == nil || h.notifier == nil {
		return
	}
	n := h.notifier
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, key := range keys {
		if sent {
			delete(n.muted, event+"\x00"+key)
		} else {
			n.muted[event+"\x00"+key] = true
		}
	}
}

// incidentAlerted reports whether the alert of an incident was sent and
// forgets it.
func (h *Handler) incidentAlerted(event, key string) bool {
	if h == nil || h.notifier == nil {
		return true
	}
	n := h.notifier
	n.mu.Lock()
	defer n.mu.Unlock()
	muted := n.muted[event+"\x00"+key]
	delete(n.muted, event+"\x00"+key)
	return !muted
}

// send delivers to every configured channel and reports the failures.
func (n *notifier) send(ctx context.Context, s notifySettings, msg notifyMessage) error {
	var errs []string
	if s.TelegramToken != "" && s.TelegramChatID != "" {
		if err := n.sendTelegram(ctx, s, msg); err != nil {
			errs = append(errs, "Telegram: "+err.Error())
		}
	}
	if s.WebhookURL != "" {
		if err := n.sendWebhook(ctx, s.WebhookURL, msg); err != nil {
			errs = append(errs, "Webhook: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "；"))
	}
	return nil
}

func notifyText(msg notifyMessage) string {
	return fmt.Sprintf("[%s] %s\n%s\n时间：%s", msg.App, msg.Title, msg.Message,
		time.UnixMilli(msg.Time).Format("2006-01-02 15:04:05 MST"))
}

func (n *notifier) sendTelegram(ctx context.Context, s notifySettings, msg notifyMessage) error {
	base := strings.TrimRight(s.TelegramAPIBase, "/")
	if base == "" {
		base = defaultTelegramAPIBase
	}
	body, _ := json.Marshal(map[string]interface{}{
		"chat_id":                  s.TelegramChatID,
		"text":                     notifyText(msg),
		"disable_web_page_preview": true,
	})
	var out struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	status, raw, err := n.post(ctx, base+"/bot"+s.TelegramToken+"/sendMessage", body)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, &out) == nil && !out.OK && out.Description != "" {
		return errors.New(out.Description)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

func (n *notifier) sendWebhook(ctx context.Context, url string, msg notifyMessage) error {
	payload := struct {
		notifyMessage
		Text string `json:"text"`
	}{msg, notifyText(msg)}
	body, _ := json.Marshal(payload)
	status, _, err := n.post(ctx, url, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

func (n *notifier) post(ctx context.Context, url string, body []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		// The error text carries the URL, which holds the bot token.
		var urlErr interface{ Timeout() bool }
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			return 0, nil, errors.New("请求超时")
		}
		return 0, nil, errors.New("无法连接")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	return resp.StatusCode, raw, nil
}

func (h *Handler) nodeLabel(nodeID int64) string {
	if node, err := h.repo.GetNodeByID(nodeID); err == nil && node != nil && node.Name != "" {
		return node.Name
	}
	return fmt.Sprintf("服务器 %d", nodeID)
}

// ---- node online / offline ----

func (h *Handler) notifyNodeOffline(nodeID int64) {
	n := h.notifier
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if t := n.offlineTimers[nodeID]; t != nil {
		t.Stop()
	}
	n.offlineTimers[nodeID] = time.AfterFunc(nodeOfflineGrace, func() {
		settings, app, enabled := h.notifyConfigForNode(nodeID, notifyEventNodeOffline)
		n.mu.Lock()
		delete(n.offlineTimers, nodeID)
		// A muted outage is not marked alerted, so its recovery stays quiet.
		if !enabled || n.offlineAlerted[nodeID] || (h.wsServer != nil && h.wsServer.IsNodeOnline(nodeID)) {
			n.mu.Unlock()
			return
		}
		n.offlineAlerted[nodeID] = true
		n.mu.Unlock()
		h.sendNotify(settings, app, notifyEventNodeOffline, "服务器掉线", "服务器："+h.nodeLabel(nodeID))
	})
}

func (h *Handler) notifyNodeOnline(nodeID int64) {
	n := h.notifier
	if n == nil {
		return
	}
	n.mu.Lock()
	if t := n.offlineTimers[nodeID]; t != nil {
		t.Stop()
		delete(n.offlineTimers, nodeID)
	}
	alerted := n.offlineAlerted[nodeID]
	delete(n.offlineAlerted, nodeID)
	n.mu.Unlock()
	if alerted {
		h.notifyForNode(nodeID, notifyEventNodeOffline, "服务器恢复在线", "服务器："+h.nodeLabel(nodeID))
	}
}

// ---- node expiry ----

// checkNodeExpiry reports nodes expiring within notifyExpiryWindow once per
// expiry date; dismissed reminders stay quiet.
func (h *Handler) checkNodeExpiry(now time.Time) {
	if h.notifier == nil {
		return
	}
	nodes, err := h.repo.ListNodesWithExpiry()
	if err != nil {
		return
	}
	for _, node := range nodes {
		if !node.ExpiryTime.Valid || node.ExpiryReminderDismissed == 1 {
			continue
		}
		expiry := time.UnixMilli(node.ExpiryTime.Int64)
		left := expiry.Sub(now)
		if left > notifyExpiryWindow {
			continue
		}
		h.notifier.mu.Lock()
		sent := h.notifier.expiryAlerted[node.ID] == node.ExpiryTime.Int64
		h.notifier.mu.Unlock()
		if sent {
			continue
		}
		// A muted reminder is not marked sent, so it fires once enabled.
		settings, app, enabled := h.notifyConfigForNode(node.ID, notifyEventNodeExpiry)
		if !enabled {
			continue
		}
		h.notifier.mu.Lock()
		h.notifier.expiryAlerted[node.ID] = node.ExpiryTime.Int64
		h.notifier.mu.Unlock()
		title := "服务器即将到期"
		detail := fmt.Sprintf("剩余约 %d 天", int(left.Hours()/24)+1)
		if left <= 0 {
			title = "服务器已到期"
			detail = "已过期"
		}
		h.sendNotify(settings, app, notifyEventNodeExpiry, title, fmt.Sprintf("服务器：%s\n到期：%s（%s）",
			node.Name, expiry.Format("2006-01-02"), detail))
	}
}

// ---- test ----

// notifyTest sends a test message using the settings in the request, falling
// back to the saved ones, so the form can be checked before saving.
func (h *Handler) notifyTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req notifySettings
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	saved, app, _ := h.notifyConfig(notifyEventTest)
	if req.TelegramToken == "" {
		req.TelegramToken = saved.TelegramToken
	}
	if req.TelegramChatID == "" {
		req.TelegramChatID = saved.TelegramChatID
	}
	if req.TelegramAPIBase == "" {
		req.TelegramAPIBase = saved.TelegramAPIBase
	}
	if req.WebhookURL == "" {
		req.WebhookURL = saved.WebhookURL
	}
	req.TelegramToken = strings.TrimSpace(req.TelegramToken)
	req.TelegramChatID = strings.TrimSpace(req.TelegramChatID)
	req.WebhookURL = strings.TrimSpace(req.WebhookURL)
	if req.empty() {
		response.WriteJSON(w, response.ErrDefault("请先填写 Telegram Bot Token 和 Chat ID，或 Webhook 地址"))
		return
	}
	msg := notifyMessage{
		Event: notifyEventTest, Title: "测试通知",
		Message: "收到这条消息说明通知配置正确。", Time: time.Now().UnixMilli(), App: app,
	}
	if err := h.notifier.send(r.Context(), req, msg); err != nil {
		response.WriteJSON(w, response.ErrDefault("发送失败："+err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}
