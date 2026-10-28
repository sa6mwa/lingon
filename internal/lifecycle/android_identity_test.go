package lifecycle

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
)

func TestAndroidDevelopmentIdentityUpdatesExistingRelease(t *testing.T) {
	w, _ := testWorkflow(t)
	testGit(t, w, "tag", "v0.5.1")
	testGit(t, w, "commit", "--allow-empty", "-m", "fix(android): preserve foreground walls")
	commit := testGit(t, w, "rev-parse", "--short=12", "HEAD")
	got, err := w.androidReleaseIdentity(context.Background())
	want := fmt.Sprintf("v0.5.1-dev.g%s 5001", commit)
	if err != nil || got != want {
		t.Fatalf("local APK identity=%q err=%v; want %q to update installed code 5001", got, err, want)
	}
	if official, err := w.exactVersion(context.Background()); err != nil || official != "0.0.0" {
		t.Fatalf("development identity changed official release policy: %q %v", official, err)
	}
}

func TestAndroidDevelopmentIdentityUsesOnlyReachableLightweightReleases(t *testing.T) {
	w, _ := testWorkflow(t)
	testGit(t, w, "tag", "v0.4.9")
	testGit(t, w, "tag", "v0.5.1")
	testGit(t, w, "tag", "-a", "v0.9.0", "-m", "annotated history is not a release identity")
	testGit(t, w, "commit", "--allow-empty", "-m", "fix(android): local update")
	unrelated := testGit(t, w, "commit-tree", "HEAD^{tree}", "-m", "unrelated release")
	testGit(t, w, "tag", "v9.0.0", unrelated)
	commit := testGit(t, w, "rev-parse", "--short=12", "HEAD")
	got, err := w.androidReleaseIdentity(context.Background())
	want := "v0.5.1-dev.g" + commit + " 5001"
	if err != nil || got != want {
		t.Fatalf("identity=%q err=%v; want %q", got, err, want)
	}
}

func TestAndroidDevelopmentIdentityWithoutReleaseHistory(t *testing.T) {
	w, _ := testWorkflow(t)
	commit := testGit(t, w, "rev-parse", "--short=12", "HEAD")
	got, err := w.androidReleaseIdentity(context.Background())
	want := "v0.0.0-dev.g" + commit + " 1"
	if err != nil || got != want {
		t.Fatalf("identity=%q err=%v; want %q", got, err, want)
	}
}

func TestAndroidDirtyTaggedTreeIsADevelopmentAPK(t *testing.T) {
	w, _ := testWorkflow(t)
	testGit(t, w, "tag", "v0.5.1")
	writeTestFile(t, filepath.Join(w.root, "local-change.txt"), []byte("local change"))
	commit := testGit(t, w, "rev-parse", "--short=12", "HEAD")
	got, err := w.androidReleaseIdentity(context.Background())
	want := "v0.5.1-dev.g" + commit + ".dirty 5001"
	if err != nil || got != want {
		t.Fatalf("identity=%q err=%v; want %q", got, err, want)
	}
}

func TestAndroidReleaseIdentityMatchesTaggedArtifactPolicy(t *testing.T) {
	for _, tc := range []struct {
		tag, want string
	}{
		{"v0.0.0", "v0.0.0 1"},
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
