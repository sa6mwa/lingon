package lifecycle

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGradleHeapMatchesBuildStage(t *testing.T) {
	for _, tc := range []struct{ task, heap string }{
		{":app:testDebugUnitTest", "512m"},
		{":app:assembleDebugAndroidTest", "512m"},
		{":app:assembleRelease", "1536m"},
	} {
		t.Run(tc.task, func(t *testing.T) {
			gate := gradleGate("fixture", tc.task)
			var jvm []string
			for _, arg := range gate.args {
				if strings.HasPrefix(arg, "-Dorg.gradle.jvmargs=") {
					jvm = append(jvm, arg)
				}
			}
			if len(jvm) != 1 || !strings.Contains(jvm[0], "-Xmx"+tc.heap+" ") || !strings.Contains(jvm[0], "-XX:ActiveProcessorCount=4") {
				t.Fatalf("JVM budget for %s: %v", tc.task, jvm)
			}
		})
	}
}

func TestAndroidMakeHeapMatchesBuildStage(t *testing.T) {
	for _, tc := range []struct{ target, heap string }{{"build", "512m"}, {"release", "1536m"}} {
		t.Run(tc.target, func(t *testing.T) {
			output, err := exec.Command("make", "--no-print-directory", "-n", "-C", "../../android", tc.target).CombinedOutput()
			if err != nil {
				t.Fatalf("dry-run %s: %v: %s", tc.target, err, output)
			}
			wanted := "-Dorg.gradle.jvmargs=-Xmx" + tc.heap + " -XX:ActiveProcessorCount=4"
			if !strings.Contains(string(output), wanted) {
				t.Fatalf("missing stage heap %q: %s", wanted, output)
			}
		})
	}
}
