package buildscripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootAndroidInstallReleaseUsesSelectedPhoneAndReleaseAPK(t *testing.T) {
	root := repoRoot(t)
	buildDir := filepath.Join(root, "android", "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatalf("create Android build directory: %v", err)
	}
	workDir, err := os.MkdirTemp(buildDir, "install-release-test-")
	if err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workDir) })

	adbPath := filepath.Join(workDir, "adb")
	adbLog := filepath.Join(workDir, "adb.log")
	adbScript := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$FAKE_ADB_LOG\"\n" +
		"if [ \"$1\" = devices ]; then\n" +
		"  printf 'List of devices attached\\nphone-1\\tdevice product:phone\\nphone-2\\tdevice product:phone\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = -s ] && [ \"$2\" = phone-2 ] && [ \"$3\" = install ] && [ \"$4\" = -r ]; then\n" +
		"  printf 'Success\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(adbPath, []byte(adbScript), 0o755); err != nil {
		t.Fatalf("write fake adb: %v", err)
	}
	apkPath := filepath.Join(workDir, "app-release.apk")
	if err := os.WriteFile(apkPath, []byte("apk"), 0o644); err != nil {
		t.Fatalf("write fake APK: %v", err)
	}

	cmd := exec.Command("make", "android-install-release", "ADB="+adbPath, "APK="+apkPath, "ADB_SERIAL=phone-2")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "FAKE_ADB_LOG="+adbLog)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("root release install failed: %v\n%s", err, output)
	}
	log, err := os.ReadFile(adbLog)
	if err != nil {
		t.Fatalf("read fake adb calls: %v", err)
	}
	if !strings.Contains(string(log), "devices -l") ||
		!strings.Contains(string(log), "-s phone-2 install -r "+apkPath) {
		t.Fatalf("release install did not use the selected device and APK:\n%s", log)
	}
}

func TestRootAndroidInstallReleaseBuildsCurrentAPKByDefault(t *testing.T) {
	root := repoRoot(t)
	for _, tc := range []struct {
		name      string
		apk       string
		wantBuild bool
	}{
		{name: "default APK", wantBuild: true},
		{name: "explicit APK", apk: "APK=/tmp/custom-release.apk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"-n", "android-install-release"}
			if tc.apk != "" {
				args = append(args, tc.apk)
			}
			cmd := exec.Command("make", args...)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("release install dry run failed: %v\n%s", err, output)
			}
			plan := string(output)
			buildAt := strings.Index(plan, "./gradlew :app:assembleRelease")
			installAt := strings.Index(plan, "install -r")
			if installAt < 0 {
				t.Fatalf("release install missing from plan:\n%s", plan)
			}
			if tc.wantBuild && (buildAt < 0 || buildAt > installAt) {
				t.Fatalf("current release must be built before installation:\n%s", plan)
			}
			if !tc.wantBuild && buildAt >= 0 {
				t.Fatalf("explicit APK should install without rebuilding:\n%s", plan)
			}
		})
	}
}
