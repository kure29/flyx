package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGlobalTrafficManagerRemoveServicesDropsCachedEntries(t *testing.T) {
	m := &GlobalTrafficManager{serviceTraffic: map[string]*ServiceTraffic{
		"svc-a": {ServiceName: "svc-a"},
		"svc-b": {ServiceName: "svc-b"},
	}}

	m.RemoveServices("svc-a")

	if _, ok := m.serviceTraffic["svc-a"]; ok {
		t.Fatalf("expected svc-a traffic entry to be removed")
	}
	if _, ok := m.serviceTraffic["svc-b"]; !ok {
		t.Fatalf("expected svc-b traffic entry to remain")
	}
}

func TestGlobalTrafficManagerRetainServicesDropsStaleEntries(t *testing.T) {
	m := &GlobalTrafficManager{serviceTraffic: map[string]*ServiceTraffic{
		"svc-a": {ServiceName: "svc-a"},
		"svc-b": {ServiceName: "svc-b"},
	}}

	m.RetainServices(map[string]struct{}{"svc-b": {}})

	if _, ok := m.serviceTraffic["svc-a"]; ok {
		t.Fatalf("expected stale svc-a traffic entry to be removed")
	}
	if _, ok := m.serviceTraffic["svc-b"]; !ok {
		t.Fatalf("expected active svc-b traffic entry to remain")
	}
}

func TestGlobalTrafficManagerAddTrafficIgnoresUnregisteredService(t *testing.T) {
	m := &GlobalTrafficManager{serviceTraffic: make(map[string]*ServiceTraffic)}

	m.AddTraffic("deleted-service", 10, 20)

	if _, ok := m.serviceTraffic["deleted-service"]; ok {
		t.Fatalf("expected unregistered service traffic to be ignored")
	}
}

func TestGlobalTrafficManagerCollectAndReportDropsStaleEntriesAfterReporting(t *testing.T) {
	origReportDo := reportDo
	origReportURL := httpReportURL
	origAESCrypto := httpAESCrypto
	defer func() {
		reportDo = origReportDo
		httpReportURL = origReportURL
		httpAESCrypto = origAESCrypto
	}()

	httpReportURL = "http://panel.example.com/flow/upload?secret=abc"
	httpAESCrypto = nil

	var requestBody string
	reportDo = func(_ context.Context, req *http.Request, _ time.Duration) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestBody = string(body)
		var batch trafficEnvelope
		if err := json.Unmarshal(body, &batch); err != nil {
			t.Fatal(err)
		}
		ack, _ := json.Marshal(map[string]any{"stream": batch.Stream, "sequence": batch.Sequence})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(ack))),
		}, nil
	}

	m := &GlobalTrafficManager{
		serviceTraffic: map[string]*ServiceTraffic{
			"stale-service": {ServiceName: "stale-service", UpBytes: 10, DownBytes: 20},
		},
		ctx: context.Background(),
	}

	m.collectAndReport()

	if !strings.Contains(requestBody, "stale-service") {
		t.Fatalf("expected pending stale traffic to be reported first, body=%s", requestBody)
	}
	if _, ok := m.serviceTraffic["stale-service"]; ok {
		t.Fatalf("expected stale traffic entry to be removed after report collection")
	}
}
