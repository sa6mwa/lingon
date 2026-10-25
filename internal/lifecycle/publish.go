package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const githubRepository = "sa6mwa/lingon"

type publication struct {
	Git, GitHub, Container bool
	ManifestHash, ImageID  string
}

func (w *workflow) publicationPath(r receipt) string {
	return filepath.Join(w.work, "published-v"+r.Version+".json")
}

func (w *workflow) publication(r receipt) (publication, error) {
	p := publication{ManifestHash: r.ManifestHash, ImageID: r.ImageID}
	data, err := os.ReadFile(w.publicationPath(r))
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.ManifestHash != r.ManifestHash || p.ImageID != r.ImageID {
		return p, fmt.Errorf("publication state disagrees with verified artifacts")
	}
	return p, nil
}

func (w *workflow) savePublication(r receipt, p publication) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := w.publicationPath(r)
	if err := os.WriteFile(path+".tmp", append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (w *workflow) publish(ctx context.Context) error {
	r, err := w.verified(ctx)
	if err != nil {
		return err
	}
	if !r.FullProof || r.Version == "0.0.0" {
		return fmt.Errorf("only make release produces publishable full-proof artifacts")
	}
	if err := w.publishPreconditions(ctx); err != nil {
		return err
	}
	branch, err := w.git(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch != "trunk" {
		return fmt.Errorf("publish requires tagged trunk")
	}
	p, err := w.publication(r)
	if err != nil {
		return err
	}
	tag := "v" + r.Version
	remoteTag, err := w.remoteRef(ctx, "refs/tags/"+tag)
	if err != nil {
		return err
	}
	if remoteTag != "" && remoteTag != r.Commit {
		return fmt.Errorf("remote tag conflicts with immutable release")
	}
	if remoteTag == "" {
		if p.Git {
			return fmt.Errorf("previously pushed release tag disappeared; stop and investigate")
		}
		if err := w.unchanged(ctx, r.Commit); err != nil {
			return err
		}
		if err := w.sameBranch(ctx, "trunk"); err != nil {
			return err
		}
		if version, err := w.exactVersion(ctx); err != nil || version != r.Version {
			return fmt.Errorf("local release tag changed before publication")
		}
		// Atomic push prevents a branch-only publication if tag creation fails.
		// Object IDs prevent a concurrent HEAD/tag movement from substituting
		// an unverified commit in the actual refspecs.
		if _, err := w.git(ctx, "push", "--atomic", "origin", r.Commit+":refs/heads/trunk", r.Commit+":refs/tags/"+tag); err != nil {
			return err
		}
	}
	if err := w.remotePublished(ctx, r); err != nil {
		return err
	}
	p.Git = true
	if err := w.savePublication(r, p); err != nil {
		return err
	}
	if err := w.publishGitHub(ctx, r); err != nil {
		return err
	}
	p.GitHub = true
	if err := w.savePublication(r, p); err != nil {
		return err
	}
	// Docker Hub is deliberately last. Retry this step with push-container;
	// neither git nor GitHub publication is repeated and no rebuild occurs.
	return w.pushContainer(ctx)
}

func (w *workflow) publishPreconditions(ctx context.Context) error {
	remote, err := w.git(ctx, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	allowed := remote == "git@github.com:"+githubRepository+".git" || remote == "https://github.com/"+githubRepository+".git" || remote == "https://github.com/"+githubRepository || remote == "ssh://git@github.com/"+githubRepository+".git"
	if !allowed {
		return fmt.Errorf("origin must target github.com/%s before publishing", githubRepository)
	}
	out, err := w.exec(ctx, w.root, "gh", "api", "repos/"+githubRepository+"/immutable-releases", "-H", "X-GitHub-Api-Version: 2026-03-10")
	if err != nil {
		return fmt.Errorf("GitHub release immutability must be enabled and readable before pushing: %w", err)
	}
	var setting struct{ Enabled bool }
	if err := json.Unmarshal(out, &setting); err != nil {
		return err
	}
	if !setting.Enabled {
		return fmt.Errorf("enable GitHub release immutability for %s before publication", githubRepository)
	}
	return nil
}

func (w *workflow) remotePublished(ctx context.Context, r receipt) error {
	tag, err := w.remoteRef(ctx, "refs/tags/v"+r.Version)
	if err != nil || tag != r.Commit {
		return fmt.Errorf("remote lightweight tag does not match verified commit")
	}
	branch, err := w.remoteRef(ctx, "refs/heads/trunk")
	if err != nil || branch != r.Commit {
		return fmt.Errorf("remote trunk does not match verified commit")
	}
	return nil
}

type githubRelease struct {
	TagName string
	IsDraft bool
	Assets  []struct{ Name string }
}

func (w *workflow) publishGitHub(ctx context.Context, r receipt) error {
	// Listing distinguishes absence from an auth/network failure. Never treat
	// an arbitrary failed gh release view as permission to create a release.
	out, err := w.exec(ctx, w.root, "gh", "release", "list", "--repo", githubRepository, "--limit", "10000", "--json", "tagName,isDraft")
	if err != nil {
		return err
	}
	var releases []githubRelease
	if err := json.Unmarshal(out, &releases); err != nil {
		return err
	}
	found := false
	for _, rel := range releases {
		found = found || rel.TagName == "v"+r.Version
	}
	if !found {
		notes, err := w.git(ctx, "log", "-1", "--pretty=%B")
		if err != nil {
			return err
		}
		path := filepath.Join(w.work, "notes-v"+r.Version+".md")
		if err := os.WriteFile(path, []byte(notes+"\n"), 0o600); err != nil {
			return err
		}
		if _, err := w.exec(ctx, w.root, "gh", "release", "create", "v"+r.Version, "--repo", githubRepository, "--verify-tag", "--draft", "--title", "Lingon v"+r.Version, "--notes-file", path); err != nil {
			return err
		}
	}
	out, err = w.exec(ctx, w.root, "gh", "release", "view", "v"+r.Version, "--repo", githubRepository, "--json", "tagName,isDraft,assets")
	if err != nil {
		return err
	}
	var rel githubRelease
	if err := json.Unmarshal(out, &rel); err != nil {
		return err
	}
	if rel.TagName != "v"+r.Version {
		return fmt.Errorf("GitHub release tag disagrees")
	}
	names, err := verifyChecksums(w.dist(r.Version), r.Version)
	if err != nil {
		return err
	}
	wanted := make(map[string]bool)
	for _, name := range names {
		wanted[name] = true
	}
	existing := make(map[string]bool)
	for _, asset := range rel.Assets {
		if !wanted[asset.Name] || existing[asset.Name] {
			return fmt.Errorf("unexpected or duplicate GitHub asset: %s", asset.Name)
		}
		existing[asset.Name] = true
	}
	if len(existing) > 0 {
		work, err := os.MkdirTemp(w.work, "github-verify-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		for _, name := range names {
			if !existing[name] {
				continue
			}
			if _, err := w.exec(ctx, w.root, "gh", "release", "download", "v"+r.Version, "--repo", githubRepository, "--dir", work, "--pattern", name); err != nil {
				return err
			}
			remote, err := hashFile(filepath.Join(work, name))
			local, localErr := hashFile(filepath.Join(w.dist(r.Version), name))
			if err != nil || localErr != nil || remote != local {
				return fmt.Errorf("immutable GitHub asset differs: %s; do not replace it", name)
			}
		}
	}
	for _, name := range names {
		if existing[name] {
			continue
		}
		if !rel.IsDraft {
			return fmt.Errorf("published release is missing %s; roll forward, never edit published assets", name)
		}
		if _, err := w.exec(ctx, w.root, "gh", "release", "upload", "v"+r.Version, filepath.Join(w.dist(r.Version), name), "--repo", githubRepository); err != nil {
			return err
		}
	}
	if rel.IsDraft {
		// Re-enter inspection to download and hash every uploaded asset before
		// publishing. The second call finds the completed draft, uploads nothing.
		if err := w.verifyGitHubAssets(ctx, r, names); err != nil {
			return err
		}
		_, err := w.exec(ctx, w.root, "gh", "release", "edit", "v"+r.Version, "--repo", githubRepository, "--draft=false")
		return err
	}
	return nil
}

func (w *workflow) verifyGitHubAssets(ctx context.Context, r receipt, names []string) error {
	work, err := os.MkdirTemp(w.work, "github-complete-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	for _, name := range names {
		if _, err := w.exec(ctx, w.root, "gh", "release", "download", "v"+r.Version, "--repo", githubRepository, "--dir", work, "--pattern", name); err != nil {
			return err
		}
		remote, err := hashFile(filepath.Join(work, name))
		local, localErr := hashFile(filepath.Join(w.dist(r.Version), name))
		if err != nil || localErr != nil || remote != local {
			return fmt.Errorf("GitHub upload checksum mismatch: %s", name)
		}
	}
	return nil
}

func (w *workflow) pushContainer(ctx context.Context) error {
	r, err := w.verified(ctx)
	if err != nil {
		return err
	}
	p, err := w.publication(r)
	if err != nil {
		return err
	}
	if !r.FullProof || !p.Git || !p.GitHub || r.Image != registryImage+":v"+r.Version {
		return fmt.Errorf("container publication requires completed git and GitHub publication")
	}
	if err := w.remotePublished(ctx, r); err != nil {
		return err
	}
	if p.Container {
		if err := w.verifyRegistryDigest(ctx, r); err != nil {
			return err
		}
		fmt.Println("Container was already pushed; no rebuild or overwrite.")
		return nil
	}
	digest := filepath.Join(w.work, "docker-v"+r.Version+".digest")
	_, exists, err := w.registryManifest(ctx, "v"+r.Version)
	if err != nil {
		return err
	}
	if exists {
		// A prior successful push may have lost its response/state write. Only
		// its recorded digest can authorize recovery; never overwrite a tag.
		if err := w.verifyRegistryDigest(ctx, r); err != nil {
			return err
		}
		p.Container = true
		return w.savePublication(r, p)
	}
	if _, err := os.Stat(digest); err == nil {
		return fmt.Errorf("previously pushed container tag disappeared; stop and investigate")
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := w.exec(ctx, w.root, "podman", "manifest", "push", "--all", "--digestfile="+digest, r.Image, "docker://"+r.Image); err != nil {
		return fmt.Errorf("registry push failed; git/GitHub remain published. Retry make push-container without rebuilding: %w", err)
	}
	data, err := os.ReadFile(digest)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(data)), "sha256:") {
		return fmt.Errorf("registry push did not record its digest")
	}
	if err := w.verifyRegistryDigest(ctx, r); err != nil {
		return err
	}
	p.Container = true
	if err := w.savePublication(r, p); err != nil {
		return err
	}
	fmt.Printf("Release complete: https://github.com/%s/releases/tag/v%s; %s@%s\n", githubRepository, r.Version, registryImage, strings.TrimSpace(string(data)))
	return nil
}

func (w *workflow) registryManifest(ctx context.Context, ref string) ([]byte, bool, error) {
	// A different fully-qualified registry hostname bypasses Podman's local
	// docker.io manifest lookup and performs a real Docker Hub read.
	out, err := w.exec(ctx, w.root, "podman", "manifest", "inspect", "registry-1.docker.io/pktsystems/lingon"+refSeparator(ref)+ref)
	if err != nil {
		if strings.Contains(string(out), "manifest unknown") || strings.Contains(string(out), "name unknown") {
			return nil, false, nil
		}
		return nil, false, err
	}
	if err := inspectImageManifest(out); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func refSeparator(ref string) string {
	if strings.HasPrefix(ref, "sha256:") {
		return "@"
	}
	return ":"
}

func (w *workflow) verifyRegistryDigest(ctx context.Context, r receipt) error {
	data, err := os.ReadFile(filepath.Join(w.work, "docker-v"+r.Version+".digest"))
	if err != nil {
		return fmt.Errorf("registry version already exists without this release's push digest; never overwrite it: %w", err)
	}
	digest := strings.TrimSpace(string(data))
	if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		return fmt.Errorf("invalid recorded Docker Hub digest")
	}
	tag, exists, err := w.registryManifest(ctx, "v"+r.Version)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("published Docker Hub tag is missing")
	}
	pinned, exists, err := w.registryManifest(ctx, digest)
	if err != nil {
		return err
	}
	if !exists || manifestHash(tag) != manifestHash(pinned) {
		return fmt.Errorf("registry tag moved away from its recorded digest; roll forward")
	}
	return nil
}
