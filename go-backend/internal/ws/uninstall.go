package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"go-backend/internal/security"
)

const uninstallReplyTimeout = 30 * time.Second

// serveUninstalledNode handles an agent whose server was deleted while it was
// offline: it is sent UninstallAgent and never registered as a node. It
// reports false, leaving the request untouched, when nothing is pending.
func (s *Server) serveUninstalledNode(w http.ResponseWriter, r *http.Request, secret string) bool {
	pending, err := s.repo.GetNodeUninstallBySecret(secret)
	if err != nil || pending == nil {
		return false
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return true
	}
	defer conn.Close()

	crypto, _ := security.NewAESCrypto(secret)
	requestID := fmt.Sprintf("uninstall_%d", time.Now().UnixNano())
	payload, err := json.Marshal(map[string]interface{}{"type": "UninstallAgent", "data": nil, "requestId": requestID})
	if err != nil {
		return true
	}
	if crypto != nil {
		encrypted, err := crypto.Encrypt(payload)
		if err != nil {
			return true
		}
		if payload, err = json.Marshal(map[string]interface{}{"encrypted": true, "data": encrypted, "timestamp": time.Now().UnixMilli()}); err != nil {
			return true
		}
	}
	if err := writeWSMessage(&connWrap{conn: conn}, websocket.TextMessage, payload); err != nil {
		return true
	}

	_ = conn.SetReadDeadline(time.Now().Add(uninstallReplyTimeout))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return true // Still pending; retried on the next connection.
		}
		text := decryptIfNeeded(raw, crypto, secret)
		if !strings.Contains(text, requestID) {
			continue
		}
		var resp commandResponse
		if json.Unmarshal([]byte(text), &resp) != nil || resp.RequestID != requestID {
			continue
		}
		switch {
		case resp.Success:
			log.Printf("deleted server %q: agent uninstalled after reconnecting", pending.NodeName)
		case strings.Contains(resp.Message, "未知命令"):
			// An old agent that cannot uninstall would otherwise be asked again
			// on every reconnect.
			log.Printf("deleted server %q: agent too old to uninstall itself; remove it manually", pending.NodeName)
		default:
			log.Printf("deleted server %q: agent uninstall failed: %s", pending.NodeName, resp.Message)
			return true
		}
		_ = s.repo.DeleteNodeUninstall(secret)
		return true
	}
}
