package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestRemoteSourceNodeOrderPersistsAcrossRefresh(t *testing.T) {
	repository, err := repo.Open(t.TempDir() + "/source-order.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	h := New(repository, "unit-test-secret")
	save := func(body map[string]any, wantSuccess bool) json.RawMessage {
		t.Helper()
		body["kind"] = "file"
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/source/save", bytes.NewReader(payload))
		recorder := httptest.NewRecorder()
		h.remoteSourceSave(recorder, request)
		var result struct {
			Code int             `json:"code"`
			Msg  string          `json:"msg"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if (result.Code == 0) != wantSuccess {
			t.Fatalf("response code %d: %s", result.Code, result.Msg)
		}
		return result.Data
	}
	readSourceID := func(data json.RawMessage) int64 {
		t.Helper()
		var result struct{ Source remoteSourceView }
		if err := json.Unmarshal(data, &result); err != nil || result.Source.ID == 0 {
			t.Fatalf("missing saved source: %s (%v)", data, err)
		}
		return result.Source.ID
	}
	rowsByName := func() map[string]model.NodeInbound {
		t.Helper()
		rows, err := repository.ListNodeInbounds()
		if err != nil {
			t.Fatal(err)
		}
		nodes := make(map[string]model.NodeInbound, len(rows))
		for _, row := range rows {
			nodes[row.Name] = row
		}
		return nodes
	}
	group := &model.NodeGroup{}
	assertOutput := func(want ...string) {
		t.Helper()
		context, err := h.loadSubContext()
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, node := range h.groupNodes(context, group, nil) {
			got = append(got, node.Name)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("node group output = %v, want %v", got, want)
		}
	}

	firstLine := "trojan://password@a.example.com:443#first"
	secondLine := "trojan://password@b.example.com:443#second"
	thirdLine := "trojan://password@c.example.com:443#third"
	sourceID := readSourceID(save(map[string]any{"name": "source", "content": firstLine + "\n" + secondLine}, true))
	otherID := readSourceID(save(map[string]any{"name": "other", "content": "trojan://password@d.example.com:443#other"}, true))
	group.SourceIDs = formatIDList([]int64{sourceID})
	assertOutput("first", "second")
	nodes := rowsByName()
	firstID, secondID := nodes["first"].ID, nodes["second"].ID
	if nodes["other"].SourceID != otherID {
		t.Fatal("missing unrelated node")
	}
	save(map[string]any{"id": sourceID, "name": "source", "nodeOrder": []int64{secondID, firstID}}, true)
	assertOutput("second", "first")
	save(map[string]any{"id": sourceID, "name": "changed", "nodeOrder": []int64{firstID, nodes["other"].ID}}, false)
	assertOutput("second", "first")
	if source, err := repository.GetRemoteSource(sourceID); err != nil || source == nil || source.Name != "source" {
		t.Fatalf("invalid order must not persist source changes: %+v (%v)", source, err)
	}
	save(map[string]any{"id": sourceID, "name": "source", "content": thirdLine + "\n" + firstLine + "\n" + secondLine}, true)
	assertOutput("second", "first", "third")
	save(map[string]any{"id": sourceID, "name": "source", "content": thirdLine + "\n" + firstLine}, true)
	assertOutput("first", "third")
}
