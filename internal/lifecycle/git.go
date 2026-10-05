package lifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var conventionalMessage = regexp.MustCompile(`^(feat|fix|perf|refactor|build|ci|test|docs|chore)(\([a-zA-Z0-9_./-]+\))?!?: [^\r\n]+$`)

func (w *workflow) clean(ctx context.Context) error {
	root, err := w.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if root != w.root {
		return fmt.Errorf("run from the repository root (got %q)", root)
	}
	status, err := w.git(ctx, "status", "--porcelain=v1", "-uall")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("commit intended changes before release; worktree is dirty:\n%s", status)
	}
	if _, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
		return fmt.Errorf("release requires an attached branch: %w", err)
	}
	for _, marker := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"} {
		path, err := w.git(ctx, "rev-parse", "--git-path", marker)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(w.root, path)
		}
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("finish the existing Git operation before release: %s", marker)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (w *workflow) unchanged(ctx context.Context, commit string) error {
	if err := w.clean(ctx); err != nil {
		return err
	}
	head, err := w.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != commit {
		return fmt.Errorf("HEAD changed during verification: expected %s, got %s", commit, head)
	}
	return nil
}

func (w *workflow) sameBranch(ctx context.Context, expected string) error {
	branch, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return err
	}
	if branch != expected {
		return fmt.Errorf("branch changed during verification: expected %s, got %s", expected, branch)
	}
	return nil
}

func (w *workflow) exactVersion(ctx context.Context) (string, error) {
	tags, err := w.git(ctx, "tag", "--points-at", "HEAD")
	if err != nil {
		return "", err
	}
	version := "0.0.0"
	for _, tag := range strings.Fields(tags) {
		if !stableVersion.MatchString(tag) {
			continue
		}
		kind, err := w.git(ctx, "cat-file", "-t", "refs/tags/"+tag)
		if err != nil {
			return "", err
		}
		if kind != "commit" {
			return "", fmt.Errorf("%s must be a lightweight tag, not %s", tag, kind)
		}
		if version != "0.0.0" {
			return "", fmt.Errorf("multiple exact release tags on HEAD")
		}
		version = strings.TrimPrefix(tag, "v")
	}
	return version, nil
}

func newerVersion(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := range 3 {
		x, ex := strconv.ParseUint(pa[i], 10, 32)
		y, ey := strconv.ParseUint(pb[i], 10, 32)
		if ex != nil || ey != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func (w *workflow) remoteRef(ctx context.Context, ref string) (string, error) {
	out, err := w.git(ctx, "ls-remote", "--refs", "origin", ref)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	if len(fields) != 2 || fields[1] != ref {
		return "", fmt.Errorf("ambiguous remote ref %s", ref)
	}
	return fields[0], nil
}

func (w *workflow) release(ctx context.Context, opts Options) error {
	if err := w.clean(ctx); err != nil {
		return err
	}
	branch, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return err
	}
	version, err := w.exactVersion(ctx)
	if err != nil {
		return err
	}
	// A tagged local release can be built once. A pushed release is immutable.
	if branch == "trunk" && version != "0.0.0" {
		if opts.Version != "" && strings.TrimPrefix(opts.Version, "v") != version {
			return fmt.Errorf("requested version disagrees with exact HEAD tag")
		}
		remote, err := w.remoteRef(ctx, "refs/tags/v"+version)
		if err != nil {
			return err
		}
		if remote != "" {
			return fmt.Errorf("v%s is already pushed; use publish to retry unchanged artifacts or roll forward", version)
		}
		return w.finalRelease(ctx)
	}
	tag := "v" + strings.TrimPrefix(opts.Version, "v")
	if !stableVersion.MatchString(tag) || tag == "v0.0.0" {
		return fmt.Errorf("release requires VERSION=X.Y.Z (v0.0.0 is reserved for rehearsals)")
	}
	if androidVersionCode(strings.TrimPrefix(tag, "v")) == "0" {
		return fmt.Errorf("release version exceeds Android versionCode limits: each component <1000 and code <=2100000000")
	}
	if branch != "trunk" && (!conventionalMessage.MatchString(strings.Split(opts.Message, "\n")[0]) || strings.ContainsAny(opts.Message, "\r\x00")) {
		return fmt.Errorf("release requires RELEASE_MESSAGE='feat(scope): durable change' for the squash commit")
	}
	tags, err := w.git(ctx, "tag", "--list")
	if err != nil {
		return err
	}
	remoteTags, err := w.git(ctx, "ls-remote", "--refs", "--tags", "origin")
	if err != nil {
		return err
	}
	for _, item := range append(strings.Fields(tags), strings.Fields(remoteTags)...) {
		existing := strings.TrimPrefix(item, "refs/tags/")
		if stableVersion.MatchString(existing) && !newerVersion(tag, existing) {
			return fmt.Errorf("%s must exceed existing release %s", tag, existing)
		}
	}
	base, err := w.git(ctx, "rev-parse", "refs/heads/trunk")
	if err != nil {
		return err
	}
	remote, err := w.remoteRef(ctx, "refs/heads/trunk")
	if err != nil {
		return err
	}
	if remote != base {
		return fmt.Errorf("local trunk must match origin/trunk before preparation; update it with a fast-forward first")
	}
	candidate, err := w.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if _, err := w.git(ctx, "merge-base", "--is-ancestor", base, candidate); err != nil {
		return fmt.Errorf("candidate must contain current trunk; resolve divergence before release: %w", err)
	}
	fmt.Printf("Local release plan: %s -> trunk at %s; %s; 6 CLI archives, signed APK, checksums, Linux amd64/arm64 container. Publication is separate.\n", branch, base, tag)
	if err := w.prerelease(ctx); err != nil {
		return err
	}
	// Recheck every pinned input after the expensive candidate rehearsal.
	if err := w.unchanged(ctx, candidate); err != nil {
		return err
	}
	if err := w.sameBranch(ctx, branch); err != nil {
		return err
	}
	if current, err := w.git(ctx, "rev-parse", "trunk"); err != nil || current != base {
		return fmt.Errorf("trunk changed during rehearsal")
	}
	if current, err := w.remoteRef(ctx, "refs/heads/trunk"); err != nil || current != base {
		return fmt.Errorf("remote trunk changed during rehearsal")
	}
	if current, err := w.remoteRef(ctx, "refs/tags/"+tag); err != nil || current != "" {
		return fmt.Errorf("release tag appeared during rehearsal")
	}
	if branch != "trunk" {
		tree, err := w.git(ctx, "rev-parse", "HEAD^{tree}")
		if err != nil {
			return err
		}
		if _, err := w.git(ctx, "switch", "trunk"); err != nil {
			return err
		}
		if _, err := w.git(ctx, "merge", "--squash", candidate); err != nil {
			return err
		}
		if _, err := w.git(ctx, "commit", "-m", opts.Message); err != nil {
			return err
		}
		if actual, err := w.git(ctx, "rev-parse", "HEAD^{tree}"); err != nil || actual != tree {
			return fmt.Errorf("squash tree differs from verified candidate; stop before tagging")
		}
	}
	releaseCommit, err := w.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if err := w.unchanged(ctx, releaseCommit); err != nil {
		return err
	}
	if err := w.sameBranch(ctx, "trunk"); err != nil {
		return err
	}
	if _, err := w.git(ctx, "-c", "tag.gpgSign=false", "tag", tag, releaseCommit); err != nil {
		return err
	}
	return w.finalRelease(ctx)
}
