# Local CI and releases

Lingon's lifecycle is implemented in Go under `internal/lifecycle`, invoked by
Make. `make help` is the command index. Run orchestration from the repository
root on Linux. Commands, suites, builds, and publication steps are serial even
under `make -j`. Child commands have null stdin and their own process sessions;
interactive tests retain their test-owned PTYs. Full logs and bounded command
tails are retained under ignored `build/lifecycle/`.

## Release preparation

Commit intended source and documentation changes on a feature/fix branch. Resolve
review findings before entering release preparation. Keep the worktree clean,
including the index and nonignored untracked files. Local `trunk` must match
`origin/trunk`, and the candidate must contain that commit. Resolve divergence in
an ordinary fix iteration, before starting release. Never repair a failed gate
inside a release attempt.

`make prerelease` runs the full proof and an incremental artifact rehearsal.
`make test-all` uses the same graph. Neither squashes, tags, pushes, nor publishes.
Untagged rehearsals use `0.0.0`; that version cannot be published.

The proof graph asserts formatting, module integrity, vet, golint and
golangci-lint, uncached root Go tests and race tests, the Android Go tools module,
all Go integration packages (including actual owned-PTY and browser suites),
Android unit tests and instrumentation compilation, the full Android end-to-end
suite, and every remaining instrumentation test class. Android class selection is
discovered from test source. Per-batch JUnit reports must account for every
required method; missing, duplicate, failed or unexpectedly skipped results block
release. The TLS sweep permits only its cleartext-only test to skip; a separate
cleartext harness run must pass that exact test. Missing tools, missing runners and failures are
blockers; selecting a subset through ambient Android variables cannot satisfy
release proof. All verification commands share the systemd user slice `lingonverification.slice`,
with an aggregate 100% CPU quota, 3 GiB memory ceiling and zero swap.
`make` contains lifecycle compilation and all proof gates; direct commands use
`bash scripts/verify-contained.sh <command> [arguments...]`. Containment is
mandatory and larger limits are rejected. Android owns and cleans up its
emulator/adb/relay resources, using 1 vCPU, 1024 MiB guest RAM and a 512 MiB
Gradle build heap and a 256 MiB default heap for other JVM launchers.
App and instrumentation APKs are built before emulator startup. The installed
AndroidJUnitRunner executes through native adb instrumentation, without live
Gradle/UTP JVMs. Its result parser rejects crashes, incomplete or duplicate results,
failures and unauthorized skips, and retains raw protocol plus per-batch JUnit XML.
The emulator
uses `-lowram -memory 1024 -cores 1`; startup verifies its actual generated
hardware configuration, because ordinary RAM settings are otherwise enlarged
by the emulator. Host GPU rendering is retained, with Vulkan and the host
window disabled to fit the budget. Framebuffer size and density are scaled
together from 1080×2400 / 420 dpi to 720×1600 / 280 dpi, preserving the
medium phone's logical layout bounds; startup asserts the actual display values.
Instrumentation batches contain at most ten methods. App/test processes reset
between batches so instrumentation allocations
do not accumulate in one long-lived process. One owned emulator remains for
the sweep and is joined during cleanup; each batch retains its JUnit evidence.
Podman build/runtime checks join the same slice. These full sweeps
can take substantial time; run no other heavy work alongside them.

Existing relay replay and cursor-position benchmarks run three times with
allocation reporting. Their results are retained as observations, not invented
latency thresholds or promises of BSD/Darwin runtime coverage.

The Linux integration runner requires `/bin/bash` and `less`. The lifecycle
checks these explicitly before expensive gates because developer-focused PTY
tests may otherwise skip when those tools are missing. The optional startup
comparison test requires a separately supplied baseline executable; the release
graph records its required replay/cursor benchmarks without claiming that
optional comparison ran.

Choose the actual release version and a Conventional Commit message describing
the durable change:

```sh
make release VERSION=1.2.3 RELEASE_MESSAGE='feat(session): add terminal management'
```

`1.2.3` is an example, not an automatically selected next version. Stable release
versions must be greater than every stable local/remote `vX.Y.Z` tag. Prerelease
tags do not affect stable ordering. Discuss breaking changes and major-version
decisions explicitly. Android version codes use `major*1000000+minor*1000+patch`;
each component must be less than 1000 and the result must fit Android's limit.

The command reports its local plan, runs the complete candidate rehearsal, checks
that candidate/trunk/remote inputs stayed pinned, squashes onto `trunk` as one
Conventional Commit, and proves that the resulting tree equals the tested tree.
It preserves the source branch. It creates a lightweight tag with signing
disabled for the tag (`git -c tag.gpgSign=false tag v1.2.3`). Annotated/signed tags,
multiple exact stable tags and inferred historical versions cannot establish a
release version. `make print-release-version` prints an exact lightweight stable
tag on HEAD, or `0.0.0` when HEAD is untagged.

After tagging, the command cleans release-owned outputs and the Android build,
runs the entire proof again, and rebuilds/verifies the final matrix. Only this
successful final tagged gate records full proof. `make release-matrix` provides
an untagged incremental artifact rehearsal; it cannot create publishable proof.
`make package-verify` checks existing artifacts without rebuilding them.

A failed final gate leaves the local commit/tag, attempt marker, and logs for
inspection. It never retags, amends, resets, repairs, retries a correctness gate,
or publishes. Start a separate fix iteration and explicitly decide how to handle
the unpublished local state. The pipeline refuses another build of that release
attempt. A pushed release can only roll forward to another version.

## Artifact contract

Required CLI targets are Linux, FreeBSD and Darwin, each for `amd64` and `arm64`.
All use `CGO_ENABLED=0`, `-trimpath`, the host Go toolchain in local mode, and an
exact injected version/commit stamp. Linux/FreeBSD ELF executables must have no
dynamic loader or dynamic segment. Darwin Mach-O executables may import only
macOS system libraries/frameworks and have no runtime search paths. CPU, OS ABI,
Go build identity, clean VCS revision and linked stamp are inspected in the
extracted payloads. The Linux host binary and container execute version smoke
tests. Other targets are cross-built and inspected, not claimed as runtime-tested.

Final files live under `dist/vX.Y.Z/`:

- `lingon-X.Y.Z-{linux,freebsd,darwin}-{amd64,arm64}.tar.gz` (six archives).
- `lingon-X.Y.Z-android.apk` (signed with the checked-in expected certificate).
- `lingon-X.Y.Z-RELEASE.json` (commit, Go toolchain and container provenance).
- `lingon-X.Y.Z-CHECKSUMS` (SHA-256 and the complete upload set).

Each CLI archive contains only `lingon-X.Y.Z-OS-ARCH/lingon`, the adjacent
`lingonx -> lingon` symlink and `LICENSE`. Archives have commit-derived timestamps.
Checksums require exact set equality: missing, duplicated, escaping, stale or
extra files fail. Extraction validates layout before writing; privacy scans use
bounded buffers and inspect expanded archives and APK entries. Home/repository/
cache paths, local file URLs and instrumentation leaks fail verification.

Android release identity is passed through Gradle properties, not edits to
`build.gradle.kts`. Publication verifies the actual APK package name, version
name/code and signer. Developer `make android-release` remains a local APK build;
its version name and code come together from the lifecycle's exact lightweight
tag policy (`v0.0.0` and code `1` for an untagged local rehearsal). To install a
verified release without rebuilding, use:

```sh
make android-install-release APK="$PWD/dist/v1.2.3/lingon-1.2.3-android.apk" ADB_SERIAL=PHONE
```

Podman is the only container engine. `release-matrix` builds a Linux amd64/arm64
manifest from the same verified CLI bytes. The certificate stage runs natively;
the cross-target final scratch images need no QEMU. Its Alpine base is resolved
to a digest once per build and recorded. Images carry exact version/revision
labels. `make container` verifies this existing image instead of rebuilding it.
The release reference is `docker.io/pktsystems/lingon:vX.Y.Z`.

## Publication and retries

Run `make publish` only when publication of the selected release is authorized.
Preparation itself never pushes. GitHub CLI and Docker Hub authentication must
already be configured. `origin` must target `github.com/sa6mwa/lingon`.

Enable GitHub release immutability once in the repository settings; publication
checks it before any push and never changes settings implicitly. The GitHub
account needs permission to read that setting. See [GitHub's immutable-release
contract](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)
and [the settings API](https://docs.github.com/en/rest/repos/repos#check-if-immutable-releases-are-enabled-for-a-repository).

`make publish` rechecks source/tag identity, full proof, every checksum and the
recorded local container manifest. It atomically pushes `trunk` and its lightweight
tag, verifies remote identities, creates a GitHub draft, uploads only checksum-
listed files plus CHECKSUMS, downloads and verifies those bytes, then publishes
the draft. A partial draft can resume by adding missing files after verifying all
existing bytes. Wrong, extra or different assets stop publication. Published
assets are never deleted, replaced, or appended; missing published assets require
a new release. No `--clobber`, force push, remote-tag deletion or tag movement is
part of this workflow. GitHub immutability protects the published tag/assets.

The **last step** pushes the already-built manifest with Podman to
`docker.io/pktsystems/lingon:vX.Y.Z`. Login or network failure fails the remaining
release step while preserving successful Git/GitHub publication. Log in/fix the
external failure, then run:

```sh
make push-container
```

That target verifies the same source/artifacts/local manifest and retries only
the Docker Hub step. It records the returned registry digest and verifies the
version tag against that digest. Existing version tags cannot be overwritten; a
lost response can recover only using this attempt's recorded digest. Keep
`build/lifecycle/`, `dist/` and local Podman images until publication completes.
Correctness failures never retry automatically. Release invalidation, if needed,
is an explicit engineer decision; the normal recovery is a new version.

## Local Podman Kube services

The old root manifest using live home-directory credentials is retired.
`devenv.yaml.in` is the sole service definition.

The relay binds its container interface while the published host port remains
loopback-only. A failed readiness check retains pod diagnostics in the lifecycle
log before bounded teardown, including when the caller cancels the operation.

Use:

```sh
make dev-up LINGON_DEV_IMAGE=docker.io/pktsystems/lingon:v1.2.3
make dev-ps
make dev-logs
make dev-down
make dev-reset
```

The renderer substitutes only known, quoted values and writes mode-0600
`build/devenv/devenv.yaml`. Names and default high ports are checkout-specific;
`LINGON_DEV_PORT` overrides the loopback-only host port. State and generated TLS
credentials remain under `build/devenv/state/`, never under the user's live
Lingon directory. `keep-id` plus explicit caller UID/GID keeps service writes
removable by the ordinary user. Startup requires a real health response verified
with the generated development CA; a failed readiness gate tears down its pod.
Reset tears down with the same manifest before deleting owned state. No sudo,
ownership-changing mounts, recursive chown or container-engine fallback exists.
