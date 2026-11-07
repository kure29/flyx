package handler

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-backend/internal/geoip"
	"go-backend/internal/store/model"
)

// The test database (internal/geoip/testdata) maps 203.0.113.0/24 → JP,
// 198.51.100.0/24 → HK and 2001:db8:1::/48 → US.
func testGeoIP(t *testing.T) *geoip.DB {
	t.Helper()
	data, err := os.ReadFile("../../geoip/testdata/test-country.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), geoip.FileName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	db := geoip.New(path, false)
	if err := db.Load(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func TestNormalizeNodeRegion(t *testing.T) {
	for in, want := range map[string]string{"hk": "HK", " Jp ": "JP", "": "", "USA": "", "1A": "", "香港": ""} {
		if got := normalizeNodeRegion(in); got != want {
			t.Errorf("normalizeNodeRegion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectNodeRegions(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	h.SetGeoIP(testGeoIP(t))
	now := time.Now()
	nodes := []model.Node{
		{Name: "ip", ServerIP: "203.0.113.7"},
		{Name: "v4 first", ServerIP: "2001:db8:1::5", ServerIPV4: sql.NullString{String: "198.51.100.1", Valid: true}},
		{Name: "v6 only", ServerIP: "", ServerIPV6: sql.NullString{String: "[2001:db8:1::9]", Valid: true}},
		{Name: "private", ServerIP: "10.0.0.8"},
		{Name: "admin's choice", ServerIP: "203.0.113.8", Region: "SG"},
		{Name: "remote", ServerIP: "203.0.113.9", IsRemote: 1},
	}
	for i := range nodes {
		nodes[i].Secret, nodes[i].Port, nodes[i].Status, nodes[i].CreatedTime = "s"+nodes[i].Name, "1000", 1, now.UnixMilli()
		if err := r.DB().Create(&nodes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	h.detectNodeRegions(context.Background(), false)
	got := func() map[string]model.Node {
		var rows []model.Node
		if err := r.DB().Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		out := map[string]model.Node{}
		for _, n := range rows {
			out[n.Name] = n
		}
		return out
	}
	want := map[string][2]string{ // detected, from
		"ip":             {"JP", "203.0.113.7"},
		"v4 first":       {"HK", "198.51.100.1"},
		"v6 only":        {"US", "2001:db8:1::9"},
		"private":        {"", ""},
		"admin's choice": {"JP", "203.0.113.8"},
		"remote":         {"", ""},
	}
	rows := got()
	for name, w := range want {
		if n := rows[name]; n.RegionDetected != w[0] || n.RegionDetectedFrom != w[1] {
			t.Errorf("%s: detected %q from %q, want %q from %q", name, n.RegionDetected, n.RegionDetectedFrom, w[0], w[1])
		}
	}
	if rows["admin's choice"].EffectiveRegion() != "SG" || rows["ip"].EffectiveRegion() != "JP" {
		t.Fatal("the admin's region must win over the detected one")
	}

	// An unchanged address is not looked up again; a changed one is.
	if err := r.SetNodeDetectedRegion(rows["ip"].ID, "XX", "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Model(&model.Node{}).Where("id = ?", rows["v4 first"].ID).
		Update("server_ip_v4", "203.0.113.20").Error; err != nil {
		t.Fatal(err)
	}
	h.detectNodeRegions(context.Background(), false)
	rows = got()
	if rows["ip"].RegionDetected != "XX" || rows["v4 first"].RegionDetected != "JP" {
		t.Fatalf("after address change: ip=%q v4=%q", rows["ip"].RegionDetected, rows["v4 first"].RegionDetected)
	}
	// A new database (force) looks at everything again.
	h.detectNodeRegions(context.Background(), true)
	if rows = got(); rows["ip"].RegionDetected != "JP" {
		t.Fatalf("forced detection kept %q", rows["ip"].RegionDetected)
	}

	// The status page shows the country, never the address.
	if err := r.UpsertConfig(statusPageEnabledKey, "true", now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	code, raw, data := statusPageRequest(t, h)
	if code != 0 {
		t.Fatalf("code = %d: %s", code, raw)
	}
	regions := map[string]string{}
	for _, n := range data.Nodes {
		regions[n.Name] = n.Region
	}
	if regions["ip"] != "JP" || regions["admin's choice"] != "SG" || regions["private"] != "" {
		t.Fatalf("status regions = %v", regions)
	}
	if strings.Contains(raw, "203.0.113") || strings.Contains(raw, "region_detected") {
		t.Fatalf("status leaks addresses: %s", raw)
	}
}

func TestDetectNodeRegionsWithoutDatabase(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	h.SetGeoIP(geoip.New(filepath.Join(t.TempDir(), "missing.mmdb"), false))
	n := model.Node{Name: "a", Secret: "s", ServerIP: "203.0.113.7", Port: "1", CreatedTime: 1}
	if err := r.DB().Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	h.detectNodeRegions(context.Background(), false)
	var got model.Node
	if err := r.DB().First(&got, n.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Without data nothing is recorded, so the address is looked up once
	// the database arrives.
	if got.RegionDetectedFrom != "" {
		t.Fatalf("recorded %q without a database", got.RegionDetectedFrom)
	}
}

func TestApplyNodeRegionRequest(t *testing.T) {
	h, r := setupStatusPageHandler(t)
	n := model.Node{Name: "a", Secret: "s", ServerIP: "203.0.113.7", Port: "1", CreatedTime: 1, Region: "JP"}
	if err := r.DB().Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	region := func() string {
		var got model.Node
		if err := r.DB().First(&got, n.ID).Error; err != nil {
			t.Fatal(err)
		}
		return got.Region
	}
	steps := []struct {
		req  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"name": "a"}, "JP"}, // older client: untouched
		{map[string]interface{}{"region": "hk"}, "HK"},
		{map[string]interface{}{"region": "nonsense"}, ""},
	}
	for _, s := range steps {
		if err := h.applyNodeRegionRequest(n.ID, s.req); err != nil {
			t.Fatal(err)
		}
		if got := region(); got != s.want {
			t.Fatalf("after %v region = %q, want %q", s.req, got, s.want)
		}
	}
}
