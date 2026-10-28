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

func TestTrafficSpoolRecoversCursorAndSplitsInvalidItemsAcrossRestarts(t *testing.T) {
	oldDo, oldURL, oldAES := reportDo, httpReportURL, httpAESCrypto
	defer func() { reportDo, httpReportURL, httpAESCrypto = oldDo, oldURL, oldAES }()
	httpReportURL, httpAESCrypto = "http://panel.invalid/flow/upload?secret=test", nil
	path := filepath.Join(t.TempDir(), "traffic.json")
	spool, err := openTrafficSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	spool.state.Next = 8
	if err := spool.save(spool.state); err != nil {
		t.Fatal(err)
	}
	m := &GlobalTrafficManager{spool: spool, ctx: context.Background(), serviceTraffic: map[string]*ServiceTraffic{"good": {DownBytes: 100}, "bad": {DownBytes: 200}}}
	cursors := map[string]int64{}
	totals := map[string]int64{}
	sawGap, sawSplit := false, false
	reportDo = func(_ context.Context, req *http.Request, _ time.Duration) (*http.Response, error) {
		var batch trafficEnvelope
		if err := json.NewDecoder(req.Body).Decode(&batch); err != nil {
			t.Fatal(err)
		}
		status, reason := 200, ""
		if batch.Sequence > cursors[batch.Stream]+1 {
			status, reason = 409, "reset_stream"
			sawGap = true
		} else {
			for _, item := range batch.Items {
				if item.N == "bad" {
					status, reason = 400, "invalid_batch"
					if len(batch.Items) > 1 {
						sawSplit = true
					}
					break
				}
			}
			if status == 200 && batch.Sequence > cursors[batch.Stream] {
				for _, item := range batch.Items {
					totals[item.N] += item.D
				}
				cursors[batch.Stream] = batch.Sequence
			}
		}
		data, _ := json.Marshal(map[string]any{"stream": batch.Stream, "sequence": batch.Sequence, "error": reason})
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	}
	for i := 0; i < 12; i++ {
		if i == 1 {
			m.serviceTraffic["later"] = &ServiceTraffic{DownBytes: 25}
		}
		m.collectAndReport()
		recovered, err := openTrafficSpool(path)
		if err != nil {
			t.Fatal(err)
		}
		m = &GlobalTrafficManager{spool: recovered, ctx: context.Background(), serviceTraffic: map[string]*ServiceTraffic{}}
	}
	if !sawGap || !sawSplit || totals["good"] != 100 || totals["later"] != 25 || totals["bad"] != 0 {
		t.Fatalf("recovery gap=%v split=%v totals=%v", sawGap, sawSplit, totals)
	}
	files, err := filepath.Glob(path + ".rejected-*.json")
	if err != nil || len(files) != 1 {
		t.Fatalf("quarantine=%v %v", files, err)
	}
	var rejected struct{ Batch trafficEnvelope }
	data, _ := os.ReadFile(files[0])
	if err := json.Unmarshal(data, &rejected); err != nil {
		t.Fatal(err)
	}
	if len(rejected.Batch.Items) != 1 || rejected.Batch.Items[0].N != "bad" || rejected.Batch.Items[0].D != 200 {
		t.Fatalf("wrong quarantine: %+v", rejected)
	}
	if m.spool.state.Pending != nil || len(m.spool.state.Retry) > 0 || len(m.spool.state.Buffered) > 0 {
		t.Fatalf("queue stuck: %+v", m.spool.state)
	}
}

func TestTrafficRecoveryPersistenceFailureKeepsOriginalIdentity(t *testing.T) {
	for _, reason := range []trafficReportRejection{"reset_stream", "invalid_batch", "identity_conflict"} {
		t.Run(string(reason), func(t *testing.T) {
			s, err := openTrafficSpool("")
			if err != nil {
				t.Fatal(err)
			}
			s.state.Pending = &trafficEnvelope{Stream: s.state.Stream, Sequence: 1, Items: []TrafficReportItem{{N: "bad", D: 100}}}
			s.path = filepath.Join(t.TempDir(), "missing", "traffic.json")
			before, _ := json.Marshal(s.state)
			if err := s.recoverRejected(reason); err == nil {
				t.Fatal("expected persistence failure")
			}
			after, _ := json.Marshal(s.state)
			if string(before) != string(after) {
				t.Fatal("failed recovery mutated state")
			}
		})
	}
}

func TestReliableTrafficIgnoresUnboundErrors(t *testing.T) {
	oldDo, oldURL, oldAES := reportDo, httpReportURL, httpAESCrypto
	defer func() { reportDo, httpReportURL, httpAESCrypto = oldDo, oldURL, oldAES }()
	httpReportURL, httpAESCrypto = "http://panel.invalid/flow/upload?secret=test", nil
	batch := trafficEnvelope{Stream: strings.Repeat("d", 32), Sequence: 1}
	for _, body := range []string{`bad request`, `{"stream":"wrong","sequence":1,"error":"invalid_batch"}`, `{"stream":"` + batch.Stream + `","sequence":1,"error":"unknown"}`} {
		reportDo = func(context.Context, *http.Request, time.Duration) (*http.Response, error) {
			return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		err := sendReliableTrafficReport(context.Background(), batch)
		var rejection trafficReportRejection
		if err == nil || errors.As(err, &rejection) {
			t.Fatalf("unsafe error recovery: %v", err)
		}
	}
}
