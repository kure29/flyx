package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

const testLatencyPool = `[
	{"name":"电信","host":"ct.example.com","port":80,"enabled":true,"pinned":true},
	{"name":"联通","host":"cu.example.com","port":80,"enabled":true},
	{"name":"移动","host":"cm.example.com","port":80,"enabled":false}]`

func TestTargetsForNode(t *testing.T) {
	pool, err := parseLatencyTargets(testLatencyPool)
	if err != nil {
		t.Fatal(err)
	}
	names := func(ts []latencyTarget) string {
		var out []string
		for _, t := range ts {
			n := t.Name
			if t.Pinned {
				n += "*"
			}
			out = append(out, n)
		}
		return strings.Join(out, ",")
	}
	for raw, want := range map[string]string{
		"": "电信*,联通",
		`{"points":["移动","电信"],"pinned":["移动"]}`: "电信,移动*",
		`{"points":["已删除"]}`:                     "电信*,联通",
		`{"points":[],"pinned":[]}`:              "",
		`not json`:                               "电信*,联通",
	} {
		if got := names(targetsForNode(pool, raw)); got != want {
			t.Errorf("%q: got %s, want %s", raw, got, want)
		}
	}
}

func postTargets(t *testing.T, h *Handler, body string) map[string]interface{} {
	t.Helper()
	w := httptest.NewRecorder()
	h.nodeLatencyTargetsSet(w, httptest.NewRequest(http.MethodPost, "/api/v1/node/latency/targets", bytes.NewBufferString(body)))
	var res map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res
}

func nodeChoice(t *testing.T, h *Handler, id int64) string {
	t.Helper()
	nodes, err := h.repo.ListMonitorNodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.ID == id {
			return n.LatencyTargets
		}
	}
	t.Fatalf("node %d not found", id)
	return ""
}

func TestNodeLatencyTargetsSet(t *testing.T) {
	h, r := setupLatencyHandler(t, testLatencyPool)
	a := mustCreateLatencyNode(t, r, "a", 1, 0)
	b := mustCreateLatencyNode(t, r, "b", 1, 0)
	calls := stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		return map[string]interface{}{"success": true, "averageTime": 20.0}, nil
	})

	for body, want := range map[string]string{
		`{"ids":[1],"custom":true,"points":["不存在"]}`: "测试点不存在",
		`{"ids":[],"custom":false}`:                  "参数错误",
	} {
		if res := postTargets(t, h, body); res["code"].(float64) == 0 || !strings.Contains(res["msg"].(string), want) {
			t.Errorf("%s: %v", body, res)
		}
	}

	res := postTargets(t, h, `{"ids":[`+itoa(a)+`,`+itoa(b)+`],"custom":true,"points":["移动","移动","联通"],"pinned":["移动","电信"]}`)
	if res["code"].(float64) != 0 {
		t.Fatalf("save: %v", res)
	}
	want := `{"points":["移动","联通"],"pinned":["移动"]}`
	if got := nodeChoice(t, h, a); got != want {
		t.Fatalf("stored %s, want %s", got, want)
	}
	// The save measured both nodes' own points right away, not the defaults.
	if len(*calls) != 4 || strings.Contains(strings.Join(*calls, ","), "ct.example.com") {
		t.Fatalf("measured %v", *calls)
	}
	if p, ok := h.nodeLatency.get(a); !ok || len(p.Points) != 2 || !p.Points[1].Pinned {
		t.Fatalf("latency after save = %+v", p)
	}

	if res := postTargets(t, h, `{"ids":[`+itoa(b)+`],"custom":false}`); res["code"].(float64) != 0 {
		t.Fatalf("clear: %v", res)
	}
	if got := nodeChoice(t, h, b); got != "" {
		t.Fatalf("cleared choice = %q", got)
	}

	// No points: the node stops measuring and its old result goes away.
	*calls = nil
	if res := postTargets(t, h, `{"ids":[`+itoa(a)+`],"custom":true,"points":[]}`); res["code"].(float64) != 0 {
		t.Fatalf("none: %v", res)
	}
	if got := nodeChoice(t, h, a); got != `{"points":[],"pinned":[]}` {
		t.Fatalf("stored %s", got)
	}
	if _, ok := h.nodeLatency.get(a); ok || len(*calls) != 0 {
		t.Fatalf("a node measuring nothing still has a result / was measured: %v", *calls)
	}
}

func TestSyncNodeTargetChoicesFollowsRenames(t *testing.T) {
	h, r := setupLatencyHandler(t, testLatencyPool)
	a := mustCreateLatencyNode(t, r, "a", 1, 0)
	b := mustCreateLatencyNode(t, r, "b", 1, 0)
	if err := r.SetNodesLatencyTargets([]int64{a}, `{"points":["电信","移动"],"pinned":["电信"]}`); err != nil {
		t.Fatal(err)
	}
	if err := r.SetNodesLatencyTargets([]int64{b}, `{"points":["移动"],"pinned":[]}`); err != nil {
		t.Fatal(err)
	}
	none := mustCreateLatencyNode(t, r, "none", 1, 0)
	if err := r.SetNodesLatencyTargets([]int64{none}, `{"points":[],"pinned":[]}`); err != nil {
		t.Fatal(err)
	}
	prev := h.latencyPool()
	// 电信 renamed (same address), 移动 removed.
	next, err := parseLatencyTargets(`[
		{"name":"广东电信","host":"ct.example.com","port":80,"enabled":true},
		{"name":"联通","host":"cu.example.com","port":80,"enabled":true}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got := renamedTargets(prev, next); len(got) != 1 || got["电信"] != "广东电信" {
		t.Fatalf("renames = %v", got)
	}
	h.syncNodeTargetChoices(prev, next)
	if got := nodeChoice(t, h, a); got != `{"points":["广东电信"],"pinned":["广东电信"]}` {
		t.Fatalf("renamed choice = %s", got)
	}
	if got := nodeChoice(t, h, b); got != "" {
		t.Fatalf("a choice left empty follows the defaults, got %s", got)
	}
	if got := nodeChoice(t, h, none); got != `{"points":[],"pinned":[]}` {
		t.Fatalf("a node measuring nothing on purpose keeps it, got %s", got)
	}
}

func TestProbeNodeLatencyUsesEachNodesPoints(t *testing.T) {
	h, r := setupLatencyHandler(t, testLatencyPool)
	own := mustCreateLatencyNode(t, r, "own", 1, 0)
	def := mustCreateLatencyNode(t, r, "default", 1, 0)
	if err := r.SetNodesLatencyTargets([]int64{own}, `{"points":["移动"],"pinned":["移动"]}`); err != nil {
		t.Fatal(err)
	}
	stubLatencyPing(t, func(int64, string) (map[string]interface{}, error) {
		return map[string]interface{}{"success": true, "averageTime": 10.0}, nil
	})
	results, err := h.probeNodeLatency(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if p := results[own]; len(p.Points) != 1 || p.Points[0].Name != "移动" || !p.Points[0].Pinned {
		t.Fatalf("own node points = %+v", p.Points)
	}
	if p := results[def]; len(p.Points) != 2 || p.Points[0].Name != "电信" {
		t.Fatalf("default node points = %+v", p.Points)
	}
}
