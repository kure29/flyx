package handler

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	// Only the leading x.y.z counts as the version; a stage number such as
	// the 1 in "rc.1" must not read as a fourth version part.
	releaseNumberPattern = regexp.MustCompile(`^\d+(?:\.\d+)*`)
	releaseStagePattern  = regexp.MustCompile(`(alpha|beta|rc)[.-]?(\d+)`)
)

// releaseVersion is a tag split into what orders it: the numbers, then the
// stage (alpha < beta < rc < stable) and the stage's own number.
type releaseVersion struct {
	numbers     []int
	stageRank   int
	stageNumber int
}

func parseReleaseVersion(tag string) releaseVersion {
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(tag)), "v")

	var v releaseVersion
	if core := releaseNumberPattern.FindString(normalized); core != "" {
		for _, s := range strings.Split(core, ".") {
			n, _ := strconv.Atoi(s)
			v.numbers = append(v.numbers, n)
		}
	}
	switch {
	case strings.Contains(normalized, "rc"):
		v.stageRank = 3
	case strings.Contains(normalized, "beta"):
		v.stageRank = 2
	case strings.Contains(normalized, "alpha"):
		v.stageRank = 1
	case stableVersionPattern.MatchString(normalized):
		v.stageRank = 4
	}
	if m := releaseStagePattern.FindStringSubmatch(normalized); m != nil {
		v.stageNumber, _ = strconv.Atoi(m[2])
	}
	return v
}

// compareReleaseVersions orders two tags by version, not by when they were
// published: negative when a is older than b. It matches the panel's
// frontend (utils/version-update.ts), so both report the same latest version.
// A build without a version ("dev") sorts below every release.
func compareReleaseVersions(a, b string) int {
	va, vb := parseReleaseVersion(a), parseReleaseVersion(b)
	for i := 0; i < len(va.numbers) || i < len(vb.numbers); i++ {
		var x, y int
		if i < len(va.numbers) {
			x = va.numbers[i]
		}
		if i < len(vb.numbers) {
			y = vb.numbers[i]
		}
		if x != y {
			return x - y
		}
	}
	if va.stageRank != vb.stageRank {
		return va.stageRank - vb.stageRank
	}
	return va.stageNumber - vb.stageNumber
}

// releaseIsNewer reports whether latest is a newer version than current, so
// a panel is never offered an older release as an update.
func releaseIsNewer(current, latest string) bool {
	return strings.TrimSpace(latest) != "" && compareReleaseVersions(current, latest) < 0
}

// latestReleaseTag is the highest published tag of a channel. GitHub lists
// releases by publish time, which puts a later patch for an older line
// above a newer release.
func latestReleaseTag(releases []githubRelease, channel string) (string, bool) {
	channel = normalizeReleaseChannel(channel)
	latest := ""
	for _, r := range releases {
		tag := strings.TrimSpace(r.TagName)
		if r.Draft || tag == "" || releaseChannelFromTag(tag) != channel {
			continue
		}
		if latest == "" || compareReleaseVersions(tag, latest) > 0 {
			latest = tag
		}
	}
	return latest, latest != ""
}

// sortReleasesNewestFirst orders a release list by version, newest first.
func sortReleasesNewestFirst(items []systemUpgradeReleaseData) {
	sort.SliceStable(items, func(i, j int) bool {
		return compareReleaseVersions(items[i].Version, items[j].Version) > 0
	})
}
