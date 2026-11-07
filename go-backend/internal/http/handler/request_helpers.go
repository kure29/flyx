package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/http/response"
)

type batchFailureDetail struct {
	ID     int64  `json:"id"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

type batchOperationResult struct {
	SuccessCount int                  `json:"successCount"`
	FailCount    int                  `json:"failCount"`
	Failures     []batchFailureDetail `json:"failures,omitempty"`
}

func appendBatchFailure(failures []batchFailureDetail, id int64, name string, err error) []batchFailureDetail {
	reason := normalizeBatchFailureReason(errString(err))
	if reason == "" {
		reason = "未知错误"
	}
	return append(failures, batchFailureDetail{
		ID:     id,
		Name:   strings.TrimSpace(name),
		Reason: reason,
	})
}

func appendBatchFailureReason(failures []batchFailureDetail, id int64, name, reason string) []batchFailureDetail {
	normalized := normalizeBatchFailureReason(reason)
	if normalized == "" {
		normalized = "未知错误"
	}
	return append(failures, batchFailureDetail{
		ID:     id,
		Name:   strings.TrimSpace(name),
		Reason: normalized,
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func normalizeBatchFailureReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return ""
	}
	if strings.EqualFold(trimmed, errForwardNotFound.Error()) {
		return "转发不存在"
	}
	return trimmed
}

func asAnySlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	if arr, ok := v.([]interface{}); ok {
		return arr
	}
	return nil
}

func asMapSlice(v interface{}) []map[string]interface{} {
	arr := asAnySlice(v)
	if arr == nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func asMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok && m != nil {
		return m
	}
	return map[string]interface{}{}
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int, int32, int64:
		return fmt.Sprintf("%v", t)
	default:
		b, _ := json.Marshal(t)
		return strings.Trim(string(b), "\"")
	}
}

func asInt(v interface{}, def int) int {
	s := asString(v)
	if s == "" {
		return def
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}

func asInt64(v interface{}, def int64) int64 {
	s := asString(v)
	if s == "" {
		return def
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return i
}

func asFloat(v interface{}, def float64) float64 {
	s := asString(v)
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

func idFromBody(r *http.Request, w http.ResponseWriter) int64 {
	return asInt64FromBodyKey(r, w, "id")
}

func asInt64FromBodyKey(r *http.Request, w http.ResponseWriter, key string) int64 {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return 0
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return 0
	}
	id := asInt64(req[key], 0)
	if id <= 0 {
		response.WriteJSON(w, response.ErrDefault("参数错误"))
		return 0
	}
	return id
}

func idsFromBody(r *http.Request, w http.ResponseWriter) []int64 {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return nil
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return nil
	}
	arr := asAnySlice(req["ids"])
	if len(arr) == 0 {
		response.WriteJSON(w, response.ErrDefault("ids不能为空"))
		return nil
	}
	ids := make([]int64, 0, len(arr))
	for _, x := range arr {
		id := asInt64(x, 0)
		if id > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func nullableText(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func nullableUnixMilli(v int64) interface{} {
	if v <= 0 {
		return nil
	}
	return v
}

func defaultString(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

func asInt64Slice(v interface{}) []int64 {
	arr := asAnySlice(v)
	if len(arr) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(arr))
	for _, x := range arr {
		if id := asInt64(x, 0); id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func parsePortRangeMinMax(input string) (int, int) {
	input = strings.TrimSpace(input)
	if input == "" {
		return 0, 0
	}
	minPort, maxPort := 0, 0
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			r := strings.SplitN(part, "-", 2)
			if len(r) != 2 {
				continue
			}
			start, err1 := strconv.Atoi(strings.TrimSpace(r[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(r[1]))
			if err1 != nil || err2 != nil || start <= 0 || end <= 0 {
				continue
			}
			if end < start {
				start, end = end, start
			}
			if minPort == 0 || start < minPort {
				minPort = start
			}
			if maxPort == 0 || end > maxPort {
				maxPort = end
			}
			continue
		}
		p, err := strconv.Atoi(part)
		if err != nil || p <= 0 {
			continue
		}
		if minPort == 0 || p < minPort {
			minPort = p
		}
		if maxPort == 0 || p > maxPort {
			maxPort = p
		}
	}
	return minPort, maxPort
}

func asBool(v interface{}, def bool) bool {
	s := strings.TrimSpace(strings.ToLower(asString(v)))
	if s == "" {
		return def
	}
	switch s {
	case "1", "t", "true", "yes", "y":
		return true
	case "0", "f", "false", "no", "n":
		return false
	default:
		return def
	}
}
