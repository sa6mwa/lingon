package lifecycle

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type command struct {
	Dir, Name string
	Args      []string
	Env       map[string]string
	Unset     []string
}

type runner interface {
	run(context.Context, command) ([]byte, error)
}

type processRunner struct{ log io.Writer }

// Command output is captured in a repository-owned log, never attached to the
// caller's terminal. Each child has its own process session and null stdin.
func (r processRunner) run(ctx context.Context, c command) ([]byte, error) {
	fmt.Fprintf(r.log, "\n$ %s %s\n", c.Name, strings.Join(c.Args, " "))
	cmd := exec.CommandContext(ctx, "setsid", append([]string{"--wait", c.Name}, c.Args...)...)
	cmd.Dir = c.Dir
	cmd.Env = commandEnv(c.Env, c.Unset)
	// setsid owns this child session. Cancellation signals only that owned
	// process group, allowing the Android harness's cleanup trap to run.
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		if err == syscall.ESRCH {
			return cmd.Process.Kill()
		}
		return err
	}
	cmd.WaitDelay = 20 * time.Second
	var output tailBuffer
	writer := io.MultiWriter(r.log, &output)
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Run(); err != nil {
		return output.Bytes(), fmt.Errorf("%s failed: %w (see build/lifecycle logs)", c.Name, err)
	}
	return output.Bytes(), nil
}

type tailBuffer struct{ buffer bytes.Buffer }

func (b *tailBuffer) Len() int       { return b.buffer.Len() }
func (b *tailBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *tailBuffer) String() string { return b.buffer.String() }

func (b *tailBuffer) Write(p []byte) (int, error) {
	const limit = 1024 * 1024
	n := len(p)
	if len(p) >= limit {
		b.buffer.Reset()
		_, _ = b.buffer.Write(p[len(p)-limit:])
		return n, nil
	}
	if b.Len()+len(p) > limit {
		b.buffer.Next(b.Len() + len(p) - limit)
	}
	_, _ = b.buffer.Write(p)
	return n, nil
}

func (b *tailBuffer) WriteString(s string) (int, error) {
	const limit = 1024 * 1024
	if len(s) >= limit {
		b.buffer.Reset()
		_, _ = b.buffer.WriteString(s[len(s)-limit:])
		return len(s), nil
	}
	return b.Write([]byte(s))
}

func commandEnv(overrides map[string]string, unset []string) []string {
	values := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		values[key] = value
	}
	for _, key := range unset {
		delete(values, key)
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

// Options selects a local lifecycle operation. Version and Message are required
// only when creating a new release; publication accepts an existing receipt.
type Options struct {
	Root, Operation, Version, Message string
}

type workflow struct {
	root, work string
	r          runner
	finalBuild bool
}

func (w *workflow) exec(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	return w.r.run(ctx, command{Dir: dir, Name: name, Args: args})
}

func (w *workflow) git(ctx context.Context, args ...string) (string, error) {
	out, err := w.exec(ctx, w.root, "git", args...)
	return strings.TrimSpace(string(out)), err
}

// Run executes one serial lifecycle operation. It never repairs a failed gate,
// deletes a release tag, or silently continues with partial verification.
func Run(ctx context.Context, opts Options) error {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return err
	}
	// Resolve the root before creating anything: invocation from a subdirectory
	// must never put generated state beside source files.
	rootCmd := exec.CommandContext(ctx, "setsid", "--wait", "git", "-C", root, "rev-parse", "--show-toplevel")
	rootOut, err := rootCmd.Output()
	if err != nil {
		return fmt.Errorf("resolve lifecycle repository root: %w", err)
	}
	root = strings.TrimSpace(string(rootOut))
	if opts.Operation != "version" && opts.Operation != "android-version" && !strings.HasPrefix(opts.Operation, "dev-") {
		check := exec.CommandContext(ctx, "bash", filepath.Join(root, "scripts", "verify-contained.sh"), "--check")
		if out, err := check.CombinedOutput(); err != nil {
			return fmt.Errorf("verification containment: %w: %s", err, out)
		}
	}
	work := filepath.Join(root, "build", "lifecycle")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return err
	}
	// O_EXCL prevents two release/dev processes from racing even under make -j.
	lock := filepath.Join(work, "operation.lock")
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("another lifecycle operation is running; inspect %s before removing a stale lock: %w", lock, err)
	}
	fmt.Fprintf(f, "pid=%d operation=%s\n", os.Getpid(), opts.Operation)
	_ = f.Close()
	defer os.Remove(lock)
	log, err := os.CreateTemp(work, opts.Operation+"-*.log")
	if err != nil {
		return err
	}
	defer log.Close()
	fmt.Fprintf(os.Stderr, "Lifecycle log: %s\n", log.Name())
	w := workflow{root: root, work: work, r: processRunner{log: log}}
	return w.run(ctx, opts)
}

func (w *workflow) run(ctx context.Context, opts Options) error {
	switch opts.Operation {
	case "version":
		v, err := w.exactVersion(ctx)
		if err == nil {
			fmt.Println(v)
		}
		return err
	case "android-version":
		identity, err := w.androidReleaseIdentity(ctx)
		if err == nil {
			fmt.Println(identity)
		}
		return err
	case "prerelease":
		return w.prerelease(ctx)
	case "release":
		return w.release(ctx, opts)
	case "release-matrix":
		return w.matrix(ctx)
	case "package-verify":
		_, err := w.verified(ctx)
		return err
	case "publish":
		return w.publish(ctx)
	case "push-container":
		return w.pushContainer(ctx)
	case "container":
		return w.container(ctx)
	case "dev-up", "dev-down", "dev-ps", "dev-logs", "dev-reset":
		return w.dev(ctx, opts.Operation)
	default:
		return fmt.Errorf("unknown lifecycle operation %q", opts.Operation)
	}
}
