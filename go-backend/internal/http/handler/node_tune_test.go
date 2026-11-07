package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNodeTuneRejectsBadRequests(t *testing.T) {
	for body, want := range map[string]string{
		`{"id":0}`:                                     "参数错误",
		`{"id":1,"action":"apply"}`:                    "至少选择",
		`{"id":1,"action":"reboot"}`:                   "未知操作",
		`{"id":1,"action":"deep"}`:                     "确认深度优化",
		`{"id":1,"action":"deep","maxTrafficGB":1001}`: "确认深度优化",
		`{"id":1,"extra":true}`:                        "参数错误",
		`{"id":1,"bandwidthMbps":1000,"rttMs":0}`:      "延迟需在",
		`{"id":1,"bandwidthMbps":0,"rttMs":150}`:       "带宽需在",
	} {
		rec := httptest.NewRecorder()
		(&Handler{}).nodeTune(rec, httptest.NewRequest(http.MethodPost, "/api/v1/node/tune", strings.NewReader(body)))
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s: %s", body, rec.Body.String())
		}
	}
}

func TestNodeTuneAutomaticCommandTransport(t *testing.T) {
	agent := newCleanupAgent(t)
	for _, item := range []struct{ body, command, mode string }{
		{`{"id":1,"action":"auto"}`, "TuneStart", "auto"},
		{`{"id":1,"action":"deep","maxTrafficGB":40}`, "TuneStart", "deep"},
		{`{"id":1,"action":"restoreAuto"}`, "TuneStart", "restore"},
		{`{"id":1,"action":"cancel"}`, "TuneCancel", ""},
		{`{"id":1,"action":"status"}`, "TuneStatus", ""},
	} {
		rec := httptest.NewRecorder()
		agent.h.nodeTune(rec, httptest.NewRequest(http.MethodPost, "/api/v1/node/tune", strings.NewReader(item.body)))
		var envelope struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 {
			t.Fatalf("%s: %s", item.body, rec.Body.String())
		}
		commands := agent.commandsOfType(item.command)
		if len(commands) == 0 {
			t.Fatalf("missing %s", item.command)
		}
		var data struct {
			Mode          string  `json:"mode"`
			MaxTrafficGB  float64 `json:"maxTrafficGB"`
			BandwidthMbps int     `json:"bandwidthMbps"`
		}
		if err := json.Unmarshal(commands[len(commands)-1].Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.Mode != item.mode || data.BandwidthMbps != 0 {
			t.Fatalf("wrong command: %+v", data)
		}
		if item.mode == "deep" && data.MaxTrafficGB != 40 {
			t.Fatal("traffic budget not forwarded")
		}
	}
}
