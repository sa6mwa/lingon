package buildmeta

import (
	"strings"
	"testing"
)

func TestReleaseVersionUsesOnlyCompleteStamp(t *testing.T) {
	original := Stamp
	t.Cleanup(func() { Stamp = original })
	for stamp, want := range map[string]string{
		"LINGON_RELEASE:unverified":                                    "",
		"LINGON_RELEASE:v1.2.3:" + strings.Repeat("a", 40) + ":END":    "v1.2.3",
		"LINGON_RELEASE:v1.2.3:short:END":                              "",
		"OTHER:v1.2.3:" + strings.Repeat("a", 40) + ":END":             "",
		"LINGON_RELEASE:v1.2.3:" + strings.Repeat("a", 40) + ":broken": "",
	} {
		Stamp = stamp
		if got := ReleaseVersion(); got != want {
			t.Fatalf("%q => %q, want %q", stamp, got, want)
		}
	}
}
