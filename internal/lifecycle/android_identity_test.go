package lifecycle

import (
	"context"
	"testing"
)

func TestAndroidReleaseIdentityMatchesTaggedArtifactPolicy(t *testing.T) {
	for _, tc := range []struct {
		tag, want string
	}{
		{"", "v0.0.0 1"},
		{"v1.2.3", "v1.2.3 1002003"},
		{"v0.9.1", "v0.9.1 9001"},
		{"v999.999.999", "v999.999.999 999999999"},
		{"v1.1000.0", ""},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			w, _ := testWorkflow(t)
			if tc.tag != "" {
				testGit(t, w, "tag", tc.tag)
			}
			got, err := w.androidReleaseIdentity(context.Background())
			if tc.want == "" {
				if err == nil {
					t.Fatal("invalid Android release identity accepted")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("identity=%q err=%v; want %q", got, err, tc.want)
			}
		})
	}
}
