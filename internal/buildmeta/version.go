package buildmeta

import "strings"

// Stamp is set by the linker to LINGON_RELEASE:vX.Y.Z:<commit>:END. Go omits
// ldflags from trimpath build information, so the stamp also lets cross-target
// package verification check the actual linked version without executing it.
var Stamp = "LINGON_RELEASE:unverified"

// ReleaseVersion returns the linked release version, or an empty string for a
// development build without an injected lifecycle stamp.
func ReleaseVersion() string {
	parts := strings.Split(Stamp, ":")
	if len(parts) != 4 || parts[0] != "LINGON_RELEASE" || !strings.HasPrefix(parts[1], "v") || len(parts[2]) != 40 || parts[3] != "END" {
		return ""
	}
	return parts[1]
}
