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
			buildAt := strings.Index(plan, ":app:assembleRelease")
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

func TestAndroidReleasePassesLifecycleIdentityToGradle(t *testing.T) {
	work := lifecycleWork(t)
	makefile, err := os.ReadFile(filepath.Join(repoRoot(t), "android", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"Makefile":                             makefile,
		"scripts/generate-release-keystore.sh": []byte("#!/bin/sh\nexit 0\n"),
		"scripts/verify-release-cert.sh":       []byte("#!/bin/sh\nexit 0\n"),
		"sdk/build-tools/36/apksigner":         []byte("#!/bin/sh\nexit 0\n"),
		"gradlew":                              []byte("#!/bin/sh\nset -e\nprintf '%s\\n' \"$@\" > \"$FAKE_GRADLE_LOG\"\nmkdir -p app/build/outputs/apk/release\nprintf 'fixture' > app/build/outputs/apk/release/app-release.apk\n"),
	}
	for name, data := range files {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	log := filepath.Join(work, "gradle.log")
	cmd := exec.Command("make", "release", "ANDROID_SDK_ROOT="+filepath.Join(work, "sdk"),
		"GRADLE=bash "+filepath.Join(repoRoot(t), "scripts", "verify-contained.sh")+" ./gradlew",
		"RELEASE_IDENTITY_CMD=printf 'v1.2.3 1002003'", "VERSION_CMD=printf 'v1.2.3'")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "FAKE_GRADLE_LOG="+log)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("release build failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	args := string(data)
	if !strings.Contains(args, "-Plingon.versionName=v1.2.3\n") || !strings.Contains(args, "-Plingon.versionCode=1002003\n") {
		t.Fatalf("Gradle lost the paired lifecycle identity:\n%s", args)
	}
}
