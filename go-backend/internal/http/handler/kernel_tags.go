package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
)

// Tags are free-form labels ("出站", "落地", "香港") on inbounds and imported
// nodes; the list filters by them. They are stored comma-separated.

const (
	maxInboundTags   = 10
	maxInboundTagLen = 20
)

// normalizeTags trims, de-duplicates and validates tags, returning them
// joined for storage.
func normalizeTags(tags []string) (string, error) {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		if strings.ContainsAny(t, ",，") {
			return "", errors.New("标签不能包含逗号")
		}
		if len([]rune(t)) > maxInboundTagLen {
			return "", fmt.Errorf("标签“%s”超过 %d 字", t, maxInboundTagLen)
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > maxInboundTags {
		return "", fmt.Errorf("最多 %d 个标签", maxInboundTags)
	}
	return strings.Join(out, ","), nil
}

func splitTags(s string) []string {
	out := []string{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// inboundTags replaces the tags of one or more inbounds.
func (h *Handler) inboundTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		IDs  []int64  `json:"ids"`
		Tags []string `json:"tags"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if len(req.IDs) == 0 {
		response.WriteJSON(w, response.ErrDefault("ids不能为空"))
		return
	}
	tags, err := normalizeTags(req.Tags)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	n, err := h.repo.SetNodeInboundTags(req.IDs, tags, time.Now().UnixMilli())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if n == 0 {
		response.WriteJSON(w, response.ErrDefault("节点服务不存在"))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}
