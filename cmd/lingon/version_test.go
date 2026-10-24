package main

import (
	"bytes"
	"strings"
	"testing"

	"pkt.systems/lingon/internal/buildmeta"
)

func TestVersionCommandReportsLinkedReleaseIdentity(t *testing.T) {
	old := buildmeta.Stamp
	t.Cleanup(func() { buildmeta.Stamp = old })
	buildmeta.Stamp = "LINGON_RELEASE:v1.2.3:" + strings.Repeat("a", 40) + ":END"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "pkt.systems/lingon v1.2.3\n"},
		{[]string{"--semver"}, "1.2.3\n"},
		{[]string{"--version"}, "v1.2.3\n"},
	} {
		cmd := NewVersionCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(tc.args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if out.String() != tc.want {
			t.Fatalf("%v => %q want %q", tc.args, out.String(), tc.want)
		}
	}
}
