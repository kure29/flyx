package handler

import "testing"

func TestCompareReleaseVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign only
	}{
		{"3.4.3", "3.4.3", 0},
		{"3.4.3", "v3.4.3", 0},
		{"3.4.10", "3.4.9", 1},
		{"3.10.0", "3.9.9", 1},
		{"3.4", "3.4.1", -1},
		{"3.4.0", "3.4.0-rc.1", 1},
		{"3.4.0-rc.2", "3.4.0-rc.1", 1},
		{"3.4.0-rc.1", "3.4.0-beta.9", 1},
		{"3.4.0-beta.1", "3.4.0-alpha.9", 1},
		{"dev", "3.4.3", -1},
	}
	for _, c := range cases {
		got := compareReleaseVersions(c.a, c.b)
		if (got > 0) != (c.want > 0) || (got < 0) != (c.want < 0) {
			t.Errorf("compare(%q, %q) = %d, want sign of %d", c.a, c.b, got, c.want)
		}
	}
}

func TestReleaseIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"3.4.2", "3.4.3", true},
		{"3.4.3", "3.4.3", false},
		// A later patch for an older line is not an update.
		{"3.4.3", "3.3.46", false},
		{"v3.4.3", "3.4.3", false},
		// A build without a version is offered the latest release.
		{"dev", "3.4.3", true},
		{"3.4.3", "", false},
	}
	for _, c := range cases {
		if got := releaseIsNewer(c.current, c.latest); got != c.want {
			t.Errorf("releaseIsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// GitHub lists releases by publish time; the latest is the highest version.
func TestLatestReleaseTagPicksHighestVersion(t *testing.T) {
	releases := []githubRelease{
		{TagName: "3.3.46"}, // published last, a patch for the older line
		{TagName: "3.5.0-beta.1"},
		{TagName: "3.4.4", Draft: true},
		{TagName: "3.4.3"},
		{TagName: "3.4.2"},
		{TagName: "3.3.45"},
	}
	if tag, ok := latestReleaseTag(releases, releaseChannelStable); !ok || tag != "3.4.3" {
		t.Fatalf("stable = %q %v, want 3.4.3", tag, ok)
	}
	if tag, ok := latestReleaseTag(releases, releaseChannelDev); !ok || tag != "3.5.0-beta.1" {
		t.Fatalf("dev = %q %v, want 3.5.0-beta.1", tag, ok)
	}
	if _, ok := latestReleaseTag(nil, releaseChannelStable); ok {
		t.Fatal("no releases should find nothing")
	}

	items := releasesForChannel(releases, releaseChannelStable)
	got := make([]string, len(items))
	for i, it := range items {
		got[i] = it.Version
	}
	want := []string{"3.4.3", "3.4.2", "3.3.46", "3.3.45"}
	if len(got) != len(want) {
		t.Fatalf("list = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("list = %v, want %v", got, want)
		}
	}
}

func TestSystemUpgradeVersionResponseNeverOffersOlderRelease(t *testing.T) {
	data := systemUpgradeVersionResponse("3.4.3", releaseChannelStable, "3.3.46", nil, systemUpgradeCapabilityData{})
	if data.HasUpdate {
		t.Fatal("3.3.46 offered as an update to 3.4.3")
	}
	data = systemUpgradeVersionResponse("3.4.2", releaseChannelStable, "3.4.3", nil, systemUpgradeCapabilityData{})
	if !data.HasUpdate {
		t.Fatal("3.4.3 not offered to 3.4.2")
	}
}
