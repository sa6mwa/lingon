package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type gate struct {
	name, dir, tool string
	args            []string
	env             map[string]string
}

var goEnvironment = map[string]string{
	"GOMAXPROCS": "4", "GOFLAGS": "-p=1", "GOTOOLCHAIN": "local", "GOOS": "linux", "GOARCH": runtime.GOARCH, "CGO_ENABLED": "1",
}

func gradleGate(name string, tasks ...string) gate {
	heap := "512m"
	for _, task := range tasks {
		if task == ":app:assembleRelease" || task == ":app:assembleDebug" || task == ":app:assembleDebugAndroidTest" || task == ":app:installDebug" {
			// APK dex merging uses the artifact-build heap. Verification
			// builds its APKs before emulator startup, within the same budget.
			heap = "1536m"
			break
		}
	}
	args := []string{"--no-daemon", "--max-workers=1", "--rerun-tasks", "--no-build-cache", "-Dorg.gradle.parallel=false", "-Dkotlin.compiler.execution.strategy=in-process", "-Dorg.gradle.jvmargs=-Xmx" + heap + " -XX:ActiveProcessorCount=4 -Dfile.encoding=UTF-8"}
	return gate{name: name, dir: "android", tool: "./gradlew", args: append(args, tasks...)}
}

func proofGates() []gate {
	return []gate{
		{name: "repository hygiene", tool: "bash", args: []string{"scripts/check-hygiene.sh"}},
		{name: "PTY shell prerequisite", tool: "/bin/bash", args: []string{"--version"}},
		{name: "PTY pager prerequisite", tool: "less", args: []string{"--version"}},
		{name: "module integrity", tool: "go", args: []string{"mod", "verify"}},
		{name: "vet", tool: "go", args: []string{"vet", "./..."}},
		{name: "golint", tool: "golint", args: []string{"-set_exit_status", "./..."}},
		{name: "golangci-lint", tool: "bash", args: []string{"scripts/golangci-lint.sh", "run", "--concurrency=1", "./..."}},
		{name: "CLI build and install", tool: "bash", args: []string{"scripts/check-install.sh"}},
		{name: "Go tests", tool: "go", args: []string{"test", "-count=1", "-timeout=20m", "./..."}},
		{name: "Go race tests", tool: "go", args: []string{"test", "-race", "-count=1", "-timeout=20m", "./..."}},
		{name: "Android Go tools vet", dir: "android", tool: "go", args: []string{"vet", "./..."}},
		{name: "Android Go tools race tests", dir: "android", tool: "go", args: []string{"test", "-race", "-count=1", "-timeout=10m", "./..."}},
		{name: "PTY and browser integration", tool: "go", args: []string{"test", "-race", "-tags=integration", "-count=1", "-timeout=30m", "./integration/..."}},
		gradleGate("Android unit and instrumentation build", ":app:testDebugUnitTest", ":app:compileDebugAndroidTestKotlin"),
		{name: "full Android end-to-end sweep", tool: "bash", args: []string{"android/scripts/run-integration-tests.sh"}, env: map[string]string{
			"LINGON_IT_ONLY": "", "LINGON_IT_CLASS": "", "LINGON_IT_CLEARTEXT": "0", "LINGON_IT_KEEP_EMULATOR": "0", "LINGON_IT_REUSE_DEVICE": "0", "LINGON_IT_CPU_QUOTA_PERCENT": "400", "LINGON_IT_MEMORY_MAX": "4G",
		}},
		{name: "Android cleartext transport", tool: "bash", args: []string{"android/scripts/run-integration-tests.sh"}, env: map[string]string{
			"LINGON_IT_ONLY": "cleartext_share_authentication_uses_token_fallback", "LINGON_IT_CLASS": "", "LINGON_IT_CLEARTEXT": "1", "LINGON_IT_KEEP_EMULATOR": "0", "LINGON_IT_REUSE_DEVICE": "0", "LINGON_IT_CPU_QUOTA_PERCENT": "400", "LINGON_IT_MEMORY_MAX": "4G",
		}},
		// EndToEndTest is not the entire instrumentation tree: run the remaining
		// UI/render/share-intent classes through the same owned-device harness.
		{name: "remaining Android instrumentation", tool: "bash", args: []string{"android/scripts/run-integration-tests.sh"}, env: map[string]string{
			"LINGON_IT_ONLY": "", "LINGON_IT_CLASS": "__ALL_NON_E2E__", "LINGON_IT_CLEARTEXT": "0", "LINGON_IT_KEEP_EMULATOR": "0", "LINGON_IT_REUSE_DEVICE": "0", "LINGON_IT_CPU_QUOTA_PERCENT": "400", "LINGON_IT_MEMORY_MAX": "4G",
		}},
		{name: "performance observations", tool: "go", args: []string{"test", "./internal/relay", "./internal/terminal/emu", "-run=^$", "-bench=Benchmark(WSReplay|AbsoluteCursorPositionScrollRegion)$", "-benchmem", "-benchtime=1s", "-count=3"}},
	}
}

func (w *workflow) proof(ctx context.Context, commit string) error {
	branch, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return err
	}
	// Formatting is a read-only assertion, not an automatic repair.
	files, err := w.git(ctx, "ls-files", "*.go")
	if err != nil {
		return err
	}
	out, err := w.exec(ctx, w.root, "gofmt", append([]string{"-l"}, strings.Split(files, "\n")...)...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "" {
		return fmt.Errorf("formatting gate failed:\n%s", out)
	}
	for _, g := range proofGates() {
		fmt.Printf("Gate: %s\n", g.name)
		env := make(map[string]string)
		for key, value := range goEnvironment {
			env[key] = value
		}
		for key, value := range g.env {
			env[key] = value
		}
		if env["LINGON_IT_CLASS"] == "__ALL_NON_E2E__" {
			classes, err := w.instrumentationClasses()
			if err != nil {
				return err
			}
			env["LINGON_IT_CLASS"] = classes
		}
		out, err := w.r.run(ctx, command{Dir: filepath.Join(w.root, g.dir), Name: g.tool, Args: g.args, Env: env})
		if err != nil {
			return fmt.Errorf("gate %q: %w", g.name, err)
		}
		if g.tool == "bash" && len(g.args) > 0 && g.args[0] == "android/scripts/run-integration-tests.sh" {
			if err := w.verifyAndroidCompletion(ctx, out, env); err != nil {
				return fmt.Errorf("gate %q coverage: %w", g.name, err)
			}
		}
		if err := w.unchanged(ctx, commit); err != nil {
			return err
		}
		if err := w.sameBranch(ctx, branch); err != nil {
			return err
		}
	}
	return nil
}

func (w *workflow) prerelease(ctx context.Context) error {
	if err := w.clean(ctx); err != nil {
		return err
	}
	commit, err := w.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if err := w.proof(ctx, commit); err != nil {
		return err
	}
	return w.matrix(ctx)
}

func (w *workflow) finalRelease(ctx context.Context) error {
	version, err := w.exactVersion(ctx)
	if err != nil {
		return err
	}
	if version == "0.0.0" {
		return fmt.Errorf("final release needs an exact lightweight tag")
	}
	marker := filepath.Join(w.work, "release-v"+version+".attempt")
	f, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("release %s already attempted; do not rerun a failed gate or rebuild verified bytes: %w", version, err)
	}
	_ = f.Close()
	commit, err := w.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	// Only release-owned generated data is cleaned. Never delete caches,
	// credentials, a dev environment, or a previous immutable release's bytes.
	if err := os.RemoveAll(filepath.Join(w.root, "dist", "v"+version)); err != nil {
		return err
	}
	if _, err := w.r.run(ctx, command{Dir: filepath.Join(w.root, "android"), Name: "./gradlew", Args: gradleGate("clean", "clean").args}); err != nil {
		return err
	}
	if err := w.proof(ctx, commit); err != nil {
		return err
	}
	w.finalBuild = true
	defer func() { w.finalBuild = false }()
	if err := w.matrix(ctx); err != nil {
		return err
	}
	r, err := w.verified(ctx)
	if err != nil {
		return err
	}
	r.FullProof = true
	if err := w.saveReceipt(r); err != nil {
		return err
	}
	fmt.Printf("Local release v%s verified at %s. Next: make publish.\n", version, commit)
	return nil
}
