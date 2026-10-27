package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrafficSpoolRetriesSameBatchAcrossRestartAndLostAcknowledgement(t *testing.T) {
	oldDo, oldURL, oldAES := reportDo, httpReportURL, httpAESCrypto
	defer func() { reportDo, httpReportURL, httpAESCrypto = oldDo, oldURL, oldAES }()
	httpReportURL = "http://panel.invalid/flow/upload?secret=test"
	httpAESCrypto = nil
	path := filepath.Join(t.TempDir(), "traffic.json")
	spool, err := openTrafficSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	m := &GlobalTrafficManager{spool: spool, ctx: context.Background(), serviceTraffic: map[string]*ServiceTraffic{"closed": {UpBytes: 40, DownBytes: 100}}}
	var batches []trafficEnvelope
	fail := true
	reportDo = func(_ context.Context, req *http.Request, _ time.Duration) (*http.Response, error) {
		if req.URL.Path != "/flow/upload/v2" {
			t.Errorf("unsafe legacy fallback: %s", req.URL.Path)
		}
		var batch trafficEnvelope
		if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
			t.Fatal(err)
		}
		batches = append(batches, batch)
		if fail {
			return nil, errors.New("response lost after server committed")
		}
		data, _ := json.Marshal(map[string]any{"stream": batch.Stream, "sequence": batch.Sequence})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	}
	m.collectAndReport()
	recovered, err := openTrafficSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.state.Pending == nil {
		t.Fatal("pending upload lost on restart")
	}
	m = &GlobalTrafficManager{spool: recovered, ctx: context.Background(), serviceTraffic: map[string]*ServiceTraffic{"closed": {DownBytes: 25}}}
	fail = false
	m.collectAndReport()
	m.collectAndReport()
	if len(batches) != 3 || batches[0].Stream != batches[1].Stream || batches[0].Sequence != batches[1].Sequence || batches[2].Sequence != 2 {
		t.Fatalf("retry identities changed: %+v", batches)
	}
	if batches[1].Items[0].D != 100 || batches[2].Items[0].D != 25 {
		t.Fatalf("pending and new traffic mixed: %+v", batches)
	}
	final, err := openTrafficSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	if final.state.Pending != nil || len(final.state.Buffered) != 0 || final.state.Next != 3 {
		t.Fatalf("ACK not persisted: %+v", final.state)
	}
}

func TestTrafficSpoolWriteFailureKeepsMemory(t *testing.T) {
	spool, err := openTrafficSpool("")
	if err != nil {
		t.Fatal(err)
	}
	spool.path = filepath.Join(t.TempDir(), "missing", "traffic.json")
	m := &GlobalTrafficManager{spool: spool, serviceTraffic: map[string]*ServiceTraffic{"s": {DownBytes: 100}}}
	m.collectAndReport()
	if _, down := m.GetServiceTraffic("s"); down != 100 {
		t.Fatal("failed persistence drained traffic")
	}
}

func TestReliableTrafficFallbackRequiresMatchingAcknowledgement(t *testing.T) {
	oldDo, oldURL, oldAES := reportDo, httpReportURL, httpAESCrypto
	oldPreferred := loadPreferredURL(&preferredUploadURL)
	defer func() {
		reportDo, httpReportURL, httpAESCrypto = oldDo, oldURL, oldAES
		storePreferredURL(&preferredUploadURL, oldPreferred)
	}()
	storePreferredURL(&preferredUploadURL, "")
	httpReportURL, httpAESCrypto = "http://first.invalid/flow/upload?secret=test,http://second.invalid/flow/upload?secret=test", nil
	batch := trafficEnvelope{Stream: strings.Repeat("b", 32), Sequence: 7, Items: []TrafficReportItem{{N: "s", D: 100}}}
	calls := 0
	valid := false
	reportDo = func(_ context.Context, req *http.Request, _ time.Duration) (*http.Response, error) {
		calls++
		var received trafficEnvelope
		if err := json.NewDecoder(req.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		if received.Stream != batch.Stream || received.Sequence != batch.Sequence || received.Items[0].D != 100 {
			t.Fatal("fallback changed the retry identity")
		}
		if req.URL.Path != "/flow/upload/v2" || req.URL.Query().Get("secret") != "test" {
			t.Fatal("wrong fallback endpoint")
		}
		body := "ok" // Old panels may answer 200; that is not a commit ACK.
		if valid && calls%2 == 0 {
			data, _ := json.Marshal(batch)
			body = string(data)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
	if err := sendReliableTrafficReport(context.Background(), batch); err == nil {
		t.Fatal("accepted a legacy acknowledgement")
	}
	valid = true
	if err := sendReliableTrafficReport(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("fallback calls=%d", calls)
	}
}

func TestTrafficSpoolStopPersistsAndCorruptionIsNotOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	spool, err := openTrafficSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	m := &GlobalTrafficManager{spool: spool, serviceTraffic: map[string]*ServiceTraffic{"s": {DownBytes: 100}}}
	m.Stop()
	saved, err := openTrafficSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.state.Pending == nil || saved.state.Pending.Items[0].D != 100 {
		t.Fatal("shutdown discarded pending traffic")
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openTrafficSpool(path); err == nil {
		t.Fatal("silently discarded a corrupt spool")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "broken" {
		t.Fatal("overwrote corrupt evidence")
	}
}
