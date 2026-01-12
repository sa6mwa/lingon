# AGENTS.md

You are collaborating with a highly opinionated Go architect. Optimize for Go-idiomatic design, separation of concerns, and developer experience (DX). Do NOT jump into implementation: propose options + tradeoffs first.

## Workflow (mandatory)
1. Restate goal + constraints.
2. Propose 1–3 designs with tradeoffs.
3. Get alignment before writing significant code.
4. Implement in small, reviewable steps.
5. Run quality gates (see below).
6. Git commit messages must follow Conventional Commits: https://www.conventionalcommits.org/en/v1.0.0/

## Refactors (strong preference)
- Avoid feature flags for refactoring tasks.
- Prefer clean refactors with a clear cutover:
  - No lingering “legacy” implementations, structs, or parallel codepaths.
  - Remove dead code and migrate call sites in the same change-set/sequence.
- Only keep parallel implementations or “legacy” structures if explicitly requested.

## Architecture & packaging
- Separation starts at the package boundary:
  - Public API packages for exported surfaces.
  - `internal/...` for non-exported implementation details.
- If there are two or more variants/adapters of an implementation: **use an interface**.
- Constructors:
  - Provide a `New...` constructor for implementations.
  - **Constructors must return the interface type, never a concrete type.**
- Cyclic imports:
  - If cyclic imports occur, extract core application functionality into a `core` package or subpackage
    so both main/module code and subpackages can import it without cycles.

## Public API shape (strong preference)
- Inputs:
  - If a user-facing function or interface method takes more than 4 parameters total (including `ctx context.Context`),
    do not bloat the signature. Put the non-`ctx` inputs into a request/input struct instead (e.g. `FooRequest`).
- Outputs:
  - A user-facing function or interface method must return **no more than two values**: `(T, error)` (or `(Response, error)`).
  - If more than one non-error value needs to be returned, the first return value must be a **response/result struct**
    that carries all outputs (e.g. `FooResponse`), and the second return value is `error`.

## Documentation & generators
- Every package must have a `doc.go` with standard Go package comment documentation.
- Code generation:
  - If generators are not tightly bound to a single package: put `generate.go` at the top-level module folder.
  - If tightly bound to a single package: put `generate.go` in that package folder (and place any generator runner `main` packages underneath as appropriate).

## Quality gates (always run before “done”)
- `go test ./...`
- `go vet ./...`
- `golint ./...`
- `golangci-lint run ./...`

## Repo hygiene
- If `.golangci.yml` does not exist in repo root, create and seed it with the contents below.

```yaml
version: "2"
linters:
  exclusions:
    rules:
      # errcheck noise we explicitly accept for now
      - linters: [errcheck]
        path: ".*_test\\.go"
      - linters: [errcheck]
        text: "resp.Body.Close"
      - linters: [errcheck]
        text: "fmt.Fprint"
      # staticcheck style nits we don't want to chase
      - linters: [staticcheck]
        text: "QF1003"
      - linters: [staticcheck]
        text: "S1017"
      - linters: [staticcheck]
        text: "QF1001"
      - linters: [staticcheck]
        text: "S1009"
```

