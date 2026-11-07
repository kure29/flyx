package handler

import (
	"fmt"
	"strings"
	"testing"
)

func TestDecodeReleasesKeepsReleasesBeforeCut(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 30; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"tag_name":"v1.%d.0","assets":[{"name":"%s"}]}`, 30-i, strings.Repeat("x", 1000))
	}
	b.WriteString("]")
	full := b.String()

	all, err := decodeReleases(strings.NewReader(full))
	if err != nil || len(all) != 30 || all[0].TagName != "v1.30.0" {
		t.Fatalf("full list: %d releases, err %v", len(all), err)
	}
	// Cut in the middle of the 11th release, as a size limit would.
	cut := full[:strings.Index(full, `"v1.20.0"`)+5]
	part, err := decodeReleases(strings.NewReader(cut))
	if err != nil || len(part) != 10 || part[9].TagName != "v1.21.0" {
		t.Fatalf("cut list: %d releases, err %v", len(part), err)
	}
	if _, err := decodeReleases(strings.NewReader(`[{"tag_na`)); err == nil {
		t.Fatal("a cut before any release should fail")
	}
	if _, err := decodeReleases(strings.NewReader(`{"message":"rate limited"}`)); err == nil {
		t.Fatal("an object instead of a list should fail")
	}
}
